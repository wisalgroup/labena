// Package ratelimit provides transport-independent admission limits.
package ratelimit

import (
	"context"
	"errors"
	"time"
)

// Limit allows a burst and replenishes Rate admissions per Period.
type Limit struct {
	Rate   int
	Burst  int
	Period time.Duration
}

func (l Limit) Validate() error {
	if l.Rate < 1 || l.Burst < 1 || l.Period < time.Millisecond || l.Period > 24*time.Hour {
		return errors.New("ratelimit: invalid limit")
	}
	return nil
}

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// Limiter must atomically decide and charge an admission across callers.
// An error is not permission to proceed. Implementations must support concurrent use.
type Limiter interface {
	Allow(context.Context, string, Limit) (Decision, error)
}
