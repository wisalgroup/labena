// Package integrations verifies external identity credentials without owning accounts.
package integrations

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"

	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
)

// Google uses Google's maintained validator and its cached, rotating signing keys.
// It owns no goroutines and supports concurrent use. Never log submitted credentials.
type Google struct {
	validator         *idtoken.Validator
	audience          string
	trustedPresenters map[string]struct{}
}

// NewGoogle accepts tokens for clientID. The audience itself is a trusted presenter;
// trustedPresenterIDs adds clients allowed to present tokens for that audience.
// Presenter IDs are copied and never become additional accepted audiences.
func NewGoogle(clientID string, trustedPresenterIDs ...string) (*Google, error) {
	if strings.TrimSpace(clientID) == "" {
		return nil, domain.ErrSessionConfig
	}
	presenters := map[string]struct{}{clientID: {}}
	for _, id := range trustedPresenterIDs {
		if strings.TrimSpace(id) == "" {
			return nil, domain.ErrSessionConfig
		}
		presenters[id] = struct{}{}
	}
	validator, err := idtoken.NewValidator(context.Background(), option.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}))
	if err != nil {
		return nil, err
	}
	return &Google{validator: validator, audience: clientID, trustedPresenters: presenters}, nil
}

func (g *Google) Verify(ctx context.Context, credential string) (domain.SocialIdentity, error) {
	if len(credential) == 0 || len(credential) > 16384 {
		return domain.SocialIdentity{}, domain.ErrSocialInvalid
	}
	payload, err := g.validator.Validate(ctx, credential, g.audience)
	if err != nil {
		if ctx.Err() != nil {
			return domain.SocialIdentity{}, ctx.Err()
		}
		return domain.SocialIdentity{}, domain.ErrSocialInvalid
	}
	// Mobile clients may present tokens for the web/server audience. Trust their
	// client IDs explicitly without broadening the audience checked above.
	if azp, exists := payload.Claims["azp"]; exists {
		presenter, ok := azp.(string)
		if !ok {
			return domain.SocialIdentity{}, domain.ErrSocialInvalid
		}
		if _, trusted := g.trustedPresenters[presenter]; !trusted {
			return domain.SocialIdentity{}, domain.ErrSocialInvalid
		}
	}
	return googleIdentity(payload, time.Now())
}

func googleIdentity(p *idtoken.Payload, now time.Time) (domain.SocialIdentity, error) {
	if p == nil || (p.Issuer != "https://accounts.google.com" && p.Issuer != "accounts.google.com") || p.Subject == "" || len(p.Subject) > 255 || p.Expires <= now.Unix() || p.IssuedAt <= 0 || p.IssuedAt > now.Add(time.Minute).Unix() {
		return domain.SocialIdentity{}, domain.ErrSocialInvalid
	}
	email, _ := p.Claims["email"].(string)
	name, _ := p.Claims["name"].(string)
	verified, _ := p.Claims["email_verified"].(bool)
	hd, _ := p.Claims["hd"].(string)
	// Google is not current email-ownership authority for third-party addresses.
	// Already-linked identities still authenticate by subject, without updating email.
	authoritative := verified && (strings.HasSuffix(strings.ToLower(email), "@gmail.com") || hd != "")
	return domain.SocialIdentity{Subject: p.Subject, Email: email, Name: name, EmailVerified: authoritative}, nil
}
