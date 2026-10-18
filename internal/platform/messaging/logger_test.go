package messaging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/ThreeDotsLabs/watermill"
)

func TestLibraryDiagnosticsUseDebugAndKeepErrorsVisible(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelDebug} {
		t.Run(level.String(), func(t *testing.T) {
			var output bytes.Buffer
			logger := Logger{Log: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: level}))}.With(watermill.LogFields{"handler_name": "welcome"})
			logger.Info("Adding handler", nil)
			logger.Info("Pub/Sub closed", nil)
			for _, message := range []string{"Adding handler", "Pub/Sub closed"} {
				if strings.Contains(output.String(), message) != (level == slog.LevelDebug) {
					t.Fatal(output.String())
				}
			}
			if level == slog.LevelDebug && !strings.Contains(output.String(), `"level":"DEBUG"`) {
				t.Fatal(output.String())
			}
			logger.Error("delivery failed", errors.New("private-error"), nil)
			if !strings.Contains(output.String(), `"level":"ERROR"`) || !strings.Contains(output.String(), "delivery failed") || strings.Contains(output.String(), "private-error") {
				t.Fatal(output.String())
			}
		})
	}
}

func TestDiagnosticsDoNotExposePayloads(t *testing.T) {
	var output bytes.Buffer
	logger := Logger{Log: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	logger.With(watermill.LogFields{"query_args": "private-argument"}).Error("delivery failed", errors.New("private-error"), watermill.LogFields{
		"query": "private-query", "payload": "private-payload", "metadata": "private-metadata", "handler_name": "welcome",
	})
	if strings.Contains(output.String(), "private-") || !strings.Contains(output.String(), "welcome") || !strings.Contains(output.String(), "error_type") {
		t.Fatal(output.String())
	}
}
