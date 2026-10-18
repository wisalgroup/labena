package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Instrument installs telemetry once, before the borrowed DB is used concurrently.
// name is a fixed pool label, never a DSN. Providers must be non-nil (no-op is valid).
// The returned cleanup unregisters pool observation; call it before closing DB.
// Bun hooks cannot be removed. Raw database/sql calls bypass query instrumentation.
// Recorded query spans include sanitized SQL when supported; values are never captured.
func Instrument(db *bun.DB, name string, tp trace.TracerProvider, mp metric.MeterProvider) (func() error, error) {
	if db == nil || tp == nil || mp == nil {
		return nil, errors.New("database: telemetry requires DB and providers")
	}
	meter := mp.Meter("github.com/wisalgroup/labena/database")
	duration, err := meter.Float64Histogram("db.client.operation.duration", metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10))
	if err != nil {
		return nil, err
	}
	connections, err := meter.Int64ObservableGauge("db.pool.connections")
	if err != nil {
		return nil, err
	}
	limit, err := meter.Int64ObservableGauge("db.pool.max_connections")
	if err != nil {
		return nil, err
	}
	waits, err := meter.Int64ObservableCounter("db.pool.waits")
	if err != nil {
		return nil, err
	}
	waitTime, err := meter.Float64ObservableCounter("db.pool.wait.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	attrs := []attribute.KeyValue{attribute.String("pool", name)}
	registration, err := meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		stats := db.DB.Stats()
		o.ObserveInt64(connections, int64(stats.InUse), metric.WithAttributes(append(attrs, attribute.String("state", "in_use"))...))
		o.ObserveInt64(connections, int64(stats.Idle), metric.WithAttributes(append(attrs, attribute.String("state", "idle"))...))
		options := metric.WithAttributes(attrs...)
		o.ObserveInt64(limit, int64(stats.MaxOpenConnections), options)
		o.ObserveInt64(waits, stats.WaitCount, options)
		o.ObserveFloat64(waitTime, stats.WaitDuration.Seconds(), options)
		return nil
	}, connections, limit, waits, waitTime)
	if err != nil {
		return nil, err
	}
	db.AddQueryHook(telemetryHook{tracer: tp.Tracer("github.com/wisalgroup/labena/database"), duration: duration, name: name, dialect: db.Dialect().Name()})
	return registration.Unregister, nil
}

type telemetryHook struct {
	tracer   trace.Tracer
	duration metric.Float64Histogram
	name     string
	dialect  dialect.Name
}

type querySpanKey struct{}

func (h telemetryHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	operation := queryOperation(event)
	ctx, span := h.tracer.Start(ctx, operation, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("db.operation.name", operation), attribute.String("pool", h.name)))
	if span.IsRecording() {
		if query := sanitizedQuery(event.Query, h.dialect); query != "" {
			span.SetAttributes(attribute.String("db.query.text", query))
		}
	}
	if event.Stash == nil {
		event.Stash = make(map[any]any)
	}
	event.Stash[querySpanKey{}] = span
	return ctx
}
func (h telemetryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	span := event.Stash[querySpanKey{}].(trace.Span)
	defer span.End()
	outcome := "success"
	if event.Err != nil && !errors.Is(event.Err, sql.ErrNoRows) {
		outcome = "error"
		span.SetStatus(codes.Error, "database operation failed")
	}
	h.duration.Record(ctx, time.Since(event.StartTime).Seconds(), metric.WithAttributes(attribute.String("pool", h.name), attribute.String("db.operation.name", queryOperation(event)), attribute.String("outcome", outcome)))
}

func queryOperation(event *bun.QueryEvent) string {
	operation := event.Operation()
	switch operation {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "BEGIN", "COMMIT", "ROLLBACK", "CREATE", "ALTER", "DROP", "TRUNCATE":
		return operation
	default:
		return "OTHER"
	}
}
