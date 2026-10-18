package messaging

import (
	"fmt"
	"log/slog"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/wisalgroup/labena/pkg/logging"
)

// Logger adapts Watermill diagnostics to the application logger. Routine library
// lifecycle and routing messages use debug; failures retain error severity.
type Logger struct {
	Log *slog.Logger
}

func (l Logger) Error(msg string, err error, fields watermill.LogFields) {
	args := logArgs(fields)

	if err != nil {
		args = append(args, "error_type", fmt.Sprintf("%T", err), "error_category", logging.ErrorCategory(err))
	}

	l.logger().Error(msg, args...)
}

func (l Logger) Info(msg string, fields watermill.LogFields) {
	l.logger().Debug(msg, logArgs(fields)...)
}

func (l Logger) Debug(msg string, fields watermill.LogFields) {
	l.logger().Debug(msg, logArgs(fields)...)
}

func (l Logger) Trace(msg string, fields watermill.LogFields) {
	// slog has no built-in Trace level.
	l.logger().Debug(msg, logArgs(fields)...)
}

func (l Logger) With(fields watermill.LogFields) watermill.LoggerAdapter {
	return Logger{
		Log: l.logger().With(logArgs(fields)...),
	}
}

func (l Logger) logger() *slog.Logger {
	if l.Log != nil {
		return l.Log
	}

	return slog.Default()
}

func logArgs(fields watermill.LogFields) []any {
	args := make([]any, 0, len(fields)*2)

	// Keep diagnostic identity, never metadata, message bodies or arbitrary
	// handler fields that may carry private data.
	for key, value := range fields {
		switch key {
		case "topic", "handler_name", "message_uuid", "subscriber_uuid", "publisher_uuid", "pubsub_uuid", "retry_no", "max_retries", "wait_time", "message_count":
			args = append(args, key, value)
		}
	}

	return args
}

var _ watermill.LoggerAdapter = Logger{}
