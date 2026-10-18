package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestSavepointIsolation(t *testing.T) {
	for _, mode := range []string{"success", "error", "SQL error", "panic", "nested error", "managed commit"} {
		t.Run(mode, func(t *testing.T) {
			db := testDB(t)
			manager := testTxManager(t, db)
			var escaped context.Context
			err := manager.Execute(context.Background(), func(ctx context.Context) error {
				if err := insert(ctx, db, 1); err != nil {
					return err
				}
				var scopeErr error
				var panicked bool
				func() {
					defer func() { panicked = recover() != nil }()
					scopeErr = manager.Savepoint(ctx, func(child context.Context) error {
						escaped = child
						if err := insert(child, db, 2); err != nil {
							return err
						}
						switch mode {
						case "error":
							return errors.New("handler failed")
						case "SQL error":
							return insert(child, db, 1)
						case "panic":
							panic("handler failed")
						case "nested error":
							_ = manager.Execute(child, func(context.Context) error { return errors.New("nested failure") })
						case "managed commit":
							_ = manager.Commit(child)
						}
						return nil
					})
				}()
				if panicked != (mode == "panic") || (scopeErr != nil) != (mode != "success" && mode != "panic") {
					t.Fatalf("panic=%v error=%v", panicked, scopeErr)
				}
				if _, err := Executor(escaped, db); !errors.Is(err, sql.ErrTxDone) {
					t.Fatalf("savepoint context remained usable: %v", err)
				}
				return insert(ctx, db, 3)
			})
			want := 2
			if mode == "success" {
				want = 3
			}
			if err != nil || count(t, db) != want {
				t.Fatalf("parent transaction: %v, rows=%d want=%d", err, count(t, db), want)
			}
		})
	}
}

func TestSavepointDependsOnOuterCommit(t *testing.T) {
	db := testDB(t)
	manager := testTxManager(t, db)
	if err := manager.Savepoint(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrNoTransaction) {
		t.Fatal(err)
	}
	failure := errors.New("outer failure")
	err := manager.Execute(context.Background(), func(ctx context.Context) error {
		if err := manager.Savepoint(ctx, func(ctx context.Context) error { return insert(ctx, db, 1) }); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) || count(t, db) != 0 {
		t.Fatalf("outer rollback: %v", err)
	}
}
