package database

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/logging"
)

func TestConfigValidation(t *testing.T) {
	for _, cfg := range []Config{
		{}, {DSN: "x", Driver: "unknown"}, {DSN: "x", MaxOpenConns: -1},
		{DSN: "x", MaxOpenConns: 1, MaxIdleConns: 2}, {DSN: "x", MaxIdleConns: -1},
		{DSN: "x", ConnMaxLifetime: -time.Second}, {DSN: "x", PingTimeout: -time.Second},
		{DSN: "x", ConnectTimeout: -time.Second}, {DSN: "x", SlowQueryThreshold: -time.Second},
	} {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("accepted invalid config: %v", cfg)
		}
	}
	cfg, err := (Config{DSN: "postgres://localhost/app"}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Driver != DriverPostgres || cfg.MaxOpenConns != 20 || cfg.PingTimeout <= 0 {
		t.Fatalf("defaults: %v", cfg)
	}
}

func TestInMemorySQLiteKeepsOneConnection(t *testing.T) {
	db, err := Open(context.Background(), Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE entries (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO entries VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	if count(t, db) != 1 || db.Stats().MaxOpenConnections != 1 {
		t.Fatal("private database not retained")
	}
}

func TestContextLoggerAndSecretRedaction(t *testing.T) {
	var startup, request bytes.Buffer
	startupLogger, _ := logging.New(&startup, logging.Options{Level: slog.LevelDebug})
	requestLogger, _ := logging.New(&request, logging.Options{Level: slog.LevelDebug})
	ctx := logging.WithLogger(context.Background(), startupLogger.With("scope", "startup"))
	db, err := Open(ctx, Config{Name: "test", Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	queryCtx := logging.WithLogger(context.Background(), requestLogger.With("request_id", "request-123"))
	var value string
	if err := db.QueryRowContext(queryCtx, "SELECT ?", "sensitive-value").Scan(&value); err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(queryCtx, "SELECT sensitive_secret_identifier")
	output := request.String()
	if !strings.Contains(output, "request-123") || !strings.Contains(output, "database query") {
		t.Fatal("query logger did not use request context", output)
	}
	if strings.Contains(output, "sensitive") || strings.Contains(startup.String(), "request-123") {
		t.Fatal("query data leaked or request used startup logger", output)
	}
	cfg := Config{DSN: "postgres://user:secret-password@localhost/db"}
	var decoded Config
	if err := json.Unmarshal([]byte(`{"dsn":"postgres://user:secret-password@localhost/db"}`), &decoded); err != nil || decoded.DSN != cfg.DSN {
		t.Fatal("cannot decode DSN configuration", err)
	}
	encoded, _ := json.Marshal(cfg)
	for _, value := range []string{string(encoded), fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg)} {
		if strings.Contains(value, "secret-password") {
			t.Fatal("config leaked DSN")
		}
	}
	_, err = OpenSQL(context.Background(), Config{DSN: "postgres://user:secret-password@%invalid/db"})
	var connectionErr *ConnectionError
	if !errors.As(err, &connectionErr) || connectionErr.Unwrap() == nil {
		t.Fatalf("missing typed cause: %v", err)
	}
	if strings.Contains(err.Error(), "secret-password") {
		t.Fatal("error leaked DSN")
	}
}

func TestOpenCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db, err := OpenSQL(ctx, Config{Driver: DriverSQLite, DSN: ":memory:"})
	if db != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled open: %v %v", db, err)
	}
}

func TestNilDatabaseAndOptions(t *testing.T) {
	if _, err := NewTxManager(nil, nil); !errors.Is(err, ErrNilDB) {
		t.Fatal(err)
	}
	if _, err := Executor(context.Background(), nil); !errors.Is(err, ErrNilDB) {
		t.Fatal(err)
	}
	db := testDB(t)
	if _, err := NewTxManager(db, &sql.TxOptions{Isolation: sql.IsolationLevel(999)}); err == nil {
		t.Fatal("invalid isolation accepted")
	}
	options := sql.TxOptions{ReadOnly: true}
	manager, err := NewTxManager(db, &options)
	if err != nil {
		t.Fatal(err)
	}
	options.ReadOnly = false
	if !manager.options.ReadOnly {
		t.Fatal("retained mutable options")
	}
}

func TestNestedOptionsMismatchRollsBack(t *testing.T) {
	db := testDB(t)
	outer, _ := NewTxManager(db, nil)
	inner, _ := NewTxManager(db, &sql.TxOptions{ReadOnly: true})
	err := outer.Execute(context.Background(), func(ctx context.Context) error {
		if err := insert(ctx, db, 1); err != nil {
			return err
		}
		_ = inner.Execute(ctx, func(context.Context) error { t.Error("mismatched callback ran"); return nil })
		return nil
	})
	if !errors.Is(err, ErrTxOptionsMismatch) || !errors.Is(err, ErrRollbackOnly) || count(t, db) != 0 {
		t.Fatal(err)
	}
}
