// Package testdb creates isolated databases for integration tests. It must not be
// imported by application code. Both backends apply published package templates with Goose.
package testdb

import (
	"context"
	"crypto/rand"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func Open(t *testing.T) *bun.DB {
	t.Helper()
	driver := database.Driver(os.Getenv("DATABASE_TEST_DRIVER"))
	if driver == "" {
		driver = database.DriverPostgres
	}
	return OpenDriver(t, driver)
}

func OpenDriver(t *testing.T, driver database.Driver) *bun.DB {
	t.Helper()
	key := "DATABASE_TEST_POSTGRES"
	if driver == database.DriverMySQL {
		key = "DATABASE_TEST_MYSQL"
	}
	dsn := os.Getenv(key)
	if dsn == "" {
		t.Skip(key + " not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := database.DefaultConfig()
	cfg.Driver = driver
	cfg.DSN = dsn
	admin, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	name := "test_" + strings.ToLower(rand.Text())
	create, drop := "CREATE SCHEMA "+name, "DROP SCHEMA "+name+" CASCADE"
	if driver == database.DriverMySQL {
		create, drop = "CREATE DATABASE "+name, "DROP DATABASE "+name
	}
	if _, err = admin.ExecContext(ctx, create); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, drop); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if driver == database.DriverMySQL {
		parsed, err := mysql.ParseDSN(dsn)
		if err != nil {
			t.Fatal(err)
		}
		parsed.DBName = name
		cfg.DSN = parsed.FormatDSN()
	} else {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("search_path", name)
		parsed.RawQuery = query.Encode()
		cfg.DSN = parsed.String()
	}
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	provider, err := migrate.NewProvider(ctx, db, Migrations(t, driver))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}
