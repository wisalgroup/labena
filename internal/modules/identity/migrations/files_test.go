package migrations_test

import (
	"io/fs"
	"testing"

	identitymigrations "github.com/wisalgroup/labena/internal/modules/identity/migrations"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func TestIdentityMigrationRegistration(t *testing.T) {
	var registry migrate.Registry
	if err := registry.Register("identity", identitymigrations.Files); err != nil {
		t.Fatal(err)
	}
	history, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	names, err := fs.Glob(history, "*.sql")
	if err != nil || len(names) != 5 {
		t.Fatal("expected five Identity baseline migrations", names, err)
	}
}
