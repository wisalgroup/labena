package middleware

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestScheduledFailureIsCategorizedAndRedacted(t *testing.T) {
	var output bytes.Buffer
	callback := LogScheduledTask(slog.New(slog.NewJSONHandler(&output, nil)))
	callback(nil, fmt.Errorf("private-credential: %w", context.DeadlineExceeded))
	if !strings.Contains(output.String(), `"error_category":"timeout"`) || strings.Contains(output.String(), "private-credential") {
		t.Fatal("unsafe or missing category", output.String())
	}
}
