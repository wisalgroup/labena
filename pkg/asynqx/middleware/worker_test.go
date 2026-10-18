package middleware

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/hibiken/asynq"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestNativeHandlerTracingAndPanic(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	defer tp.Shutdown(t.Context())
	ctx, parent := tp.Tracer("test").Start(t.Context(), "enqueue", trace.WithSpanKind(trace.SpanKindProducer))
	headers := map[string]string{}
	propagation.TraceContext{}.Inject(ctx, propagation.MapCarrier(headers))
	task := asynq.NewTaskWithHeaders("example.send", []byte(`{}`), headers)
	mux := asynq.NewServeMux()
	mw, err := ObserveWorker(mux, tp, metricnoop.NewMeterProvider(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	mux.Use(mw)
	called := false
	mux.HandleFunc("example.send", func(ctx context.Context, task *asynq.Task) error {
		called = true
		if !trace.SpanFromContext(ctx).SpanContext().IsValid() {
			t.Error("missing span")
		}
		return nil
	})
	if err := mux.ProcessTask(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	ended := spans.Ended()
	if !called || len(ended) != 1 || len(ended[0].Links()) != 1 || ended[0].Links()[0].SpanContext.SpanID() != parent.SpanContext().SpanID() {
		t.Fatal("missing producer link")
	}
	parent.End()
	mux.HandleFunc("panic", func(context.Context, *asynq.Task) error { panic("secret payload") })
	if err := mux.ProcessTask(t.Context(), asynq.NewTask("panic", nil)); err == nil || err.Error() != "queue: handler panic (string)" {
		t.Fatal(err)
	}
}
