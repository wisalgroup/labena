// Package database configures SQL pools, Bun dialects, and transaction scopes.
// It never runs migrations or owns pools returned to callers.
package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/wisalgroup/labena/pkg/logging"
)

// ConnectionError preserves the driver error for errors.Is/As without putting
// DSN or server-supplied connection details in normal error output. Unwrapping
// deliberately exposes the original error; do not log it without redaction.
type ConnectionError struct {
	Op    string
	cause error
}

func (e *ConnectionError) Error() string { return "database: " + e.Op + " failed" }
func (e *ConnectionError) Unwrap() error { return e.cause }

// Open returns a ready-to-use Bun database. The caller owns Close. Its embedded
// DB is the same database/sql pool and can be shared with queues and migrations.
func Open(ctx context.Context, cfg Config) (*bun.DB, error) {
	cfg, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	pool, err := OpenSQL(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var db *bun.DB
	switch cfg.Driver {
	case DriverPostgres:
		db = bun.NewDB(pool, pgdialect.New())
	case DriverMySQL:
		db = bun.NewDB(pool, mysqldialect.New(mysqldialect.WithTimeLocation("UTC")))
	case DriverSQLite:
		db = bun.NewDB(pool, sqlitedialect.New())
	default:
		_ = pool.Close()
		return nil, errors.New("database: unsupported dialect")
	}
	db.AddQueryHook(queryLogger{name: cfg.Name, slow: cfg.SlowQueryThreshold})
	return db, nil
}

// OpenSQL opens the same configured pool without a Bun wrapper, for SQL-only
// consumers such as migration commands. The caller owns Close.
func OpenSQL(ctx context.Context, cfg Config) (*sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	var pool *sql.DB
	switch cfg.Driver {
	case DriverPostgres:
		parsed, err := pgx.ParseConfig(cfg.DSN)
		if err != nil {
			return nil, &ConnectionError{Op: "parse DSN", cause: err}
		}
		parsed.ConnectTimeout = cfg.ConnectTimeout
		// Bun formats its own placeholders. Avoid pgx's prepared-statement cache;
		// use the extended protocol for parameterized database/sql callers as well.
		parsed.DefaultQueryExecMode = pgx.QueryExecModeExec
		pool = stdlib.OpenDB(*parsed)
	case DriverMySQL:
		parsed, err := mysql.ParseDSN(cfg.DSN)
		if err != nil {
			return nil, &ConnectionError{Op: "parse DSN", cause: err}
		}
		parsed.Timeout = cfg.ConnectTimeout
		parsed.ParseTime = true
		parsed.Loc = time.UTC
		if parsed.Params == nil {
			parsed.Params = make(map[string]string)
		}
		parsed.Params["time_zone"] = "'+00:00'"
		connector, err := mysql.NewConnector(parsed)
		if err != nil {
			return nil, &ConnectionError{Op: "configure MySQL", cause: err}
		}
		pool = sql.OpenDB(connector)
	case DriverSQLite:
		pool, err = sql.Open(sqliteshim.ShimName, cfg.DSN)
		if err != nil {
			return nil, &ConnectionError{Op: "open", cause: err}
		}
	}
	pool.SetMaxOpenConns(cfg.MaxOpenConns)
	pool.SetMaxIdleConns(cfg.MaxIdleConns)
	pool.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	pool.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	ping, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := pool.PingContext(ping); err != nil {
		cause := errors.Join(err, ping.Err(), pool.Close())
		return nil, &ConnectionError{Op: "ping", cause: cause}
	}
	logging.FromContext(ctx).DebugContext(ctx, "database connected", "database", cfg.Name, "driver", cfg.Driver, "max_open_conns", cfg.MaxOpenConns)
	return pool, nil
}
