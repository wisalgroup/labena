package migrate

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/wisalgroup/labena/pkg/logging"
)

func TestMigrationProgressOmitsStatements(t *testing.T) {
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := logging.WithLogger(context.Background(), logger.With("deployment", "test-123"))
	progress := slog.New(progressHandler{logging.FromContext(ctx).Handler()}).With("logger", "goose")
	progress.InfoContext(ctx, "executing statement", "statement", "secret SQL")
	progress.InfoContext(ctx, "migration completed", "version", 20260923134042, "statement", "secret SQL")
	if strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "statement") {
		t.Fatal("SQL leaked", output.String())
	}
	if !strings.Contains(output.String(), "test-123") || !strings.Contains(output.String(), "migration completed") {
		t.Fatal("missing context metadata", output.String())
	}
}
