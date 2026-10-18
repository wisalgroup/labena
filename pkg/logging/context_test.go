package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestScopedLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil)).With("request_id", "request-1")
	ctx := WithLogger(context.Background(), logger)
	FromContext(ctx).InfoContext(ctx, "hello")
	if !strings.Contains(output.String(), `"request_id":"request-1"`) {
		t.Fatal(output.String())
	}
	if FromContext(context.Background()) == nil {
		t.Fatal("nil fallback")
	}
}
