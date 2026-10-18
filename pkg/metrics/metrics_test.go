package metrics

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIndependentRegistriesAndScrape(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	first, err := New("first", logger)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Shutdown(context.Background())
	second, err := New("second", logger)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Shutdown(context.Background())
	counter, err := first.Meter("test").Int64Counter("example.operations")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 3)
	for _, tc := range []struct {
		p    *Provider
		want bool
	}{{first, true}, {second, false}} {
		response := httptest.NewRecorder()
		tc.p.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
		if response.Code != 200 {
			t.Fatalf("scrape: %d %s", response.Code, response.Body.String())
		}
		text := response.Body.String()
		if strings.Contains(text, "example_operations_total") != tc.want {
			t.Fatal("registries share state")
		}
		if !strings.Contains(text, "go_goroutines") {
			t.Fatal("missing Go runtime collector")
		}
		response = httptest.NewRecorder()
		tc.p.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
		if response.Code != 404 {
			t.Fatal("scrape handler exposes unrelated paths")
		}
	}
}
