// Package metrics owns an isolated OpenTelemetry meter provider and Prometheus
// scrape handler. It installs no global providers and starts no listeners.
package metrics

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Provider owns its registry and SDK. Borrow MeterProvider for instrumentation.
// Stop scrapes and instrument users before Shutdown. It is safe for concurrent use.
type Provider struct {
	*sdkmetric.MeterProvider
	handler  http.Handler
	registry *prometheus.Registry
}

// New creates a private registry with Go and process collectors. ServiceName is
// bounded deployment metadata, not a request/tenant identifier. The logger is borrowed.
func New(serviceName string, logger *slog.Logger) (*Provider, error) {
	if strings.TrimSpace(serviceName) == "" || logger == nil {
		return nil, errors.New("metrics: service name and logger are required")
	}
	registry := prometheus.NewRegistry()
	for _, collector := range []prometheus.Collector{collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})} {
		if err := registry.Register(collector); err != nil {
			return nil, err
		}
	}
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return nil, err
	}
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(resource.NewSchemaless(attribute.String("service.name", serviceName))),
		sdkmetric.WithCardinalityLimit(2000),
	)
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorLog:            slog.NewLogLogger(logger.Handler(), slog.LevelError),
		MaxRequestsInFlight: 2, CoalesceGather: true, Timeout: 5 * time.Second,
	})
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", handler)
	return &Provider{MeterProvider: provider, handler: mux, registry: registry}, nil
}

// Handler exposes only /metrics. The caller owns the listener and its access
// policy. Do not mount on a public business router. Scrapes do not contact a backend.
func (p *Provider) Handler() http.Handler { return p.handler }

// Registerer lets native library instrumentation share the isolated scrape registry.
func (p *Provider) Registerer() prometheus.Registerer { return p.registry }
