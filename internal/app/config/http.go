package config

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"time"

	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/pkg/httpserver"
)

// HTTPConfig configures the application HTTP listener.
type HTTPConfig struct {
	TLSCertFile           string        `env:"HTTP_TLS_CERT_FILE"`
	TLSKeyFile            string        `env:"HTTP_TLS_KEY_FILE"`
	HTTP2Enabled          bool          `env:"HTTP_HTTP2_ENABLED" envDefault:"false"`
	DocsEnabled           bool          `env:"HTTP_DOCS_ENABLED" envDefault:"true"`
	ReadinessDependencies bool          `env:"HTTP_READINESS_DEPENDENCIES" envDefault:"false"`
	TrustedProxies        []string      `env:"HTTP_TRUSTED_PROXIES"`
	ReadinessTimeout      time.Duration `env:"HTTP_READINESS_TIMEOUT" envDefault:"1s"`
	CORS                  CORSConfig
	Address               string        `env:"HTTP_ADDRESS" envDefault:":8080"`
	ReadHeaderTimeout     time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" envDefault:"5s"`
	ReadTimeout           time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout          time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"15s"`
	IdleTimeout           time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`
	ShutdownTimeout       time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"10s"`
	MaxHeaderBytes        int           `env:"HTTP_MAX_HEADER_BYTES" envDefault:"1048576"`
}

// ServerConfig returns the settings for the HTTP server.
func (c HTTPConfig) ServerConfig() httpserver.Config {
	return httpserver.Config{TLSCertFile: c.TLSCertFile, TLSKeyFile: c.TLSKeyFile, HTTP2: c.HTTP2Enabled, Address: c.Address, ReadHeaderTimeout: c.ReadHeaderTimeout, ReadTimeout: c.ReadTimeout, WriteTimeout: c.WriteTimeout, IdleTimeout: c.IdleTimeout, ShutdownTimeout: c.ShutdownTimeout, MaxHeaderBytes: c.MaxHeaderBytes}
}

func (c HTTPConfig) Validate() error {
	if _, err := c.TrustedProxyPrefixes(); err != nil {
		return err
	}
	if c.ReadinessDependencies && c.ReadinessTimeout <= 0 {
		return errors.New("config: HTTP readiness timeout must be positive")
	}
	if err := c.CORS.Validate(); err != nil {
		return err
	}
	if c.Address == "" {
		return errors.New("config: HTTP address is required")
	}
	if _, err := httpserver.New(c.ServerConfig(), http.NotFoundHandler()); err != nil {
		return fmt.Errorf("config: HTTP: %w", err)
	}
	return nil
}

// TrustedProxyPrefixes translates environment values to the HTTP owner's policy.
func (c HTTPConfig) TrustedProxyPrefixes() ([]netip.Prefix, error) {
	prefixes, err := api.ParseTrustedProxies(c.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("config: HTTP_TRUSTED_PROXIES: %w", err)
	}
	return prefixes, nil
}
