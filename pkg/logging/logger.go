package logging

import (
	"errors"
	"io"
	"log/slog"

	"github.com/lmittmann/tint"
)

// Options selects the output handler without changing application logging calls.
// The zero value uses JSON at INFO level.
type Options struct {
	Format  Format
	Level   slog.Level
	NoColor bool
}

// New creates an independent logger; it does not replace slog.Default or own
// the writer. Tint is intended for development; JSON remains the default.
func New(output io.Writer, options Options) (*slog.Logger, error) {
	if output == nil {
		return nil, errors.New("log output is required")
	}
	if err := options.Format.Validate(); err != nil {
		return nil, err
	}
	if options.Format == FormatTint {
		return slog.New(withTracing(tint.NewTextHandler(output, &tint.Options{Level: options.Level, NoColor: options.NoColor, ReplaceAttr: tintLevel}))), nil
	}
	return slog.New(withTracing(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: options.Level}))), nil
}

// tintLevel keeps severity names consistent with JSON and log consumers.
func tintLevel(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) != 0 || attr.Key != slog.LevelKey {
		return attr
	}
	level, ok := attr.Value.Any().(slog.Level)
	if !ok {
		return attr
	}
	attr = slog.String(attr.Key, level.String())
	switch {
	case level >= slog.LevelError:
		return tint.Attr(9, attr)
	case level >= slog.LevelWarn:
		return tint.Attr(11, attr)
	case level >= slog.LevelInfo:
		return tint.Attr(10, attr)
	default:
		return attr
	}
}
