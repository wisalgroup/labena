package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"golang.org/x/time/rate"
)

// OTLPOptions configures asynchronous OTLP/HTTP log delivery. Endpoint is the
// full URL including /v1/logs. Put backend credentials in a collector, not this URL.
// Zero timeouts use five seconds for export and three seconds for shutdown.
// An empty ServiceName uses unknown_service for standalone callers.
type OTLPOptions struct {
	Endpoint        string
	ServiceName     string
	Environment     string
	ExportTimeout   time.Duration
	ShutdownTimeout time.Duration
}

func (o OTLPOptions) Validate() error {
	u, err := url.Parse(o.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("logging: OTLP endpoint must be an HTTP(S) URL without credentials, query, or fragment")
	}
	if o.ExportTimeout < 0 || o.ShutdownTimeout < 0 {
		return errors.New("logging: OTLP timeouts must not be negative")
	}
	return nil
}

func openOTLP(ch Channel) (slog.Handler, io.Closer, error) {
	o := ch.OTLP
	if strings.TrimSpace(o.ServiceName) == "" {
		o.ServiceName = "unknown_service"
	}
	if o.ExportTimeout == 0 {
		o.ExportTimeout = 5 * time.Second
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = 3 * time.Second
	}
	exporter, err := otlploghttp.New(context.Background(),
		otlploghttp.WithEndpointURL(o.Endpoint),
		otlploghttp.WithTimeout(o.ExportTimeout),
		otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: false}),
		otlploghttp.WithHeaders(map[string]string{}),
		otlploghttp.WithCompression(otlploghttp.NoCompression),
	)
	if err != nil {
		return nil, nil, errors.New("logging: cannot initialize OTLP exporter")
	}
	attrs := []attribute.KeyValue{attribute.String("service.name", o.ServiceName)}
	if o.Environment != "" {
		attrs = append(attrs, attribute.String("deployment.environment.name", o.Environment))
	}
	// Diagnostics use an independent stderr logger, never the exporting channel.
	reporter := &logExporter{Exporter: exporter, logger: slog.New(slog.NewJSONHandler(os.Stderr, nil)), limit: rate.NewLimiter(rate.Every(time.Minute), 1)}
	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(resource.NewSchemaless(attrs...)),
		sdklog.WithAttributeCountLimit(64),
		sdklog.WithAttributeValueLengthLimit(4096),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(reporter,
			sdklog.WithMaxQueueSize(2048), sdklog.WithExportMaxBatchSize(512),
			sdklog.WithExportInterval(time.Second), sdklog.WithExportTimeout(o.ExportTimeout))),
	)
	handler := otelslog.NewHandler("github.com/wisalgroup/labena/logging", otelslog.WithLoggerProvider(provider))
	return &levelHandler{Handler: handler, level: ch.Options.Level}, &logProvider{provider: provider, timeout: o.ShutdownTimeout}, nil
}

type levelHandler struct {
	slog.Handler
	level slog.Level
}

func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level && h.Handler.Enabled(ctx, level)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{Handler: h.Handler.WithAttrs(attrs), level: h.level}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{Handler: h.Handler.WithGroup(name), level: h.level}
}

type logProvider struct {
	provider *sdklog.LoggerProvider
	timeout  time.Duration
}

func (p *logProvider) Close() error { return p.Shutdown(context.Background()) }

func (p *logProvider) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return p.provider.Shutdown(ctx)
}

type logExporter struct {
	sdklog.Exporter
	logger *slog.Logger
	limit  *rate.Limiter
	failed atomic.Uint64
}

func (e *logExporter) Export(ctx context.Context, records []sdklog.Record) error {
	if err := e.Exporter.Export(ctx, records); err != nil {
		count := e.failed.Add(uint64(len(records)))
		if e.limit.Allow() {
			e.logger.Warn("log export failed; batch discarded", "component", "logging", "failed_records", count)
		}
	}
	// Report locally without exposing raw exporter errors through OTel globals.
	return nil
}
