// Package hasher provides bounded bcrypt hashing, independent of account policy.
package hasher

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrBusy   = errors.New("hasher: hashing capacity exhausted")
	ErrLength = errors.New("hasher: plaintext must contain 1 to 72 bytes")
)

// Hasher supports concurrent use. New supplies two CPU slots and bcrypt cost 10.
// Empty stored hashes use a dummy hash so absent credentials still perform work.
type Hasher struct {
	slots chan struct{}
	dummy []byte
}

func New() (*Hasher, error) {
	dummy, err := bcrypt.GenerateFromPassword([]byte("non-account dummy credential"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Hasher{slots: make(chan struct{}, 2), dummy: dummy}, nil
}
func (h *Hasher) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case h.slots <- struct{}{}:
		return nil
	default:
		return ErrBusy
	}
}

// Hash never truncates inputs. Strength policy belongs to the caller.
func (h *Hasher) Hash(ctx context.Context, plain string) (string, error) {
	if len(plain) == 0 || len(plain) > 72 {
		return "", ErrLength
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer func() { <-h.slots }()
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err == nil {
		err = ctx.Err()
	}
	return string(hash), err
}

// Compare returns false for mismatches and malformed/unsupported stored hashes.
// Costs above 14 are rejected before work, bounding even imported hash settings.
// Cancellation is checked around bcrypt work; bcrypt cannot be interrupted mid-hash.
func (h *Hasher) Compare(ctx context.Context, plain, encoded string) (bool, error) {
	if len(plain) > 72 {
		return false, nil
	}
	hash := []byte(encoded)
	if encoded == "" {
		hash = h.dummy
	}
	if len(hash) != 60 {
		return false, nil
	}
	cost, err := bcrypt.Cost(hash)
	if err != nil || cost < bcrypt.DefaultCost || cost > 14 {
		return false, nil
	}
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer func() { <-h.slots }()
	match := bcrypt.CompareHashAndPassword(hash, []byte(plain)) == nil
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// The dummy hash equalizes work; it can never represent a credential.
	return encoded != "" && match, nil
}
