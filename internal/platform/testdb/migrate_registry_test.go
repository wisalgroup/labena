package testdb_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func TestRegistryCombinesSources(t *testing.T) {
	var registry migrate.Registry
	shared := fstest.MapFS{
		"20260101000000_shared.sql": {Data: []byte("shared")},
		"20260101000002_shared.sql": {Data: []byte("later shared")},
		"README.md":                 {Data: []byte("ignored")},
	}
	orders := fstest.MapFS{"20260101000001_orders.sql": {Data: []byte("orders")}}
	// Reverse registration order still yields a globally ordered history.
	if err := registry.Register("internal/modules/orders/migrations", orders); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("internal/modules/shared/migrations", shared); err != nil {
		t.Fatal(err)
	}
	history, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"20260101000000_shared.sql", "20260101000001_orders.sql", "20260101000002_shared.sql"}
	if err := fstest.TestFS(history, want...); err != nil {
		t.Fatal(err)
	}
	files, err := fs.Glob(history, "*.sql")
	if err != nil || strings.Join(files, ",") != strings.Join(want, ",") {
		t.Fatal(files, err)
	}
	body, err := fs.ReadFile(history, want[1])
	if err != nil || string(body) != "orders" {
		t.Fatal(string(body), err)
	}
	if _, err := fs.ReadFile(history, "README.md"); err == nil {
		t.Fatal("included non-SQL file")
	}
}

func TestRegistryRejectsConflictingHistories(t *testing.T) {
	for _, tt := range []struct{ name, filename, body string }{
		{"same filename", "20260101000000_shared.sql", ""},
		{"same version", "20260101000000_orders.sql", ""},
		{"invalid date", "20261301000000_orders.sql", ""},
		{"serial version", "001_orders.sql", ""},
		{"wrong dialect", "20260101000001_orders.sql", "-- platform: mysql jobs orders\n"},
		{"duplicate table", "20260101000001_orders.sql", "-- platform: postgres jobs jobs\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var registry migrate.Registry
			if err := registry.Register("shared/migrations", fstest.MapFS{"20260101000000_shared.sql": {Data: []byte("-- platform: postgres jobs jobs\n")}}); err != nil {
				t.Fatal(err)
			}
			if err := registry.Register("orders/migrations", fstest.MapFS{tt.filename: {Data: []byte(tt.body)}}); err != nil {
				t.Fatal(err)
			}
			if _, err := registry.FS(database.DriverPostgres); err == nil {
				t.Fatal("accepted conflicting history")
			}
		})
	}
}

func TestRegistrySourcesAndEmptyHistory(t *testing.T) {
	var registry migrate.Registry
	if err := registry.Register("orders/migrations", fstest.MapFS{}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		path  string
		files fs.FS
	}{
		{"orders/migrations", fstest.MapFS{}},
		{"../migrations", fstest.MapFS{}},
		{"missing", nil},
	} {
		if err := registry.Register(tt.path, tt.files); err == nil {
			t.Fatal("accepted invalid source", tt.path)
		}
	}
	history, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(history); err != nil {
		t.Fatal(err)
	}
}
