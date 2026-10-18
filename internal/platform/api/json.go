package api

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

func decodeJSON(data []byte, value any) error {
	if !utf8.Valid(data) {
		return errors.New("JSON must be UTF-8")
	}
	// Inspect tokens without materializing string values or copying Huma's body
	// buffer. Our field check includes case-folded duplicates, so the decoder
	// need not maintain a second name registry. Keep v1 surrogate handling.
	decoder := jsontext.NewDecoder(bytes.NewBuffer(data), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	if err := uniqueJSONFields(decoder, 0); err != nil {
		return err
	}
	// Unmarshal consumes one complete document. The body is already buffered by
	// Huma; another streaming decoder would allocate and copy that buffer again.
	return json.Unmarshal(data, value)
}

// Reject duplicate object members (including case variants accepted by Go's
// struct decoder) before validating credentials or applying business effects.
func uniqueJSONFields(decoder *jsontext.Decoder, depth int) error {
	if depth > 1000 {
		return errors.New("JSON nesting exceeds the supported depth")
	}
	token, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	switch token.Kind() {
	case '{':
		seen := map[string]bool{}
		for decoder.PeekKind() != '}' {
			key, err := decoder.ReadToken()
			if err != nil {
				return err
			}
			if key.Kind() != '"' {
				return errors.New("expected object member")
			}
			name := strings.Map(func(r rune) rune {
				folded := r
				for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
					if next < folded {
						folded = next
					}
				}
				return folded
			}, key.String())
			if seen[name] {
				return errors.New("duplicate JSON member")
			}
			seen[name] = true
			if err := uniqueJSONFields(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.PeekKind() != ']' {
			if err := uniqueJSONFields(decoder, depth+1); err != nil {
				return err
			}
		}
	case '0':
		// Match encoding/json.Token's float64 range check, including when
		// the eventual typed destination uses json.Number or a custom decoder.
		_, err := token.Float()
		return err
	default:
		return nil
	}
	_, err = decoder.ReadToken()
	return err
}
