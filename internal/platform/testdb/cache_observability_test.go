package testdb_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/cache"
	"github.com/wisalgroup/labena/pkg/cache/memory"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestCacheTelemetryPreservesOutcomes(t *testing.T) {
	ctx := context.Background()
	base, err := memory.New(10, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	defer tp.Shutdown(ctx)
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer mp.Shutdown(ctx)
	store, err := cache.WithTelemetry(base, tp, mp)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(ctx, "secret-key"); found || err != nil {
		t.Fatal("miss changed")
	}
	if err := store.Set(ctx, "secret-key", []byte("secret-value"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if value, found, err := store.Get(ctx, "secret-key"); !found || err != nil || string(value) != "secret-value" {
		t.Fatal("hit changed")
	}
	if err := store.Set(ctx, "secret-key", nil, -1); err != cache.ErrInvalidTTL {
		t.Fatal("error identity changed", err)
	}
	if err := store.Delete(ctx, "secret-key"); err != nil {
		t.Fatal(err)
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatal(err)
	}
	outcomes := map[string]bool{}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if histogram, ok := m.Data.(metricdata.Histogram[float64]); ok {
				for _, point := range histogram.DataPoints {
					value, _ := point.Attributes.Value("outcome")
					outcomes[value.AsString()] = true
				}
			}
		}
	}
	for _, outcome := range []string{"hit", "miss", "success", "error"} {
		if !outcomes[outcome] {
			t.Errorf("missing %s", outcome)
		}
	}
	if strings.Contains(fmt.Sprint(data), "secret") {
		t.Fatal("cache metric data leaked")
	}
	if len(spans.Ended()) != 5 {
		t.Fatal("missing spans")
	}
	for _, span := range spans.Ended() {
		if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "secret") {
			t.Fatal("cache span data leaked")
		}
	}
}

func TestValueOperationsThroughTelemetry(t *testing.T) {
	ctx := context.Background()
	base, _ := memory.New(100, 256)
	defer base.Close(ctx)
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	defer tp.Shutdown(ctx)
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer mp.Shutdown(ctx)
	store, err := cache.WithTelemetry(base, tp, mp)
	if err != nil {
		t.Fatal(err)
	}
	checkValueOperations(t, store)
	names := map[string]bool{}
	for _, span := range spans.Ended() {
		names[span.Name()] = true
	}
	for _, name := range []string{"cache.exists", "cache.clear", "cache.get_multi", "cache.set_multi", "cache.get", "cache.set"} {
		if !names[name] {
			t.Error("missing span", name)
		}
	}
}
