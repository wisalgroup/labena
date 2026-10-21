package config

import (
	"time"

	"github.com/wisalgroup/labena/pkg/ratelimit"
)

// Authentication always uses Redis limits when enabled.
// Enabled controls the additional broad API limit, not the authentication limits.
type RateLimitConfig struct {
	Enabled   bool          `env:"API_RATE_LIMIT_ENABLED" envDefault:"false"`
	RedisURL  string        `env:"RATE_LIMIT_REDIS_URL" envDefault:"redis://127.0.0.1:6379/2" json:"-"`
	Namespace string        `env:"RATE_LIMIT_NAMESPACE" envDefault:"labena"`
	Requests  int           `env:"API_RATE_LIMIT_REQUESTS" envDefault:"120"`
	Burst     int           `env:"API_RATE_LIMIT_BURST" envDefault:"120"`
	Period    time.Duration `env:"API_RATE_LIMIT_PERIOD" envDefault:"1m"`
}

func (c RateLimitConfig) Limit() ratelimit.Limit {
	return ratelimit.Limit{Rate: c.Requests, Burst: c.Burst, Period: c.Period}
}
