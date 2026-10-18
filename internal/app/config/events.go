package config

import (
	"errors"
	"time"
)

// EventsConfig configures process-local GoChannel pub/sub and router shutdown.
// Events are ephemeral, including registration reactions.
type EventsConfig struct {
	Driver          string        `env:"EVENTS_DRIVER" envDefault:"gochannel"`
	ShutdownTimeout time.Duration `env:"EVENTS_SHUTDOWN_TIMEOUT" envDefault:"10s"`
}

func (c EventsConfig) Validate() error {
	if c.Driver != "gochannel" {
		return errors.New("events: driver must be gochannel")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("events: shutdown timeout must be positive")
	}
	return nil
}
