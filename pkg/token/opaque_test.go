package token

import (
	"bytes"
	"strings"
	"testing"
)

func TestOpaque(t *testing.T) {
	first, hash, err := NewOpaque()
	if err != nil {
		t.Fatal(err)
	}
	next, nextHash, err := NewOpaque()
	if err != nil {
		t.Fatal(err)
	}
	if first == next || bytes.Equal(hash, nextHash) || len(first) != 43 || len(hash) != 32 {
		t.Fatal("invalid opaque credentials")
	}
	actual, err := HashOpaque(first)
	if err != nil || !bytes.Equal(actual, hash) {
		t.Fatal(err)
	}
	for _, raw := range []string{"", first + "=", strings.Repeat("!", 43)} {
		if _, err := HashOpaque(raw); err != ErrInvalid {
			t.Fatal("invalid encoding accepted", err)
		}
	}
}
