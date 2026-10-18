package cache

import "errors"

// Stable errors allow callers to handle admission failures with errors.Is.
var (
	ErrInvalidValue = errors.New("cache value is not valid JSON")
	ErrClosed       = errors.New("cache is closed")
	ErrFull         = errors.New("cache capacity exhausted")
	ErrTooLarge     = errors.New("cache entry exceeds size limit")
	ErrInvalidTTL   = errors.New("cache TTL must be positive")
	ErrInvalidKey   = errors.New("cache key must contain 1 to 255 UTF-8 bytes without NUL")
)
