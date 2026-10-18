package entities

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/shared/enums"
)

// VerificationChallenge is owned by Identity. Destination is private contact data.
// CodeDigest holds an HMAC-SHA256 digest, never the original code. The verification
// service must bind it to this challenge and enforce consumption under a transaction.
type VerificationChallenge struct {
	bun.BaseModel `bun:"table:verification_challenges,alias:vc"`
	ID            uuid.UUID                 `bun:"id,pk,type:uuid"`
	UserID        *uuid.UUID                `bun:"user_id,type:uuid"`
	Purpose       enums.VerificationPurpose `bun:"purpose,notnull"`
	Channel       enums.VerificationChannel `bun:"channel,notnull"`
	Destination   string                    `bun:"destination,notnull" json:"-"`
	CodeDigest    []byte                    `bun:"code_digest,notnull" json:"-"`
	AttemptCount  int                       `bun:"attempt_count,notnull"`
	MaxAttempts   int                       `bun:"max_attempts,notnull"`
	ExpiresAt     time.Time                 `bun:"expires_at,notnull"`
	ConsumedAt    *time.Time                `bun:"consumed_at"`
	InvalidatedAt *time.Time                `bun:"invalidated_at"`
	CreatedAt     time.Time                 `bun:"created_at,notnull"`
}

// Validate checks structural storage invariants. Destination normalization, abuse
// limits, authorization, and atomic verification belong to the operation boundary.
func (c VerificationChallenge) Validate() error {
	if c.ID == uuid.Nil || (c.UserID != nil && *c.UserID == uuid.Nil) {
		return ErrInvalidChallengeIDs
	}
	if !c.Purpose.Valid() {
		return ErrUnsupportedVerificationPurpose
	}
	switch c.Purpose {
	case enums.PurposeVerifyEmail, enums.PurposeRegistration, enums.PurposeSignIn:
	case enums.PurposeLinkContact, enums.PurposeChangeContact, enums.PurposeRecovery:
		if c.UserID == nil {
			return ErrChallengeAccountRequired
		}
	}
	if !c.Channel.Valid() {
		return ErrUnsupportedVerificationChannel
	}
	if strings.TrimSpace(c.Destination) == "" || len(c.CodeDigest) != 32 {
		return ErrInvalidChallengeCredential
	}
	if c.MaxAttempts <= 0 || c.AttemptCount < 0 || c.AttemptCount > c.MaxAttempts {
		return ErrInvalidVerificationAttempts
	}
	if c.CreatedAt.IsZero() || !c.ExpiresAt.After(c.CreatedAt) {
		return ErrInvalidChallengeLifetime
	}
	if c.ConsumedAt != nil && (c.ConsumedAt.Before(c.CreatedAt) || !c.ConsumedAt.Before(c.ExpiresAt)) {
		return ErrInvalidChallengeConsumption
	}
	if c.InvalidatedAt != nil && c.InvalidatedAt.Before(c.CreatedAt) {
		return ErrInvalidChallengeInvalidation
	}
	if c.ConsumedAt != nil && c.InvalidatedAt != nil {
		return ErrConflictingChallengeState
	}
	return nil
}
