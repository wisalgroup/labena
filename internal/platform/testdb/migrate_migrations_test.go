package testdb_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3/lock"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func pools(t *testing.T) (*sql.DB, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("DATABASE_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("DATABASE_TEST_POSTGRES not set")
	}
	cfg := database.DefaultConfig()
	cfg.DSN = dsn
	admin, err := database.OpenSQL(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	schema := "migrationtest_" + strings.ToLower(rand.Text())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close()
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	cfg.DSN = parsed.String()
	cfg.MaxOpenConns = 1
	cfg.MaxIdleConns = 1
	first, err := database.OpenSQL(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := database.OpenSQL(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	return first, second
}

func TestMigrationLockHonorsCancellation(t *testing.T) {
	first, second := pools(t)
	conn, err := first.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock($1)", lock.DefaultLockID); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", lock.DefaultLockID)
	provider, err := migrate.NewProvider(context.Background(), bun.NewDB(second, pgdialect.New()), testdb.Migrations(t, database.DriverPostgres))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := provider.Up(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock timeout: %v", err)
	}
}
