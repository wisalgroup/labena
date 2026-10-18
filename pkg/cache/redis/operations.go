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

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := cache.ValidateKey(key); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	count, err := s.client.Exists(ctx, s.prefix+key).Result()
	if err != nil {
		return false, fmt.Errorf("cache/redis: exists: %w", err)
	}
	return count != 0, nil
}

// Clear scans only this namespace and unlinks keys in batches. It never flushes
// the database. All cluster primaries are scanned. Concurrent writes/topology
// changes may survive; quiesce writers for a complete clear. Errors can leave
// partial progress. The operation has one deadline for the entire traversal.
func (s *Store) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	// Namespace validation allows glob characters; escape them before MATCH.
	pattern := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`).Replace(s.prefix) + "*"
	scan := func(ctx context.Context, client *redis.Client) error {
		return s.clearKeys(ctx, client, pattern)
	}
	var err error
	if cluster, ok := s.client.(*redis.ClusterClient); ok {
		err = cluster.ForEachMaster(ctx, scan)
	} else {
		err = s.clearKeys(ctx, s.client, pattern)
	}
	if err != nil {
		return fmt.Errorf("cache/redis: clear: %w", err)
	}
	return nil
}

func (s *Store) clearKeys(ctx context.Context, client redis.Cmdable, pattern string) error {
	var cursor uint64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		keys, next, err := client.Scan(ctx, cursor, pattern, 256).Result()
		if err != nil {
			return err
		}
		// SCAN COUNT is a hint, so also bound each unlink pipeline explicitly.
		for start := 0; start < len(keys); start += 256 {
			end := min(start+256, len(keys))
			_, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
				for _, key := range keys[start:end] {
					if strings.HasPrefix(key, s.prefix) {
						pipe.Unlink(ctx, key)
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func (s *Store) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cache.ValidateKeys(keys); err != nil {
		return nil, err
	}
	result := make(map[string][]byte, len(keys))
	if len(keys) == 0 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	// Bound replies before Redis serializes them. Each pipeline returns at most
	// MaxBatchBytes of value data, independent of externally written values.
	entryLimit := min(s.cfg.MaxEntryBytes, cache.MaxBatchBytes)
	chunkSize := min(64, max(1, cache.MaxBatchBytes/entryLimit))
	unique := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !seen[key] {
			unique = append(unique, key)
			seen[key] = true
		}
	}
	total := 0
	for start := 0; start < len(unique); start += chunkSize {
		batch := unique[start:min(start+chunkSize, len(unique))]
		commands := make([]*redis.Cmd, len(batch))
		_, pipelineErr := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
			for i, key := range batch {
				limit := max(0, min(entryLimit-len(key), cache.MaxBatchBytes-total-len(key)))
				commands[i] = pipe.Eval(ctx, boundedGet, []string{s.prefix + key}, limit)
			}
			return nil
		})
		// Inspect every result: a missing first key must not hide a later failure.
		for i, command := range commands {
			value, err := command.Text()
			if errors.Is(err, redis.Nil) {
				continue
			}
			if tooLarge(err) {
				return nil, cache.ErrTooLarge
			}
			if err != nil {
				return nil, fmt.Errorf("cache/redis: get multi: %w", err)
			}
			key := batch[i]
			total += len(key) + len(value)
			if total > cache.MaxBatchBytes {
				return nil, cache.ErrTooLarge
			}
			result[key] = []byte(value)
		}
		if pipelineErr != nil && !errors.Is(pipelineErr, redis.Nil) {
			return nil, fmt.Errorf("cache/redis: get multi: %w", pipelineErr)
		}
	}
	return result, nil
}

func (s *Store) SetMulti(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	keys, err := cache.ValidateItems(ctx, items, ttl, s.cfg.MaxEntryBytes)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	millis := ttl.Milliseconds()
	if ttl%time.Millisecond != 0 {
		millis++
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	// Single-key commands work across cluster slots; each value/TTL pair is
	// atomic, but the pipeline as a whole is not a transaction.
	_, err = s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, key := range keys {
			pipe.Do(ctx, "SET", s.prefix+key, items[key], "PX", millis)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("cache/redis: set multi: %w", err)
	}
	return nil
}
