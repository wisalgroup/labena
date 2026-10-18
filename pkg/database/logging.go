package database

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
	"github.com/wisalgroup/labena/pkg/logging"
)

// queryLogger resolves the logger for each execution, preserving request/job
// attributes. It never logs SQL, arguments, DSNs, or raw server error messages.
type queryLogger struct {
	name string
	slow time.Duration
}

func (h queryLogger) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context { return ctx }
func (h queryLogger) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	operation := queryOperation(event)
	elapsed := time.Since(event.StartTime)
	level := slog.LevelDebug
	attributes := []any{"database", h.name, "operation", operation, "duration", elapsed}
	if event.Err != nil && !errors.Is(event.Err, sql.ErrNoRows) {
		level = slog.LevelError
		kind := "driver"
		if errors.Is(event.Err, context.Canceled) {
			kind = "canceled"
			level = slog.LevelDebug
		}
		if errors.Is(event.Err, context.DeadlineExceeded) {
			kind = "deadline"
			level = slog.LevelWarn
		}
		attributes = append(attributes, "error_kind", kind)
		var state interface{ SQLState() string }
		if errors.As(event.Err, &state) {
			attributes = append(attributes, "sql_state", state.SQLState())
		}
	} else if elapsed >= h.slow {
		level = slog.LevelWarn
	}
	logging.FromContext(ctx).Log(ctx, level, "database query", attributes...)
}
