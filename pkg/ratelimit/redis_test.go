package ratelimit

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestRedisSharedAdmission(t *testing.T) {
	raw := os.Getenv("RATE_LIMIT_TEST_REDIS")
	if raw == "" {
		t.Skip("RATE_LIMIT_TEST_REDIS required")
	}
	opts, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	opts.ContextTimeoutEnabled = true
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	namespace := "limiter-test:" + uuid.NewString()
	a, err := NewRedis(client, namespace)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewRedis(client, namespace)
	if err != nil {
		t.Fatal(err)
	}
	policy := Limit{Rate: 1, Burst: 10, Period: time.Hour}
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			limiter := a
			if i%2 == 0 {
				limiter = b
			}
			result, err := limiter.Allow(context.Background(), "shared", policy)
			if err != nil {
				t.Error(err)
				return
			}
			if result.Allowed {
				allowed.Add(1)
			} else if result.RetryAfter <= 0 {
				t.Error("missing retry delay")
			}
		}(i)
	}
	wg.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("allowed %d, want 10", allowed.Load())
	}
	keys, err := client.Keys(context.Background(), "*"+namespace+"*").Result()
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys: %v %v", keys, err)
	}
	t.Cleanup(func() { client.Del(context.Background(), keys...) })
	ttl, err := client.TTL(context.Background(), keys[0]).Result()
	if err != nil || ttl <= 0 {
		t.Fatalf("missing expiry: %v %v", ttl, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := a.Allow(ctx, "cancelled", policy); err == nil || result.Allowed {
		t.Fatal("cancelled request admitted")
	}
	if err := client.Del(context.Background(), keys...).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if result, err := a.Allow(context.Background(), "unavailable", policy); err == nil || result.Allowed {
		t.Fatal("backend failure admitted")
	}
}
