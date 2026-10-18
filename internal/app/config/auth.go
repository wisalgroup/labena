package config

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
)

// AuthConfig is the single environment schema for authentication settings.
type AuthConfig struct {
	GoogleEnabled             bool          `env:"AUTH_GOOGLE_ENABLED" envDefault:"false"`
	GoogleClientID            string        `env:"AUTH_GOOGLE_CLIENT_ID"`
	GoogleTrustedPresenterIDs []string      `env:"AUTH_GOOGLE_TRUSTED_PRESENTER_IDS" envSeparator:","` // Additional azp clients; never additional audiences.
	Enabled                   bool          `env:"AUTH_ENABLED" envDefault:"false"`
	AccessTokenKey            string        `env:"AUTH_ACCESS_TOKEN_KEY" json:"-"`
	TokenIssuer               string        `env:"AUTH_TOKEN_ISSUER" envDefault:"go-modular"`
	TokenAudience             string        `env:"AUTH_TOKEN_AUDIENCE" envDefault:"go-modular-api"`
	AccessTTL                 time.Duration `env:"AUTH_ACCESS_TTL" envDefault:"168h"` // 7 days
	RefreshTTL                time.Duration `env:"AUTH_REFRESH_TTL" envDefault:"0"`   // Zero disables refresh-token expiry; rotation and revocation still apply.
	SessionTTL                time.Duration `env:"AUTH_SESSION_TTL" envDefault:"0"`   // Zero disables the absolute session deadline.
	PhoneVerificationEnabled  bool          `env:"AUTH_PHONE_VERIFICATION_ENABLED" envDefault:"false"`
	VerificationKey           string        `env:"AUTH_VERIFICATION_KEY" json:"-"`
}

func (c AuthConfig) Validate() error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("config: authentication: %w", err)
	}
	return nil
}

// VerificationSecret decodes an independently generated 32-byte base64 secret.
// Share it across replicas. Rotation invalidates outstanding challenges and rate-limit keys.
func (c AuthConfig) VerificationSecret() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(c.VerificationKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("identity: AUTH_VERIFICATION_KEY must be a base64-encoded 32-byte secret")
	}
	return key, nil
}

func (c AuthConfig) AccessSecret() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(c.AccessTokenKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("identity: AUTH_ACCESS_TOKEN_KEY must be a base64-encoded 32-byte secret")
	}
	verification, err := c.VerificationSecret()
	if err != nil {
		return nil, err
	}
	if bytes.Equal(key, verification) {
		return nil, errors.New("identity: access token and verification secrets must be different")
	}
	return key, nil
}

// validate checks the environment settings and delegates session policy to its owner.
func (c AuthConfig) validate() error {
	if c.GoogleEnabled && (!c.Enabled || !strings.HasSuffix(c.GoogleClientID, ".apps.googleusercontent.com") || strings.ContainsAny(c.GoogleClientID, " \t\r\n")) {
		return errors.New("identity: Google sign-in requires authentication and a Google OAuth client ID")
	}
	if c.GoogleEnabled {
		for _, id := range c.GoogleTrustedPresenterIDs {
			if !strings.HasSuffix(id, ".apps.googleusercontent.com") || strings.ContainsAny(id, " \t\r\n") {
				return errors.New("identity: AUTH_GOOGLE_TRUSTED_PRESENTER_IDS must contain Google OAuth client IDs")
			}
		}
	}
	if c.Enabled {
		if _, err := c.AccessSecret(); err != nil {
			return err
		}
		if strings.TrimSpace(c.TokenIssuer) == "" || strings.TrimSpace(c.TokenAudience) == "" {
			return errors.New("identity: token issuer and audience are required")
		}
		if err := c.SessionPolicy().Validate(); err != nil {
			return fmt.Errorf("identity: invalid AUTH_ACCESS_TTL, AUTH_REFRESH_TTL or AUTH_SESSION_TTL: %w", err)
		}
	}
	if c.PhoneVerificationEnabled {
		return errors.New("identity: phone verification is not implemented; keep AUTH_PHONE_VERIFICATION_ENABLED=false")
	}

	return nil
}

// SessionPolicy applies Identity's session policy to the configured lifetimes.
func (c AuthConfig) SessionPolicy() domain.SessionPolicy {
	return domain.SessionPolicy{
		AccessTokenLifetime:     c.AccessTTL,
		RefreshTokenLifetime:    c.RefreshTTL,
		AbsoluteSessionLifetime: c.SessionTTL,
		AllowConcurrentSessions: domain.AllowConcurrentSessions,
	}
}
