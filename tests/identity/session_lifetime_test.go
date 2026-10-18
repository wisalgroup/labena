package identity_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wisalgroup/labena/internal/app/config"
	identityhttp "github.com/wisalgroup/labena/internal/modules/identity/delivery/http"
	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/pkg/pagination"
	"github.com/wisalgroup/labena/pkg/token"
)

func TestSessionLifetimeSettings(t *testing.T) {
	for _, tc := range []struct {
		name             string
		refresh, session time.Duration
		valid            bool
	}{
		{"unlimited", 0, 0, true},
		{"sliding refresh only", 7 * 24 * time.Hour, 0, true},
		{"absolute deadline only", 0, 30 * 24 * time.Hour, true},
		{"both limits", 7 * 24 * time.Hour, 30 * 24 * time.Hour, true},
		{"negative refresh", -time.Second, 0, false},
		{"negative session", 0, -time.Second, false},
		{"refresh shorter than access", time.Minute, 0, false},
		{"session shorter than access", 0, time.Minute, false},
		{"session shorter than refresh", 48 * time.Hour, 24 * time.Hour, false},
		{"refresh above maximum", 31 * 24 * time.Hour, 0, false},
		{"session above maximum", 0, 91 * 24 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := domain.SessionPolicy{AccessTokenLifetime: 15 * time.Minute, RefreshTokenLifetime: tc.refresh, AbsoluteSessionLifetime: tc.session, AllowConcurrentSessions: domain.AllowConcurrentSessions}
			if err := policy.Validate(); (err == nil) != tc.valid {
				t.Fatal("policy validation", err)
			}
			cfg := config.Defaults().Auth
			cfg.AccessTTL = policy.AccessTokenLifetime
			cfg.Enabled = true
			cfg.VerificationKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
			cfg.AccessTokenKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
			cfg.RefreshTTL, cfg.SessionTTL = tc.refresh, tc.session
			if cfg.SessionPolicy() != policy {
				t.Fatal("configuration did not preserve the selected policy", cfg.SessionPolicy())
			}
			if err := cfg.Validate(); (err == nil) != tc.valid {
				t.Fatal("configuration validation", err)
			} else if !tc.valid && !errors.Is(err, domain.ErrSessionConfig) {
				t.Fatal("configuration lost the policy failure", err)
			}
		})
	}
}

func TestAccessLifetimeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		lifetime time.Duration
		valid    bool
	}{
		{0, false}, {time.Minute - 1, false}, {time.Minute, true},
		{time.Hour, true}, {7 * 24 * time.Hour, true}, {7*24*time.Hour + 1, false},
	} {
		cfg := config.Defaults().Auth
		cfg.Enabled = true
		cfg.VerificationKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
		cfg.AccessTokenKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
		cfg.AccessTTL = tc.lifetime
		if err := cfg.Validate(); (err == nil) != tc.valid {
			t.Fatalf("access lifetime %s: %v", tc.lifetime, err)
		}
	}
}

