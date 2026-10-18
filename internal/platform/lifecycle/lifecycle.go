// Package lifecycle supervises process readiness, ordered drain and resource ownership.
// It knows nothing about application modules, transports or configuration sources.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/wisalgroup/labena/pkg/logging"
)

var (
	ErrStarted         = errors.New("lifecycle: already started or closed")
	ErrRunning         = errors.New("lifecycle: work is still running")
	ErrShutdownTimeout = errors.New("lifecycle: shutdown timed out")
)

// Config bounds startup, all drain phases together, and resource cleanup.
type Config struct{ StartupTimeout, ShutdownTimeout, CleanupTimeout time.Duration }

// Runtime owns one execution and the resources acquired for it. Register resources
// before Run, then Close after Run returns. Close refuses to release dependencies
// while work is active, including work that outlives the shutdown budget.
// Own is construction-only; Run and Close are safe against concurrent calls.
type Runtime struct {
	cfg                   Config
	logger                *slog.Logger
	cleanups              []cleanup
	ready, stopping       chan struct{}
	stopOnce              sync.Once
	mu                    sync.Mutex
	used, running, closed bool
	executions            []*execution
	closeOnce             sync.Once
	closeErr              error
}

// New constructs a lifetime without starting work. Durations must be positive.
func New(cfg Config, logger *slog.Logger) (*Runtime, error) {
	if cfg.StartupTimeout <= 0 || cfg.ShutdownTimeout <= 0 || cfg.CleanupTimeout <= 0 {
		return nil, errors.New("lifecycle: timeouts must be positive")
	}
	if logger == nil {
		return nil, errors.New("lifecycle: logger is required")
	}
	return &Runtime{cfg: cfg, logger: logger, ready: make(chan struct{}), stopping: make(chan struct{})}, nil
}

// Ready closes after every selected process has become ready.
func (a *Runtime) Ready() <-chan struct{} { return a.ready }

// Stopping closes before drain begins, including when construction is closed.
func (a *Runtime) Stopping() <-chan struct{} { return a.stopping }

// Run starts the selected processes and joins them in phase order on cancellation
// or failure. Normal cancellation returns nil after successful drain. Call Close
// afterward to release resources. An unexpected process exit fails the group.
func (a *Runtime) Run(ctx context.Context, processes ...Process) error {
	if err := a.start(); err != nil {
		return err
	}
	defer a.endRun()
	ctx = logging.WithLogger(ctx, a.logger)
	startup, cancel := context.WithTimeout(ctx, a.cfg.StartupTimeout)
	defer cancel()
	return a.runRunners(ctx, startup, processes)
}

// AfterReady delays a process until all prerequisites are ready. Cancellation
// while waiting never starts the underlying process.
func AfterReady(r Runner, dependencies ...<-chan struct{}) Runner {
	return afterReady{runner: r, dependencies: slices.Clone(dependencies)}
}

func (a *Runtime) start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.used || a.closed {
		return ErrStarted
	}
	a.used = true
	a.running = true
	return nil
}

func (a *Runtime) endRun() {
	a.stopOnce.Do(func() { close(a.stopping) })
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
}

// Runner owns its work until Run returns. Cancellation stops admission and
// drains or cancels in-flight work. Ready closes only after startup succeeds.
// Every process implements the same contract, including native library adapters.
type Runner interface {
	Run(context.Context) error
	Ready() <-chan struct{}
}

// Process names a runner and its drain phase. Lower phases stop first; all
// runners in the same phase are canceled together before any are joined.
type Process struct {
	Name   string
	Runner Runner
	Phase  int
}
type execution struct {
	process Process
	cancel  context.CancelFunc
	done    chan struct{}
	err     error // Published by closing done.
}

