package service

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
)

// Device describes one login session; Name is a display label, not a trusted identity.
type Device struct {
	Platform enums.SessionPlatform
	Name     string
}

// ProofInput contains a client-supplied code. Purpose is always chosen by the operation.
// ClientIP must come from the trusted transport boundary, never a request body.
type ProofInput struct {
	ChallengeID uuid.UUID
	Code        string
	ClientIP    string
}

// SignInInput is used only for OTP sign-in (including first-time registration).
type SignInInput struct {
	Name   string
	Proof  ProofInput
	Device Device
}

// SocialSignInInput carries transport/device context for social login.
// Provider sign-in accepts neither an OTP proof nor a caller-selected initial name.
type SocialSignInInput struct {
	ClientIP string
	Device   Device
}

// Signup names are optional; proof confirmation never renames an existing account.
func signupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	if utf8.RuneCountInString(name) > 100 || strings.ContainsAny(name, "\r\n") {
		return "", entities.ErrInvalidUserName
	}
	return name, nil
}

// Tokens are returned once. Never log, persist or serialize them outside the
// successful authentication response. A lost rotation response requires sign-in.
type Tokens struct {
	AccessToken      string     `json:"access_token"`
	RefreshToken     string     `json:"refresh_token"`
	TokenType        string     `json:"token_type"`
	ExpiresIn        int64      `json:"expires_in"`
	RefreshExpiresAt *time.Time `json:"refresh_expires_at"` // Nil means no time-based expiry.
	SessionID        uuid.UUID  `json:"session_id"`
}

// UpdateProfileInput uses pointers for partial updates. Add future editable fields
// explicitly; identity, contact verification and account status are not profile edits.
type UpdateProfileInput struct{ Name *string }
