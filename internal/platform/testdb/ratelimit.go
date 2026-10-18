package testdb

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/wisalgroup/labena/pkg/ratelimit"
)

// RateLimiter uses a disposable Redis backend and a unique namespace per fixture.
// Keys expire automatically; never flush a database shared by parallel tests.
func RateLimiter(t testing.TB) ratelimit.Limiter {
	t.Helper()
	url := os.Getenv("RATE_LIMIT_TEST_REDIS")
	if url == "" {
		t.Skip("RATE_LIMIT_TEST_REDIS not set")
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	options.ContextTimeoutEnabled = true
	options.DialTimeout = time.Second
	options.ReadTimeout = time.Second
	options.WriteTimeout = time.Second
	client := redis.NewClient(options)
	t.Cleanup(func() { client.Close() })
	limiter, err := ratelimit.NewRedis(client, "test:"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}
