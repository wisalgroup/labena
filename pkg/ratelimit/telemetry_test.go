package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type limiterFunc func(context.Context, string, Limit) (Decision, error)

func (f limiterFunc) Allow(ctx context.Context, key string, limit Limit) (Decision, error) {
	return f(ctx, key, limit)
}

func TestTelemetryPreservesDecisionsAndRedactsInputs(t *testing.T) {
	failure := errors.New("private-backend-credential")
	for _, tc := range []struct {
		name     string
		decision Decision
		err      error
	}{
		{"allowed", Decision{Allowed: true}, nil},
		{"denied", Decision{RetryAfter: time.Second}, nil},
		{"error", Decision{}, failure},
		{"canceled", Decision{}, fmt.Errorf("wrapped: %w", context.Canceled)},
		{"timeout", Decision{}, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			defer meters.Shutdown(t.Context())
			recorder := tracetest.NewSpanRecorder()
			traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			defer traces.Shutdown(t.Context())
			policy := Limit{Rate: 1, Burst: 2, Period: time.Minute}
			calls := 0
			limiter, err := WithTelemetry(limiterFunc(func(ctx context.Context, key string, limit Limit) (Decision, error) {
				calls++
				if key != "private-user-email" || limit != policy {
					t.Fatal("arguments changed")
				}
				return tc.decision, tc.err
			}), traces, meters)
			if err != nil {
				t.Fatal(err)
			}
			got, err := limiter.Allow(t.Context(), "private-user-email", policy)
			if got != tc.decision || err != tc.err || calls != 1 {
				t.Fatal("result or call count changed")
			}
			spans := recorder.Ended()
			if len(spans) != 1 || len(spans[0].Attributes()) != 1 || spans[0].Attributes()[0].Value.AsString() != tc.name {
				t.Fatal("unexpected span attributes")
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(t.Context(), &data); err != nil {
				t.Fatal(err)
			}
			count := int64(0)
			for _, scope := range data.ScopeMetrics {
				for _, m := range scope.Metrics {
					if m.Name == "ratelimit.decisions" {
						sum := m.Data.(metricdata.Sum[int64])
						for _, point := range sum.DataPoints {
							attrs := point.Attributes.ToSlice()
							if len(attrs) != 1 || attrs[0].Value.AsString() != tc.name {
								t.Fatal("unbounded labels")
							}
							count += point.Value
						}
					}
				}
			}
			if count != 1 {
				t.Fatal("missing decision metric")
			}
			output := fmt.Sprintf("%+v %+v %+v", data, spans[0].Attributes(), spans[0].Status())
			if strings.Contains(output, "private-") {
				t.Fatal("sensitive input exported")
			}
		})
	}
}
