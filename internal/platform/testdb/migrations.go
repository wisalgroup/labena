package testdb

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	cachemigrations "github.com/wisalgroup/labena/pkg/cache/migrations"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

// Migrations publishes the package templates into a temporary directory. Test
// versions are fixed so fixtures are independent of the application's opt-in SQL.
func Migrations(t *testing.T, driver database.Driver) fs.FS {
	t.Helper()
	if driver == "" {
		driver = database.DriverPostgres
	}
	var tables []migrate.Table
	tables = append(tables, migrate.Table{Source: cachemigrations.Templates, Template: "cache_entries", Name: "cache_entries"})
	published, err := migrate.Publish(context.Background(), t.TempDir(), driver, tables...)
	if err != nil {
		t.Fatal(err)
	}
	files := make(fstest.MapFS)
	for i, result := range published {
		body, err := os.ReadFile(result.Path)
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("202601010000%02d_%s", i, filepath.Base(result.Path)[15:])
		files[name] = &fstest.MapFile{Data: body, Mode: 0o644}
	}
	return files
}

// Migrate prepares a disposable integration database using package templates.
func Migrate(t *testing.T, cfg database.Config) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, err := migrate.NewProvider(ctx, db, Migrations(t, cfg.Driver))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
}
