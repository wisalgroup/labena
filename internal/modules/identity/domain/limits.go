package domain

import "time"

// AuthRateLimitPolicy controls admission before code delivery, proof checking,
// provider verification. Each count sets both the initial burst
// capacity and the number of admissions replenished over RefillPeriod. Budgets
// replenish continuously across application instances through the shared limiter.
// Admitted work consumes budget even if a later operation fails; successful work
// also counts. Zero values are invalid; start with DefaultAuthRateLimitPolicy.
type AuthRateLimitPolicy struct {
	// RefillPeriod is the duration needed to replenish a full budget from empty.
	// It must be positive and at most 24 hours; the default is one hour.
	// NewAuthService also requires it to be at least the verification code's
	// CodeLifetime. Resending a code does not restart this period or refill a budget.
	RefillPeriod time.Duration

	// CodeRequestsPerDestination is the code-request burst capacity and refill
	// count per RefillPeriod for one normalized destination email address.
	// All code purposes and client IPs share that destination's request budget.
	// It must be at least 1; the default is 5.
	CodeRequestsPerDestination int

	// CodeRequestsPerIP is the code-request burst capacity and refill count per
	// RefillPeriod for one client IP, shared across destinations and code purposes.
	// HTTP currently supplies the immediate TCP peer, so users behind a proxy or
	// NAT share its budget. It must be at least 1; the default is 30.
	CodeRequestsPerIP int

	// AttemptsPerIdentity is the attempt burst capacity and refill count per
	// RefillPeriod for an identity within an operation scope. OTP proofs share a
	// destination budget across purposes; each provider's subject has a separate budget. An identity
	// budget is shared across client IPs. Valid values are 1 through 100; the
	// default is 10. NewAuthService requires at least MaxAttemptsPerCode.
	AttemptsPerIdentity int

	// AttemptsPerIP is the attempt burst capacity and refill count per RefillPeriod
	// for one client IP within an operation scope, across all identities. OTP proof,
	// provider verification use separate budgets. Requests that
	// reach a budget check can consume it even for unknown identities or bad proofs.
	// The same peer-IP rules as CodeRequestsPerIP apply. Valid values are 1 through
	// 100; the default is 50.
	AttemptsPerIP int
}

// DefaultAuthRateLimitPolicy returns shared admission limits for authentication.
func DefaultAuthRateLimitPolicy() AuthRateLimitPolicy {
	return AuthRateLimitPolicy{
		RefillPeriod:               time.Hour,
		CodeRequestsPerDestination: 5,
		CodeRequestsPerIP:          30,
		AttemptsPerIdentity:        10,
		AttemptsPerIP:              50,
	}
}

// Validate checks local bounds. NewAuthService checks the relationships to
// VerificationCodePolicy before allowing authentication operations.
func (p AuthRateLimitPolicy) Validate() error {
	if p.RefillPeriod <= 0 || p.RefillPeriod > 24*time.Hour ||
		p.CodeRequestsPerDestination < 1 || p.CodeRequestsPerIP < 1 ||
		p.AttemptsPerIdentity < 1 || p.AttemptsPerIdentity > 100 ||
		p.AttemptsPerIP < 1 || p.AttemptsPerIP > 100 {
		return ErrVerificationConfig
	}
	return nil
}
