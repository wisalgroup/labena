package service

import (
	"context"
	"net/netip"
	"time"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
)

// SignInWithProvider verifies a provider ID token and issues an app session. Valid ID tokens
// can be presented again until expiry; no one-time exchange guarantee is provided.
func (s *authService) SignInWithProvider(ctx context.Context, provider enums.AuthProvider, credential string, input SocialSignInInput) (*SignInResult, error) {
	verifier := s.providers[provider]
	if verifier == nil {
		return nil, domain.ErrSocialProvider
	}
	if len(credential) == 0 || len(credential) > 16384 {
		return nil, domain.ErrSocialInvalid
	}
	client, err := s.socialClientKey(input.ClientIP)
	if err != nil {
		return nil, err
	}
	raw, session, refresh, err := s.newSession(input.Device)
	if err != nil {
		return nil, err
	}
	if err := s.limit(ctx, "social", nil, client, 0, s.rateLimitPolicy.AttemptsPerIP); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	identity, err := verifier.Verify(ctx, credential)
	if err != nil {
		return nil, err
	}
	if err := s.limit(ctx, "social-subject", s.digester.Key("social-subject", string(provider)+":"+identity.Subject), nil, s.rateLimitPolicy.AttemptsPerIdentity, 0); err != nil {
		return nil, err
	}
	identity.Name, err = signupName(identity.Name)
	if err != nil {
		return nil, domain.ErrSocialInvalid
	}
	user, created, err := s.social.CompleteSocialSignIn(ctx, provider, identity, session, refresh, s.sessionPolicy.AllowConcurrentSessions)
	if err != nil {
		return nil, err
	}
	if created {
		publishRegistration(ctx, s.eventPublisher, user)
	}
	tokens, err := s.tokens(session, refresh, raw)
	if err != nil {
		return nil, err
	}
	return &SignInResult{Tokens: tokens, User: user, Created: created}, nil
}

func (s *authService) socialClientKey(ip string) ([]byte, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil, domain.ErrSocialInvalid
	}
	return s.digester.Key("social-client", addr.Unmap().String()), nil
}
