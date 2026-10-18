// Package redis caches bytes using native Redis expiration. It borrows an existing
// client from pkg/redis and supports standalone, Sentinel and Cluster clients.
package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"

	"github.com/wisalgroup/labena/pkg/cache"
)

type Config struct {
	Namespace        string        // default default; keys use cache:<namespace>:<key>
	MaxEntryBytes    int           // key + value bytes on Set; default 1 MiB
	OperationTimeout time.Duration // default 5 seconds
}

// withDefaults resolves zero settings without changing the caller's value.
func (c Config) withDefaults() Config {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
	if c.MaxEntryBytes == 0 {
		c.MaxEntryBytes = 1 << 20
	}
	if c.OperationTimeout == 0 {
		c.OperationTimeout = 5 * time.Second
	}
	return c
}

// Validate checks settings without I/O. Zero fields use the documented defaults.
func (c Config) Validate() error {
	c = c.withDefaults()

	if cache.ValidateKey(c.Namespace) != nil || strings.Contains(c.Namespace, ":") || c.MaxEntryBytes < 1 || c.OperationTimeout <= 0 {
		return errors.New("cache/redis: invalid namespace, size limit or timeout")
	}
	return nil
}

// Store is safe for concurrent use. The caller owns the client and closes it after
// all users stop. Configure context-aware I/O using pkg/redis.Open or OpenURL.
type Store struct {
	client redis.UniversalClient
	cfg    Config
	prefix string
}

func New(client redis.UniversalClient, cfg Config) (*Store, error) {
	if client == nil {
		return nil, errors.New("cache/redis: nil client")
	}
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Store{client: client, cfg: cfg, prefix: "cache:" + cfg.Namespace + ":"}, nil
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := cache.ValidateKey(key); err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	value, err := s.client.Eval(ctx, boundedGet, []string{s.prefix + key}, max(0, s.cfg.MaxEntryBytes-len(key))).Text()
	if tooLarge(err) {
		return nil, false, cache.ErrTooLarge
	}
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache/redis: get: %w", err)
	}
	return []byte(value), true, nil
}

func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cache.ValidateKey(key); err != nil {
		return err
	}
	if ttl <= 0 {
		return cache.ErrInvalidTTL
	}
	if len(value) > s.cfg.MaxEntryBytes-len(key) {
		return cache.ErrTooLarge
	}
	millis := ttl.Milliseconds()
	if ttl%time.Millisecond != 0 {
		millis++
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	// One command stores value and expiry together. Integer PX avoids truncating
	// sub-millisecond TTLs or overflowing time.Duration when rounding up.
	if err := s.client.Do(ctx, "SET", s.prefix+key, value, "PX", millis).Err(); err != nil {
		return fmt.Errorf("cache/redis: set: %w", err)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cache.ValidateKey(key); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	if err := s.client.Del(ctx, s.prefix+key).Err(); err != nil {
		return fmt.Errorf("cache/redis: delete: %w", err)
	}
	return nil
}

var _ cache.Store = (*Store)(nil)

// Remember returns a cached JSON value or loads it on a miss. See cache.Remember
// for validation, error, expiry and concurrent-loading semantics.
func (s *Store) Remember(ctx context.Context, key string, ttl time.Duration, load func(context.Context) (any, error)) (any, error) {
	return cache.Remember(ctx, s, key, ttl, load)
}

// The size check and read are one Redis operation, so concurrent writers cannot
// replace a checked value with an oversized one before it is returned.
const boundedGet = `local size = redis.call('STRLEN', KEYS[1])
if size > tonumber(ARGV[1]) then return redis.error_reply('CACHE_VALUE_TOO_LARGE') end
return redis.call('GET', KEYS[1])`

func tooLarge(err error) bool {
	return err != nil && (err.Error() == "CACHE_VALUE_TOO_LARGE" || err.Error() == "ERR CACHE_VALUE_TOO_LARGE")
}
