package service

import (
	"context"
	"maps"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/integrations"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/modules/shared/events"
	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/internal/platform/messaging"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/logging"
	"github.com/wisalgroup/labena/pkg/mailview"
	"github.com/wisalgroup/labena/pkg/otp"
	"github.com/wisalgroup/labena/pkg/ratelimit"
	"github.com/wisalgroup/labena/pkg/token"
)

// authService owns the shared state for verification and every sign-in method.
// Topic files hold methods on this receiver; there are no nested service objects.
type authService struct {
	verification           repository.VerificationRepository
	sessions               repository.SessionRepository
	social                 repository.SocialRepository
	verificationCodePolicy domain.VerificationCodePolicy
	sessionPolicy          domain.SessionPolicy
	rateLimitPolicy        domain.AuthRateLimitPolicy
	digester               *otp.Digester
	limiter                ratelimit.Limiter
	access                 *token.Access
	mailer                 mail.Sender
	mailRenderer           *mailview.Renderer
	eventPublisher         messaging.Publisher
	providers              map[enums.AuthProvider]integrations.SocialProvider
	notifier               *notification.Notifier
	transactions           *database.TxManager
}

// AuthOptions supplies borrowed dependencies and validated policies.
// VerificationKey is copied. Providers is copied; provider instances are borrowed.
// Repositories are borrowed and must share the application pool.
// Access enables sign-in and session operations; nil leaves them disabled.
// Mailer and MailRenderer are both required for authentication.
type AuthOptions struct {
	// Notifier and Transactions atomically detach push destinations on logout.
	Notifier               *notification.Notifier
	Transactions           *database.TxManager
	VerificationRepository repository.VerificationRepository
	SessionRepository      repository.SessionRepository
	SocialRepository       repository.SocialRepository
	VerificationKey        []byte

	// VerificationCodePolicy applies to email sign-in codes.
	// Supply domain.DefaultVerificationCodePolicy or an explicitly valid policy.
	VerificationCodePolicy domain.VerificationCodePolicy

	// RateLimitPolicy controls shared admission budgets. Its RefillPeriod must
	// cover CodeLifetime, and AttemptsPerIdentity must allow MaxAttemptsPerCode.
	// Supply domain.DefaultAuthRateLimitPolicy or an explicitly valid policy.
	RateLimitPolicy domain.AuthRateLimitPolicy

	// SessionPolicy controls all sign-in methods and refresh rotation. It is
	// required and validated only when Access enables authentication.
	SessionPolicy domain.SessionPolicy

	Access         *token.Access
	Mailer         mail.Sender
	MailRenderer   *mailview.Renderer
	Limiter        ratelimit.Limiter
	EventPublisher messaging.Publisher
	Providers      map[enums.AuthProvider]integrations.SocialProvider
}

// NewAuthService validates configuration and borrows dependencies without I/O or
// goroutines. Repositories share the app pool; clients support concurrent use.
// Disabled operations fail closed. A construction failure returns a nil interface.
func NewAuthService(options AuthOptions) (AuthService, error) {
	if options.VerificationRepository == nil || options.Limiter == nil || len(options.VerificationKey) < 32 {
		return nil, domain.ErrVerificationConfig
	}
	if err := options.VerificationCodePolicy.Validate(); err != nil {
		return nil, err
	}
	if err := options.RateLimitPolicy.Validate(); err != nil {
		return nil, err
	}
	if options.RateLimitPolicy.RefillPeriod < options.VerificationCodePolicy.CodeLifetime ||
		options.RateLimitPolicy.AttemptsPerIdentity < options.VerificationCodePolicy.MaxAttemptsPerCode {
		return nil, domain.ErrVerificationConfig
	}
	if options.Access != nil {
		if options.SessionRepository == nil || options.Mailer == nil || options.MailRenderer == nil {
			return nil, domain.ErrSessionConfig
		}
		if err := options.SessionPolicy.Validate(); err != nil {
			return nil, err
		}
	}
	if len(options.Providers) > 0 && (options.Access == nil || options.SocialRepository == nil) {
		return nil, domain.ErrSessionConfig
	}
	for name, provider := range options.Providers {
		if !name.Valid() || provider == nil {
			return nil, domain.ErrSessionConfig
		}
	}
	digester, err := otp.NewDigester(options.VerificationKey)
	if err != nil {
		return nil, err
	}
	if options.Notifier != nil && options.Transactions == nil {
		return nil, domain.ErrSessionConfig
	}
	return &authService{notifier: options.Notifier, transactions: options.Transactions,
		verification: options.VerificationRepository, sessions: options.SessionRepository,
		social:                 options.SocialRepository,
		verificationCodePolicy: options.VerificationCodePolicy, sessionPolicy: options.SessionPolicy,
		rateLimitPolicy: options.RateLimitPolicy, digester: digester, limiter: options.Limiter,
		access: options.Access, mailer: options.Mailer, mailRenderer: options.MailRenderer, eventPublisher: options.EventPublisher,
		providers: maps.Clone(options.Providers),
	}, nil
}

// Publish only after persistence succeeds. A local publication failure cannot
// undo a committed account and must not turn a successful signup into a retry.
func publishRegistration(ctx context.Context, publisher messaging.Publisher, user *entities.User) {
	if publisher == nil || user == nil {
		return
	}
	event := events.NewUserRegistered(user.ID)
	if err := publisher.PublishWithID(ctx, event.Type, event.ID.String(), event); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "identity: local registration event publication failed", "event_type", event.Type)
	}
}
