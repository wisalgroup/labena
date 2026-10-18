package ratelimit

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

// Redis uses redis_rate's atomic GCRA algorithm and automatic expiry. The client
// is borrowed. Namespace isolates budgets from other apps and ordinary cache keys.
type Redis struct {
	limiter *redis_rate.Limiter
	prefix  string
}

func NewRedis(client redis.UniversalClient, namespace string) (*Redis, error) {
	if client == nil || strings.TrimSpace(namespace) == "" {
		return nil, errors.New("ratelimit: client and namespace are required")
	}
	return &Redis{limiter: redis_rate.NewLimiter(client), prefix: namespace + ":"}, nil
}
func (r *Redis) Allow(ctx context.Context, key string, limit Limit) (Decision, error) {
	if err := limit.Validate(); err != nil {
		return Decision{}, err
	}
	if len(key) == 0 || len(key) > 512 {
		return Decision{}, errors.New("ratelimit: invalid key")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result, err := r.limiter.Allow(ctx, r.prefix+key, redis_rate.Limit{Rate: limit.Rate, Burst: limit.Burst, Period: limit.Period})
	if err != nil {
		if ctx.Err() != nil {
			return Decision{}, ctx.Err()
		}
		return Decision{}, errors.New("ratelimit: backend unavailable")
	}
	return Decision{Allowed: result.Allowed > 0, RetryAfter: result.RetryAfter}, nil
}
