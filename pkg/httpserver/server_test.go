package httpserver_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/httpserver"
)

func TestGracefulDrainPreservesRequestContext(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	requestErr := make(chan error, 1)
	s, err := httpserver.New(httpserver.Config{Address: "127.0.0.1:0"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		requestErr <- r.Context().Err()
		w.WriteHeader(http.StatusNoContent)
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-s.Ready():
	case err := <-done:
		t.Fatalf("startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timed out")
	}
	response := make(chan error, 1)
	go func() {
		client := http.Client{Timeout: 5 * time.Second}
		r, err := client.Get("http://" + s.Addr().String())
		if err == nil {
			_, _ = io.Copy(io.Discard, r.Body)
			r.Body.Close()
			if r.StatusCode != http.StatusNoContent {
				err = errors.New("unexpected status")
			}
		}
		response <- err
	}()
	<-entered
	cancel()
	close(release)
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	if err := <-requestErr; err != nil {
		t.Fatalf("request canceled before drain: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDrainTimeoutCancelsHandlers(t *testing.T) {
	entered := make(chan struct{})
	joined := make(chan struct{})
	s, err := httpserver.New(httpserver.Config{Address: "127.0.0.1:0", ShutdownTimeout: 20 * time.Millisecond}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(joined) }))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-s.Ready():
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timed out")
	}
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		client := http.Client{Timeout: 5 * time.Second}
		r, err := client.Get("http://" + s.Addr().String())
		if err == nil {
			r.Body.Close()
		}
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain: %v", err)
	}
	<-joined
	<-requestDone
}

func TestBindFailureDoesNotSignalReady(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	s, err := httpserver.New(httpserver.Config{Address: listener.Addr().String()}, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("expected bind failure")
	}
	select {
	case <-s.Ready():
		t.Fatal("ready after failed bind")
	default:
	}
}

func TestShutdownClosesHijackedConnections(t *testing.T) {
	for _, waitOnContext := range []bool{false, true} {
		t.Run(fmt.Sprint("context=", waitOnContext), func(t *testing.T) {
			upgraded := make(chan struct{})
			joined := make(chan struct{})
			s, err := httpserver.New(httpserver.Config{Address: "127.0.0.1:0", ShutdownTimeout: time.Second}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(joined)
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				close(upgraded)
				if waitOnContext {
					<-r.Context().Done()
				} else {
					_, _ = conn.Read(make([]byte, 1))
				}
			}))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- s.Run(ctx) }()
			select {
			case <-s.Ready():
			case err := <-done:
				t.Fatal(err)
			case <-time.After(5 * time.Second):
				t.Fatal("startup timeout")
			}
			conn, err := net.DialTimeout("tcp", s.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-upgraded:
			case <-time.After(5 * time.Second):
				t.Fatal("upgrade timeout")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown did not join upgraded handler")
			}
			select {
			case <-joined:
			default:
				t.Fatal("handler still running")
			}
			conn.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
				t.Fatalf("connection not closed: %v", err)
			}
		})
	}
}
