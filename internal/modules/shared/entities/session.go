package entities

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/shared/enums"
)

// Session is an authenticated login owned by Identity, independent of device identity.
// Refresh credentials live exclusively in SessionRefreshToken; this ID is their family.
type Session struct {
	bun.BaseModel `bun:"table:sessions,alias:s"`
	ID            uuid.UUID             `bun:"id,pk,type:uuid"`
	UserID        uuid.UUID             `bun:"user_id,type:uuid,notnull"`
	Platform      enums.SessionPlatform `bun:"platform,notnull"`
	DeviceName    *string               `bun:"device_name"`
	CreatedAt     time.Time             `bun:"created_at,notnull"`
	LastUsedAt    time.Time             `bun:"last_used_at,notnull"`
	ExpiresAt     *time.Time            `bun:"expires_at"` // Nil means no absolute session deadline.
	RevokedAt     *time.Time            `bun:"revoked_at"`
}

// Validate checks stored state. Authorization must also check account/session status
// and expiry at request time; this method does not establish an authenticated session.
func (s Session) Validate() error {
	if s.ID == uuid.Nil || s.UserID == uuid.Nil {
		return ErrInvalidSessionIDs
	}
	if !s.Platform.Valid() {
		return ErrUnsupportedSessionPlatform
	}
	if s.CreatedAt.IsZero() || s.LastUsedAt.Before(s.CreatedAt) {
		return ErrInvalidSessionLifetime
	}
	if s.ExpiresAt != nil && (!s.ExpiresAt.After(s.CreatedAt) || !s.LastUsedAt.Before(*s.ExpiresAt)) {
		return ErrInvalidSessionLifetime
	}
	if s.RevokedAt != nil && s.RevokedAt.Before(s.CreatedAt) {
		return ErrInvalidSessionRevocation
	}
	return nil
}

// SessionRefreshToken is owned by Identity. TokenHash stores a SHA-256 digest of
// a cryptographically random high-entropy credential, never a password or OTP.
// Consumed rows are retained to detect reuse; rotation must lock the owning session.
type SessionRefreshToken struct {
	bun.BaseModel `bun:"table:session_refresh_tokens,alias:rt"`
	ID            uuid.UUID  `bun:"id,pk,type:uuid"`
	SessionID     uuid.UUID  `bun:"session_id,type:uuid,notnull"`
	TokenHash     []byte     `bun:"token_hash,notnull" json:"-"`
	CreatedAt     time.Time  `bun:"created_at,notnull"`
	ExpiresAt     *time.Time `bun:"expires_at"` // Nil means valid until consumed or the session ends.
	ConsumedAt    *time.Time `bun:"consumed_at"`
}

// Validate checks the record; the session service must also enforce the parent
// session's absolute expiry and revocation state in the rotation transaction.
func (t SessionRefreshToken) Validate() error {
	if t.ID == uuid.Nil || t.SessionID == uuid.Nil || len(t.TokenHash) != 32 {
		return ErrInvalidRefreshTokenCredential
	}
	if t.CreatedAt.IsZero() || (t.ExpiresAt != nil && !t.ExpiresAt.After(t.CreatedAt)) {
		return ErrInvalidRefreshTokenLifetime
	}
	if t.ConsumedAt != nil && (t.ConsumedAt.Before(t.CreatedAt) || (t.ExpiresAt != nil && !t.ConsumedAt.Before(*t.ExpiresAt))) {
		return ErrInvalidRefreshTokenConsumption
	}
	return nil
}
