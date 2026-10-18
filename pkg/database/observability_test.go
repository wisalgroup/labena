package database

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/uptrace/bun"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestDatabaseTelemetry(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	defer tp.Shutdown(ctx)
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer mp.Shutdown(ctx)
	stop, err := Instrument(db, "primary", tp, mp)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	ctx, parent := tp.Tracer("test").Start(ctx, "parent")
	var value string
	if err := db.NewSelect().ColumnExpr("?", "secret-value").Scan(ctx, &value); err != nil {
		t.Fatal(err)
	}
	if value != "secret-value" {
		t.Fatal("query behavior changed")
	}
	if _, err := db.ExecContext(ctx, "SELECT 'secret-value' FROM missing_table"); err == nil {
		t.Fatal("expected SQL error")
	}
	// Ambiguous text is omitted without losing the operation or changing its result.
	if err := db.NewSelect().ColumnExpr("?", `secret\value`).Scan(ctx, &value); err != nil {
		t.Fatal(err)
	}
	if value != `secret\value` {
		t.Fatal("query behavior changed when text was omitted")
	}
	parent.End()
	ended := spans.Ended()
	if len(ended) != 4 {
		t.Fatalf("got %d spans", len(ended))
	}
	for i, span := range ended[:3] {
		wantQuery := []string{"SELECT ?", "SELECT ? FROM missing_table", ""}[i]
		query := ""
		for _, attr := range span.Attributes() {
			if attr.Key == "db.query.text" {
				query = attr.Value.AsString()
			}
		}
		if query != wantQuery {
			t.Errorf("query text = %q, want %q", query, wantQuery)
		}
		if span.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Fatal("lost parent")
		}
		if strings.Contains(fmt.Sprint(span.Attributes(), span.Status(), span.Events(), span.Name()), "secret") {
			t.Fatal("query data leaked")
		}
		if (span.Status().Code == codes.Error) != (i == 1) {
			t.Fatal("incorrect query status")
		}
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			names[m.Name] = true
		}
	}
	for _, name := range []string{"db.client.operation.duration", "db.pool.connections", "db.pool.max_connections", "db.pool.waits", "db.pool.wait.duration"} {
		if !names[name] {
			t.Errorf("missing %s", name)
		}
	}
	if strings.Contains(fmt.Sprint(data), "secret") || strings.Contains(fmt.Sprint(data), "db.query.text") {
		t.Fatal("query data leaked in metrics")
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	data = metricdata.ResourceMetrics{}
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "db.pool.connections" {
				if gauge, ok := m.Data.(metricdata.Gauge[int64]); ok && len(gauge.DataPoints) != 0 {
					t.Fatal("pool callback still registered")
				}
			}
		}
	}
}

func TestPostgresQueryText(t *testing.T) {
	db := postgresTestDB(t)
	verifyQueryTextCRUD(t, db)
}

func TestSQLiteQueryText(t *testing.T) {
	db, err := Open(t.Context(), Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	verifyQueryTextCRUD(t, db)
}

func verifyQueryTextCRUD(t *testing.T, db *bun.DB) {
	t.Helper()
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, "CREATE TABLE query_entries (id INTEGER PRIMARY KEY, email TEXT, code_digest BYTEA)"); err != nil {
		t.Fatal(err)
	}
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	mp := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()); _ = mp.Shutdown(context.Background()) })
	stop, err := Instrument(db, "primary", tp, mp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stop() })
	ctx, parent := tp.Tracer("test").Start(ctx, "request")
	defer parent.End()
	entry := &struct {
		ID         int
		Email      string
		CodeDigest []byte
	}{ID: 424242, Email: "private@example.com", CodeDigest: []byte("private-digest")}
	if _, err := db.NewInsert().Model(entry).ModelTableExpr("query_entries").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	entry.Email = "changed@example.com"
	if _, err := db.NewUpdate().Model(entry).ModelTableExpr("query_entries").Where("id = ?", entry.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	var email string
	if err := db.NewSelect().TableExpr("query_entries").ColumnExpr("email").Where("id = ?", entry.ID).Scan(ctx, &email); err != nil {
		t.Fatal(err)
	}
	if email != entry.Email {
		t.Fatal("query behavior changed")
	}
	if _, err := db.NewDelete().TableExpr("query_entries").Where("id = ?", entry.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	ended := spans.Ended()
	if len(ended) != 4 {
		t.Fatalf("got %d spans, want 4", len(ended))
	}
	for _, span := range ended {
		if span.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Error("query lost its request parent")
		}
		query := ""
		for _, attr := range span.Attributes() {
			if attr.Key == "db.query.text" {
				query = attr.Value.AsString()
			}
		}
		if !strings.Contains(query, "query_entries") || !strings.Contains(query, "?") {
			t.Errorf("missing query structure: %q", query)
		}
		for _, secret := range []string{"private@example.com", "changed@example.com", "424242", "private-digest", "707269766174652d646967657374"} {
			if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), secret) {
				t.Error("query value leaked into telemetry")
			}
		}
	}
}
