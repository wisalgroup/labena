package hasher

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashAndCompare(t *testing.T) {
	h, err := New()
	if err != nil {
		t.Fatal(err)
	}
	plain := "a long memorable password 🌍"
	a, err := h.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("hashes are not salted")
	}
	if cost, err := bcrypt.Cost([]byte(a)); err != nil || cost != bcrypt.DefaultCost {
		t.Fatal(cost, err)
	}
	for _, tc := range []struct {
		plain, hash string
		match       bool
	}{{plain, a, true}, {"wrong", a, false}, {plain, "", false}, {"non-account dummy credential", "", false}, {plain, "invalid", false}, {plain, strings.Replace(a, "$10$", "$31$", 1), false}, {strings.Repeat("x", 73), a, false}} {
		match, err := h.Compare(t.Context(), tc.plain, tc.hash)
		if err != nil || match != tc.match {
			t.Fatal(match, err)
		}
	}
	if _, err := h.Hash(t.Context(), strings.Repeat("界", 25)); !errors.Is(err, ErrLength) {
		t.Fatal("overlength Unicode accepted", err)
	}
	max := strings.Repeat("x", 72)
	hash, err := h.Hash(t.Context(), max)
	if err != nil {
		t.Fatal(err)
	}
	if match, err := h.Compare(t.Context(), max+"ignored suffix", hash); err != nil || match {
		t.Fatal("password truncated", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := h.Hash(ctx, plain); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	h.slots <- struct{}{}
	h.slots <- struct{}{}
	if _, err := h.Hash(t.Context(), plain); !errors.Is(err, ErrBusy) {
		t.Fatal("overload was not bounded", err)
	}
	<-h.slots
	<-h.slots
}
