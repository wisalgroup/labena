package asynqx

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// NewWorker borrows the native server, handler and Redis pool. It performs no I/O.
// Run must be called once; callers must not start or shut down the server separately.
// The owner closes the pool only after Run returns.
func NewWorker(server *asynq.Server, handler asynq.Handler, pool *redis.Client, startupTimeout time.Duration) (*Worker, error) {
	if server == nil || handler == nil || pool == nil || startupTimeout <= 0 {
		return nil, errors.New("queue: server, handler, Redis pool and positive startup timeout required")
	}
	return &Worker{server: server, handler: &workerCalls{next: handler}, pool: pool, ready: make(chan struct{}), startupTimeout: startupTimeout}, nil
}

// Worker adapts native Start/Shutdown to a caller-owned context, without OS signal handling.
// Run joins all handler calls before returning; handlers must cooperate with cancellation.
type Worker struct {
	server         *asynq.Server
	handler        *workerCalls
	pool           *redis.Client
	ready          chan struct{}
	startupTimeout time.Duration
}

func (r *Worker) Ready() <-chan struct{} { return r.ready }
func (r *Worker) Run(ctx context.Context) error {
	startup, cancel := context.WithTimeout(ctx, r.startupTimeout)
	err := r.pool.Ping(startup).Err()
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("queue: Redis unavailable at worker startup")
	}
	if err := r.server.Start(r.handler); err != nil {
		return err
	}
	close(r.ready)
	<-ctx.Done()
	r.server.Shutdown()
	r.handler.closeAndWait()
	return nil
}

// Asynq may requeue a timed-out task while its Go handler is still unwinding.
// Keep borrowed app resources alive until those calls return. The gate rejects
// late invocations after native shutdown, so Wait cannot race with a new Add.
type workerCalls struct {
	next   asynq.Handler
	mu     sync.Mutex
	closed bool
	active sync.WaitGroup
}

func (c *workerCalls) ProcessTask(ctx context.Context, t *asynq.Task) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return context.Canceled
	}
	c.active.Add(1)
	c.mu.Unlock()
	defer c.active.Done()
	return c.next.ProcessTask(ctx, t)
}
func (c *workerCalls) closeAndWait() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.active.Wait()
}
