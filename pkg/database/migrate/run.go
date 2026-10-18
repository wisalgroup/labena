package migrate

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/logging"
)

// Run opens a dedicated database migration session, executes an explicit Goose
// action, and closes the pool. It never starts application services.
func Run(ctx context.Context, cfg database.Config, history fs.FS, action string, timeout time.Duration) (err error) {
	if action != "up" && action != "down" && action != "status" && action != "version" && action != "refresh" {
		return errors.New("migrations: unknown action")
	}
	if timeout <= 0 {
		return errors.New("migrations: timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if cfg.Driver != "" && cfg.Driver != database.DriverPostgres && cfg.Driver != database.DriverMySQL {
		return errors.New("migrations: PostgreSQL or MySQL driver required")
	}
	cfg.Name = "migrations"
	if err := cfg.Validate(); err != nil {
		return err
	}
	// Ordinary migration work uses one session. Refresh reserves a second
	// session to hold the advisory lock across reset and Goose replay.
	cfg.MaxOpenConns = 1
	cfg.MaxIdleConns = 1
	if action == "refresh" {
		cfg.MaxOpenConns = 2
		cfg.MaxIdleConns = 2
	}
	db, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	return RunDB(ctx, db, history, action, timeout)
}

// RunDB runs migrations through a caller-owned database pool. Goose holds the
// advisory lock and migration work on its dedicated session; the pool stays open.
// Refresh requires at least two available connections: one holds the lock while
// the other replays migrations. Prefer Run for a dedicated refresh pool.
func RunDB(ctx context.Context, db *bun.DB, history fs.FS, action string, timeout time.Duration) (err error) {
	if db == nil {
		return errors.New("migrations: nil database")
	}
	if action != "up" && action != "down" && action != "status" && action != "version" && action != "refresh" {
		return errors.New("migrations: unknown action")
	}
	if timeout <= 0 {
		return errors.New("migrations: timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if action == "refresh" {
		return refresh(ctx, db, history)
	}
	provider, err := NewProvider(ctx, db, history)
	if err != nil {
		return err
	}
	logger := logging.FromContext(ctx)
	switch action {
	case "up":
		_, err = provider.Up(ctx)
	case "down":
		_, err = provider.Down(ctx)
	case "status":
		statuses, statusErr := provider.Status(ctx)
		if statusErr != nil {
			return statusErr
		}
		for _, status := range statuses {
			logger.InfoContext(ctx, "migration status", "version", status.Source.Version, "path", status.Source.Path, "state", status.State)
		}
	case "version":
		version, versionErr := provider.GetDBVersion(ctx)
		if versionErr != nil {
			return versionErr
		}
		logger.InfoContext(ctx, "database version", "version", version)
	}
	return err
}
