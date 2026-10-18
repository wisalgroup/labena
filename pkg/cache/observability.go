package cache

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// WithTelemetry borrows a store and native providers. It preserves Get's miss
// semantics and records only fixed operations/outcomes, never keys or values.
// Close the original store/client after consumers stop, not this wrapper.
func WithTelemetry(store Store, tp trace.TracerProvider, mp metric.MeterProvider) (Store, error) {
	if store == nil || tp == nil || mp == nil {
		return nil, errors.New("cache: telemetry requires store and providers")
	}
	duration, err := mp.Meter("github.com/wisalgroup/labena/cache").Float64Histogram("cache.operation.duration", metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10))
	if err != nil {
		return nil, err
	}
	return &observedStore{store: store, tracer: tp.Tracer("github.com/wisalgroup/labena/cache"), duration: duration}, nil
}

type observedStore struct {
	store    Store
	tracer   trace.Tracer
	duration metric.Float64Histogram
}

func (s *observedStore) observe(ctx context.Context, op string) (context.Context, func(string, error)) {
	started := time.Now()
	ctx, span := s.tracer.Start(ctx, "cache."+op)
	return ctx, func(outcome string, err error) {
		defer span.End()
		if err != nil {
			outcome = "error"
			span.SetStatus(codes.Error, "cache operation failed")
		}
		span.SetAttributes(attribute.String("cache.operation", op), attribute.String("outcome", outcome))
		s.duration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attribute.String("cache.operation", op), attribute.String("outcome", outcome)))
	}
}
func (s *observedStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, finish := s.observe(ctx, "get")
	value, found, err := s.store.Get(ctx, key)
	outcome := "miss"
	if found {
		outcome = "hit"
	}
	finish(outcome, err)
	return value, found, err
}
func (s *observedStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	ctx, finish := s.observe(ctx, "set")
	err := s.store.Set(ctx, key, value, ttl)
	finish("success", err)
	return err
}
func (s *observedStore) Delete(ctx context.Context, key string) error {
	ctx, finish := s.observe(ctx, "delete")
	err := s.store.Delete(ctx, key)
	finish("success", err)
	return err
}

func (s *observedStore) Exists(ctx context.Context, key string) (bool, error) {
	ctx, finish := s.observe(ctx, "exists")
	found, err := s.store.Exists(ctx, key)
	outcome := "miss"
	if found {
		outcome = "hit"
	}
	finish(outcome, err)
	return found, err
}

func (s *observedStore) Clear(ctx context.Context) error {
	ctx, finish := s.observe(ctx, "clear")
	err := s.store.Clear(ctx)
	finish("success", err)
	return err
}

func (s *observedStore) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	ctx, finish := s.observe(ctx, "get_multi")
	values, err := s.store.GetMulti(ctx, keys)
	finish("success", err)
	return values, err
}

func (s *observedStore) SetMulti(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	ctx, finish := s.observe(ctx, "set_multi")
	err := s.store.SetMulti(ctx, items, ttl)
	finish("success", err)
	return err
}

func (s *observedStore) Remember(ctx context.Context, key string, ttl time.Duration, load func(context.Context) (any, error)) (any, error) {
	// Use this wrapper's Get/Set so cache spans and metrics are preserved.
	return Remember(ctx, s, key, ttl, load)
}
