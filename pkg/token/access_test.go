package token

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestAccessTokenValidation(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	access, err := NewAccess(key, "issuer", "audience")
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{UserID: uuid.New(), SessionID: uuid.New()}
	raw, err := access.Issue(principal, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := access.Parse(raw)
	if err != nil || parsed != principal {
		t.Fatal(parsed, err)
	}
	other, _ := NewAccess(bytes.Repeat([]byte{2}, 32), "issuer", "audience")
	if _, err := other.Parse(raw); !errors.Is(err, ErrInvalid) {
		t.Fatal("wrong key accepted", err)
	}
	for _, name := range []string{"algorithm", "issuer", "audience", "expired", "missing expiry", "future issued", "missing issued", "subject", "session", "type", "signature"} {
		t.Run(name, func(t *testing.T) {
			c := claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "issuer", Audience: []string{"audience"}, Subject: principal.UserID.String(), ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(time.Now().Add(-time.Second)), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}, SessionID: principal.SessionID.String()}
			method := jwt.SigningMethod(jwt.SigningMethodHS256)
			signingKey := key
			typ := "at+jwt"
			switch name {
			case "algorithm":
				method = jwt.SigningMethodHS512
			case "issuer":
				c.Issuer = "other"
			case "audience":
				c.Audience = []string{"other"}
			case "expired":
				c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
			case "missing expiry":
				c.ExpiresAt = nil
			case "future issued":
				c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
			case "missing issued":
				c.IssuedAt = nil
			case "subject":
				c.Subject = uuid.Nil.String()
			case "session":
				c.SessionID = uuid.Nil.String()
			case "type":
				typ = "JWT"
			case "signature":
				signingKey = bytes.Repeat([]byte{3}, 32)
			}
			token := jwt.NewWithClaims(method, c)
			token.Header["typ"] = typ
			raw, err := token.SignedString(signingKey)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := access.Parse(raw); !errors.Is(err, ErrInvalid) {
				t.Fatal("accepted invalid token", err)
			}
		})
	}
}
