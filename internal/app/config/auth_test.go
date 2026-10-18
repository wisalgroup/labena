package config

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestVerificationConfiguration(t *testing.T) {
	cfg := Defaults()
	cfg.Auth.Enabled = true
	cfg.Auth.AccessTokenKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled without delivery and storage")
	}
	cfg.Database.DSN = "postgres://localhost/test"
	cfg.Auth.VerificationKey = "private-invalid-secret"
	if err := cfg.Validate(); err == nil || strings.Contains(err.Error(), cfg.Auth.VerificationKey) {
		t.Fatal("invalid key exposed or accepted", err)
	}
	cfg.Auth.VerificationKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	key, err := cfg.Auth.VerificationSecret()
	if err != nil || len(key) != 32 {
		t.Fatal(err)
	}
}

func TestPhoneVerificationIsExplicitlyDisabled(t *testing.T) {
	cfg := Defaults()
	if cfg.Auth.PhoneVerificationEnabled {
		t.Fatal("phone verification enabled by default")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Auth.PhoneVerificationEnabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "AUTH_PHONE_VERIFICATION_ENABLED=false") {
		t.Fatal("unsupported SMS flow accepted", err)
	}
}

func TestAuthenticationConfiguration(t *testing.T) {
	cfg := Defaults()
	if cfg.Auth.AccessTTL != 7*24*time.Hour || cfg.Auth.RefreshTTL != 0 || cfg.Auth.SessionTTL != 0 {
		t.Fatal("expected seven-day access tokens and unlimited default refresh/session lifetimes")
	}
	cfg.Auth.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("authentication without verification")
	}
	cfg.Database.DSN = "postgres://localhost/test"
	cfg.Auth.VerificationKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("v", 32)))
	cfg.Auth.AccessTokenKey = cfg.Auth.VerificationKey
	if err := cfg.Validate(); err == nil {
		t.Fatal("reused key")
	}
	cfg.Auth.AccessTokenKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Auth.SessionTTL = 30 * 24 * time.Hour
	cfg.Auth.RefreshTTL = cfg.Auth.SessionTTL + 1
	if err := cfg.Validate(); err == nil {
		t.Fatal("refresh can outlive session")
	}
}

func TestGoogleConfiguration(t *testing.T) {
	cfg := Defaults()
	cfg.Auth.GoogleEnabled = true
	cfg.Auth.GoogleClientID = "example.apps.googleusercontent.com"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Google enabled without authentication")
	}
	cfg.Auth.Enabled = true
	cfg.Database.DSN = "postgres://localhost/test"
	cfg.Auth.VerificationKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("v", 32)))
	cfg.Auth.AccessTokenKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Auth.GoogleClientID = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("missing audience accepted")
	}
}

func TestGooglePresenterConfiguration(t *testing.T) {
	const android = "android.apps.googleusercontent.com"
	const ios = "ios.apps.googleusercontent.com"
	for _, tc := range []struct {
		name    string
		value   string
		want    []string
		invalid bool
	}{
		{name: "default"},
		{name: "one presenter", value: android, want: []string{android}},
		{name: "multiple presenters", value: android + "," + ios, want: []string{android, ios}},
		{name: "invalid client ID", value: "not-a-google-client", invalid: true},
		{name: "empty entry", value: android + ",," + ios, invalid: true},
		{name: "trailing comma", value: android + ",", invalid: true},
		{name: "whitespace", value: android + ", " + ios, invalid: true},
		{name: "wrong suffix", value: android + ".example.com", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := FromEnvironment(map[string]string{
				"AUTH_GOOGLE_ENABLED":               "true",
				"AUTH_GOOGLE_CLIENT_ID":             "web.apps.googleusercontent.com",
				"AUTH_GOOGLE_TRUSTED_PRESENTER_IDS": tc.value,
				"AUTH_ENABLED":                      "true",
				"AUTH_VERIFICATION_KEY":             base64.StdEncoding.EncodeToString([]byte(strings.Repeat("v", 32))),
				"AUTH_ACCESS_TOKEN_KEY":             base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))),
				"DATABASE_URL":                      "postgres://localhost/test",
			})
			if tc.invalid {
				if err == nil || !strings.Contains(err.Error(), "AUTH_GOOGLE_TRUSTED_PRESENTER_IDS") {
					t.Fatalf("expected presenter configuration error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(cfg.Auth.GoogleTrustedPresenterIDs, tc.want) {
				t.Fatalf("presenters=%v, want %v", cfg.Auth.GoogleTrustedPresenterIDs, tc.want)
			}
			if cfg.Auth.GoogleClientID != "web.apps.googleusercontent.com" {
				t.Fatal("presenter configuration changed the audience")
			}
		})
	}
}

func TestAuthenticationResources(t *testing.T) {
	cfg := Defaults()
	cfg.Auth.Enabled = true
	cfg.Database.DSN = "postgres://localhost/test"
	cfg.Auth.VerificationKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("v", 32)))
	cfg.Auth.AccessTokenKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Config){
		"mail":              func(c *Config) { c.Mail.Host = "" },
		"database":          func(c *Config) { c.Database.DSN = "" },
		"limiter URL":       func(c *Config) { c.RateLimit.RedisURL = "invalid" },
		"limiter isolation": func(c *Config) { c.RateLimit.RedisURL = c.Queue.RedisURL },
		"challenge key":     func(c *Config) { c.Auth.VerificationKey = "" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := cfg
			change(&invalid)
			if err := invalid.Validate(); err == nil {
				t.Fatal("authentication accepted missing resource")
			}
		})
	}
}
