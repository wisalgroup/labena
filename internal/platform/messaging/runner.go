package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"
)

// Runner owns explicit router shutdown and all admitted handler calls.
// Watermill Run can return after its close timeout while handlers still run.
// Joining those calls here keeps borrowed app resources alive until they finish.
type Runner struct {
	router     *message.Router
	ready      chan struct{}
	calls      eventCalls
	startupErr error // Published by router.Running or completion of router.Run.
}

// RegisterRunner installs lifecycle middleware and takes ownership of router
// execution. Call it once before adding other middleware or handlers, then Run
// after registration. It performs no I/O and starts no goroutines itself.
func RegisterRunner(router *message.Router) (*Runner, error) {
	if router == nil {
		return nil, errors.New("messaging: router is required")
	}
	r := &Runner{router: router, ready: make(chan struct{})}
	// Install first, outside retry, timeout and telemetry middleware.
	router.AddMiddleware(r.calls.middleware)
	router.AddSubscriberDecorators(func(sub message.Subscriber) (message.Subscriber, error) {
		return startupSubscriber{Subscriber: sub, failed: func(err error) { r.startupErr = errors.Join(r.startupErr, err) }}, nil
	})
	return r, nil
}

func (r *Runner) Ready() <-chan struct{} { return r.ready }

func (r *Runner) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// An empty native router waits for a handler even after cancellation.
	// There is no delivery loop to start when no subscriptions are installed.
	if len(r.router.Handlers()) == 0 {
		close(r.ready)
		<-ctx.Done()
		return r.router.Close()
	}
	work, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.router.Run(work) }()
	var runErr error
	finished := false
	select {
	case <-r.router.Running():
		if r.startupErr == nil {
			close(r.ready)
			select {
			case runErr = <-done:
				finished = true
			case <-ctx.Done():
			}
		}
	case runErr = <-done:
		finished = true
	case <-ctx.Done():
		// Unblock a context-aware Subscribe during partial startup.
		cancel()
	}
	r.calls.stop()
	closeErr := r.router.Close()
	cancel()
	if !finished {
		runErr = <-done
	}
	r.calls.active.Wait()
	if ctx.Err() != nil && errors.Is(runErr, context.Canceled) {
		runErr = nil
	}
	return errors.Join(r.startupErr, runErr, closeErr)
}

// Watermill counts handlers before subscribing. Returning a Subscribe error
// leaves unstarted handlers in its wait group forever (v1.5.3). A closed stream
// lets every native handler finish; the runner still fails startup with the
// original error, never signals readiness, and joins any admitted calls.
type startupSubscriber struct {
	message.Subscriber
	failed func(error)
}

func (s startupSubscriber) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	messages, err := s.Subscriber.Subscribe(ctx, topic)
	if err == nil {
		return messages, nil
	}
	s.failed(fmt.Errorf("subscribe %s: %w", topic, err))
	closed := make(chan *message.Message)
	close(closed)
	return closed, nil
}

type eventCalls struct {
	mu     sync.Mutex
	closed bool
	active sync.WaitGroup
}

func (c *eventCalls) middleware(next message.HandlerFunc) message.HandlerFunc {
	return func(msg *message.Message) ([]*message.Message, error) {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, context.Canceled
		}
		c.active.Add(1)
		c.mu.Unlock()
		defer c.active.Done()
		return next(msg)
	}
}

func (c *eventCalls) stop() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
}
