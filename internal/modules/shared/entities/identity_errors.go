package entities

import "github.com/wisalgroup/labena/pkg/apperror"

// Identity entity failures have stable identity for errors.Is, including through
// wrapping. Shared entities cannot import the Identity module's domain package.
// Invalid server-managed IDs, credentials, and lifecycle state remain internal
// failures; public validation failures describe unsupported caller choices.
var (
	ErrInvalidUserStatus   = apperror.Validation("Unsupported account status.")
	ErrUserTransition      = apperror.Conflict("The account cannot make this status transition.")
	ErrUserInactive        = apperror.Forbidden("The account is not active.")
	ErrInvalidPhone        = apperror.InvalidFields(map[string][]string{"phone": {"Provide a phone number in international E.164 format."}})
	ErrInvalidContactState = apperror.Internal("identity: inconsistent contact verification state")
	ErrContactChanged      = apperror.Conflict("The contact has changed. Start verification again.")

	ErrInvalidUserName = apperror.InvalidFields(map[string][]string{"name": {"Name must contain between 1 and 100 characters."}})

	ErrInvalidEmail = apperror.InvalidFields(map[string][]string{"email": {"Provide a valid email address of at most 254 characters."}})
)

var (
	ErrInvalidAuthIdentityIDs         = apperror.Internal("identity: identity and user IDs are required")
	ErrUnsupportedAuthProvider        = apperror.Validation("identity: unsupported provider")
	ErrMissingProviderSubject         = apperror.Internal("identity: provider subject is required")
	ErrInvalidAuthIdentityTimestamps  = apperror.Internal("identity: invalid identity timestamps")
	ErrInvalidChallengeIDs            = apperror.Internal("identity: invalid challenge IDs")
	ErrChallengeAccountRequired       = apperror.Internal("identity: challenge requires an account")
	ErrUnsupportedVerificationPurpose = apperror.Validation("identity: unsupported verification purpose")
	ErrUnsupportedVerificationChannel = apperror.Validation("identity: unsupported verification channel")
	ErrInvalidChallengeCredential     = apperror.Internal("identity: destination and SHA-256 code digest are required")
	ErrInvalidVerificationAttempts    = apperror.Internal("identity: invalid verification attempts")
	ErrInvalidChallengeLifetime       = apperror.Internal("identity: invalid challenge lifetime")
	ErrInvalidChallengeConsumption    = apperror.Internal("identity: invalid challenge consumption time")
	ErrInvalidChallengeInvalidation   = apperror.Internal("identity: invalid challenge invalidation time")
	ErrConflictingChallengeState      = apperror.Internal("identity: challenge cannot be both consumed and invalidated")
	ErrInvalidSessionIDs              = apperror.Internal("identity: session and user IDs are required")
	ErrUnsupportedSessionPlatform     = apperror.Validation("identity: unsupported session platform")
	ErrInvalidSessionLifetime         = apperror.Internal("identity: invalid session lifetime")
	ErrInvalidSessionRevocation       = apperror.Internal("identity: invalid session revocation time")
	ErrInvalidRefreshTokenCredential  = apperror.Internal("identity: refresh token IDs and SHA-256 hash are required")
	ErrInvalidRefreshTokenLifetime    = apperror.Internal("identity: invalid refresh token lifetime")
	ErrInvalidRefreshTokenConsumption = apperror.Internal("identity: invalid refresh token consumption time")
	ErrInvalidUserState               = apperror.Internal("identity: invalid account identity or timestamps")
)
