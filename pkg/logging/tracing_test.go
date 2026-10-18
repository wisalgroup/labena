package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestTraceCorrelationFollowsContextAndSelectedChannel(t *testing.T) {
	var normal, audit bytes.Buffer
	outputs := map[string]*bytes.Buffer{"normal": &normal, "audit": &audit}
	manager, err := Open(Config{Default: []string{"normal"}, Channels: map[string]Channel{"normal": {Driver: "capture", Path: "normal"}, "audit": {Driver: "capture", Path: "audit"}}, Drivers: map[string]Driver{"capture": func(ch Channel) (slog.Handler, io.Closer, error) {
		return slog.NewJSONHandler(outputs[ch.Path], nil), nil, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer provider.Shutdown(context.Background())
	parentCtx, parent := provider.Tracer("test").Start(context.Background(), "parent")
	defer parent.End()
	ctx, child := provider.Tracer("test").Start(parentCtx, "child")
	defer child.End()
	logger := manager.Default().With("component", "orders").WithGroup("operation")
	ctx = WithLogger(ctx, logger)
	selected, err := Channels(ctx, "audit")
	if err != nil {
		t.Fatal(err)
	}
	selected.InfoContext(ctx, "child")
	selected.InfoContext(parentCtx, "parent")
	selected.Info("without context")
	if normal.Len() != 0 {
		t.Fatal("selected log copied to default")
	}
	decoder := json.NewDecoder(&audit)
	for _, want := range []string{child.SpanContext().SpanID().String(), parent.SpanContext().SpanID().String(), ""} {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["component"] != "orders" {
			t.Fatal("lost logger attributes")
		}
		fields, _ := record["operation"].(map[string]any)
		if want == "" {
			if _, ok := fields["trace_id"]; ok {
				t.Fatal("stale trace ID")
			}
			continue
		}
		if fields["span_id"] != want || fields["trace_id"] != parent.SpanContext().TraceID().String() || fields["trace_sampled"] != true {
			t.Fatalf("correlation: %v", record)
		}
	}
}

func TestStandaloneCorrelationDoesNotDuplicate(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, Options{})
	if err != nil {
		t.Fatal(err)
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	defer provider.Shutdown(context.Background())
	ctx, span := provider.Tracer("test").Start(context.Background(), "unsampled")
	defer span.End()
	logger.InfoContext(ctx, "record")
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["trace_sampled"] != false || record["trace_id"] != span.SpanContext().TraceID().String() {
		t.Fatal(record)
	}
	if bytes.Count(output.Bytes(), []byte(`"trace_id"`)) != 1 {
		t.Fatal("duplicate correlation fields")
	}
}
