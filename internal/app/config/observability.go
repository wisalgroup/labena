package config

import (
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/wisalgroup/labena/internal/platform/logviewer"
	"github.com/wisalgroup/labena/pkg/logging"
	"github.com/wisalgroup/labena/pkg/tracing"
)

// LoggingConfig selects default channels. Output settings live below in Go.
type LoggingConfig struct {
	DefaultChannels []string `env:"LOG_CHANNELS" envDefault:"stderr"`
	OTLPEndpoint    string   `env:"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT" envDefault:"http://localhost:4318/v1/logs"`
	Viewer          LogViewerConfig
}

// LogViewerConfig enables the local, independently authenticated HTTP viewer.
type LogViewerConfig struct {
	Enabled       bool   `env:"LOG_VIEWER_ENABLED" envDefault:"false"`
	Username      string `env:"LOG_VIEWER_USERNAME"`
	Password      string `env:"LOG_VIEWER_PASSWORD"`
	BufferEntries int    `env:"LOG_VIEWER_BUFFER_ENTRIES" envDefault:"2000"`
	BufferBytes   int    `env:"LOG_VIEWER_BUFFER_BYTES" envDefault:"8388608"`
}

func (c LogViewerConfig) ViewerConfig() logviewer.Config {
	return logviewer.Config{Username: c.Username, Password: c.Password, BufferEntries: c.BufferEntries, BufferBytes: c.BufferBytes}
}

func (c LogViewerConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	return c.ViewerConfig().Validate()
}

// Channels defines the application's outputs. Edit each channel independently;
// paths, formats, levels and colors are deliberately not environment settings.
func (c LoggingConfig) Channels(appName, environment string) logging.Config {
	channels := map[string]logging.Channel{
		"stderr": {
			Driver:  "stderr",
			Options: logging.Options{Format: logging.FormatTint, Level: slog.LevelInfo, NoColor: false},
		},
		"stdout": {
			Driver:  "stdout",
			Options: logging.Options{Format: logging.FormatTint, Level: slog.LevelInfo, NoColor: false},
		},
		"observability": {
			Driver:  "otlp",
			OTLP:    logging.OTLPOptions{Endpoint: c.OTLPEndpoint, ServiceName: appName, Environment: environment},
			Options: logging.Options{Level: slog.LevelInfo},
		},
		"file": {
			Driver:   "file",
			Path:     "storage/logs/app.log",
			FileMode: logging.FileDaily, // Use FileDaily for app-YYYY-MM-DD.log (UTC).
			KeepDays: 14,                // Daily mode: 0 keeps all; N keeps today and N-1 prior days.
			Options:  logging.Options{Format: logging.FormatTint, Level: slog.LevelWarn, NoColor: true},
		},
	}
	names := make([]string, len(c.DefaultChannels))
	for i, name := range c.DefaultChannels {
		names[i] = strings.TrimSpace(name)
	}
	return logging.Config{Default: names, Channels: channels}
}

// TracingConfig owns application environment settings; collectors remain optional.
type TracingConfig struct {
	TrustIncoming   bool          `env:"TRACING_TRUST_INCOMING" envDefault:"false"`
	Enabled         bool          `env:"TRACING_ENABLED" envDefault:"false"`
	Endpoint        string        `env:"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT" envDefault:"http://localhost:4318/v1/traces"`
	SampleRatio     float64       `env:"TRACING_SAMPLE_RATIO" envDefault:"1"`
	ExportTimeout   time.Duration `env:"TRACING_EXPORT_TIMEOUT" envDefault:"5s"`
	ShutdownTimeout time.Duration `env:"TRACING_SHUTDOWN_TIMEOUT" envDefault:"3s"`
}

func (c TracingConfig) ProviderConfig(appName string) tracing.Config {
	return tracing.Config{Enabled: c.Enabled, ServiceName: appName, Endpoint: c.Endpoint, SampleRatio: c.SampleRatio, ExportTimeout: c.ExportTimeout, ShutdownTimeout: c.ShutdownTimeout}
}

// MetricsConfig controls the separate, optional Prometheus scrape listener.
type MetricsConfig struct {
	Enabled bool   `env:"METRICS_ENABLED" envDefault:"false"`
	Address string `env:"METRICS_ADDRESS" envDefault:"127.0.0.1:9090"`
}

func (c MetricsConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	_, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return errors.New("config: METRICS_ADDRESS must be host:port")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return errors.New("config: invalid metrics port")
	}
	if strings.TrimSpace(c.Address) != c.Address {
		return errors.New("config: invalid metrics address")
	}
	return nil
}