func TestSessionLifetimeModesPostgres(t *testing.T) {
	for _, tc := range []struct {
		name             string
		refresh, session time.Duration
	}{
		{"unlimited", 0, 0},
		{"sliding refresh only", 7 * 24 * time.Hour, 0},
		{"absolute deadline only", 0, 30 * 24 * time.Hour},
		{"both limits", 7 * 24 * time.Hour, 30 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := verificationDB(t)
			repos := newRepositories(db)
			sender := &codeDelivery{codes: map[string]string{}}
			access, err := token.NewAccess(bytes.Repeat([]byte{3}, 32), "test", "api")
			if err != nil {
				t.Fatal(err)
			}
			options := authenticationOptions(t, repos, bytes.Repeat([]byte{4}, 32), sender)
			options.Access = access
			options.SessionPolicy = domain.SessionPolicy{AccessTokenLifetime: 15 * time.Minute, RefreshTokenLifetime: tc.refresh, AbsoluteSessionLifetime: tc.session, AllowConcurrentSessions: true}
			svc, err := service.NewAuthService(options)
			if err != nil {
				t.Fatal(err)
			}
			login := func(email string) *service.Tokens {
				t.Helper()
				id, err := svc.RequestSignInCode(t.Context(), email, "192.0.2.1")
				if err != nil {
					t.Fatal(err)
				}
				result, err := svc.SignInWithCode(t.Context(), service.SignInInput{Proof: service.ProofInput{ChallengeID: id, Code: sender.code(email), ClientIP: "192.0.2.1"}, Device: service.Device{Platform: enums.PlatformWeb}})
				if err != nil {
					t.Fatal(err)
				}
				return result.Tokens
			}
			credentials := login("lifetime@example.com")
			principal, err := svc.Authenticate(t.Context(), credentials.AccessToken)
			if err != nil {
				t.Fatal(err)
			}
			page, err := svc.ListSessions(t.Context(), principal.UserID, pagination.NewPagination(1, 20))
			if err != nil || len(page.Items) != 1 {
				t.Fatal("active session missing", page, err)
			}
			session := page.Items[0]
			if (session.ExpiresAt == nil) != (tc.session == 0) || (credentials.RefreshExpiresAt == nil) != (tc.refresh == 0 && tc.session == 0) {
				t.Fatal("incorrect nullable expiry", session.ExpiresAt, credentials.RefreshExpiresAt)
			}
			if credentials.ExpiresIn <= 0 || credentials.ExpiresIn > 900 {
				t.Fatal("access token must remain short-lived", credentials.ExpiresIn)
			}

			if tc.session == 0 && tc.refresh == 0 {
				// A dormant session remains usable beyond the previous absolute limit.
				past := time.Now().Add(-400 * 24 * time.Hour)
				if _, err := db.NewUpdate().Model((*entities.Session)(nil)).Set("created_at = ?, last_used_at = ?", past, past).Where("id = ?", session.ID).Exec(t.Context()); err != nil {
					t.Fatal(err)
				}
				if _, err := db.NewUpdate().Model((*entities.SessionRefreshToken)(nil)).Set("created_at = ?", past).Where("session_id = ?", session.ID).Exec(t.Context()); err != nil {
					t.Fatal(err)
				}
				for _, wire := range []struct {
					value any
					field string
				}{
					{identityhttp.SessionResponse{ExpiresAt: session.ExpiresAt}, "expires_at"},
					{identityhttp.TokensResponse{RefreshExpiresAt: credentials.RefreshExpiresAt}, "refresh_expires_at"},
				} {
					encoded, err := json.Marshal(wire.value)
					if err != nil {
						t.Fatal(err)
					}
					var fields map[string]json.RawMessage
					if err := json.Unmarshal(encoded, &fields); err != nil {
						t.Fatal(err)
					}
					if string(fields[wire.field]) != "null" {
						t.Fatal("unlimited expiry must serialize explicitly as null", string(encoded))
					}
				}
			}
			if tc.session > 0 {
				// Rotation must respect a nearing absolute deadline, even with refresh expiry disabled.
				deadline := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second)
				if _, err := db.NewUpdate().Model((*entities.Session)(nil)).Set("expires_at = ?", deadline).Where("id = ?", session.ID).Exec(t.Context()); err != nil {
					t.Fatal(err)
				}
				session.ExpiresAt = &deadline
			}
			next, err := svc.Refresh(t.Context(), credentials.RefreshToken)
			if err != nil {
				t.Fatal("rotation failed", err)
			}
			if _, err := svc.Authenticate(t.Context(), next.AccessToken); err != nil {
				t.Fatal(err)
			}
			if tc.session > 0 {
				if next.RefreshExpiresAt == nil || !next.RefreshExpiresAt.Equal(*session.ExpiresAt) || next.ExpiresIn > 120 {
					t.Fatal("rotation exceeded session deadline", next)
				}
			} else if tc.refresh > 0 {
				if next.RefreshExpiresAt == nil || time.Until(*next.RefreshExpiresAt) < tc.refresh-time.Minute {
					t.Fatal("refresh lifetime did not slide", next.RefreshExpiresAt)
				}
			} else if next.RefreshExpiresAt != nil {
				t.Fatal("rotation imposed an expiry")
			}

			if count, err := svc.PruneSessions(t.Context(), 1); err != nil || count != 0 {
				t.Fatal("cleanup removed active session history", count, err)
			}
			// Replay still revokes the newest credential, including unlimited sessions.
			if _, err := svc.Refresh(t.Context(), credentials.RefreshToken); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatal("replay accepted", err)
			}
			if _, err := svc.Authenticate(t.Context(), next.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatal("replay did not revoke access", err)
			}
			if _, err := svc.Refresh(t.Context(), next.RefreshToken); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatal("revoked refresh accepted", err)
			}
			for i := 0; i < 3; i++ {
				count, err := svc.PruneSessions(t.Context(), 1)
				if err != nil || count > 2 {
					t.Fatal("cleanup exceeded its batch size", count, err)
				}
			}
			if count, err := db.NewSelect().Model((*entities.Session)(nil)).Where("id = ?", session.ID).Count(t.Context()); err != nil || count != 0 {
				t.Fatal("revoked session not cleaned", count, err)
			}
			if count, err := db.NewSelect().Model((*entities.SessionRefreshToken)(nil)).Where("session_id = ?", session.ID).Count(t.Context()); err != nil || count != 0 {
				t.Fatal("revoked history not cleaned", count, err)
			}

			loggedIn := login("logout@example.com")
			who, err := svc.Authenticate(t.Context(), loggedIn.AccessToken)
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.RevokeSession(t.Context(), who.UserID, who.SessionID); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Authenticate(t.Context(), loggedIn.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatal("logout left access active", err)
			}
			if _, err := svc.Refresh(t.Context(), loggedIn.RefreshToken); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatal("logout left refresh active", err)
			}
		})
	}
}
