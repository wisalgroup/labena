package testdb_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wisalgroup/labena/pkg/cache"
	"testing"
	"time"

	"github.com/wisalgroup/labena/internal/platform/testdb"
	cachedb "github.com/wisalgroup/labena/pkg/cache/database"
	"github.com/wisalgroup/labena/pkg/database"
)

func TestTransactionsNamespacesAndPruning(t *testing.T) {
	for _, kind := range []database.Driver{database.DriverPostgres, database.DriverMySQL} {
		t.Run(string(kind), func(t *testing.T) {
			db := testdb.OpenDriver(t, kind)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			s, err := cachedb.New(db, cachedb.Config{Namespace: "one"})
			if err != nil {
				t.Fatal(err)
			}
			other, err := cachedb.New(db, cachedb.Config{Namespace: "two"})
			if err != nil {
				t.Fatal(err)
			}
			txs, err := database.NewTxManager(db, nil)
			if err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New("rollback")
			if err := txs.Execute(ctx, func(ctx context.Context) error {
				if err := s.Set(ctx, "transaction", []byte("staged"), time.Hour); err != nil {
					return err
				}
				v, ok, err := s.Get(ctx, "transaction")
				if err != nil || !ok || string(v) != "staged" {
					return fmt.Errorf("transaction read: %s %v %v", v, ok, err)
				}
				return sentinel
			}); !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
			if _, ok, err := s.Get(ctx, "transaction"); err != nil || ok {
				t.Fatal("rollback cached value", ok, err)
			}
			if err := txs.Execute(ctx, func(ctx context.Context) error { return s.Set(ctx, "transaction", []byte("committed"), time.Hour) }); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"a", "b", "c", "locked"} {
				if err := s.Set(ctx, key, []byte("expired"), time.Hour); err != nil {
					t.Fatal(err)
				}
			}
			if err := other.Set(ctx, "a", []byte("other"), time.Hour); err != nil {
				t.Fatal(err)
			}
			past := time.Now().UTC().Add(-time.Hour)
			if _, err := db.ExecContext(ctx, "UPDATE cache_entries SET expires_at=? WHERE cache_key<>'transaction'", past); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := s.Get(ctx, "a"); err != nil || ok {
				t.Fatal("expired read", ok, err)
			}
			if n, err := s.PruneExpired(ctx, 1); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			// Keep a refresh uncommitted while pruning. SKIP LOCKED must preserve it.
			if _, err := tx.ExecContext(ctx, "UPDATE cache_entries SET value=?0,expires_at=?1 WHERE namespace='one' AND cache_key='locked'", []byte("refreshed"), time.Now().UTC().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if n, err := s.PruneExpired(ctx, 100); err != nil || n != 2 {
				t.Fatal(n, err)
			}

			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if value, ok, err := s.Get(ctx, "locked"); err != nil || !ok || string(value) != "refreshed" {
				t.Fatal(string(value), ok, err)
			}
			// A timed-out remote write can still have reached the server. Check the
			// deadline independently of the refresh/prune invariant above.
			locked, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer locked.Rollback()
			if _, err := locked.ExecContext(ctx, "UPDATE cache_entries SET value=value WHERE namespace='one' AND cache_key='locked'"); err != nil {
				t.Fatal(err)
			}

			bounded, err := cachedb.New(db, cachedb.Config{Namespace: "one", OperationTimeout: 50 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			if err := bounded.Set(ctx, "locked", []byte("ambiguous result"), time.Hour); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("blocked write did not respect operation timeout", err)
			}
			if err := locked.Rollback(); err != nil {
				t.Fatal(err)
			}

			var count int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM cache_entries WHERE namespace='two'").Scan(&count); err != nil || count != 1 {
				t.Fatal("pruned another namespace", count, err)
			}
			if n, err := other.PruneExpired(ctx, 10); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			if n, err := s.PruneExpired(ctx, 10); err != nil || n != 0 {
				t.Fatal(n, err)
			}
			if _, err := s.PruneExpired(ctx, 0); err == nil {
				t.Fatal("accepted unbounded prune")
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := s.Get(ctx, "transaction"); err == nil || ok {
				t.Fatal("backend failure treated as miss")
			}
		})
	}
}

func TestBulkClearTransactionsAndIsolation(t *testing.T) {
	for _, kind := range []database.Driver{database.DriverPostgres, database.DriverMySQL} {
		t.Run(string(kind), func(t *testing.T) {
			db := testdb.OpenDriver(t, kind)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			s, _ := cachedb.New(db, cachedb.Config{Namespace: "one"})
			other, _ := cachedb.New(db, cachedb.Config{Namespace: "two"})
			if err := cache.SetMulti[any](ctx, s, map[string]any{"a": 1, "b": 2}, time.Hour); err != nil {
				t.Fatal(err)
			}
			if err := cache.SetMulti[any](ctx, s, map[string]any{"a": 3, "c": 4}, time.Hour); err != nil {
				t.Fatal(err)
			}
			if err := cache.SetMulti[any](ctx, other, map[string]any{"a": "other"}, time.Hour); err != nil {
				t.Fatal(err)
			}
			txs, _ := database.NewTxManager(db, nil)
			rollback := errors.New("rollback")
			if err := txs.Execute(ctx, func(ctx context.Context) error {
				if err := s.Clear(ctx); err != nil {
					return err
				}
				if values, err := cache.GetMulti[any](ctx, s, []string{"a", "b", "c"}); err != nil || len(values) != 0 {
					return fmt.Errorf("clear in transaction: %v %v", values, err)
				}
				if err := cache.SetMulti[any](ctx, s, map[string]any{"new": "pending"}, time.Hour); err != nil {
					return err
				}
				if exists, err := s.Exists(ctx, "new"); err != nil || !exists {
					return fmt.Errorf("exists in transaction: %v %v", exists, err)
				}
				return rollback
			}); !errors.Is(err, rollback) {
				t.Fatal(err)
			}
			values, err := cache.GetMulti[any](ctx, s, []string{"a", "b", "c", "new"})
			if err != nil || len(values) != 3 || values["a"] != json.Number("3") {
				t.Fatal("rollback failed", values, err)
			}
			if err := s.Clear(ctx); err != nil {
				t.Fatal(err)
			}
			if exists, err := other.Exists(ctx, "a"); err != nil || !exists {
				t.Fatal("cleared other namespace", exists, err)
			}
			if _, err := cache.Remember[any](ctx, s, "forever", cache.MaxTTL, func(context.Context) (any, error) { return "long-lived", nil }); err != nil {
				t.Fatal(err)
			}
			var expires time.Time
			if err := db.QueryRowContext(ctx, "SELECT expires_at FROM cache_entries WHERE namespace='one' AND cache_key='forever'").Scan(&expires); err != nil {
				t.Fatal(err)
			}
			if expires.Before(time.Now().Add(290 * 365 * 24 * time.Hour)) {
				t.Fatal("maximum TTL truncated", expires)
			}
			if _, err := db.ExecContext(ctx, "UPDATE cache_entries SET expires_at=? WHERE namespace='one'", time.Now().Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			if exists, err := s.Exists(ctx, "forever"); err != nil || exists {
				t.Fatal("expired exists", exists, err)
			}
			if values, err := cache.GetMulti[any](ctx, s, []string{"forever"}); err != nil || len(values) != 0 {
				t.Fatal("expired bulk read", values, err)
			}
		})
	}
}
