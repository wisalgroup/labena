package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"
	"net/url"
)

func postgresTestDB(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("DATABASE_TEST_POSTGRES not set")
	}
	cfg := DefaultConfig()
	cfg.DSN = dsn
	admin, err := OpenSQL(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	schema := "dbtest_" + strings.ToLower(rand.Text())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close()
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	cfg.DSN = parsed.String()
	db, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPostgresReadOnlyAndCancellation(t *testing.T) {
	db := postgresTestDB(t)
	if _, err := db.Exec("CREATE TABLE entries(id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	manager, _ := NewTxManager(db, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	err := manager.Execute(context.Background(), func(ctx context.Context) error { return insert(ctx, db, 1) })
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("read only: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = db.ExecContext(ctx, "SELECT pg_sleep(10)")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("query deadline: %v", err)
	}
	if count(t, db) != 0 {
		t.Fatal("read-only transaction wrote data")
	}
}

func TestPostgresRawTransactionAndAbortedCommit(t *testing.T) {
	db := postgresTestDB(t)
	if _, err := db.Exec("CREATE TABLE entries(id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	manager, _ := NewTxManager(db, nil)
	if err := manager.Execute(context.Background(), func(ctx context.Context) error {
		tx, err := TxFromContext(ctx, db)
		if err != nil {
			return err
		}
		_, err = tx.Tx.ExecContext(ctx, "INSERT INTO entries(id) VALUES ($1)", 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := manager.Execute(context.Background(), func(ctx context.Context) error {
		_ = insert(ctx, db, 1) // PostgreSQL aborts this transaction even if the error is swallowed.
		return nil
	})
	if err == nil {
		t.Fatal("reported success for an aborted commit")
	}
	if count(t, db) != 1 {
		t.Fatal("unexpected persisted rows")
	}
}
