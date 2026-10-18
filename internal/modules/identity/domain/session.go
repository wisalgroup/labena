package domain

import (
	"time"

	"github.com/wisalgroup/labena/pkg/apperror"
)

// AllowConcurrentSessions controls subsequent sign-ins. Changing it does not
// revoke existing sessions; sign-in applies it atomically under the account lock.
const AllowConcurrentSessions = true

var (
	ErrUnauthenticated = apperror.Unauthenticated("Authentication is required or the credential is no longer valid.")
	ErrSessionNotFound = apperror.NotFound("The session was not found.")
	ErrSessionConfig   = apperror.Internal("identity: invalid session configuration")
	ErrSessionInput    = apperror.Validation("Provide a supported platform and a device name of at most 100 characters.")
)

// SessionPolicy governs token issuance and authenticated-session lifetime.
// Revocation, account status and refresh-token reuse can end access before expiry.
// Access tokens always expire. Zero refresh/session durations disable their
// respective time limits; app configuration supplies the configured lifetimes.
type SessionPolicy struct {
	// AccessTokenLifetime is the maximum duration of each issued access token,
	// capped by the session's remaining lifetime. It must be between one minute
	// and seven days, inclusive. The application's default is seven days.
	AccessTokenLifetime time.Duration

	// RefreshTokenLifetime is the duration granted to a new refresh token at
	// sign-in and on each successful rotation, capped by the session's remaining
	// lifetime when the session has a deadline. Zero, the default, disables the
	// refresh token's own expiry; logout and other revocations still invalidate it.
	// A positive value must be at least AccessTokenLifetime and at most 30 days.
	// Each rotation starts this duration again, creating an inactivity limit.
	// Rotation always consumes the previous token, even when expiry is disabled.
	RefreshTokenLifetime time.Duration

	// AbsoluteSessionLifetime is the maximum duration from session creation to
	// final expiry. Activity and token rotation never extend this deadline.
	// Zero, the default, means there is no absolute deadline. A positive value
	// must be at least AccessTokenLifetime and any positive RefreshTokenLifetime,
	// and at most 90 days. A new sign-in is required after a configured deadline.
	// Removing the deadline does not disable logout, revocation or account checks.
	AbsoluteSessionLifetime time.Duration

	// AllowConcurrentSessions controls whether a successful sign-in retains the
	// account's other sessions. False revokes them atomically while creating the
	// new session; true allows sessions on multiple devices. Changing the policy
	// alone does not revoke existing sessions. The zero value is false; app
	// configuration uses the AllowConcurrentSessions policy constant.
	AllowConcurrentSessions bool
}

// Validate requires expiring access tokens and ordered positive lifetimes, while
// allowing zero to disable refresh expiry or the absolute session deadline.
func (p SessionPolicy) Validate() error {
	if p.AccessTokenLifetime < time.Minute || p.AccessTokenLifetime > 7*24*time.Hour ||
		p.RefreshTokenLifetime < 0 || p.RefreshTokenLifetime > 30*24*time.Hour ||
		(p.RefreshTokenLifetime > 0 && p.RefreshTokenLifetime < p.AccessTokenLifetime) ||
		p.AbsoluteSessionLifetime < 0 || p.AbsoluteSessionLifetime > 90*24*time.Hour ||
		(p.AbsoluteSessionLifetime > 0 && (p.AbsoluteSessionLifetime < p.AccessTokenLifetime || p.AbsoluteSessionLifetime < p.RefreshTokenLifetime)) {
		return ErrSessionConfig
	}
	return nil
}
