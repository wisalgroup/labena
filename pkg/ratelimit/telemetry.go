package ratelimit

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// WithTelemetry observes decisions without recording keys, policy values or error
// messages. It borrows next and the providers; it does not own their lifecycle.
// Install once during construction. Returned errors and decisions are unchanged.
func WithTelemetry(next Limiter, traces trace.TracerProvider, meters metric.MeterProvider) (Limiter, error) {
	if next == nil || traces == nil || meters == nil {
		return nil, errors.New("ratelimit: limiter and telemetry providers required")
	}
	meter := meters.Meter("github.com/wisalgroup/labena/ratelimit")
	decisions, err := meter.Int64Counter("ratelimit.decisions", metric.WithDescription("Rate limiter calls by outcome"))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("ratelimit.duration", metric.WithUnit("s"), metric.WithDescription("Rate limiter call duration"))
	if err != nil {
		return nil, err
	}
	return &observedLimiter{next, traces.Tracer("github.com/wisalgroup/labena/ratelimit"), decisions, duration}, nil
}

type observedLimiter struct {
	next      Limiter
	tracer    trace.Tracer
	decisions metric.Int64Counter
	duration  metric.Float64Histogram
}

func (l *observedLimiter) Allow(ctx context.Context, key string, limit Limit) (Decision, error) {
	ctx, span := l.tracer.Start(ctx, "ratelimit allow", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	started := time.Now()
	decision, err := l.next.Allow(ctx, key, limit)
	outcome := "allowed"
	switch {
	case errors.Is(err, context.Canceled):
		outcome = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		outcome = "timeout"
	case err != nil:
		outcome = "error"
	case !decision.Allowed:
		outcome = "denied"
	}
	attr := attribute.String("outcome", outcome)
	span.SetAttributes(attr)
	if err != nil {
		span.SetStatus(codes.Error, "rate limiter failed")
	}
	options := metric.WithAttributes(attr)
	l.decisions.Add(ctx, 1, options)
	l.duration.Record(ctx, time.Since(started).Seconds(), options)
	return decision, err
}
