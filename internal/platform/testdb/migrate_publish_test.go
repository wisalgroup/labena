package testdb_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	cachemigrations "github.com/wisalgroup/labena/pkg/cache/migrations"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func cacheTable(name string) migrate.Table {
	return migrate.Table{Source: cachemigrations.Templates, Template: "cache_entries", Name: name}
}

func TestPublishPreservesApplicationEditsAndRenamedFiles(t *testing.T) {
	for _, driver := range []database.Driver{database.DriverPostgres, database.DriverMySQL} {
		t.Run(string(driver), func(t *testing.T) {
			dir := t.TempDir()
			first, err := migrate.Publish(context.Background(), dir, driver, cacheTable("work_items"))
			if err != nil || len(first) != 1 || !first[0].Created {
				t.Fatal(first, err)
			}
			body, err := os.ReadFile(first[0].Path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "{{") || !strings.Contains(string(body), "work_items_expiry") {
				t.Fatal("table and index names were not rendered")
			}
			if (driver == database.DriverMySQL) != strings.Contains(string(body), "NO TRANSACTION") {
				t.Fatal("wrong SQL dialect")
			}
			body = append(body, []byte("\n-- Application customization\n")...)
			renamed := filepath.Join(dir, filepath.Base(first[0].Path)[:15]+"custom_work.sql")
			if err := os.WriteFile(renamed, body, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(first[0].Path); err != nil {
				t.Fatal(err)
			}
			repeated, err := migrate.Publish(context.Background(), dir, driver, cacheTable("work_items"))
			if err != nil || len(repeated) != 1 || repeated[0].Created || repeated[0].Path != renamed {
				t.Fatal(repeated, err)
			}
			actual, _ := os.ReadFile(renamed)
			if string(actual) != string(body) {
				t.Fatal("application edits were overwritten")
			}
		})
	}
}

func TestPublishAppendsAfterExistingVersions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20990101000000_orders.sql"), []byte("-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := migrate.Publish(context.Background(), dir, database.DriverPostgres, cacheTable("jobs"), migrate.Table{Source: cachemigrations.Templates, Template: "cache_entries", Name: "event_subscriptions"})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"20990101000001_jobs.sql", "20990101000002_event_subscriptions.sql"} {
		if filepath.Base(results[i].Path) != want {
			t.Fatal(results)
		}
	}
	results, err = migrate.Publish(context.Background(), dir, database.DriverPostgres, cacheTable("jobs"))
	if err != nil || results[0].Created {
		t.Fatal(results, err)
	}
}

func TestPublishRejectsDialectAndMissingTemplate(t *testing.T) {
	dir := t.TempDir()
	if _, err := migrate.Publish(context.Background(), dir, database.DriverPostgres, cacheTable("jobs")); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		driver database.Driver
		table  migrate.Table
	}{
		{database.DriverMySQL, cacheTable("another_table")},
		{database.DriverPostgres, migrate.Table{Source: cachemigrations.Templates, Template: "missing", Name: "jobs"}},
	} {
		if _, err := migrate.Publish(context.Background(), dir, request.driver, request.table); err == nil {
			t.Fatal("accepted conflicting history")
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.sql"))
	if len(files) != 1 {
		t.Fatal(files)
	}
}

func TestPublishValidatesBeforeWriting(t *testing.T) {
	for _, name := range []string{"bad;name", "../jobs", "", "missing_template", "duplicate_table"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			bad := cacheTable(name)
			if name == "missing_template" {
				bad.Template = "missing"
			}
			if name == "duplicate_table" {
				bad.Name = "jobs"
			}
			if _, err := migrate.Publish(context.Background(), dir, database.DriverPostgres, cacheTable("jobs"), bad); err == nil {
				t.Fatal("accepted invalid selection")
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatal("partial output", files)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := migrate.Publish(ctx, t.TempDir(), database.DriverPostgres, cacheTable("jobs")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := migrate.Publish(context.Background(), t.TempDir(), database.DriverSQLite, cacheTable("jobs")); err == nil {
		t.Fatal("accepted unsupported dialect")
	}
}

func TestPublishProtectsExistingFilesAndPublisherLock(t *testing.T) {
	for _, name := range []string{".publish.lock", "20990101000000_jobs.sql"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			filename := filepath.Join(dir, name)
			if err := os.WriteFile(filename, []byte("user owned"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := migrate.Publish(context.Background(), dir, database.DriverPostgres, cacheTable("jobs")); err == nil {
				t.Fatal("accepted existing file")
			}
			body, _ := os.ReadFile(filename)
			if string(body) != "user owned" {
				t.Fatal("overwrote existing file")
			}
		})
	}
}

func TestProviderEmptyHistoryAndDialectGuard(t *testing.T) {
	sqlDB, err := sql.Open("pgx", "postgres://unused:unused@127.0.0.1:1/unused")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db := bun.NewDB(sqlDB, pgdialect.New())
	if _, err := migrate.NewProvider(context.Background(), db, fstest.MapFS{}); !errors.Is(err, goose.ErrNoMigrations) {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := migrate.Publish(context.Background(), dir, database.DriverMySQL, cacheTable("jobs")); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.NewProvider(context.Background(), db, os.DirFS(dir)); err == nil || !strings.Contains(err.Error(), "targets mysql") {
		t.Fatal(err)
	}
	if _, err := fs.Stat(os.DirFS(dir), ".publish.lock"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("publisher left a lock", err)
	}
}
