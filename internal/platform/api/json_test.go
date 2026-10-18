package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func referenceDecodeJSON(data []byte, value any) error {
	if !utf8.Valid(data) {
		return errors.New("JSON must be UTF-8")
	}
	if err := referenceUniqueJSONFields(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON value")
	}
	return nil
}

// Reject duplicate object members (including case variants accepted by Go's
// struct decoder) before validating credentials or applying business effects.
func referenceUniqueJSONFields(decoder *json.Decoder, depth int) error {
	if depth > 1000 {
		return errors.New("JSON nesting exceeds the supported depth")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("expected object member")
			}
			name = strings.Map(func(r rune) rune {
				folded := r
				for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
					if next < folded {
						folded = next
					}
				}
				return folded
			}, name)
			if seen[name] {
				return errors.New("duplicate JSON member")
			}
			seen[name] = true
			if err := referenceUniqueJSONFields(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := referenceUniqueJSONFields(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

// Differential fuzzing protects the existing acceptance policy and decoded value,
// not parser error text. The frozen streaming reference is deliberately slower.
func FuzzDecodeJSON(f *testing.F) {
	for _, s := range []string{
		`{}`, `[]`, `null`, `true`, `123`, `"text"`, `{} {}`,
		`{"name":"Ada","Name":"Other"}`, `{"k":1,"\u212a":2}`,
		`{"s":1,"ſ":2}`, `{"Σ":1,"ς":2}`, `{"a":{"key":1,"key":2}}`,
		`[{"a":1},{"a":2}]`, `{"a":"\ud800"}`, `{"\ud800":1,"\ufffd":2}`,
		`{"a":"\\\"[{}]"}`, `{"n":1e1000}`, `1e-1000`, `{`, `[1,]`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		original := append([]byte(nil), data...)
		var got, want any
		err := decodeJSON(data, &got)
		referenceErr := referenceDecodeJSON(data, &want)
		if (err == nil) != (referenceErr == nil) {
			t.Fatalf("acceptance differs: new=%v old=%v input=%q", err, referenceErr, data)
		}
		if err == nil && !reflect.DeepEqual(got, want) {
			t.Fatalf("decoded values differ: got=%#v want=%#v", got, want)
		}
		if !bytes.Equal(data, original) {
			t.Fatal("decoder mutated caller-owned bytes")
		}
	})
}

func TestDecodeJSONDepthAndOwnership(t *testing.T) {
	for _, depth := range []int{999, 1000, 1001, 1002} {
		for _, center := range []string{"0", ""} {
			data := []byte(strings.Repeat("[", depth) + center + strings.Repeat("]", depth))
			var got, want any
			err := decodeJSON(data, &got)
			referenceErr := referenceDecodeJSON(data, &want)
			if (err == nil) != (referenceErr == nil) {
				t.Fatalf("depth %d center %q: new=%v old=%v", depth, center, err, referenceErr)
			}
		}
	}
	data := []byte(`{"name":"original"}`)
	var got struct{ Name string }
	if err := decodeJSON(data, &got); err != nil {
		t.Fatal(err)
	}
	for i := range data {
		data[i] = 'x'
	}
	if got.Name != "original" {
		t.Fatal("decoded string aliases the request buffer")
	}
}
