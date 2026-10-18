package tracing

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func enabled(endpoint string) Config {
	return Config{Enabled: true, ServiceName: "test-service", Endpoint: endpoint, SampleRatio: 1, ExportTimeout: time.Second, ShutdownTimeout: time.Second}
}

func TestDisabled(t *testing.T) {
	p, err := Open(context.Background(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, span := p.Tracer("test").Start(context.Background(), "ignored")
	if span.IsRecording() {
		t.Fatal("disabled tracing records spans")
	}
	span.End()
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestExportAndShutdown(t *testing.T) {
	var mu sync.Mutex
	var received collector.ExportTraceServiceRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		mu.Lock()
		err = proto.Unmarshal(body, &received)
		mu.Unlock()
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer server.Close()
	p, err := Open(context.Background(), enabled(server.URL+"/v1/traces"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, parent := p.Tracer("orders").Start(context.Background(), "create order")
	_, child := p.Tracer("orders").Start(ctx, "save order")
	child.End()
	parent.End()
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received.ResourceSpans) != 1 {
		t.Fatalf("resource spans: %d", len(received.ResourceSpans))
	}
	r := received.ResourceSpans[0]
	if r.Resource.Attributes[0].Value.GetStringValue() != "test-service" {
		t.Fatal("missing service identity")
	}
	spans := r.ScopeSpans[0].Spans
	if len(spans) != 2 || !bytes.Equal(spans[0].TraceId, spans[1].TraceId) || !bytes.Equal(spans[0].ParentSpanId, spans[1].SpanId) {
		t.Fatal("lost parent relationship")
	}
}

func TestExporterOutageIsBoundedAndRedacted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "secret-response-body", http.StatusInternalServerError)
	}))
	defer server.Close()
	var logs bytes.Buffer
	p, err := Open(context.Background(), enabled(server.URL+"/v1/traces"), slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	_, span := p.Tracer("test").Start(context.Background(), "test")
	span.End()
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "batch discarded") || strings.Contains(logs.String(), "secret-response-body") || strings.Contains(logs.String(), server.URL) {
		t.Fatalf("unsafe or missing diagnostics: %s", logs.String())
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, endpoint := range []string{"ftp://localhost", "http://user:secret@localhost", "http://localhost?token=secret", ":bad"} {
		cfg := enabled(endpoint)
		if err := cfg.Validate(); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("endpoint validation: %v", err)
		}
	}
	cfg := enabled("http://localhost:4318/v1/traces")
	cfg.SampleRatio = 2
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid sample ratio accepted")
	}
}

func TestShutdownDeadline(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-release
	}))
	defer server.Close()
	defer close(release)
	cfg := enabled(server.URL + "/v1/traces")
	cfg.ExportTimeout = time.Second
	cfg.ShutdownTimeout = 50 * time.Millisecond
	p, err := Open(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	_, span := p.Tracer("test").Start(context.Background(), "slow export")
	span.End()
	done := make(chan error, 1)
	go func() { done <- p.Shutdown(context.Background()) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("export did not start")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown exceeded its budget")
	}
}
