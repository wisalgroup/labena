package httpserver_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/httpserver"
)

func tlsFiles(t *testing.T) (string, string, *tls.Config) {
	t.Helper()
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	defer fixture.Close()
	cert := fixture.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(fixture.Certificate())
	return certFile, keyFile, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
}

func TestTLSProtocols(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "http1", true: "http2"}[enabled], func(t *testing.T) {
			cert, key, clientTLS := tlsFiles(t)
			server, err := httpserver.New(httpserver.Config{Address: "127.0.0.1:0", TLSCertFile: cert, TLSKeyFile: key, HTTP2: enabled}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.TLS == nil {
					t.Error("missing TLS state")
				}
				w.WriteHeader(204)
			}))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- server.Run(ctx) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(5 * time.Second):
					t.Error("shutdown timed out")
				}
			})
			select {
			case <-server.Ready():
			case err := <-done:
				t.Fatal(err)
			case <-time.After(5 * time.Second):
				t.Fatal("startup timed out")
			}
			for _, attemptHTTP2 := range []bool{false, true} {
				transport := &http.Transport{TLSClientConfig: clientTLS.Clone(), ForceAttemptHTTP2: attemptHTTP2}
				client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
				res, err := client.Get("https://" + server.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				res.Body.Close()
				transport.CloseIdleConnections()
				want := 1
				if enabled && attemptHTTP2 {
					want = 2
				}
				if res.ProtoMajor != want || res.StatusCode != 204 {
					t.Fatalf("got %s/%d; want HTTP/%d", res.Proto, res.StatusCode, want)
				}
			}
		})
	}
}

func TestTLSShutdownClosesHijack(t *testing.T) {
	cert, key, clientTLS := tlsFiles(t)
	entered := make(chan struct{})
	server, err := httpserver.New(httpserver.Config{Address: "127.0.0.1:0", TLSCertFile: cert, TLSKeyFile: key, HTTP2: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		close(entered)
		_, _ = conn.Read(make([]byte, 1))
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	select {
	case <-server.Ready():
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timed out")
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", server.Addr().String(), clientTLS)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("hijack timed out")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TLS socket survived shutdown")
	}
}

func TestTLSValidationAndStartupFailure(t *testing.T) {
	for _, cfg := range []httpserver.Config{{HTTP2: true}, {TLSCertFile: "cert"}, {TLSKeyFile: "key"}} {
		if _, err := httpserver.New(cfg, http.NotFoundHandler()); err == nil {
			t.Fatal("accepted incomplete TLS config")
		}
	}
	server, err := httpserver.New(httpserver.Config{TLSCertFile: "missing-cert", TLSKeyFile: "missing-key"}, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Run(context.Background()); err == nil {
		t.Fatal("accepted missing certificates")
	}
	select {
	case <-server.Ready():
		t.Fatal("ready after TLS failure")
	default:
	}
}

func TestHTTP2GracefulDrain(t *testing.T) {
	cert, key, clientTLS := tlsFiles(t)
	entered, release := make(chan struct{}), make(chan struct{})
	contextErr := make(chan error, 1)
	server, err := httpserver.New(httpserver.Config{Address: "127.0.0.1:0", TLSCertFile: cert, TLSKeyFile: key, HTTP2: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Error("request did not negotiate HTTP/2")
		}
		close(entered)
		<-release
		contextErr <- r.Context().Err()
		w.WriteHeader(204)
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	select {
	case <-server.Ready():
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timeout")
	}
	transport := &http.Transport{TLSClientConfig: clientTLS, ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	result := make(chan error, 1)
	go func() {
		res, err := client.Get("https://" + server.Addr().String())
		if err == nil {
			res.Body.Close()
			if res.StatusCode != 204 {
				err = fmt.Errorf("status %d", res.StatusCode)
			}
		}
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request timeout")
	}
	cancel()
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := <-contextErr; err != nil {
		t.Fatal("request context canceled before drain", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timeout")
	}
}
