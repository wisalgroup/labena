package platform_test

import (
	"io/fs"
	"testing"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	modulemigrations "github.com/wisalgroup/labena/internal/platform/media/migrations"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func databaseFixture(t *testing.T) (*bun.DB, uuid.UUID) {
	t.Helper()
	db := testdb.OpenDriver(t, database.DriverPostgres)
	var registry migrate.Registry
	for name, files := range map[string]fs.FS{"cache": testdb.Migrations(t, database.DriverPostgres), "capability": modulemigrations.Files} {
		if err := registry.Register(name, files); err != nil {
			t.Fatal(err)
		}
	}
	files, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := migrate.NewProvider(t.Context(), db, files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	if applied, err := provider.Up(t.Context()); err != nil || len(applied) != 0 {
		t.Fatal("non-idempotent baseline", err)
	}
	return db, uuid.New()
}
