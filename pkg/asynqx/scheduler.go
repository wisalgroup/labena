package asynqx

import (
	"context"
	"errors"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// NewSchedulerRunner borrows a scheduler; registration must finish before Run.
// Call Run once, without also calling the native Start/Run/Shutdown methods.
// The caller closes the pool after Run returns. Construction performs no I/O.
func NewSchedulerRunner(s *asynq.Scheduler, pool *redis.Client, startupTimeout time.Duration) (*SchedulerRunner, error) {
	if s == nil || pool == nil || startupTimeout <= 0 {
		return nil, errors.New("asynqx: scheduler, Redis pool and positive startup timeout required")
	}
	return &SchedulerRunner{scheduler: s, pool: pool, startupTimeout: startupTimeout, ready: make(chan struct{})}, nil
}

// SchedulerRunner stops recurring publication when the caller cancels its context.
type SchedulerRunner struct {
	scheduler      *asynq.Scheduler
	pool           *redis.Client
	startupTimeout time.Duration
	ready          chan struct{}
}

func (r *SchedulerRunner) Ready() <-chan struct{} { return r.ready }
func (r *SchedulerRunner) Run(ctx context.Context) error {
	startup, cancel := context.WithTimeout(ctx, r.startupTimeout)
	err := r.pool.Ping(startup).Err()
	cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return errors.New("asynqx: Redis unavailable at scheduler startup")
	}
	if err := r.scheduler.Start(); err != nil {
		return err
	}
	close(r.ready)
	<-ctx.Done()
	r.scheduler.Shutdown()
	return nil
}
