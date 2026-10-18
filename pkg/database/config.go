package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Config describes one connection pool. DSN must not be logged. Zero durations
// and MaxOpenConns select bounded defaults; MaxIdleConns follows database/sql:
// zero disables idle connections. Use DefaultConfig for recommended idle pooling.
type Config struct {
	Name               string        `json:"name,omitempty"`
	Driver             Driver        `json:"driver"`
	DSN                string        `json:"dsn,omitempty"`
	MaxOpenConns       int           `json:"max_open_conns"`
	MaxIdleConns       int           `json:"max_idle_conns"`
	ConnMaxLifetime    time.Duration `json:"conn_max_lifetime"`
	ConnMaxIdleTime    time.Duration `json:"conn_max_idle_time"`
	ConnectTimeout     time.Duration `json:"connect_timeout"`
	PingTimeout        time.Duration `json:"ping_timeout"`
	SlowQueryThreshold time.Duration `json:"slow_query_threshold"`
}

// DefaultConfig returns a PostgreSQL pool configuration. Supply DSN before Open.
func DefaultConfig() Config {
	return Config{Driver: DriverPostgres, MaxOpenConns: 20, MaxIdleConns: 5,
		ConnMaxLifetime: 30 * time.Minute, ConnMaxIdleTime: 5 * time.Minute,
		ConnectTimeout: 5 * time.Second, PingTimeout: 5 * time.Second, SlowQueryThreshold: 500 * time.Millisecond}
}

func (c Config) normalized() (Config, error) {
	if c.Driver == "" {
		c.Driver = DriverPostgres
	}
	if err := c.Driver.Validate(); err != nil {
		return c, err
	}
	if strings.TrimSpace(c.DSN) == "" {
		return c, errors.New("database: DSN is required")
	}
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 20
	}
	if c.ConnMaxLifetime == 0 {
		c.ConnMaxLifetime = 30 * time.Minute
	}
	if c.ConnMaxIdleTime == 0 {
		c.ConnMaxIdleTime = 5 * time.Minute
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	if c.SlowQueryThreshold == 0 {
		c.SlowQueryThreshold = 500 * time.Millisecond
	}
	if c.PingTimeout == 0 {
		c.PingTimeout = 5 * time.Second
	}
	if c.MaxOpenConns < 1 || c.MaxIdleConns < 0 || c.MaxIdleConns > c.MaxOpenConns {
		return c, errors.New("database: pool requires 0 <= MaxIdleConns <= MaxOpenConns and MaxOpenConns > 0")
	}
	if c.SlowQueryThreshold < 0 {
		return c, errors.New("database: slow query threshold must not be negative")
	}
	if c.ConnMaxLifetime < 0 || c.ConnMaxIdleTime < 0 || c.ConnectTimeout < 0 || c.PingTimeout < 0 {
		return c, errors.New("database: timeouts and connection lifetimes must not be negative")
	}
	// A private in-memory SQLite database is tied to one physical connection.
	// Keep it alive and prevent different callers from seeing different schemas.
	if c.Driver == DriverSQLite && (c.DSN == ":memory:" || strings.Contains(c.DSN, "mode=memory") || strings.HasPrefix(c.DSN, "file::memory:")) {
		c.MaxOpenConns = 1
		c.MaxIdleConns = 1
		c.ConnMaxLifetime = 0
		c.ConnMaxIdleTime = 0
	}
	return c, nil
}

// Validate checks configuration without opening connections.
func (c Config) Validate() error { _, err := c.normalized(); return err }

// String deliberately excludes connection secrets, including malformed DSNs.
func (c Config) String() string {
	return fmt.Sprintf("database.Config{Name:%q Driver:%q MaxOpenConns:%d MaxIdleConns:%d}", c.Name, c.Driver, c.MaxOpenConns, c.MaxIdleConns)
}
func (c Config) GoString() string { return c.String() }

// MarshalJSON redacts the DSN while retaining normal JSON decoding support for
// configuration loaders. Serialized configuration is not a credential backup.
func (c Config) MarshalJSON() ([]byte, error) {
	type configJSON Config
	copy := configJSON(c)
	copy.DSN = ""
	return json.Marshal(copy)
}
