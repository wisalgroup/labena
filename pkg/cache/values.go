package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"
)

const (
	// MaxTTL is the largest positive time.Duration (approximately 292 years).
	// It is finite. Stores require a positive TTL; no-expiry entries are unsupported.
	MaxTTL time.Duration = 1<<63 - 1
	// MaxBatchKeys and MaxBatchBytes bound convenience-operation inputs/results.
	MaxBatchKeys  = 1000
	MaxBatchBytes = 8 << 20
)

// Remember returns a typed JSON value on a hit, or loads, encodes and caches it on a
// miss. It never loads on a cache error. Loader/encoding/write errors propagate;
// failures are not cached. Both hit and miss results are JSON-decoded into T.
// T must round-trip through JSON. For T=any, numbers remain json.Number.
// A type/encoding change requires a new key or namespace version.
// The TTL starts at Set, after loading. Concurrent misses may each call load,
// and a fill racing Delete/Clear may repopulate the key. Use versioned keys when
// that race matters. The caller owns the loader and must join its work.
func Remember[T any](ctx context.Context, store Store, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := ValidateKey(key); err != nil {
		return zero, err
	}
	if ttl <= 0 {
		return zero, ErrInvalidTTL
	}
	if load == nil {
		return zero, errors.New("cache: nil loader")
	}
	value, found, err := store.Get(ctx, key)
	if err != nil {
		return zero, err
	}
	if found {
		return decode[T](value)
	}
	result, err := load(ctx)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return zero, fmt.Errorf("cache: encode value: %w", ErrInvalidValue)
	}
	// Decode before publishing so miss and hit paths return identical JSON types.
	decoded, err := decode[T](encoded)
	if err != nil {
		return zero, err
	}
	if err := store.Set(ctx, key, encoded, ttl); err != nil {
		return zero, err
	}
	return decoded, nil
}

// DecodeValue decodes one complete JSON value. Objects are map[string]any,
// arrays are []any, and numbers are json.Number to preserve integer precision.
// Null is nil and is distinct from a cache miss. The result is caller-owned.
// Invalid raw bytes are errors; they are not silently treated as misses.
func DecodeValue(value []byte) (any, error) { return decode[any](value) }

func decode[T any](value []byte) (T, error) {
	var zero T
	if len(value) > MaxBatchBytes {
		return zero, ErrTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var result T
	if err := decoder.Decode(&result); err != nil {
		return zero, ErrInvalidValue
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return zero, ErrInvalidValue
	}
	return result, nil
}

// ValidateKeys validates a bounded batch before any backend operation.
func ValidateKeys(keys []string) error {
	if len(keys) > MaxBatchKeys {
		return ErrTooLarge
	}
	for _, key := range keys {
		if err := ValidateKey(key); err != nil {
			return err
		}
	}
	return nil
}

// ValidateItems checks raw batch bounds before a driver writes anything.
// It returns sorted keys for deterministic admission. Size limits include keys.
// Callers must not mutate items or their byte slices during the operation.
func ValidateItems(ctx context.Context, items map[string][]byte, ttl time.Duration, maxEntryBytes int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ttl <= 0 {
		return nil, ErrInvalidTTL
	}
	if len(items) > MaxBatchKeys {
		return nil, ErrTooLarge
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if err := ValidateKeys(keys); err != nil {
		return nil, err
	}
	total := 0
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value := items[key]
		if len(key) > maxEntryBytes || len(value) > maxEntryBytes-len(key) || len(value) > MaxBatchBytes-total-len(key) {
			return nil, ErrTooLarge
		}
		total += len(key) + len(value)
	}
	return keys, nil
}

// GetMulti decodes JSON hits into T using one native batch read. Missing keys are
// omitted; malformed or incompatible values fail the entire result, not the cache.
// For T=any, numeric values use json.Number. Results belong to the caller.
func GetMulti[T any](ctx context.Context, store Store, keys []string) (map[string]T, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := store.GetMulti(ctx, keys)
	if err != nil {
		return nil, err
	}
	result := make(map[string]T, len(values))
	for key, value := range values {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		decoded, err := decode[T](value)
		if err != nil {
			return nil, err
		}
		result[key] = decoded
	}
	return result, nil
}

// SetMulti encodes the entire JSON batch before passing it to the native store.
// Invalid input writes nothing. Backend failures can leave partial writes.
// Callers must not mutate items while this synchronous operation is running.
func SetMulti[T any](ctx context.Context, store Store, items map[string]T, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl <= 0 {
		return ErrInvalidTTL
	}
	if len(items) > MaxBatchKeys {
		return ErrTooLarge
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if err := ValidateKeys(keys); err != nil {
		return err
	}
	encoded := make(map[string][]byte, len(items))
	total := 0
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		value, err := json.Marshal(items[key])
		if err != nil {
			return ErrInvalidValue
		}
		if len(value) > MaxBatchBytes-total-len(key) {
			return ErrTooLarge
		}
		total += len(key) + len(value)
		encoded[key] = value
	}
	return store.SetMulti(ctx, encoded, ttl)
}
