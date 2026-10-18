package app_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/wisalgroup/labena/internal/app"
	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/internal/modules/identity"
	"github.com/wisalgroup/labena/internal/platform/cli"
)

// Exercise the public assembly against real infrastructure: workers must execute
// module command factories without scheduling, and schedulers must enqueue without
// consuming tasks. Neither selection may start the registered HTTP transport.
func TestIndependentWorkerAndSchedulerProcesses(t *testing.T) {
	queueURL, limiterURL := os.Getenv("QUEUE_TEST_REDIS"), os.Getenv("RATE_LIMIT_TEST_REDIS")
	if queueURL == "" || limiterURL == "" {
		t.Skip("QUEUE_TEST_REDIS and RATE_LIMIT_TEST_REDIS not set")
	}
	for _, mode := range []struct {
		name      string
		selection app.Processes
	}{
		{"worker", app.Processes{Workers: true}},
		{"scheduler", app.Processes{Scheduler: true}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			cfg, db := databaseConfig(t)
			if err := app.Execute(t.Context(), cfg, app.Processes{}, []string{"migrate", "up"}); err != nil {
				t.Fatal(err)
			}
			cfg.Queue.RedisURL = queueURL
			cfg.Queue.Default = "selection_" + strings.ToLower(rand.Text())
			cfg.Queue.Queues = map[string]config.QueueOptions{cfg.Queue.Default: {Priority: 1}}
			cfg.Auth.Enabled = true
			cfg.Auth.VerificationKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("v", 32)))
			cfg.Auth.AccessTokenKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
			cfg.RateLimit.RedisURL = limiterURL
			occupied, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer occupied.Close()
			cfg.HTTP.Address = occupied.Addr().String()
			opts, err := redis.ParseURL(queueURL)
			if err != nil {
				t.Fatal(err)
			}
			pool := redis.NewClient(opts)
			defer pool.Close()
			inspector := asynq.NewInspectorFromRedisClient(pool)
			defer inspector.DeleteQueue(cfg.Queue.Default, true)
			client := asynq.NewClientFromRedisClient(pool)
			// This is the existing persisted task envelope, also produced by schedules.
			task := asynq.NewTask(cli.CommandTask, []byte(`{"name":"`+identity.PruneSessionsCommand+`"}`))
			queued, err := client.EnqueueContext(t.Context(), task, asynq.Queue(cfg.Queue.Default), asynq.Retention(time.Minute), asynq.MaxRetry(0))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- app.Serve(ctx, cfg, mode.selection) }()
			var once sync.Once
			stop := func() {
				once.Do(func() {
					cancel()
					select {
					case err := <-done:
						if err != nil {
							t.Error(err)
						}
					case <-time.After(10 * time.Second):
						t.Error("selected process did not stop")
					}
				})
			}
			defer stop()
			if mode.selection.Workers {
				waitFor(t, func() bool {
					info, err := inspector.GetTaskInfo(cfg.Queue.Default, queued.ID)
					return err == nil && info.State == asynq.TaskStateCompleted
				})
				entries, err := inspector.SchedulerEntries()
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if scheduleUsesQueue(entry, cfg.Queue.Default) {
						t.Fatal("worker started scheduling")
					}
				}
			} else {
				waitFor(t, func() bool {
					entries, err := inspector.SchedulerEntries()
					if err != nil {
						return false
					}
					count := 0
					for _, entry := range entries {
						if scheduleUsesQueue(entry, cfg.Queue.Default) && entry.Task.Type() == cli.CommandTask {
							count++
						}
					}
					return count == 2
				})
				servers, err := inspector.Servers()
				if err != nil {
					t.Fatal(err)
				}
				for _, server := range servers {
					if server.Queues[cfg.Queue.Default] != 0 {
						t.Fatal("scheduler started a worker")
					}
				}
				info, err := inspector.GetTaskInfo(cfg.Queue.Default, queued.ID)
				if err != nil || info.State != asynq.TaskStatePending {
					t.Fatal("scheduler consumed queued work", err)
				}
			}
			stop()
			assertAppConnectionsClosed(t, db, cfg)
		})
	}
}

func scheduleUsesQueue(entry *asynq.SchedulerEntry, queue string) bool {
	for _, option := range entry.Opts {
		if option.Type() == asynq.QueueOpt && option.Value() == queue {
			return true
		}
	}
	return false
}
