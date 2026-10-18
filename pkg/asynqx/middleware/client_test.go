package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestClientMiddleware(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	defer tp.Shutdown(t.Context())
	routes := Routing{Default: "default", Queues: map[string]int{"default": 1, "high": 3}, Tasks: map[string]string{"urgent": "high"}}
	calls := 0
	var got *asynq.Task
	var queue string
	var failure error
	next := EnqueueFunc(func(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
		calls++
		got = task
		for _, o := range opts {
			if o.Type() == asynq.QueueOpt {
				queue = o.Value().(string)
			}
		}
		if task.Headers() != nil {
			remote := propagation.TraceContext{}.Extract(context.Background(), propagation.MapCarrier(task.Headers()))
			if trace.SpanContextFromContext(remote).SpanID() != trace.SpanFromContext(ctx).SpanContext().SpanID() {
				t.Fatal("trace not propagated")
			}
		}
		return &asynq.TaskInfo{ID: "accepted", Queue: queue}, failure
	})
	client := Chain(next, ObserveClient(tp), Route(routes))
	routes.Tasks["urgent"] = "default"
	task := asynq.NewTaskWithHeaders("urgent", nil, map[string]string{"custom": "kept"}, asynq.MaxRetry(7))
	if _, err := client.EnqueueContext(t.Context(), task); err != nil || queue != "high" || got != task || task.Headers()["custom"] != "kept" {
		t.Fatal("routing, identity or headers", err)
	}
	if _, err := client.EnqueueContext(t.Context(), task, asynq.Queue("default")); err != nil || queue != "default" {
		t.Fatal("override", err)
	}
	// Native tasks without headers must still work without reconstruction.
	plain := asynq.NewTask("ordinary", nil, asynq.MaxRetry(5))
	if _, err := client.EnqueueContext(t.Context(), plain); err != nil || got != plain {
		t.Fatal(err)
	}
	before := calls
	for _, opts := range [][]asynq.Option{{nil}, {asynq.Queue("missing")}} {
		if _, err := client.EnqueueContext(t.Context(), task, opts...); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
	if _, err := client.EnqueueContext(t.Context(), nil); err == nil {
		t.Fatal("nil task accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.EnqueueContext(ctx, task); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != before {
		t.Fatal("invalid input reached backend")
	}
	failure = asynq.ErrTaskIDConflict
	if _, err := client.EnqueueContext(t.Context(), task); !errors.Is(err, failure) {
		t.Fatal("native error lost", err)
	}
}
