package testdb_test

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3/lock"

	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func TestRefreshRebuildsSchema(t *testing.T) {
	for _, driver := range []database.Driver{database.DriverPostgres, database.DriverMySQL} {
		t.Run(string(driver), func(t *testing.T) {
			db := testdb.OpenDriver(t, driver)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			history := fstest.MapFS{"20260101000000_fresh.sql": {Data: []byte("-- +goose Up\nCREATE TABLE refreshed (id INTEGER PRIMARY KEY, label VARCHAR(40));\nINSERT INTO refreshed VALUES (1, 'seed');\n-- +goose Down\nDROP TABLE refreshed;\n")}}
			for _, query := range []string{"CREATE TABLE obsolete (id INTEGER PRIMARY KEY)", "INSERT INTO obsolete VALUES (1)", "CREATE VIEW obsolete_view AS SELECT id FROM obsolete"} {
				if _, err := db.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			// Invalid histories must be rejected before anything is deleted.
			if err := migrate.RunDB(ctx, db, fstest.MapFS{}, "refresh", time.Second); err == nil {
				t.Fatal("accepted empty history")
			}
			var count int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM obsolete").Scan(&count); err != nil || count != 1 {
				t.Fatalf("validation deleted data: %d %v", count, err)
			}
			// A competing migration lock must prevent refresh from deleting objects.
			conn, err := db.DB.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			lockSQL, unlockSQL := "SELECT pg_advisory_lock($1)", "SELECT pg_advisory_unlock($1)"
			args := []any{lock.DefaultLockID}
			if driver == database.DriverMySQL {
				lockSQL = "SELECT GET_LOCK(CONCAT('labena:',LEFT(SHA2(DATABASE(),256),48)),1)"
				unlockSQL = "SELECT RELEASE_LOCK(CONCAT('labena:',LEFT(SHA2(DATABASE(),256),48)))"
				args = nil
			}
			if _, err = conn.ExecContext(ctx, lockSQL, args...); err != nil {
				conn.Close()
				t.Fatal(err)
			}
			err = migrate.RunDB(ctx, db, history, "refresh", 100*time.Millisecond)
			if _, unlockErr := conn.ExecContext(ctx, unlockSQL, args...); unlockErr != nil {
				t.Error(unlockErr)
			}
			if closeErr := conn.Close(); closeErr != nil {
				t.Error(closeErr)
			}
			if err == nil {
				t.Fatal("refresh ignored migration lock")
			}
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM obsolete").Scan(&count); err != nil || count != 1 {
				t.Fatalf("blocked refresh deleted data: %d %v", count, err)
			}
			for i := 0; i < 2; i++ {
				if err := migrate.RunDB(ctx, db, history, "refresh", 10*time.Second); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRowContext(ctx, "SELECT count(*) FROM refreshed").Scan(&count); err != nil || count != 1 {
					t.Fatalf("up did not replay cleanly: %d %v", count, err)
				}
				var schema string
				query := "SELECT current_schema()"
				if driver == database.DriverMySQL {
					query = "SELECT DATABASE()"
				}
				if err := db.QueryRowContext(ctx, query).Scan(&schema); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=?0 AND table_name IN ('obsolete','obsolete_view','jobs')", schema).Scan(&count); err != nil || count != 0 {
					t.Fatalf("old objects remain: %d %v", count, err)
				}
				if _, err := db.ExecContext(ctx, "INSERT INTO refreshed VALUES(2,'delete me')"); err != nil {
					t.Fatal(err)
				}
			}
			// Up after refresh reads the new version history and does not run twice.
			if err := migrate.RunDB(ctx, db, history, "up", time.Second); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM refreshed").Scan(&count); err != nil || count != 2 {
				t.Fatalf("version history lost: %d %v", count, err)
			}
		})
	}
}
