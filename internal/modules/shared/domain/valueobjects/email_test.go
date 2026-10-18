package valueobjects

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmailJSON(t *testing.T) {
	email, err := NewEmail(" ADA@Example.com ")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(email)
	if err != nil || string(data) != `"ada@example.com"` {
		t.Fatal(string(data), err)
	}
	var decoded Email
	if err := json.Unmarshal(data, &decoded); err != nil || !email.Equal(decoded) {
		t.Fatal(decoded, err)
	}
	for _, invalid := range []string{`null`, `"broken"`, `{}`} {
		if err := json.Unmarshal([]byte(invalid), &decoded); err == nil || !decoded.Equal(email) {
			t.Fatal("invalid decoding changed value", invalid)
		}
	}
	if _, err := NewEmail(strings.Repeat("x", 250) + "@example.com"); err == nil {
		t.Fatal("accepted overlong address")
	}
}

func TestEmailSQL(t *testing.T) {
	var email Email
	for _, src := range []any{" Ada@Example.com ", []byte("ada@example.com")} {
		if err := email.Scan(src); err != nil {
			t.Fatal(err)
		}
		value, err := email.Value()
		if err != nil || value != "ada@example.com" {
			t.Fatal(value, err)
		}
	}
	for _, src := range []any{nil, 123, "broken", []byte{}} {
		if err := email.Scan(src); err == nil || email.String() != "ada@example.com" {
			t.Fatal("invalid scan mutated email", src)
		}
	}
	if _, err := (Email{}).Value(); err == nil {
		t.Fatal("persisted invalid zero value")
	}
}
