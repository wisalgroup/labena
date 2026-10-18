// Package tracing constructs an explicitly owned OpenTelemetry provider.
// It does not install process-global providers or read application configuration.
package tracing

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Config selects optional tracing. A disabled zero value is valid.
// Endpoint is the full OTLP HTTP URL, including /v1/traces. Credentials in URLs
// are rejected; use a local collector to handle backend authentication.
type Config struct {
	Enabled         bool
	ServiceName     string
	Endpoint        string
	SampleRatio     float64
	ExportTimeout   time.Duration
	ShutdownTimeout time.Duration
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.ServiceName) == "" {
		return errors.New("tracing: service name is required")
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("tracing: endpoint must be an HTTP(S) URL without credentials, query, or fragment")
	}
	if math.IsNaN(c.SampleRatio) || c.SampleRatio < 0 || c.SampleRatio > 1 {
		return errors.New("tracing: sample ratio must be between zero and one")
	}
	if c.ExportTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return errors.New("tracing: timeouts must be positive")
	}
	return nil
}

// Provider owns the exporter and its bounded batch worker. Borrow TracerProvider
// for instrumentation; stop all producers before Shutdown. Concurrent use is safe.
type Provider struct {
	trace.TracerProvider
	shutdown func(context.Context) error
	timeout  time.Duration
}

// Open acquires an exporter and starts its bounded batch worker. It does not
// contact the collector during setup. The caller owns Shutdown after all producers
// stop. Disabled tracing owns no worker; invalid settings fail before resource setup.
// Export outages lose telemetry, not application work; failed batches are reported
// through the borrowed logger without endpoint URLs, response bodies, or span data.
func Open(ctx context.Context, cfg Config, logger *slog.Logger) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return &Provider{TracerProvider: noop.NewTracerProvider()}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, errors.New("tracing: logger is required")
	}
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(cfg.Endpoint),
		otlptracehttp.WithEncoding(otlptracehttp.EncodingProtobuf),
		otlptracehttp.WithTimeout(cfg.ExportTimeout),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
		otlptracehttp.WithHeaders(map[string]string{}),
	)
	if err != nil {
		return nil, errors.New("tracing: cannot initialize OTLP exporter")
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", cfg.ServiceName))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		sdktrace.WithRawSpanLimits(sdktrace.SpanLimits{AttributeValueLengthLimit: 1024, AttributeCountLimit: 64, EventCountLimit: 32, LinkCountLimit: 32, AttributePerEventCountLimit: 16, AttributePerLinkCountLimit: 16}),
		sdktrace.WithBatcher(&reportingExporter{SpanExporter: exporter, logger: logger, limit: rate.NewLimiter(rate.Every(time.Minute), 1)},
			sdktrace.WithMaxQueueSize(2048), sdktrace.WithMaxExportBatchSize(512),
			sdktrace.WithBatchTimeout(5*time.Second), sdktrace.WithExportTimeout(cfg.ExportTimeout)),
	)
	return &Provider{TracerProvider: provider, shutdown: provider.Shutdown, timeout: cfg.ShutdownTimeout}, nil
}

// Shutdown flushes queued spans within the smaller of the caller's deadline and
// the configured budget. It is idempotent. Success does not guarantee delivery:
// export failures are reported via logging, and a full queue drops new spans.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.shutdown == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return p.shutdown(ctx)
}

type reportingExporter struct {
	sdktrace.SpanExporter
	logger *slog.Logger
	limit  *rate.Limiter
	failed atomic.Uint64
}

func (e *reportingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if err := e.SpanExporter.ExportSpans(ctx, spans); err != nil {
		// This is the reporting boundary. Do not also forward errors to OTel's global
		// handler, which would duplicate logs and couple independent app instances.
		count := e.failed.Add(uint64(len(spans)))
		if e.limit.Allow() {
			e.logger.WarnContext(ctx, "trace export failed; batch discarded", "component", "tracing", "failed_spans", count)
		}
	}
	return nil
}
