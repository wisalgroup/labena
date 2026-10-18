// Package token issues and validates UUID-based session access tokens using golang-jwt.
// Refresh credentials are opaque and are not JWTs.
package token

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalid = errors.New("token: invalid credential")
	ErrConfig  = errors.New("token: invalid access token configuration")
)

// Principal identifies a UUID-based account and login session. Authorization and
// revocation remain the caller's responsibility.
type Principal struct{ UserID, SessionID uuid.UUID }

type Access struct {
	key              []byte
	issuer, audience string
}
type claims struct {
	jwt.RegisteredClaims
	SessionID string `json:"sid"`
}

func NewAccess(key []byte, issuer, audience string) (*Access, error) {
	if len(key) != 32 || issuer == "" || audience == "" {
		return nil, ErrConfig
	}
	return &Access{append([]byte(nil), key...), issuer, audience}, nil
}
func (a *Access) Issue(principal Principal, expires time.Time) (string, error) {
	if principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil || !expires.After(time.Now()) {
		return "", ErrConfig
	}
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	c := claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: a.issuer, Audience: jwt.ClaimStrings{a.audience}, Subject: principal.UserID.String(), ID: id.String(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expires)}, SessionID: principal.SessionID.String()}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	token.Header["typ"] = "at+jwt"
	return token.SignedString(a.key)
}
func (a *Access) Parse(raw string) (Principal, error) {
	var principal Principal
	if len(raw) == 0 || len(raw) > 4096 {
		return principal, ErrInvalid
	}
	c := new(claims)
	parsed, err := jwt.ParseWithClaims(raw, c, func(t *jwt.Token) (any, error) {
		if t.Header["typ"] != "at+jwt" {
			return nil, ErrInvalid
		}
		return a.key, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(a.issuer), jwt.WithAudience(a.audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !parsed.Valid || c.IssuedAt == nil || c.ID == "" || c.ExpiresAt == nil || !c.ExpiresAt.After(c.IssuedAt.Time) {
		return principal, ErrInvalid
	}
	userID, err := uuid.Parse(c.Subject)
	if err != nil || userID == uuid.Nil {
		return principal, ErrInvalid
	}
	sessionID, err := uuid.Parse(c.SessionID)
	if err != nil || sessionID == uuid.Nil {
		return principal, ErrInvalid
	}
	return Principal{UserID: userID, SessionID: sessionID}, nil
}
