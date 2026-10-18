// Package migrate combines registered SQL sources and runs Goose explicitly.
// Application SQL belongs to shared and module migration directories.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"

	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/logging"
)

// NewProvider runs a flat SQL history using the caller-owned Bun DB's dialect.
// Pass Registry.FS's result for registered sources or os.DirFS for a local history.
// A session lock serializes migration commands. Use direct/session-pooled connections.
func NewProvider(ctx context.Context, db *bun.DB, history fs.FS) (*goose.Provider, error) {
	return newProvider(ctx, db, history, true)
}

func newProvider(ctx context.Context, db *bun.DB, history fs.FS, locking bool) (*goose.Provider, error) {
	if db == nil || db.DB == nil {
		return nil, errors.New("migrations: nil database")
	}
	var driver goose.Dialect
	var locker lock.SessionLocker
	switch db.Dialect().Name() {
	case dialect.PG:
		driver = goose.DialectPostgres
		var err error
		locker, err = lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 300), lock.WithUnlockTimeout(1, 5))
		if err != nil {
			return nil, err
		}
	case dialect.MySQL:
		driver, locker = goose.DialectMySQL, mysqlLocker{}
	default:
		return nil, errors.New("migrations: PostgreSQL or MySQL database required")
	}
	if history == nil {
		return nil, errors.New("migrations: history is required")
	}
	if _, _, err := readHistory(history, database.Driver(driver)); err != nil {
		return nil, err
	}
	opts := []goose.ProviderOption{goose.WithDisableGlobalRegistry(true), goose.WithVerbose(true), goose.WithSlog(slog.New(progressHandler{logging.FromContext(ctx).Handler()}))}
	if locking {
		opts = append(opts, goose.WithSessionLocker(locker))
	}
	provider, err := goose.NewProvider(driver, db.DB, history, opts...)
	if errors.Is(err, goose.ErrNoMigrations) {
		return nil, fmt.Errorf("migrations: publish tables with a :tables console command or add an application migration, then rebuild: %w", err)
	}
	return provider, err
}

// Goose verbose progress includes SQL statements. Keep useful migration metadata
// while excluding statement bodies from the application's default logging path.
type progressHandler struct{ slog.Handler }

func (h progressHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "executing statement" {
		return nil
	}
	clean := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key != "statement" {
			clean.AddAttrs(attr)
		}
		return true
	})
	return h.Handler.Handle(ctx, clean)
}
func (h progressHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return progressHandler{h.Handler.WithAttrs(attrs)}
}
func (h progressHandler) WithGroup(name string) slog.Handler {
	return progressHandler{h.Handler.WithGroup(name)}
}
