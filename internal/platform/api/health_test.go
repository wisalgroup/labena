package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthDependencyChecks(t *testing.T) {
	ready, stopping := make(chan struct{}), make(chan struct{})
	calls := 0
	var failure error
	router := http.NewServeMux()
	RegisterHealthRoutes(router, ready, stopping, func(context.Context) error { calls++; return failure })
	request := func(path string, want int, body string) {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want || w.Body.String() != body {
			t.Fatalf("%s: %d %q", path, w.Code, w.Body.String())
		}
	}
	request("/readyz", 503, "starting\n")
	request("/healthz", 200, "")
	if calls != 0 {
		t.Fatal("probed before startup or during liveness")
	}
	close(ready)
	request("/readyz", 200, "")
	failure = errors.New("secret database address")
	request("/readyz", 503, "unavailable\n")
	request("/healthz", 200, "")
	failure = nil
	request("/readyz", 200, "")
	close(stopping)
	request("/readyz", 503, "stopping\n")
	if calls != 3 {
		t.Fatal("unexpected probes", calls)
	}
}

func TestHealthBoundsConcurrencyAndRechecksShutdown(t *testing.T) {
	ready, stopping := make(chan struct{}), make(chan struct{})
	close(ready)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	router := http.NewServeMux()
	RegisterHealthRoutes(router, ready, stopping, func(context.Context) error { close(entered); <-release; return nil })
	first := httptest.NewRecorder()
	go func() { defer close(done); router.ServeHTTP(first, httptest.NewRequest("GET", "/readyz", nil)) }()
	<-entered
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest("GET", "/readyz", nil))
	close(stopping)
	close(release)
	<-done
	if second.Code != 503 || second.Body.String() != "busy\n" {
		t.Fatal("concurrent probe admitted")
	}
	if first.Code != 503 || first.Body.String() != "stopping\n" {
		t.Fatal("reported ready during shutdown")
	}
}
