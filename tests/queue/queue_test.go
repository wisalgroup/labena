package queue_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	asynqmetrics "github.com/hibiken/asynq/x/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/wisalgroup/labena/internal/app/config"
	tasks "github.com/wisalgroup/labena/internal/modules/identity/jobs"
	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/internal/platform/lifecycle"
	"github.com/wisalgroup/labena/internal/platform/messaging"
	"github.com/wisalgroup/labena/pkg/asynqx"
	asynqmiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
	"github.com/wisalgroup/labena/pkg/metrics"
)

func TestRedisNativeResultsPolicyAndAdministration(t *testing.T) {
	url := os.Getenv("QUEUE_TEST_REDIS")
	if url == "" {
		t.Skip("set QUEUE_TEST_REDIS to disposable Redis")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	pool := redis.NewClient(opts)
	defer pool.Close()
	inspector := asynq.NewInspectorFromRedisClient(pool)
	name := "test_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	defer inspector.DeleteQueue(name, true)
	client := asynq.NewClientFromRedisClient(pool)
	// Administrative changes before starting workers avoid timing-dependent claims.
	discarded, err := client.EnqueueContext(t.Context(), asynq.NewTask("result", []byte(`{}`)), asynq.Queue(name), asynq.ProcessIn(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := inspector.ArchiveTask(name, discarded.ID); err != nil {
		t.Fatal(err)
	}
	waitTaskState(t, inspector, name, discarded.ID, asynq.TaskStateArchived)
	if err := inspector.RunTask(name, discarded.ID); err != nil {
		t.Fatal(err)
	}
	waitTaskState(t, inspector, name, discarded.ID, asynq.TaskStatePending)
	if err := inspector.DeleteTask(name, discarded.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.GetTaskInfo(name, discarded.ID); err == nil {
		t.Fatal("deleted task exists")
	}
	if err := inspector.PauseQueue(name); err != nil {
		t.Fatal(err)
	}
	status, err := inspector.GetQueueInfo(name)
	if err != nil || !status.Paused {
		t.Fatal(status, err)
	}
	if err := inspector.UnpauseQueue(name); err != nil {
		t.Fatal(err)
	}
	status, err = inspector.GetQueueInfo(name)
	if err != nil || status.Paused {
		t.Fatal(status, err)
	}

	temporary := errors.New("deferred without consuming retry budget")
	var calls atomic.Int32
	policyCalled := make(chan bool, 4)

	cfg := config.Defaults()
	cfg.Queue.RedisURL = url
	cfg.Queue.Default = name
	cfg.Queue.Queues = map[string]config.QueueOptions{name: {Priority: 1}}
	cfg.Queue.RetryDelay = func(n int, err error, j *asynq.Task) time.Duration {
		policyCalled <- n == 0 && j.Type() == "result" && j.ResultWriter() == nil
		return time.Millisecond
	}
	cfg.Queue.IsFailure = func(err error) bool { return !errors.Is(err, temporary) }
	mux := asynq.NewServeMux()
	worker := newWorker(t, pool, mux, asynq.Config{Concurrency: 2, Queues: map[string]int{name: 1}, ShutdownTimeout: time.Second, RetryDelayFunc: cfg.Queue.RetryDelay, IsFailure: cfg.Queue.IsFailure})
	mux.HandleFunc("result", func(ctx context.Context, j *asynq.Task) error {
		if calls.Add(1) == 1 {
			return temporary
		}
		retry, _ := asynq.GetRetryCount(ctx)
		taskQueue, _ := asynq.GetQueueName(ctx)
		if retry != 0 || taskQueue != name {
			return errors.New("classification consumed retry budget")
		}
		_, err := j.ResultWriter().Write([]byte(`{"ok":true}`))
		return err
	})
	started := make(chan struct{})
	canceled := make(chan struct{})
	mux.HandleFunc("cancel", func(ctx context.Context, j *asynq.Task) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	})
	result, err := client.EnqueueContext(t.Context(), asynq.NewTask("result", []byte(`{}`)), asynq.Queue(name), asynq.MaxRetry(1), asynq.Retention(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	cancelInfo, err := client.EnqueueContext(t.Context(), asynq.NewTask("cancel", []byte(`{}`)), asynq.Queue(name), asynq.MaxRetry(0))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitTaskState(t, inspector, name, result.ID, asynq.TaskStateCompleted)
	select {
	case ok := <-policyCalled:
		if !ok {
			t.Fatal("incorrect policy arguments")
		}
	default:
		t.Fatal("retry policy not called")
	}
	info, err := inspector.GetTaskInfo(name, result.ID)
	if err != nil || string(info.Result) != `{"ok":true}` || calls.Load() != 2 {
		t.Fatal(info, err, calls.Load())
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel task did not start")
	}
	if err := inspector.CancelProcessing(cancelInfo.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation not delivered")
	}
	waitTaskState(t, inspector, name, cancelInfo.ID, asynq.TaskStateArchived)
}

func waitTaskState(t *testing.T, i *asynq.Inspector, q, id string, state asynq.TaskState) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		info, err := i.GetTaskInfo(q, id)
		if err == nil && info.State == state {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("task %s did not reach %s: %v", id, state, err)
		case <-tick.C:
		}
	}
}

func TestRedisWorkerPublishesLocalEventAndDrains(t *testing.T) {
	url := os.Getenv("QUEUE_TEST_REDIS")
	if url == "" {
		t.Skip("set QUEUE_TEST_REDIS to disposable Redis")
	}
	cfg := config.Defaults()
	name := "test_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	cfg.Queue.RedisURL = url
	cfg.Queue.Default = name
	cfg.Queue.Queues = map[string]config.QueueOptions{"default": {Priority: 1}, name: {Priority: 1}}
	cfg.Queue.ShutdownTimeout = 2 * time.Second
	cfg.Metrics.Enabled = true
	cfg.Metrics.Address = "127.0.0.1:0"
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	pool := redis.NewClient(opts)
	defer pool.Close()
	defer asynq.NewInspectorFromRedisClient(pool).DeleteQueue(name, true)
	client := asynq.NewClientFromRedisClient(pool)
	mux := asynq.NewServeMux()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runtime, err := lifecycle.New(lifecycle.Config{StartupTimeout: 5 * time.Second, ShutdownTimeout: 5 * time.Second, CleanupTimeout: time.Second}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	bus := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})
	runtime.OwnClient("events", bus.Close)
	router, err := message.NewRouter(message.RouterConfig{CloseTimeout: time.Second}, watermill.NopLogger{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := messaging.RegisterRunner(router)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := messaging.NewPublisher(bus)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := metrics.New("test", logger)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Own("metrics", metrics.Shutdown)
	if err := metrics.Registerer().Register(asynqmetrics.NewQueueMetricsCollector(asynq.NewInspectorFromRedisClient(pool))); err != nil {
		t.Fatal(err)
	}
	observe, err := asynqmiddleware.ObserveWorker(mux, tracenoop.NewTracerProvider(), metrics.MeterProvider, logger)
	if err != nil {
		t.Fatal(err)
	}
	mux.Use(observe)
	worker := newWorker(t, pool, mux, asynq.Config{Concurrency: 2, Queues: map[string]int{name: 1}, ShutdownTimeout: 2 * time.Second})
	handled, release, eventReceived := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	mux.HandleFunc("test.lifecycle", func(ctx context.Context, _ *asynq.Task) error {
		if err := publisher.Publish(ctx, "test.local", nil); err != nil {
			return err
		}
		close(handled)
		<-release
		return nil
	})
	router.AddConsumerHandler("test.local", "test.local", bus, func(*message.Message) error { close(eventReceived); return nil })
	info, err := client.EnqueueContext(t.Context(), asynq.NewTask("test.lifecycle", []byte(`{}`)), asynq.Queue(cfg.Queue.Default), asynq.Retention(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runtime.Run(ctx, lifecycle.Process{Name: "queue", Runner: lifecycle.AfterReady(worker, events.Ready()), Phase: 0}, lifecycle.Process{Name: "events", Runner: events, Phase: 1})
	}()
	for _, signal := range []<-chan struct{}{runtime.Ready(), handled, eventReceived} {
		select {
		case <-signal:
		case err := <-done:
			t.Fatal("early exit", err)
		case <-time.After(10 * time.Second):
			t.Fatal("startup/delivery timeout")
		}
	}
	scrape := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(scrape, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(scrape.Body.String(), "asynq_queue_size") || strings.Contains(scrape.Body.String(), info.ID) {
		t.Fatal("missing backlog metric or unbounded label")
	}
	cancel()
	<-runtime.Stopping()
	if err := runtime.Close(context.Background()); !errors.Is(err, lifecycle.ErrRunning) {
		t.Fatal("closed dependencies during drain", err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown timeout")
	}
	scrape = httptest.NewRecorder()
	metrics.Handler().ServeHTTP(scrape, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(scrape.Body.String(), "queue_task_duration_seconds_count") {
		t.Fatal("missing task attempt metric", scrape.Body.String())
	}
}

func TestRedisCommandTaskResultAndAdministration(t *testing.T) {
	url := os.Getenv("QUEUE_TEST_REDIS")
	if url == "" {
		t.Skip("set QUEUE_TEST_REDIS to disposable Redis")
	}
	cfg := config.Defaults()
	cfg.Queue.RedisURL = url
	cfg.Queue.Default = "test_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	cfg.Queue.Queues = map[string]config.QueueOptions{cfg.Queue.Default: {Priority: 1}}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	pool := redis.NewClient(opts)
	defer pool.Close()
	inspector := asynq.NewInspectorFromRedisClient(pool)
	defer inspector.DeleteQueue(cfg.Queue.Default, true)
	client := asynq.NewClientFromRedisClient(pool)
	mux := asynq.NewServeMux()
	commands := &cli.ScheduledCommands{}
	mux.Handle(cli.CommandTask, commands)
	if err := commands.Register("probe.run", func() *cobra.Command {
		return &cobra.Command{Use: "run", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Probe completed.")
			return err
		}}
	}); err != nil {
		t.Fatal(err)
	}
	worker := newWorker(t, pool, mux, asynq.Config{Concurrency: 2, Queues: cfg.Queue.Priorities(), ShutdownTimeout: time.Second})
	command, err := commands.Task("probe.run")
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.EnqueueContext(t.Context(), command, asynq.Queue(cfg.Queue.Default), asynq.Retention(time.Minute), asynq.MaxRetry(0))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		current, err := inspector.GetTaskInfo(cfg.Queue.Default, info.ID)
		if err == nil && current.State == asynq.TaskStateCompleted {
			if string(current.Result) != "Probe completed.\n" {
				t.Fatalf("unexpected command output %q", current.Result)
			}
			break
		}
		select {
		case <-deadline.C:
			t.Fatalf("command did not complete: %v", err)
		case <-tick.C:
		}
	}
	output := new(strings.Builder)
	admin := cli.NewQueueCommand(inspector)
	admin.SetOut(output)
	admin.SetArgs([]string{"inspect", cfg.Queue.Default, info.ID})
	if err := admin.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), info.ID) {
		t.Fatal("CLI did not inspect task")
	}
}

func TestRedisCentralizedTaskPublication(t *testing.T) {
	url := os.Getenv("QUEUE_TEST_REDIS")
	if url == "" {
		t.Skip("set QUEUE_TEST_REDIS to disposable Redis")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	pool := redis.NewClient(opts)
	defer pool.Close()
	inspector := asynq.NewInspectorFromRedisClient(pool)
	name := "test_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	defer inspector.DeleteQueue(name, true)
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	defer tp.Shutdown(t.Context())
	publisher := asynqmiddleware.Chain(asynq.NewClientFromRedisClient(pool).EnqueueContext, asynqmiddleware.ObserveClient(tp), asynqmiddleware.Route(asynqmiddleware.Routing{Default: name, Queues: map[string]int{name: 1}}))
	accepted, err := publisher.EnqueueContext(t.Context(), asynqx.NewTask(tasks.WelcomeType, []byte(`{}`), asynq.MaxRetry(7)))
	if err != nil {
		t.Fatal(err)
	}
	info, err := inspector.GetTaskInfo(name, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.MaxRetry != 7 || info.Queue != name || info.Type != tasks.WelcomeType || string(info.Payload) != `{}` {
		t.Fatal("routing or task contract changed")
	}
	ended := spans.Ended()
	remote := propagation.TraceContext{}.Extract(context.Background(), propagation.MapCarrier(info.Headers))
	if len(ended) != 1 || !trace.SpanContextFromContext(remote).IsValid() || trace.SpanContextFromContext(remote).SpanID() != ended[0].SpanContext().SpanID() {
		t.Fatal("producer trace was not propagated")
	}
}

func newWorker(t *testing.T, pool *redis.Client, mux *asynq.ServeMux, cfg asynq.Config) *asynqx.Worker {
	t.Helper()
	cfg.LogLevel = asynq.ErrorLevel
	worker, err := asynqx.NewWorker(asynq.NewServerFromRedisClient(pool, cfg), mux, pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}
