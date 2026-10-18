package otp

import (
	"bytes"
	"testing"
	"unicode"
)

func TestCodesAndDigests(t *testing.T) {
	for range 10 {
		code, err := Generate()
		if err != nil || len(code) != 6 {
			t.Fatal(code, err)
		}
		for _, c := range code {
			if !unicode.IsDigit(c) {
				t.Fatal(code)
			}
		}
	}
	key := bytes.Repeat([]byte{3}, 32)
	d, err := NewDigester(key)
	if err != nil {
		t.Fatal(err)
	}
	first := d.Digest("id", "sign_in", "email", "person@example.com", "012345")
	key[0] = 9
	if !bytes.Equal(first, d.Digest("id", "sign_in", "email", "person@example.com", "012345")) {
		t.Fatal("borrowed mutable key")
	}
	if bytes.Equal(first, d.Digest("id", "password_reset", "email", "person@example.com", "012345")) {
		t.Fatal("purposes not separated")
	}
	if bytes.Equal(d.Digest("ab", "c"), d.Digest("a", "bc")) {
		t.Fatal("ambiguous concatenation")
	}
	if bytes.Equal(d.Key("destination", "value"), d.Key("client", "value")) {
		t.Fatal("scope collision")
	}
	if _, err := NewDigester(nil); err == nil {
		t.Fatal("missing key accepted")
	}
}
