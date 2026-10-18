package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"

	"github.com/wisalgroup/labena/internal/platform/messaging"
)

type runnerFunc func(context.Context) error

func (f runnerFunc) Run(ctx context.Context) error { return f(ctx) }
func (f runnerFunc) Ready() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}

type readyRunner struct {
	ready chan struct{}
	run   func(context.Context) error
}

func (r readyRunner) Run(ctx context.Context) error { return r.run(ctx) }
func (r readyRunner) Ready() <-chan struct{}        { return r.ready }
func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	runtime, err := New(Config{StartupTimeout: time.Second, ShutdownTimeout: time.Second, CleanupTimeout: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestNewRejectsInvalidConfigurationWithoutPanicking(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	valid := Config{StartupTimeout: time.Second, ShutdownTimeout: time.Second, CleanupTimeout: time.Second}
	for _, cfg := range []Config{
		{ShutdownTimeout: time.Second, CleanupTimeout: time.Second},
		{StartupTimeout: time.Second, ShutdownTimeout: -time.Second, CleanupTimeout: time.Second},
		{StartupTimeout: time.Second, ShutdownTimeout: time.Second},
	} {
		if runtime, err := New(cfg, logger); err == nil || runtime != nil {
			t.Fatal("invalid settings produced a runtime", runtime, err)
		}
	}
	if runtime, err := New(valid, nil); err == nil || runtime != nil {
		t.Fatal("missing logger produced a runtime", runtime, err)
	}
}

func TestDrainOrderAndOwnership(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newTestRuntime(t)
		var sequence []string
		for _, name := range []string{"database", "driver"} {
			a.Own(name, func(context.Context) error { sequence = append(sequence, name); return nil })
		}
		work := make(chan struct{})
		accepted := make(chan struct{})
		worker := Process{"worker", runnerFunc(func(ctx context.Context) error {
			select {
			case <-work:
				close(accepted)
			case <-ctx.Done():
				return errors.New("consumer stopped before producer drained")
			}
			<-ctx.Done()
			sequence = append(sequence, "worker")
			return nil
		}), 2}
		producer := Process{"producer", runnerFunc(func(ctx context.Context) error {
			<-ctx.Done()
			work <- struct{}{}
			<-accepted
			sequence = append(sequence, "producer")
			return nil
		}), 0}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- a.Run(ctx, worker, producer) }()
		<-a.ready
		if err := a.Close(context.Background()); !errors.Is(err, ErrRunning) {
			t.Fatal(err)
		}
		if err := a.Run(ctx); !errors.Is(err, ErrStarted) {
			t.Fatal(err)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(sequence, []string{"producer", "worker"}) {
			t.Fatal("Run closed resources before Close", sequence)
		}
		for range 2 {
			if err := a.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(sequence, []string{"producer", "worker", "driver", "database"}) {
			t.Fatal(sequence)
		}
	})
}

func TestStartupTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newTestRuntime(t)
		a.cfg.StartupTimeout = time.Second
		r := readyRunner{make(chan struct{}), func(ctx context.Context) error { <-ctx.Done(); return nil }}
		if err := a.Run(context.Background(), Process{"blocked", r, 0}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		select {
		case <-a.ready:
			t.Fatal("ready after failed startup")
		default:
		}
		if err := a.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestUnexpectedExitStopsSiblings(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, panicRunner := range []bool{false, true} {
			a := newTestRuntime(t)
			stopped := make(chan struct{})
			failure := errors.New("listener failed")
			err := a.Run(context.Background(),
				Process{"failed", runnerFunc(func(context.Context) error {
					if panicRunner {
						panic("broken runner")
					}
					return failure
				}), 0},
				Process{"sibling", runnerFunc(func(ctx context.Context) error { <-ctx.Done(); close(stopped); return nil }), 0})
			if err == nil || (!panicRunner && !errors.Is(err, failure)) {
				t.Fatal(err)
			}
			select {
			case <-stopped:
			default:
				t.Fatal("sibling not joined")
			}
			if err := a.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestShutdownTimeoutKeepsActiveResourcesOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newTestRuntime(t)
		a.cfg.ShutdownTimeout = time.Second
		release := make(chan struct{})
		closed := false
		a.Own("resource", func(context.Context) error { closed = true; return nil })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- a.Run(ctx, Process{"slow", runnerFunc(func(context.Context) error { <-release; return nil }), 0})
		}()
		<-a.ready
		cancel()
		if err := <-done; !errors.Is(err, ErrShutdownTimeout) {
			t.Fatal(err)
		}
		if err := a.Close(context.Background()); !errors.Is(err, ErrRunning) || closed {
			t.Fatal("closed resources in use", err)
		}
		close(release)
		synctest.Wait()
		if err := a.Close(context.Background()); err != nil || !closed {
			t.Fatal(err)
		}
	})
}

func TestCleanupPreservesErrorsAndContinuesAfterPanic(t *testing.T) {
	a := newTestRuntime(t)
	want := errors.New("close failed")
	calls := 0
	a.Own("first", func(context.Context) error { calls++; return want })
	a.Own("broken", func(context.Context) error { panic("closer") })
	if err := a.Close(context.Background()); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if err := a.Close(context.Background()); !errors.Is(err, want) || calls != 1 {
		t.Fatal(err, calls)
	}
	if err := a.Run(context.Background()); !errors.Is(err, ErrStarted) {
		t.Fatal(err)
	}
}

