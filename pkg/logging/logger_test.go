package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestHandlersPreserveLevelAndContextAttributes(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatTint} {
		t.Run(string(format), func(t *testing.T) {
			var out bytes.Buffer
			logger, err := New(&out, Options{Format: format, Level: slog.LevelWarn, NoColor: true})
			if err != nil {
				t.Fatal(err)
			}
			ctx := WithLogger(context.Background(), logger.With("request_id", "request-123"))
			FromContext(ctx).InfoContext(ctx, "hidden")
			FromContext(ctx).WarnContext(ctx, "visible")
			if strings.Contains(out.String(), "hidden") || !strings.Contains(out.String(), "request-123") || strings.Contains(out.String(), "\x1b[") {
				t.Fatal(out.String())
			}
			if format == FormatJSON && !json.Valid(bytes.TrimSpace(out.Bytes())) {
				t.Fatal("invalid JSON", out.String())
			}
		})
	}
}
func TestJSONDefaultAndInvalidFormat(t *testing.T) {
	var out bytes.Buffer
	logger, err := New(&out, Options{})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("hello")
	if !json.Valid(bytes.TrimSpace(out.Bytes())) {
		t.Fatal(out.String())
	}
	if _, err := New(&out, Options{Format: "unknown"}); err == nil {
		t.Fatal("invalid format accepted")
	}
}

func TestTintUsesFullSeverityNames(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		t.Run(level.String(), func(t *testing.T) {
			var out bytes.Buffer
			logger, err := New(&out, Options{Format: FormatTint, Level: slog.LevelDebug, NoColor: true})
			if err != nil {
				t.Fatal(err)
			}
			logger.Log(context.Background(), level, "severity probe")
			if !strings.Contains(out.String(), " "+level.String()+" severity probe") {
				t.Fatal(out.String())
			}
		})
	}
}
