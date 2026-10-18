package testdb_test

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func TestGlobalAndModuleHistoryOnEachDatabase(t *testing.T) {
	for _, driver := range []database.Driver{database.DriverPostgres, database.DriverMySQL} {
		t.Run(string(driver), func(t *testing.T) {
			db := testdb.OpenDriver(t, driver)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			baseline, err := migrate.NewProvider(ctx, db, testdb.Migrations(t, driver))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := baseline.DownTo(ctx, 0); err != nil {
				t.Fatal(err)
			}
			file := func(up, down string) *fstest.MapFile {
				prefix := ""
				if driver == database.DriverMySQL {
					prefix = "-- +goose NO TRANSACTION\n"
				}
				return &fstest.MapFile{Data: []byte(prefix + "-- +goose Up\n" + up + "\n-- +goose Down\n" + down + "\n")}
			}
			var registry migrate.Registry
			// Each later step depends on SQL from the other source. Folder order
			// cannot produce the correct rows; global timestamp order must win.
			if err := registry.Register("orders/migrations", fstest.MapFS{
				"20270101000001_orders.sql": file("INSERT INTO migration_order VALUES (1);", "DELETE FROM migration_order WHERE id=1;"),
				"20270101000003_orders.sql": file("INSERT INTO migration_order SELECT 3 FROM migration_order WHERE id=2;", "DELETE FROM migration_order WHERE id=3;"),
			}); err != nil {
				t.Fatal(err)
			}
			if err := registry.Register("shared/migrations", fstest.MapFS{
				"20270101000000_shared.sql": file("CREATE TABLE migration_order (id INTEGER PRIMARY KEY);", "DROP TABLE migration_order;"),
				"20270101000002_shared.sql": file("INSERT INTO migration_order SELECT 2 FROM migration_order WHERE id=1;", "DELETE FROM migration_order WHERE id=2;"),
			}); err != nil {
				t.Fatal(err)
			}
			history, err := registry.FS(driver)
			if err != nil {
				t.Fatal(err)
			}
			if err := migrate.RunDB(ctx, db, history, "up", 20*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := migrate.RunDB(ctx, db, history, "up", 20*time.Second); err != nil {
				t.Fatal("repeat up", err)
			}
			var count int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM migration_order").Scan(&count); err != nil || count != 3 {
				t.Fatal(count, err)
			}
			provider, err := migrate.NewProvider(ctx, db, history)
			if err != nil {
				t.Fatal(err)
			}
			statuses, err := provider.Status(ctx)
			if err != nil || len(statuses) != 4 {
				t.Fatal(statuses, err)
			}
			if err := migrate.RunDB(ctx, db, history, "down", 20*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM migration_order").Scan(&count); err != nil || count != 2 {
				t.Fatal(count, err)
			}
			version, err := provider.GetDBVersion(ctx)
			if err != nil || version != 20270101000002 {
				t.Fatal(version, err)
			}
			if _, err := provider.DownTo(ctx, 0); err != nil {
				t.Fatal(err)
			}
			if err := migrate.RunDB(ctx, db, history, "up", 20*time.Second); err != nil {
				t.Fatal("reapply", err)
			}
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM migration_order").Scan(&count); err != nil || count != 3 {
				t.Fatal(count, err)
			}
		})
	}
}
