// Package otp generates decimal codes and keyed proof digests. It does not own
// storage, expiration, retries, rate limits, or account eligibility.
package otp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

// Generate returns a uniformly random six-digit code, including leading zeroes.
func Generate() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// Digester copies its key and supports concurrent use.
type Digester struct{ key []byte }

func NewDigester(key []byte) (*Digester, error) {
	if len(key) < 32 {
		return nil, errors.New("otp: key must contain at least 32 bytes")
	}
	return &Digester{key: append([]byte(nil), key...)}, nil
}

// Key separates purposes, including proof storage and private rate-limit keys.
func (d *Digester) Key(scope, value string) []byte {
	mac := hmac.New(sha256.New, d.key)
	mac.Write([]byte(scope))
	mac.Write([]byte{0})
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

// Digest binds an ordered set of proof fields without ambiguous concatenation.
// Callers must use the same field order during creation and verification.
func (d *Digester) Digest(fields ...string) []byte {
	data, _ := json.Marshal(fields)
	return d.Key("code", string(data))
}
