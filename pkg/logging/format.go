package logging

import "errors"

// Format selects a slog handler while preserving its environment/JSON spelling.
type Format string

const (
	// FormatDefault uses JSON, preserving the zero-value behavior.
	FormatDefault Format = ""
	FormatJSON    Format = "json"
	FormatTint    Format = "tint"
)

func (f Format) Validate() error {
	switch f {
	case FormatDefault, FormatJSON, FormatTint:
		return nil
	default:
		return errors.New("log format must be json or tint")
	}
}
