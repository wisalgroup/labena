package logging

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
	"sync/atomic"
	"testing"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logpb "go.opentelemetry.io/proto/otlp/logs/v1"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"
)

func TestOTLPDeliveryCorrelationAndShutdown(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_HEADERS", "Authorization=private-secret")
	var mu sync.Mutex
	var received []*logpb.ResourceLogs
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/logs" || r.Header.Get("Authorization") != "" {
			t.Error("wrong endpoint or ambient credentials used")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var request collector.ExportLogsServiceRequest
		if err := proto.Unmarshal(body, &request); err != nil {
			t.Error(err)
		}
		mu.Lock()
		received = append(received, request.ResourceLogs...)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer server.Close()
	m, err := Open(Config{Default: []string{"telemetry"}, Channels: map[string]Channel{
		"telemetry": {Driver: "otlp", OTLP: OTLPOptions{Endpoint: server.URL + "/v1/logs", ServiceName: "test-api", Environment: "test"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	spanID, _ := trace.SpanIDFromHex("0123456789abcdef")
	ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID}))
	logger := m.Default().With("request_id", "request-123").WithGroup("operation").With("kind", "test")
	logger.DebugContext(ctx, "filtered")
	logger.InfoContext(ctx, "correlated", "count", 2)
	m.Default().Warn("without trace")
	if err := m.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	var records []*logpb.LogRecord
	for _, resource := range received {
		attrs := otlpAttrs(resource.Resource.Attributes)
		if attrs["service.name"].GetStringValue() != "test-api" || attrs["deployment.environment.name"].GetStringValue() != "test" {
			t.Fatal("resource identity missing")
		}
		for _, scope := range resource.ScopeLogs {
			records = append(records, scope.LogRecords...)
		}
	}
	if len(records) != 2 {
		t.Fatalf("got %d records; want 2", len(records))
	}
	r := records[0]
	attrs := otlpAttrs(r.Attributes)
	group := otlpAttrs(attrs["operation"].GetKvlistValue().GetValues())
	if r.Body.GetStringValue() != "correlated" || r.SeverityText != "INFO" || !bytes.Equal(r.TraceId, traceID[:]) || !bytes.Equal(r.SpanId, spanID[:]) || attrs["request_id"].GetStringValue() != "request-123" || group["count"].GetIntValue() != 2 || group["kind"].GetStringValue() != "test" {
		t.Fatal("lost message, severity, correlation or groups", r)
	}
	if len(records[1].TraceId) != 0 || records[1].Body.GetStringValue() != "without trace" {
		t.Fatal("untraced logs lost or received stale trace context")
	}
}

func otlpAttrs(values []*commonpb.KeyValue) map[string]*commonpb.AnyValue {
	result := make(map[string]*commonpb.AnyValue, len(values))
	for _, v := range values {
		result[v.Key] = v.Value
	}
	return result
}

func TestOTLPLazySelectionAndBoundedShutdown(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	m, err := Open(Config{Default: []string{"quiet"}, Channels: map[string]Channel{
		"quiet": {Driver: "discard"},
		"otlp":  {Driver: "otlp", OTLP: OTLPOptions{Endpoint: server.URL + "/v1/logs", ServiceName: "test", ExportTimeout: time.Second, ShutdownTimeout: 50 * time.Millisecond}},
	}, Drivers: map[string]Driver{"discard": func(Channel) (slog.Handler, io.Closer, error) { return slog.NewJSONHandler(io.Discard, nil), nil, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if len(m.closers) != 0 {
		t.Fatal("unselected OTLP channel opened")
	}
	logger, err := m.Logger("otlp")
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("queued")
	start := time.Now()
	_ = m.Close() // Shutdown may report its deadline; it must not block indefinitely.
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("shutdown exceeded its budget")
	}
	if requests.Load() != 1 {
		t.Fatalf("wanted one flush attempt, got %d", requests.Load())
	}
}

type failedLogExporter struct{ sdklog.Exporter }

func (failedLogExporter) Export(context.Context, []sdklog.Record) error {
	return errors.New("private-endpoint private-payload")
}

func TestOTLPFailuresUseIndependentSafeDiagnostics(t *testing.T) {
	var output bytes.Buffer
	exporter := logExporter{Exporter: failedLogExporter{}, logger: slog.New(slog.NewJSONHandler(&output, nil)), limit: rate.NewLimiter(0, 1)}
	for range 2 {
		if err := exporter.Export(t.Context(), make([]sdklog.Record, 2)); err != nil {
			t.Fatal("raw exporter error escaped to global handler")
		}
	}
	if exporter.failed.Load() != 4 || strings.Count(output.String(), "\n") != 1 || strings.Contains(output.String(), "private") {
		t.Fatal("failure diagnostics missing, unbounded, or exposing data", output.String())
	}
}

func TestOTLPValidation(t *testing.T) {
	for _, endpoint := range []string{"", "file:///tmp/log", "http://user:secret@localhost/v1/logs", "http://localhost/v1/logs?secret=value", "http://localhost/v1/logs#fragment"} {
		cfg := Config{Default: []string{"otlp"}, Channels: map[string]Channel{"otlp": {Driver: "otlp", OTLP: OTLPOptions{Endpoint: endpoint, ServiceName: "test"}}}}
		if err := cfg.Validate(); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid endpoint accepted or exposed", err)
		}
	}
}