// runRunners runs within the ownership established by Run. Separate cancellation
// contexts let upstream work drain before downstream consumers stop.
func (a *Runtime) runRunners(ctx, startup context.Context, processes []Process) error {
	base := context.WithoutCancel(ctx)
	executions := make([]*execution, len(processes))
	a.executions = executions
	exited := make(chan *execution, len(processes))
	for i, s := range processes {
		work, cancel := context.WithCancel(base)
		e := &execution{process: s, cancel: cancel, done: make(chan struct{})}
		executions[i] = e
		go func() {
			defer func() {
				if p := recover(); p != nil {
					e.err = fmt.Errorf("panic: %v", p)
				}
				close(e.done)
				exited <- e
			}()
			e.err = s.Runner.Run(work)
		}()
	}
	exitError := func(e *execution) error {
		err := e.err
		if err == nil {
			err = errors.New("returned before cancellation")
		}
		return fmt.Errorf("lifecycle: process %s: %w", e.process.Name, err)
	}
	var cause error
	var failed *execution
	for _, s := range processes {
		select {
		case <-s.Runner.Ready():
		case e := <-exited:
			failed = e
			cause = exitError(e)
		case <-startup.Done():
			if ctx.Err() == nil {
				cause = fmt.Errorf("lifecycle: startup: %w", startup.Err())
			}
		}
		if cause != nil || startup.Err() != nil {
			break
		}
	}
	if cause == nil && startup.Err() == nil {
		close(a.ready)
		select {
		case <-ctx.Done():
		case e := <-exited:
			failed = e
			cause = exitError(e)
		}
	}
	a.stopOnce.Do(func() { close(a.stopping) })
	shutdown, cancel := context.WithTimeout(base, a.cfg.ShutdownTimeout)
	defer cancel()
	drainErr := drain(shutdown, executions)
	if errors.Is(drainErr, ErrShutdownTimeout) {
		a.logger.Error("application shutdown timed out", "error_category", "timeout", "shutdown_timeout", a.cfg.ShutdownTimeout)
	}
	for _, e := range executions {
		select {
		case <-e.done:
		default:
			continue // A timed-out runner still owns its error and dependencies.
		}
		if e != failed && e.err != nil && !errors.Is(e.err, context.Canceled) {
			cause = errors.Join(cause, fmt.Errorf("lifecycle: process %s: %w", e.process.Name, e.err))
		}
	}
	return errors.Join(cause, drainErr)
}

// drain cancels each group together, then joins it before stopping its consumers.
// Sort a copy so execution order and error reporting stay in registration order.
func drain(ctx context.Context, executions []*execution) error {
	ordered := slices.Clone(executions)
	slices.SortStableFunc(ordered, func(a, b *execution) int { return int(a.process.Phase - b.process.Phase) })
	for len(ordered) > 0 {
		end := 1
		for end < len(ordered) && ordered[end].process.Phase == ordered[0].process.Phase {
			end++
		}
		for _, e := range ordered[:end] {
			e.cancel()
		}
		for _, e := range ordered[:end] {
			select {
			case <-e.done:
			case <-ctx.Done():
				for _, remaining := range executions {
					remaining.cancel()
				}
				return ErrShutdownTimeout
			}
		}
		ordered = ordered[end:]
	}
	return nil
}

// afterReady gates admission until prerequisite processes are ready. Cancellation
// while waiting exits without starting the dependent process.
type afterReady struct {
	runner       Runner
	dependencies []<-chan struct{}
}

func (r afterReady) Ready() <-chan struct{} { return r.runner.Ready() }
func (r afterReady) Run(ctx context.Context) error {
	for _, ready := range r.dependencies {
		select {
		case <-ready:
		case <-ctx.Done():
			return nil
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return r.runner.Run(ctx)
}

// Close releases resources once in reverse construction order. Call it after
// execution returns, with an uncanceled context. It applies the cleanup
// deadline, preserves context values, and refuses to close resources still in use.
// Commands must join their goroutines before returning.
func (a *Runtime) Close(ctx context.Context) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return ErrRunning
	}
	for _, e := range a.executions {
		select {
		case <-e.done:
		default:
			a.mu.Unlock()
			return ErrRunning
		}
	}
	a.closed = true
	a.mu.Unlock()
	a.stopOnce.Do(func() { close(a.stopping) })
	a.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(logging.WithLogger(ctx, a.logger), a.cfg.CleanupTimeout)
		defer cancel()
		for i := len(a.cleanups) - 1; i >= 0; i-- {
			r := a.cleanups[i]
			if err := closeDependency(ctx, r.close); err != nil {
				a.closeErr = errors.Join(a.closeErr, fmt.Errorf("lifecycle: close %s: %w", r.name, err))
			}
		}
	})
	return a.closeErr
}

// cleanup adapts owned resources to one cleanup contract. Resources are not
// services: a database connection has no application Run loop.
type cleanup struct {
	name  string
	close func(context.Context) error
}

// Own is only used during construction, immediately after acquiring a resource.
func (a *Runtime) Own(name string, close func(context.Context) error) {
	a.cleanups = append(a.cleanups, cleanup{name: name, close: close})
}

// OwnClient adapts native connection Close methods to app cleanup.
func (a *Runtime) OwnClient(name string, close func() error) {
	a.Own(name, func(context.Context) error { return close() })
}

func closeDependency(ctx context.Context, close func(context.Context) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return close(ctx)
}
