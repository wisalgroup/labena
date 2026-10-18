package domain

import (
	"time"

	"github.com/wisalgroup/labena/pkg/apperror"
)

var (
	ErrVerificationUnavailable = apperror.Unavailable("Authentication rate limiting is temporarily unavailable.")
	ErrVerificationConfig      = apperror.Internal("identity: invalid verification configuration")
	ErrVerificationInvalid     = apperror.Validation("The verification code is invalid or no longer available.")
	ErrVerificationLimited     = apperror.RateLimited("Too many verification requests. Try again later.")
	ErrVerificationDelivery    = apperror.Unavailable("Verification delivery is temporarily unavailable.")
)

// VerificationCodePolicy governs one-time email codes for signup and sign-in. Each challenge
// records its expiry and attempt limit when issued. Zero values are invalid;
// start with DefaultVerificationCodePolicy and override individual fields.
type VerificationCodePolicy struct {
	// CodeLifetime is the duration from issuance until a code expires, including
	// delivery time. It must be positive and at most one hour. The default is
	// ten minutes. Changing it affects newly issued challenges only.
	CodeLifetime time.Duration

	// ResendCooldown is the minimum duration between issuing codes for the same
	// normalized email address and purpose, enforced across application instances.
	// It must be positive and no greater than CodeLifetime; the default is one minute.
	ResendCooldown time.Duration

	// MaxAttemptsPerCode caps evaluated submissions for one unexpired challenge,
	// including the successful submission. Incorrect codes consume an attempt;
	// exhausted challenges reject further submissions. The limit is saved with
	// each new challenge, so changing it does not alter existing challenges.
	// Valid values are 1 through 10; the default is 5. Resending does not reset
	// the separate shared budgets governed by AuthRateLimitPolicy.
	MaxAttemptsPerCode int
}

// DefaultVerificationCodePolicy returns the defaults for newly issued codes.
func DefaultVerificationCodePolicy() VerificationCodePolicy {
	return VerificationCodePolicy{
		CodeLifetime:       10 * time.Minute,
		ResendCooldown:     time.Minute,
		MaxAttemptsPerCode: 5,
	}
}

// Validate checks code lifetime, resend spacing and the per-challenge attempt cap.
// NewAuthService also checks compatibility with AuthRateLimitPolicy.
func (p VerificationCodePolicy) Validate() error {
	if p.CodeLifetime <= 0 || p.CodeLifetime > time.Hour ||
		p.ResendCooldown <= 0 || p.ResendCooldown > p.CodeLifetime ||
		p.MaxAttemptsPerCode < 1 || p.MaxAttemptsPerCode > 10 {
		return ErrVerificationConfig
	}
	return nil
}
