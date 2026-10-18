package asynqx_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/wisalgroup/labena/pkg/asynqx"
	"github.com/wisalgroup/labena/pkg/asynqx/middleware"
)

func TestTaskProducerConsumerCorrelation(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(t.Context())
	task := asynqx.NewTask("example.send", []byte(`{}`), asynq.MaxRetry(7))
	if len(task.Headers()) != 0 {
		t.Fatal("construction captured context")
	}
	task.Headers()["custom"] = "kept"
	other := asynqx.NewTask("example.send", nil)
	if len(other.Headers()) != 0 {
		t.Fatal("tasks share headers")
	}
	mux := asynq.NewServeMux()
	observe, err := middleware.ObserveWorker(mux, provider, noop.NewMeterProvider(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	mux.Use(observe)
	mux.HandleFunc("example.send", func(context.Context, *asynq.Task) error { return nil })
	client := middleware.Chain(func(ctx context.Context, got *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
		if got != task || got.Headers()["custom"] != "kept" {
			t.Fatal("task reconstructed or custom header lost")
		}
		// Model serialization into a separately delivered task, without carrying the
		// producer's Go context into the worker.
		headers := make(map[string]string)
		for k, v := range got.Headers() {
			headers[k] = v
		}
		return nil, mux.ProcessTask(context.Background(), asynq.NewTaskWithHeaders(got.Type(), got.Payload(), headers))
	}, middleware.ObserveClient(provider))
	ctx, request := provider.Tracer("test").Start(t.Context(), "request")
	if _, err := client.EnqueueContext(ctx, task); err != nil {
		t.Fatal(err)
	}
	request.End()
	var producer, consumer sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		switch span.SpanKind() {
		case trace.SpanKindProducer:
			producer = span
		case trace.SpanKindConsumer:
			consumer = span
		}
	}
	if producer == nil || consumer == nil {
		t.Fatal("missing enqueue or worker span")
	}
	if producer.Parent().SpanID() != request.SpanContext().SpanID() {
		t.Fatal("enqueue lost request parent")
	}
	if consumer.Parent().IsValid() || len(consumer.Links()) != 1 || consumer.Links()[0].SpanContext.SpanID() != producer.SpanContext().SpanID() || consumer.Links()[0].SpanContext.TraceID() != producer.SpanContext().TraceID() {
		t.Fatal("worker must link to producer as an independent attempt")
	}
}
