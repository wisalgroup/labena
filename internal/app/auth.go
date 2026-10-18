package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/integrations"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/token"
)

// newAuthentication borrows resources without I/O or background work.
// Disabled authentication returns a nil interface so protected routes fail closed.
func (a *application) newAuthentication() (service.AuthService, error) {
	cfg, resources := a.cfg.Auth, a.resources
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}

	verificationKey, err := cfg.VerificationSecret()
	if err != nil {
		return nil, err
	}
	accessKey, err := cfg.AccessSecret()
	if err != nil {
		return nil, err
	}
	access, err := token.NewAccess(accessKey, cfg.TokenIssuer, cfg.TokenAudience)
	if err != nil {
		return nil, err
	}

	options := service.AuthOptions{
		VerificationRepository: repository.NewVerificationRepository(resources.DB),
		SessionRepository:      repository.NewSessionRepository(resources.DB),
		VerificationKey:        verificationKey,
		VerificationCodePolicy: domain.DefaultVerificationCodePolicy(),
		RateLimitPolicy:        domain.DefaultAuthRateLimitPolicy(),
		SessionPolicy:          cfg.SessionPolicy(),
		Access:                 access,
		Notifier:               resources.Notifier,
		Transactions:           resources.Transactions,
		Mailer:                 resources.Mailer,
		MailRenderer:           resources.MailRenderer,
		Limiter:                resources.RateLimiter,
		EventPublisher:         resources.EventPublisher,
	}
	if cfg.GoogleEnabled {
		google, err := integrations.NewGoogle(cfg.GoogleClientID, cfg.GoogleTrustedPresenterIDs...)
		if err != nil {
			return nil, err
		}
		options.SocialRepository = repository.NewSocialRepository(resources.DB)
		options.Providers = map[enums.AuthProvider]integrations.SocialProvider{
			enums.ProviderGoogle: google,
		}
	}
	return service.NewAuthService(options)
}

// pushSessionActive reuses Identity's authoritative account/session check. The
// capability receives a callback, never an Identity implementation dependency.
func (a *application) pushSessionActive() func(context.Context, notification.Recipient, string) (bool, error) {
	sessions := repository.NewSessionRepository(a.resources.DB)
	return func(ctx context.Context, recipient notification.Recipient, session string) (bool, error) {
		if recipient.Type != string(entities.UserEntityType) {
			return false, nil
		}
		userID, err := uuid.Parse(recipient.ID)
		if err != nil {
			return false, nil
		}
		sessionID, err := uuid.Parse(session)
		if err != nil {
			return false, nil
		}
		err = sessions.AuthenticateSession(ctx, userID, sessionID)
		if errors.Is(err, domain.ErrUnauthenticated) {
			return false, nil
		}
		return err == nil, err
	}
}
