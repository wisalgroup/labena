// Package logging provides execution-scoped structured logging.
package logging

import (
	"context"
	"log/slog"
)

type contextKey struct{}

// WithLogger attaches a request/job logger, not a dependency container.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		panic("logging: nil logger")
	}
	return context.WithValue(ctx, contextKey{}, logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
