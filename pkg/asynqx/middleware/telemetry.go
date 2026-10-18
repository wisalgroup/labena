package middleware

import (
	"context"
	"errors"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// ObserveClient records enqueue spans. Tasks with an initialized header map propagate
// producer context. Tasks created with NewTask still enqueue, but cannot carry
// context because Asynq has no header setter. Never enqueue the same task concurrently:
// this middleware updates its trace headers. Native task options remain untouched.
func ObserveClient(provider trace.TracerProvider) Middleware {
	tracer := provider.Tracer("github.com/wisalgroup/labena/tasks")
	return func(next EnqueueFunc) EnqueueFunc {
		return func(ctx context.Context, t *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if t == nil {
				return nil, errors.New("queue: nil task")
			}
			ctx, span := tracer.Start(ctx, "task enqueue", trace.WithSpanKind(trace.SpanKindProducer))
			defer span.End()
			span.SetAttributes(attribute.String("messaging.system", "redis"), attribute.String("messaging.message.type", t.Type()), attribute.String("messaging.operation.type", "send"))
			if headers := t.Headers(); headers != nil {
				delete(headers, "traceparent")
				delete(headers, "tracestate")
				propagation.TraceContext{}.Inject(ctx, propagation.MapCarrier(headers))
			}
			info, err := next(ctx, t, opts...)
			if err != nil {
				span.SetStatus(codes.Error, "enqueue failed")
				return nil, err
			}
			if info != nil {
				span.SetAttributes(attribute.String("messaging.message.id", info.ID), attribute.String("messaging.destination.name", info.Queue))
			}
			return info, nil
		}
	}
}
