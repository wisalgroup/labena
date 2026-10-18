// Package redis constructs Redis clients. It does not impose a cache API or
// own application-specific keys. The caller owns each returned client.
package redis

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// Open supports standalone, Sentinel, and Cluster through native options.
// Options and addresses are copied; callback-owned state must be concurrency safe.
// A zero timeout uses five seconds. Driver errors are omitted to protect secrets.
func Open(ctx context.Context, options redis.UniversalOptions, timeout time.Duration) (redis.UniversalClient, error) {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if timeout < 0 || len(options.Addrs) == 0 {
		return nil, errors.New("redis: addresses and a positive connection timeout are required")
	}
	for _, addr := range options.Addrs {
		if addr == "" {
			return nil, errors.New("redis: empty address")
		}
	}
	options.Addrs = slices.Clone(options.Addrs)
	if options.TLSConfig != nil {
		options.TLSConfig = options.TLSConfig.Clone()
	}
	// Make command I/O respect deadlines, including the initial connectivity check.
	options.ContextTimeoutEnabled = true
	client := redis.NewUniversalClient(&options)
	if err := check(ctx, client, timeout); err != nil {
		return nil, err
	}
	return client, nil
}

// OpenURL opens a standalone Redis client using the native URL parser, including
// redis, rediss and unix URLs. The caller owns Close. Zero timeout uses five seconds.
func OpenURL(ctx context.Context, url string, timeout time.Duration) (*redis.Client, error) {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if timeout < 0 {
		return nil, errors.New("redis: connection timeout must be positive")
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		return nil, errors.New("redis: invalid URL")
	}
	options.ContextTimeoutEnabled = true
	client := redis.NewClient(options)
	if err := check(ctx, client, timeout); err != nil {
		return nil, err
	}
	return client, nil
}

func check(ctx context.Context, client redis.UniversalClient, timeout time.Duration) error {
	connect, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := client.Ping(connect).Err(); err != nil {
		_ = client.Close()
		if connect.Err() != nil {
			return fmt.Errorf("redis: connect: %w", connect.Err())
		}
		return errors.New("redis: connection check failed")
	}
	return nil
}
