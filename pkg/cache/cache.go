// Package cache defines an optional cache with raw bytes and JSON convenience
// operations. Misses are not errors.
package cache

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// Store is safe for concurrent use. Keys contain 1–255 UTF-8 bytes without NUL.
// Get returns caller-owned bytes; found distinguishes empty values from misses.
// Set copies retained data and atomically replaces the value and its expiry.
// TTL must be positive; remote drivers round it up to whole milliseconds.
// Delete succeeds for missing keys. Backend errors are never treated as misses.
// Connections belong to the caller; Store does not expose connection lifecycle.
type Store interface {
	// Remember loads and caches a JSON value on a miss, with the same semantics
	// as Remember[any]. Objects become map[string]any and numbers json.Number.
	// Use the package-level Remember[T] helper when a typed result is needed.
	Remember(context.Context, string, time.Duration, func(context.Context) (any, error)) (any, error)
	Get(context.Context, string) ([]byte, bool, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
	// Exists checks presence without loading/decoding the value; expired is false.
	Exists(context.Context, string) (bool, error)
	// Clear removes this store's entries, never another namespace or connection.
	// Remote clears may be partial on error; concurrent writers may survive.
	Clear(context.Context) error
	// GetMulti returns caller-owned bytes for hits; missing keys are omitted.
	// Empty values remain hits. Duplicate keys collapse into one result.
	// Errors return no result. Reads are not a cross-key snapshot.
	GetMulti(context.Context, []string) (map[string][]byte, error)
	// SetMulti validates the full batch, then writes bytes with the same TTL.
	// Inputs must remain unchanged during the call; retained values are copied.
	// Nil and empty bytes represent an empty value, not deletion. Runtime errors
	// may leave a partial batch; there is no portable all-or-nothing guarantee.
	SetMulti(context.Context, map[string][]byte, time.Duration) error
}

// ValidateKey checks the portable key format shared by all cache drivers.
func ValidateKey(key string) error {
	if len(key) == 0 || len(key) > 255 || !utf8.ValidString(key) || strings.ContainsRune(key, 0) {
		return ErrInvalidKey
	}
	return nil
}
