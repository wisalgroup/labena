package testdb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wisalgroup/labena/internal/platform/testdb"
	cachemigrations "github.com/wisalgroup/labena/pkg/cache/migrations"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func TestPublishedTablesApplyOnEachDatabase(t *testing.T) {
	for _, driver := range []database.Driver{database.DriverPostgres, database.DriverMySQL} {
		for _, scenario := range []struct {
			name   string
			tables []migrate.Table
		}{
			{"cache", []migrate.Table{{Source: cachemigrations.Templates, Template: "cache_entries", Name: "cache_entries"}}},
		} {
			t.Run(string(driver)+"/"+scenario.name, func(t *testing.T) {
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
				dir := t.TempDir()
				if _, err := migrate.Publish(ctx, dir, driver, scenario.tables...); err != nil {
					t.Fatal(err)
				}
				provider, err := migrate.NewProvider(ctx, db, os.DirFS(dir))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := provider.Up(ctx); err != nil {
					t.Fatal(err)
				}
				query := "SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name<>'goose_db_version'"
				if driver == database.DriverMySQL {
					query = "SELECT count(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name<>'goose_db_version'"
				}
				var count int
				if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil || count != len(scenario.tables) {
					t.Fatal("unexpected table count", count, err)
				}
				if _, err := provider.DownTo(ctx, 0); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil || count != 0 {
					t.Fatal("down left tables", count, err)
				}
			})
		}
	}
}
