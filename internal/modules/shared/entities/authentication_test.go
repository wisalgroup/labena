package entities

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/pkg/apperror"
)

func TestAuthenticationRecords(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	id := uuid.MustParse("019a0000-0000-7000-8000-000000000001")
	identity := AuthIdentity{ID: id, UserID: id, Provider: enums.ProviderGoogle, ProviderSubject: "opaque-subject", CreatedAt: now}
	challenge := VerificationChallenge{ID: id, Purpose: enums.PurposeRegistration, Channel: enums.ChannelEmail, Destination: "private@example.com", CodeDigest: []byte(strings.Repeat("s", 32)), MaxAttempts: 5, CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
	expires := now.Add(time.Hour)
	session := Session{ID: id, UserID: id, Platform: enums.PlatformWeb, CreatedAt: now, LastUsedAt: now, ExpiresAt: &expires}
	token := SessionRefreshToken{ID: id, SessionID: id, TokenHash: []byte(strings.Repeat("s", 32)), CreatedAt: now, ExpiresAt: &expires}
	for _, record := range []interface{ Validate() error }{identity, challenge, session, token} {
		if err := record.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	// Unlimited lifetimes remain valid records; zero timestamps remain invalid.
	unlimitedSession, unlimitedToken := session, token
	unlimitedSession.ExpiresAt, unlimitedToken.ExpiresAt = nil, nil
	consumed := now.Add(365 * 24 * time.Hour)
	unlimitedToken.ConsumedAt = &consumed
	for _, record := range []interface{ Validate() error }{unlimitedSession, unlimitedToken} {
		if err := record.Validate(); err != nil {
			t.Fatal("unlimited record rejected", err)
		}
	}
	tests := []struct {
		name    string
		want    *apperror.Error
		invalid func() error
	}{
		{"provider", ErrUnsupportedAuthProvider, func() error { i := identity; i.Provider = "untrusted"; return i.Validate() }},
		{"subject", ErrMissingProviderSubject, func() error { i := identity; i.ProviderSubject = " "; return i.Validate() }},
		{"channel", ErrUnsupportedVerificationChannel, func() error { c := challenge; c.Channel = "push"; return c.Validate() }},
		{"purpose", ErrUnsupportedVerificationPurpose, func() error { c := challenge; c.Purpose = "unknown"; return c.Validate() }},
		{"contact owner", ErrChallengeAccountRequired, func() error { c := challenge; c.Purpose = enums.PurposeChangeContact; return c.Validate() }},
		{"attempt count", ErrInvalidVerificationAttempts, func() error { c := challenge; c.AttemptCount = 6; return c.Validate() }},
		{"zero attempt limit", ErrInvalidVerificationAttempts, func() error { c := challenge; c.MaxAttempts = 0; return c.Validate() }},
		{"plaintext code", ErrInvalidChallengeCredential, func() error { c := challenge; c.CodeDigest = []byte("123456"); return c.Validate() }},
		{"expired consumption", ErrInvalidChallengeConsumption, func() error { c := challenge; c.ConsumedAt = &c.ExpiresAt; return c.Validate() }},
		{"conflicting terminal states", ErrConflictingChallengeState, func() error { c := challenge; c.ConsumedAt = &now; c.InvalidatedAt = &now; return c.Validate() }},
		{"session platform", ErrUnsupportedSessionPlatform, func() error { s := session; s.Platform = "unknown"; return s.Validate() }},
		{"session lifetime", ErrInvalidSessionLifetime, func() error { s := session; s.ExpiresAt = &now; return s.Validate() }},
		{"unlimited session activity", ErrInvalidSessionLifetime, func() error { s := unlimitedSession; s.LastUsedAt = now.Add(-time.Second); return s.Validate() }},
		{"zero session expiry", ErrInvalidSessionLifetime, func() error { s := unlimitedSession; s.ExpiresAt = new(time.Time); return s.Validate() }},
		{"zero refresh expiry", ErrInvalidRefreshTokenLifetime, func() error { r := unlimitedToken; r.ExpiresAt = new(time.Time); return r.Validate() }},
		{"token hash", ErrInvalidRefreshTokenCredential, func() error { r := token; r.TokenHash = nil; return r.Validate() }},
		{"token expiry", ErrInvalidRefreshTokenConsumption, func() error { r := token; r.ConsumedAt = r.ExpiresAt; return r.Validate() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("validate: %w", tc.invalid())
			var app *apperror.Error
			if !errors.Is(err, tc.want) || !errors.As(err, &app) {
				t.Fatalf("expected sentinel %v, got %v", tc.want, err)
			}
			if app.Code() != tc.want.Code() {
				t.Fatal("lost error classification")
			}
		})
	}
	// Explicit wire responses must not leak credential digests or private destinations.
	for _, record := range []any{identity, challenge, token} {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"opaque-subject", "private@example.com", "CodeDigest", "TokenHash"} {
			if strings.Contains(string(data), secret) {
				t.Fatalf("private field leaked: %s", secret)
			}
		}
	}
}
