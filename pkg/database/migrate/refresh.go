package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"

	"github.com/wisalgroup/labena/pkg/logging"
)

// refresh holds the standard migration lock on a reserved session while Goose
// uses another connection. Goose Up checks history before taking its own lock,
// so reset cannot safely be hidden inside a provider lock callback.
func refresh(ctx context.Context, db *bun.DB, history fs.FS) (err error) {
	if db.Stats().MaxOpenConnections == 1 {
		return errors.New("migrations: refresh requires a pool with at least two connections; use Run for a dedicated pool")
	}
	provider, err := newProvider(ctx, db, history, false)
	if err != nil {
		return err
	}
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()
	var locker lock.SessionLocker
	driver := goose.DialectPostgres
	if db.Dialect().Name() == dialect.MySQL {
		driver = goose.DialectMySQL
		var name string
		if err := conn.QueryRowContext(ctx, "SELECT "+mysqlLockName).Scan(&name); err != nil {
			return err
		}
		locker = mysqlLocker{name: name}
	} else {
		locker, err = lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 300), lock.WithUnlockTimeout(1, 5))
		if err != nil {
			return err
		}
	}
	if err := locker.SessionLock(ctx, conn); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, locker.SessionUnlock(cleanup, conn))
	}()
	if err := resetDatabase(ctx, conn, driver); err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

func resetDatabase(ctx context.Context, conn *sql.Conn, driver goose.Dialect) error {
	if driver == goose.DialectPostgres {
		var schema, owner string
		if err := conn.QueryRowContext(ctx, `SELECT nspname,pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname=current_schema()`).Scan(&schema, &owner); err != nil {
			return err
		}
		if schema == "information_schema" || strings.HasPrefix(schema, "pg_") {
			return errors.New("migrations: cannot refresh a system schema")
		}
		logging.FromContext(ctx).WarnContext(ctx, "refreshing database schema: all objects and data will be deleted", "schema", schema)
		// Atomic drop/recreate preserves the old schema if either statement fails.
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DROP SCHEMA "+quoteIdentifier(schema, '"')+" CASCADE"); err != nil {
			return errors.Join(err, tx.Rollback())
		}
		if _, err := tx.ExecContext(ctx, "CREATE SCHEMA "+quoteIdentifier(schema, '"')+" AUTHORIZATION "+quoteIdentifier(owner, '"')); err != nil {
			return errors.Join(err, tx.Rollback())
		}
		return tx.Commit()
	}
	var name, charset, collation string
	if err := conn.QueryRowContext(ctx, `SELECT SCHEMA_NAME,DEFAULT_CHARACTER_SET_NAME,DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=DATABASE()`).Scan(&name, &charset, &collation); err != nil {
		return err
	}
	switch strings.ToLower(name) {
	case "mysql", "sys", "information_schema", "performance_schema":
		return errors.New("migrations: cannot refresh a system database")
	}
	logging.FromContext(ctx).WarnContext(ctx, "refreshing database: all objects and data will be deleted", "database", name)
	quoted := quoteIdentifier(name, '`')
	// MySQL DDL commits implicitly. A failure can leave the database absent or
	// partially rebuilt; never claim refresh is an atomic recovery mechanism.
	for _, statement := range []string{
		"DROP DATABASE " + quoted,
		"CREATE DATABASE " + quoted + " CHARACTER SET " + quoteIdentifier(charset, '`') + " COLLATE " + quoteIdentifier(collation, '`'),
		"USE " + quoted,
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrations: refresh database: %w", err)
		}
	}
	return nil
}

func quoteIdentifier(value string, quote byte) string {
	q := string(quote)
	return q + strings.ReplaceAll(value, q, q+q) + q
}
