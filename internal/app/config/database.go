package config

import (
	"errors"
	"time"

	"github.com/wisalgroup/labena/pkg/database"
)

type DatabaseConfig struct {
	Name               string          `env:"DATABASE_NAME" envDefault:""`
	Driver             database.Driver `env:"DATABASE_DRIVER" envDefault:"postgres"`
	DSN                string          `env:"DATABASE_URL" envDefault:"" json:"-"`
	MaxOpenConns       int             `env:"DATABASE_MAX_OPEN_CONNS" envDefault:"20"`
	MaxIdleConns       int             `env:"DATABASE_MAX_IDLE_CONNS" envDefault:"5"`
	ConnMaxLifetime    time.Duration   `env:"DATABASE_CONN_MAX_LIFETIME" envDefault:"30m"`
	ConnMaxIdleTime    time.Duration   `env:"DATABASE_CONN_MAX_IDLE_TIME" envDefault:"5m"`
	ConnectTimeout     time.Duration   `env:"DATABASE_CONNECT_TIMEOUT" envDefault:"5s"`
	PingTimeout        time.Duration   `env:"DATABASE_PING_TIMEOUT" envDefault:"5s"`
	SlowQueryThreshold time.Duration   `env:"DATABASE_SLOW_QUERY_THRESHOLD" envDefault:"500ms"`
}

// ConnectionConfig returns the settings for the underlying package.
func (c DatabaseConfig) ConnectionConfig() database.Config {
	var options database.Config
	options.Name = c.Name
	options.Driver = c.Driver
	options.DSN = c.DSN
	options.MaxOpenConns = c.MaxOpenConns
	options.MaxIdleConns = c.MaxIdleConns
	options.ConnMaxLifetime = c.ConnMaxLifetime
	options.ConnMaxIdleTime = c.ConnMaxIdleTime
	options.ConnectTimeout = c.ConnectTimeout
	options.PingTimeout = c.PingTimeout
	options.SlowQueryThreshold = c.SlowQueryThreshold
	return options
}

// Validate checks an enabled application database without connecting.
func (c DatabaseConfig) Validate() error {
	if c.Driver != "" && c.Driver != database.DriverPostgres && c.Driver != database.DriverMySQL {
		return errors.New("config: application database must be PostgreSQL or MySQL")
	}
	return c.ConnectionConfig().Validate()
}
