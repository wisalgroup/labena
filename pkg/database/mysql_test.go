package database

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/uptrace/bun"
)

func mysqlTestDB(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_TEST_MYSQL")
	if dsn == "" {
		t.Skip("DATABASE_TEST_MYSQL not set")
	}
	cfg := DefaultConfig()
	cfg.Driver = DriverMySQL
	cfg.DSN = dsn
	admin, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	name := "dbtest_" + strings.ToLower(rand.Text())
	if _, err := admin.ExecContext(context.Background(), "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		admin.ExecContext(ctx, "DROP DATABASE "+name)
		admin.Close()
	})
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.DBName = name
	cfg.DSN = parsed.FormatDSN()
	db, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMySQLTimeAndCancellation(t *testing.T) {
	db := mysqlTestDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE TABLE instants (value DATETIME(6))"); err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 9, 25, 14, 15, 16, 123456000, time.FixedZone("local", 3*3600))
	if _, err := db.ExecContext(ctx, "INSERT INTO instants VALUES (?)", instant); err != nil {
		t.Fatal(err)
	}
	var got time.Time
	if err := db.QueryRowContext(ctx, "SELECT value FROM instants").Scan(&got); err != nil || !got.Equal(instant) {
		t.Fatal(got, err)
	}
	var zone string
	if err := db.QueryRowContext(ctx, "SELECT @@session.time_zone").Scan(&zone); err != nil || zone != "+00:00" {
		t.Fatal(zone, err)
	}
	canceled, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	defer stop()
	if _, err := db.ExecContext(canceled, "SELECT SLEEP(10)"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
