package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/wisalgroup/labena/pkg/logging"
)

// ObserveWorker uses native Asynq middleware; install before registering handlers.
// Metrics describe attempts, including failed retries. IDs and payloads never
// become metric labels. Native Asynq owns retry and acknowledgement behavior.
func ObserveWorker(mux *asynq.ServeMux, tp trace.TracerProvider, mp metric.MeterProvider, log *slog.Logger) (asynq.MiddlewareFunc, error) {
	// Keep the existing metric and field names stable for dashboard compatibility.
	duration, err := mp.Meter("github.com/wisalgroup/labena/queue").Float64Histogram("queue.task.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	tracer := tp.Tracer("github.com/wisalgroup/labena/queue")
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) (err error) {
			start := time.Now()
			metricTask := "unknown"
			if _, pattern := mux.Handler(t); pattern != "" {
				metricTask = pattern
			}
			remote := propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier(t.Headers()))
			id, _ := asynq.GetTaskID(ctx)
			name, _ := asynq.GetQueueName(ctx)
			retry, _ := asynq.GetRetryCount(ctx)
			opts := []trace.SpanStartOption{trace.WithNewRoot(), trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(attribute.String("messaging.system", "redis"), attribute.String("messaging.destination.name", name), attribute.String("messaging.message.type", t.Type()), attribute.String("messaging.message.id", id), attribute.Int("messaging.delivery.attempt", retry+1), attribute.String("messaging.operation.type", "process"))}
			if sc := trace.SpanContextFromContext(remote); sc.IsValid() {
				opts = append(opts, trace.WithLinks(trace.Link{SpanContext: sc}))
			}
			ctx, span := tracer.Start(ctx, t.Type()+" process", opts...)
			ctx = logging.WithLogger(ctx, log.With("task_id", id, "task_name", t.Type(), "queue", name, "attempt", retry+1))
			defer func() {
				// Recovery belongs here so failed panics are also traced and measured.
				if p := recover(); p != nil {
					err = fmt.Errorf("queue: handler panic (%T)", p)
				}
				outcome := "success"
				if err != nil {
					outcome = "error"
					span.SetStatus(codes.Error, "task failed")
					logging.FromContext(ctx).WarnContext(ctx, "task attempt failed", "error_type", fmt.Sprintf("%T", err), "error_category", logging.ErrorCategory(err))
				}
				duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.String("task_name", metricTask), attribute.String("queue", name), attribute.String("outcome", outcome)))
				span.End()
			}()
			return next.ProcessTask(ctx, t)
		})
	}, nil
}

// Logger bridges native operational logs. Error details can contain arbitrary
// handler data; inspect retained failures through Asynq's administrative tools.
type Logger struct{ Log *slog.Logger }

func (l Logger) Debug(args ...interface{}) { l.Log.Debug(fmt.Sprint(args...)) }
func (l Logger) Info(args ...interface{})  { l.Log.Info(fmt.Sprint(args...)) }
func (l Logger) Warn(args ...interface{})  { l.Log.Warn("Asynq operational warning") }
func (l Logger) Error(args ...interface{}) {
	l.Log.Error("Asynq operational error; inspect worker health and retained tasks")
}
func (l Logger) Fatal(args ...interface{}) { l.Log.Error("Asynq fatal error") }
