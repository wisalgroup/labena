package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// NewOpaque returns a random 256-bit credential and its SHA-256 digest. Persist
// only the digest; the raw credential must not appear in logs or URLs.
func NewOpaque() (string, []byte, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(secret[:])
	return base64.RawURLEncoding.EncodeToString(secret[:]), digest[:], nil
}
func HashOpaque(raw string) ([]byte, error) {
	if len(raw) != 43 {
		return nil, ErrInvalid
	}
	secret, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(secret) != 32 {
		return nil, ErrInvalid
	}
	hash := sha256.Sum256(secret)
	return hash[:], nil
}
