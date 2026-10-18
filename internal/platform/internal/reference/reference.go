// Package reference defines the shared syntax of platform model references.
// It does not resolve models or authorize access. Public capability types retain
// their own meanings: media owner and notification recipient.
package reference

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var typePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

// Valid accepts a stable lowercase type name and a canonical, nonempty UTF-8 ID
// of at most 200 bytes. IDs have no surrounding whitespace or control characters.
// IDs remain opaque: UUID, numeric and application-specific identifiers are valid.
func Valid(kind, id string) bool {
	return typePattern.MatchString(kind) && id != "" && len(id) <= 200 && utf8.ValidString(id) && strings.TrimSpace(id) == id && !strings.ContainsFunc(id, unicode.IsControl)
}
