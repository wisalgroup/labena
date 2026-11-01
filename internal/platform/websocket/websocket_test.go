package websocket_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wire "github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/api/middleware"
	"github.com/wisalgroup/labena/internal/platform/auth"
	socket "github.com/wisalgroup/labena/internal/platform/websocket"
	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/httpserver"
)

type authenticator struct{ principal auth.Principal }

func (a authenticator) Authenticate(_ context.Context, token string) (auth.Principal, error) {
	if token != "valid" {
		return auth.Principal{}, apperror.Unauthenticated("Invalid token.")
	}
	return a.principal, nil
}

func start(t *testing.T, secure bool, cfg socket.Config, handle func(context.Context, *socket.Conn) error) (string, *http.Client, context.CancelFunc, <-chan error) {
	t.Helper()
	router, err := api.New(api.Options{DisableDocs: true})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := socket.New(cfg, handle)
	if err != nil {
		t.Fatal(err)
	}
	router.Mux.Handle("GET /ws", middleware.BearerHTTP(authenticator{auth.Principal{UserID: uuid.New(), SessionID: uuid.New()}})(handler))
	settings := httpserver.Config{Address: "127.0.0.1:0", ShutdownTimeout: time.Second, ReadTimeout: 500 * time.Millisecond, WriteTimeout: 500 * time.Millisecond}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	scheme := "ws"
	if secure {
		fixture := httptest.NewTLSServer(http.NotFoundHandler())
		certificate := fixture.TLS.Certificates[0]
		roots := x509.NewCertPool()
		roots.AddCert(fixture.Certificate())
		fixture.Close()
		key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		settings.TLSCertFile = filepath.Join(dir, "cert.pem")
		settings.TLSKeyFile = filepath.Join(dir, "key.pem")
		settings.HTTP2 = true
		if err := os.WriteFile(settings.TLSCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(settings.TLSKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
			t.Fatal(err)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		scheme = "wss"
	}
	server, err := httpserver.New(settings, router)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		transport.CloseIdleConnections()
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
	return scheme + "://" + server.Addr().String() + "/ws", &http.Client{Transport: transport, Timeout: 5 * time.Second}, cancel, done
}

func echo(ctx context.Context, conn *socket.Conn) error {
	if _, err := auth.CurrentUserID(ctx); err != nil {
		return err
	}
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if err := conn.Write(ctx, kind, data); err != nil {
			return err
		}
	}
}

func TestMessagesAndShutdown(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "ws", true: "wss"}[secure], func(t *testing.T) {
			url, client, stop, done := start(t, secure, socket.Config{PingInterval: 50 * time.Millisecond, PingTimeout: time.Second}, echo)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, res, err := wire.Dial(ctx, url, &wire.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Authorization": {"Bearer valid"}}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			if res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("X-Request-ID") == "" {
				t.Fatal(res.Header)
			}
			// Leave a read active for control frames, and outlive normal HTTP deadlines.
			received := make(chan string, 1)
			go func() {
				_, data, err := conn.Read(ctx)
				if err != nil {
					received <- err.Error()
				} else {
					received <- string(data)
				}
			}()
			time.Sleep(600 * time.Millisecond)
			if err := conn.Write(ctx, wire.MessageText, []byte("hello")); err != nil {
				t.Fatal(err)
			}
			if got := <-received; got != "hello" {
				t.Fatal(got)
			}
			stop()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("session prevented shutdown")
			}
			if _, _, err := conn.Read(ctx); err == nil {
				t.Fatal("connection survived shutdown")
			}
		})
	}
}

