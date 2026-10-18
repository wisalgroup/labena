package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

func testDB(t *testing.T) *bun.DB {
	t.Helper()
	var db *bun.DB
	var err error
	if os.Getenv("DATABASE_TEST_DRIVER") == "mysql" {
		db = mysqlTestDB(t)
	} else if os.Getenv("DATABASE_TEST_POSTGRES") != "" {
		db = postgresTestDB(t)
	} else {
		db, err = Open(context.Background(), Config{Name: "test", Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db"), MaxOpenConns: 1, MaxIdleConns: 1, PingTimeout: time.Second})
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.ExecContext(context.Background(), "CREATE TABLE entries (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	return db
}
func insert(ctx context.Context, db *bun.DB, id int) error {
	executor, err := Executor(ctx, db)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, "INSERT INTO entries (id) VALUES (?)", id)
	return err
}
func count(t *testing.T, db *bun.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM entries").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestCommitAndNestedJoin(t *testing.T) {
	db := testDB(t)
	m := testTxManager(t, db)
	err := m.Execute(context.Background(), func(ctx context.Context) error {
		if err := insert(ctx, db, 1); err != nil {
			return err
		}
		return testTxManager(t, db).Execute(ctx, func(ctx context.Context) error { return insert(ctx, db, 2) })
	})
	if err != nil || count(t, db) != 2 {
		t.Fatal(err)
	}
}
func TestSwallowedNestedErrorRollsBack(t *testing.T) {
	db := testDB(t)
	m := testTxManager(t, db)
	failure := errors.New("business failure")
	err := m.Execute(context.Background(), func(ctx context.Context) error {
		if err := insert(ctx, db, 1); err != nil {
			return err
		}
		_ = m.Execute(ctx, func(context.Context) error { return failure })
		return nil
	})
	if !errors.Is(err, ErrRollbackOnly) || !errors.Is(err, failure) || count(t, db) != 0 {
		t.Fatal(err)
	}
}
func TestPanicRollsBack(t *testing.T) {
	db := testDB(t)
	m := testTxManager(t, db)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic not preserved")
			}
		}()
		_ = m.Execute(context.Background(), func(ctx context.Context) error {
			if err := insert(ctx, db, 1); err != nil {
				t.Fatal(err)
			}
			panic("boom")
		})
	}()
	if count(t, db) != 0 {
		t.Fatal("panic committed")
	}
}
func TestCancellationRollsBack(t *testing.T) {
	db := testDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	err := testTxManager(t, db).Execute(ctx, func(ctx context.Context) error {
		if err := insert(ctx, db, 1); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || count(t, db) != 0 {
		t.Fatal(err)
	}
}
func TestCrossDatabaseRejected(t *testing.T) {
	db, other := testDB(t), testDB(t)
	err := testTxManager(t, db).Execute(context.Background(), func(ctx context.Context) error { return insert(ctx, other, 1) })
	if !errors.Is(err, ErrDifferentDatabase) || count(t, other) != 0 {
		t.Fatal(err)
	}
}
func TestRecoveredNestedPanicStillRollsBack(t *testing.T) {
	db := testDB(t)
	m := testTxManager(t, db)
	err := m.Execute(context.Background(), func(ctx context.Context) error {
		if err := insert(ctx, db, 1); err != nil {
			return err
		}
		func() {
			defer func() { _ = recover() }()
			_ = m.Execute(ctx, func(context.Context) error { panic("inner") })
		}()
		return nil
	})
	if !errors.Is(err, ErrRollbackOnly) || count(t, db) != 0 {
		t.Fatal(err)
	}
}

func TestExplicitTransactionOwnership(t *testing.T) {
	db := testDB(t)
	tx := testTxManager(t, db)
	ctx, err := tx.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Begin(ctx); !errors.Is(err, ErrTransactionActive) {
		t.Fatal(err)
	}
	if err := tx.Execute(ctx, func(ctx context.Context) error { return insert(ctx, db, 1) }); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, db) != 1 {
		t.Fatal("explicit transaction did not commit")
	}
	if _, err := Executor(ctx, db); !errors.Is(err, sql.ErrTxDone) {
		t.Fatal("completed context escaped to DB", err)
	}
	if err := tx.Commit(ctx); !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); !errors.Is(err, ErrNoTransaction) {
		t.Fatal(err)
	}
}

func TestExplicitRollbackAfterCancellation(t *testing.T) {
	db := testDB(t)
	tx := testTxManager(t, db)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, err := tx.Begin(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := insert(ctx, db, 1); err != nil {
		t.Fatal(err)
	}
	cancel()
	// pgx may return cancellation while discarding the unusable connection.
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, db) != 0 {
		t.Fatal("rollback committed")
	}
}

func TestSwallowedOwnershipAndEarlyErrorsRollBack(t *testing.T) {
	for _, name := range []string{"commit", "rollback", "canceled nested", "nil callback"} {
		t.Run(name, func(t *testing.T) {
			db := testDB(t)
			tx := testTxManager(t, db)
			err := tx.Execute(context.Background(), func(ctx context.Context) error {
				if err := insert(ctx, db, 1); err != nil {
					return err
				}
				switch name {
				case "commit":
					_ = tx.Commit(ctx)
				case "rollback":
					_ = tx.Rollback(ctx)
				case "canceled nested":
					child, cancel := context.WithCancel(ctx)
					cancel()
					_ = tx.Execute(child, func(context.Context) error { t.Error("callback ran"); return nil })
				case "nil callback":
					_ = tx.Execute(ctx, nil)
				}
				return nil
			})
			if !errors.Is(err, ErrRollbackOnly) || count(t, db) != 0 {
				t.Fatal(err)
			}
		})
	}
}

func testTxManager(t *testing.T, db *bun.DB) *TxManager {
	t.Helper()
	manager, err := NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}