func TestDrainCancelsWholeGroupAndPreservesErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newTestRuntime(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		peerStopped := make(chan struct{})
		firstErr, secondErr := errors.New("first drain failed"), errors.New("second drain failed")
		done := make(chan error, 1)
		go func() {
			done <- a.Run(ctx,
				Process{"first", runnerFunc(func(ctx context.Context) error {
					<-ctx.Done()
					<-peerStopped // Both peers must be canceled before joining either.
					return firstErr
				}), 0},
				Process{"second", runnerFunc(func(ctx context.Context) error {
					<-ctx.Done()
					close(peerStopped)
					return secondErr
				}), 0})
		}()
		<-a.ready
		cancel()
		err := <-done
		if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) || errors.Is(err, ErrShutdownTimeout) {
			t.Fatalf("drain lost errors or timed out: %v", err)
		}
		if err := a.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAfterReadyAdmission(t *testing.T) {
	for _, cancelBeforeReady := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelBeforeReady), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				dependency := make(chan struct{})
				started := false
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				gated := afterReady{runner: runnerFunc(func(context.Context) error {
					started = true
					return nil
				}), dependencies: []<-chan struct{}{dependency}}
				done := make(chan error, 1)
				go func() { done <- gated.Run(ctx) }()
				synctest.Wait()
				if started {
					t.Fatal("admitted work before dependency was ready")
				}
				if cancelBeforeReady {
					cancel()
				}
				close(dependency)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if started == cancelBeforeReady {
					t.Fatalf("started=%v, canceled=%v", started, cancelBeforeReady)
				}
			})
		})
	}
}

func TestEmptyMessagingRunnerStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		router, err := message.NewRouter(message.RouterConfig{CloseTimeout: time.Second}, watermill.NopLogger{})
		if err != nil {
			t.Fatal(err)
		}
		events, err := messaging.RegisterRunner(router)
		if err != nil {
			t.Fatal(err)
		}
		a := newTestRuntime(t)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- a.Run(ctx, Process{"events", events, 2}) }()
		<-a.ready
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if err := a.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMessagingTimeoutKeepsHandlerResourcesAlive(t *testing.T) {
	router, err := message.NewRouter(message.RouterConfig{CloseTimeout: 20 * time.Millisecond}, watermill.NopLogger{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := messaging.RegisterRunner(router)
	if err != nil {
		t.Fatal(err)
	}
	bus := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})
	defer bus.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	closed := false
	router.AddConsumerHandler("slow", "slow", bus, func(*message.Message) error {
		close(entered)
		<-release
		if closed {
			t.Error("resources closed while handler was active")
		}
		return nil
	})
	a := newTestRuntime(t)
	a.cfg.ShutdownTimeout = 50 * time.Millisecond
	a.OwnClient("handler dependency", func() error { closed = true; return nil })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, Process{"events", events, 2}) }()
	awaitEventSignal(t, a.ready)
	if err := bus.Publish("slow", message.NewMessage("test", nil)); err != nil {
		t.Fatal(err)
	}
	awaitEventSignal(t, entered)
	cancel()
	if err := <-done; !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("want shutdown timeout, got %v", err)
	}
	if err := a.Close(context.Background()); !errors.Is(err, ErrRunning) || closed {
		t.Fatalf("closed active resources: %v", err)
	}
	unblock()
	awaitEventSignal(t, a.executions[0].done)
	if a.executions[0].err == nil {
		t.Error("native close timeout was lost")
	}
	if err := a.Close(context.Background()); err != nil || !closed {
		t.Fatalf("cleanup after handler returned: %v", err)
	}
}

// The first Subscribe admits a handler; the second fails only after that handler
// has entered, regardless of Watermill's map iteration order.
type partialSubscriber struct {
	calls   int
	entered <-chan struct{}
	failed  chan struct{}
	failure error
}

func (s *partialSubscriber) Subscribe(ctx context.Context, _ string) (<-chan *message.Message, error) {
	s.calls++
	if s.calls == 2 {
		<-s.entered
		close(s.failed)
		return nil, s.failure
	}
	messages := make(chan *message.Message, 1)
	messages <- message.NewMessage("partial", nil)
	go func() { <-ctx.Done(); close(messages) }()
	return messages, nil
}
func (*partialSubscriber) Close() error { return nil }

func TestMessagingPartialStartupJoinsAdmittedCalls(t *testing.T) {
	router, err := message.NewRouter(message.RouterConfig{CloseTimeout: 20 * time.Millisecond}, watermill.NopLogger{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := messaging.RegisterRunner(router)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	failure := errors.New("subscribe failed")
	sub := &partialSubscriber{entered: entered, failed: make(chan struct{}), failure: failure}
	for _, name := range []string{"first", "second"} {
		router.AddConsumerHandler(name, name, sub, func(*message.Message) error { close(entered); <-release; return nil })
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- events.Run(ctx) }()
	select {
	case <-sub.failed:
	case <-ctx.Done():
		t.Fatal("partial startup did not reach failure")
	}
	select {
	case <-events.Ready():
		t.Fatal("failed startup became ready")
	default:
	}
	select {
	case err := <-done:
		t.Fatalf("returned with admitted handler running: %v", err)
	default:
	}
	unblock()
	select {
	case err := <-done:
		if !errors.Is(err, failure) {
			t.Fatalf("lost subscribe failure: %v", err)
		}
		select {
		case <-events.Ready():
			t.Fatal("failed startup became ready")
		default:
		}
	case <-ctx.Done():
		t.Fatal("partial startup was not joined")
	}
}

func awaitEventSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("event lifecycle signal timed out")
	}
}