func TestHandshakePolicies(t *testing.T) {
	url, client, _, _ := start(t, false, socket.Config{AllowedOrigins: []string{"https://app.example.com"}}, echo)
	for _, tc := range []struct {
		name, token string
		origins     []string
		status      int
	}{
		{"missing token", "", nil, 401}, {"bad token", "bad", nil, 401},
		{"same origin", "valid", []string{strings.TrimSuffix(strings.Replace(url, "ws://", "http://", 1), "/ws")}, 101},
		{"allowed", "valid", []string{"https://app.example.com"}, 101},
		{"untrusted", "valid", []string{"https://evil.example.com"}, 403},
		{"opaque", "valid", []string{"null"}, 403},
		{"duplicate", "valid", []string{"https://app.example.com", "https://evil.example.com"}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			headers := http.Header{"Origin": tc.origins}
			if tc.token != "" {
				headers.Set("Authorization", "Bearer "+tc.token)
			}
			conn, res, err := wire.Dial(ctx, url, &wire.DialOptions{HTTPClient: client, HTTPHeader: headers})
			if conn != nil {
				defer conn.CloseNow()
			}
			if res == nil || res.StatusCode != tc.status {
				t.Fatalf("response %v error %v", res, err)
			}
			if tc.status == 101 && err != nil {
				t.Fatal(err)
			}
			if tc.status == 401 && res.Header.Get("WWW-Authenticate") != "Bearer" {
				t.Fatal(res.Header)
			}
		})
	}
}

func TestOversizedMessage(t *testing.T) {
	url, client, _, _ := start(t, false, socket.Config{MaxMessageBytes: 8}, echo)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := wire.Dial(ctx, url, &wire.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Authorization": {"Bearer valid"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_ = conn.Write(ctx, wire.MessageText, []byte("too large"))
	_, _, err = conn.Read(ctx)
	if wire.CloseStatus(err) != wire.StatusMessageTooBig {
		t.Fatal(err)
	}
}

func TestWriteLimitAndHandlerClose(t *testing.T) {
	url, client, _, _ := start(t, false, socket.Config{MaxMessageBytes: 4}, func(ctx context.Context, conn *socket.Conn) error {
		if err := conn.Write(ctx, socket.MessageText, []byte("large")); !errors.Is(err, socket.ErrMessageTooLarge) {
			t.Error(err)
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := wire.Dial(ctx, url, &wire.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Authorization": {"Bearer valid"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, _, err = conn.Read(ctx)
	if wire.CloseStatus(err) != wire.StatusNormalClosure {
		t.Fatal(err)
	}
}

func TestConfigurationValidation(t *testing.T) {
	for _, cfg := range []socket.Config{
		{MaxMessageBytes: -1}, {WriteTimeout: -time.Second}, {PingInterval: -time.Second}, {PingTimeout: -time.Second},
		{AllowedOrigins: []string{"*"}}, {AllowedOrigins: []string{"https://example.com/path"}},
	} {
		if _, err := socket.New(cfg, echo); err == nil {
			t.Fatal("accepted invalid config", cfg)
		}
	}
	if _, err := socket.New(socket.Config{}, nil); err == nil {
		t.Fatal("accepted nil callback")
	}
}

func TestHeartbeatDisconnectsUnresponsivePeer(t *testing.T) {
	finished := make(chan struct{})
	url, client, _, _ := start(t, false, socket.Config{PingInterval: 20 * time.Millisecond, PingTimeout: 50 * time.Millisecond}, func(ctx context.Context, conn *socket.Conn) error {
		defer close(finished)
		return echo(ctx, conn)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := wire.Dial(ctx, url, &wire.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Authorization": {"Bearer valid"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	// No reads means this peer never handles ping frames or sends a pong.
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("heartbeat did not release session")
	}
}

func TestSlowReaderHitsWriteTimeout(t *testing.T) {
	failed := make(chan error, 1)
	url, client, _, _ := start(t, false, socket.Config{MaxMessageBytes: 256 << 10, WriteTimeout: 50 * time.Millisecond}, func(ctx context.Context, conn *socket.Conn) error {
		payload := make([]byte, 256<<10)
		for range 128 {
			if err := conn.Write(ctx, socket.MessageBinary, payload); err != nil {
				failed <- err
				return err
			}
		}
		failed <- nil
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := wire.Dial(ctx, url, &wire.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Authorization": {"Bearer valid"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	// Never read messages; eventually the socket buffers fill.
	select {
	case err := <-failed:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected bounded write deadline, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("write did not time out")
	}
}
