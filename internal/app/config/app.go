package config

import (
	"errors"
	"strings"
	"time"
)

// AppConfig defines application identity and lifecycle budgets.
type AppConfig struct {
	AppName         string        `env:"APP_NAME" envDefault:"labena"`
	Environment     string        `env:"APP_ENV" envDefault:"development"`
	StartupTimeout  time.Duration `env:"APP_STARTUP_TIMEOUT" envDefault:"30s"`
	ShutdownTimeout time.Duration `env:"APP_SHUTDOWN_TIMEOUT" envDefault:"45s"`
	CleanupTimeout  time.Duration `env:"APP_CLEANUP_TIMEOUT" envDefault:"10s"`
}

func (c AppConfig) Validate() error {
	if strings.TrimSpace(c.AppName) == "" {
		return errors.New("config: APP_NAME is required")
	}
	switch c.Environment {
	case "development", "test", "staging", "production":
	default:
		return errors.New("config: APP_ENV must be development, test, staging or production")
	}
	if c.StartupTimeout <= 0 || c.ShutdownTimeout <= 0 || c.CleanupTimeout <= 0 {
		return errors.New("config: application timeouts must be positive")
	}
	return nil
}
