// Package memory provides a process-local byte cache with synchronous writes.
package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/wisalgroup/labena/pkg/cache"
)

// DefaultMaxBytes bounds retained key/value bytes, not total process memory.
const DefaultMaxBytes int64 = 64 << 20

// Config limits retained entries and key/value bytes. MaxBytes defaults to
// DefaultMaxBytes when zero. Capacity and MaxEntryBytes must be positive.
// Map metadata, allocator overhead and caller-owned Get results are additional.
type Config struct {
	Capacity      int
	MaxEntryBytes int
	MaxBytes      int64
}

// Validate checks limits without allocating a cache. A zero MaxBytes uses the default.
func (c Config) Validate() error {
	if c.Capacity < 1 || c.MaxEntryBytes < 1 || c.MaxBytes < 0 {
		return errors.New("cache: limits must be positive")
	}
	return nil
}

type entry struct {
	value   []byte // Immutable after publication; readers may copy outside the lock.
	expires time.Time
}

// Store supports concurrent use. Its zero value is not usable; use New.
// It rejects writes with ErrFull instead of evicting live entries. Constructors
// start no goroutines. Expiration is lazy; PruneExpired allows explicit cleanup.
type Store struct {
	mu                      sync.RWMutex
	entries                 map[string]entry
	capacity, maxEntryBytes int
	maxBytes, usedBytes     int64
	// A conservative lower bound on the earliest expiry. Deletes and refreshes
	// may leave it stale; a full scan resets it. This avoids scanning a full
	// cache on every rejected write when no entry can have expired.
	nextExpiry time.Time
	closed     bool
}

// New creates a cache with a 64 MiB key/value budget. Use NewWithConfig to
// override that budget. Each entry includes its key in maxEntryBytes.
func New(capacity, maxEntryBytes int) (*Store, error) {
	return NewWithConfig(Config{Capacity: capacity, MaxEntryBytes: maxEntryBytes})
}

// NewWithConfig constructs an empty cache without preallocating its byte budget.
func NewWithConfig(cfg Config) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	return &Store{entries: make(map[string]entry), capacity: cfg.Capacity, maxEntryBytes: cfg.MaxEntryBytes, maxBytes: cfg.MaxBytes}, nil
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return s.GetInto(ctx, key, nil)
}

// GetInto appends a hit to dst, reusing its capacity when possible. The returned
// slice belongs to the caller; the cache never retains dst. On a miss or error,
// dst is returned unchanged. Pass dst[:0] to reuse a buffer between reads.
// This optional memory-specific API avoids allocating a result on every hit.
func (s *Store) GetInto(ctx context.Context, key string, dst []byte) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return dst, false, err
	}
	if err := cache.ValidateKey(key); err != nil {
		return dst, false, err
	}
	s.mu.RLock()
	if err := ctx.Err(); err != nil {
		s.mu.RUnlock()
		return dst, false, err
	}
	if s.closed {
		s.mu.RUnlock()
		return dst, false, cache.ErrClosed
	}
	item, ok := s.entries[key]
	expired := ok && !item.expires.After(time.Now())
	s.mu.RUnlock()
	if !ok {
		return dst, false, nil
	}
	if expired {
		s.mu.Lock()
		// A concurrent Set may have refreshed the value since our read.
		if current, exists := s.entries[key]; exists && !current.expires.After(time.Now()) {
			s.remove(key, current)
		}
		s.mu.Unlock()
		return dst, false, nil
	}
	return append(dst, item.value...), true, nil
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
	// Subtraction avoids overflowing int for oversized input.
	if len(key) > s.maxEntryBytes || len(value) > s.maxEntryBytes-len(key) {
		return cache.ErrTooLarge
	}
	size := int64(len(key)) + int64(len(value))
	if size > s.maxBytes {
		return cache.ErrTooLarge
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return cache.ErrClosed
	}
	old, exists := s.entries[key]
	oldSize := int64(0)
	if exists {
		oldSize = int64(len(key)) + int64(len(old.value))
	}
	full := func() bool {
		return (!exists && len(s.entries) >= s.capacity) || size > s.maxBytes-(s.usedBytes-oldSize)
	}
	if full() && !s.nextExpiry.IsZero() && !s.nextExpiry.After(time.Now()) {
		if _, err := s.prune(ctx, s.capacity); err != nil {
			return err
		}
		old, exists = s.entries[key]
		oldSize = 0
		if exists {
			oldSize = int64(len(key)) + int64(len(old.value))
		}
	}
	if full() {
		return cache.ErrFull
	}
	// Own keys too: a short substring must not retain a large caller allocation.
	// Map assignment can replace the stored key header even for an equal key.
	key = strings.Clone(key)
	expires := time.Now().Add(ttl)
	s.entries[key] = entry{value: append([]byte(nil), value...), expires: expires}
	s.usedBytes += size - oldSize
	if s.nextExpiry.IsZero() || expires.Before(s.nextExpiry) {
		s.nextExpiry = expires
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return cache.ErrClosed
	}
	if item, ok := s.entries[key]; ok {
		s.remove(key, item)
	}
	return nil
}

func (s *Store) remove(key string, item entry) {
	delete(s.entries, key)
	s.usedBytes -= int64(len(key)) + int64(len(item.value))
}

// PruneExpired removes at most limit expired entries (1–10000). It may inspect
// all entries under the write lock; schedule modest batches outside hot paths.
// Cancellation can return both a positive removal count and an error. No live
// entry is evicted. The caller owns any scheduling and shutdown of maintenance.
func (s *Store) PruneExpired(ctx context.Context, limit int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if limit < 1 || limit > 10000 {
		return 0, errors.New("cache: prune limit must be between 1 and 10000")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s.closed {
		return 0, cache.ErrClosed
	}
	return s.prune(ctx, limit)
}

func (s *Store) prune(ctx context.Context, limit int) (int, error) {
	now := time.Now()
	if s.nextExpiry.IsZero() || s.nextExpiry.After(now) {
		return 0, nil
	}
	removed := 0
	var next time.Time
	for key, item := range s.entries {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if !item.expires.After(now) {
			s.remove(key, item)
			removed++
			if removed == limit {
				return removed, nil
			}
		} else if next.IsZero() || item.expires.Before(next) {
			next = item.expires
		}
	}
	s.nextExpiry = next
	return removed, nil
}

// Usage is a point-in-time snapshot. Entries and Bytes include expired entries
// awaiting cleanup. Bytes counts keys and values only, not heap or RSS usage.
type Usage struct {
	Entries  int
	Bytes    int64
	Capacity int
	MaxBytes int64
	Closed   bool
}

// Usage reports retained logical storage without scanning entries.
func (s *Store) Usage() Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Usage{Entries: len(s.entries), Bytes: s.usedBytes, Capacity: s.capacity, MaxBytes: s.maxBytes, Closed: s.closed}
}

// Close releases all retained storage and is idempotent. It does not cancel
// in-flight reads; stop users before closing if their completion must be joined.
func (s *Store) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.entries = nil
	s.usedBytes = 0
	s.nextExpiry = time.Time{}
	return nil
}

// Remember returns a cached JSON value or loads it on a miss. See cache.Remember
// for validation, error, expiry and concurrent-loading semantics.
func (s *Store) Remember(ctx context.Context, key string, ttl time.Duration, load func(context.Context) (any, error)) (any, error) {
	return cache.Remember(ctx, s, key, ttl, load)
}
