package memory

import (
	"context"
	"time"

	"github.com/wisalgroup/labena/pkg/cache"
)

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := cache.ValidateKey(key); err != nil {
		return false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.closed {
		return false, cache.ErrClosed
	}
	item, found := s.entries[key]
	return found && item.expires.After(time.Now()), nil
}

// Clear releases retained entries and keeps the store usable. It is atomic
// relative to memory-store writes; a later write may immediately refill it.
func (s *Store) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
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
	s.entries = make(map[string]entry)
	s.usedBytes = 0
	s.nextExpiry = time.Time{}
	return nil
}

func (s *Store) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cache.ValidateKeys(keys); err != nil {
		return nil, err
	}
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return nil, cache.ErrClosed
	}
	result := make(map[string][]byte, len(keys))
	total := 0
	for _, key := range keys {
		if _, duplicate := result[key]; duplicate {
			continue
		}
		value, found, err := s.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		total += len(key) + len(value)
		if total > cache.MaxBatchBytes {
			return nil, cache.ErrTooLarge
		}
		result[key] = value
	}
	return result, nil
}

func (s *Store) SetMulti(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	maxEntry := s.maxEntryBytes
	if int64(maxEntry) > s.maxBytes {
		maxEntry = int(s.maxBytes)
	}
	keys, err := cache.ValidateItems(ctx, items, ttl, maxEntry)
	if err != nil {
		return err
	}
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return cache.ErrClosed
	}
	for _, key := range keys {
		if err := s.Set(ctx, key, items[key], ttl); err != nil {
			return err
		}
	}
	return nil
}

var _ cache.Store = (*Store)(nil)
