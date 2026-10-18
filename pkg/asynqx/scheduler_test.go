package asynqx_test

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/wisalgroup/labena/pkg/asynqx"
)

func TestSchedulerDoesNotSignalReadyWithoutRedis(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	pool := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1, DialTimeout: 50 * time.Millisecond, ContextTimeoutEnabled: true})
	defer pool.Close()
	scheduler := asynq.NewSchedulerFromRedisClient(pool, &asynq.SchedulerOpts{LogLevel: asynq.FatalLevel})
	runner, err := asynqx.NewSchedulerRunner(scheduler, pool, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err = runner.Run(t.Context()); err == nil {
		t.Fatal("unavailable startup succeeded")
	}
	select {
	case <-runner.Ready():
		t.Fatal("signaled ready")
	default:
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = runner.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSchedulerReadyAndBorrowedPool(t *testing.T) {
	url := os.Getenv("QUEUE_TEST_REDIS")
	if url == "" {
		t.Skip("QUEUE_TEST_REDIS not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	opts.ContextTimeoutEnabled = true
	pool := redis.NewClient(opts)
	defer pool.Close()
	scheduler := asynq.NewSchedulerFromRedisClient(pool, &asynq.SchedulerOpts{LogLevel: asynq.FatalLevel})
	runner, err := asynqx.NewSchedulerRunner(scheduler, pool, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case <-runner.Ready():
	case err := <-done:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("startup did not finish")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	if err = pool.Ping(t.Context()).Err(); err != nil {
		t.Fatal("borrowed pool closed", err)
	}
}
