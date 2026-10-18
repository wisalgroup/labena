package identity_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/mailviews"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/pkg/ratelimit"
	"github.com/wisalgroup/labena/pkg/token"
)

type codeMailChallenges struct {
	repository.VerificationRepository
	challenge *entities.VerificationChallenge
}

func (r *codeMailChallenges) IssueEmailChallenge(_ context.Context, challenge *entities.VerificationChallenge, _ domain.VerificationCodePolicy) error {
	r.challenge = challenge
	return nil
}

func TestSecurityCodeEmailThroughAuthService(t *testing.T) {
	address, _ := valueobjects.NewEmail("user@example.com")
	challenges := &codeMailChallenges{}
	mailer := &codeDelivery{codes: map[string]string{}}
	access, err := token.NewAccess(bytes.Repeat([]byte{1}, 32), "test", "api")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.NewAuthService(service.AuthOptions{
		VerificationRepository: challenges,
		SessionRepository:      repository.NewSessionRepository(nil),
		VerificationKey:        bytes.Repeat([]byte{2}, 32),
		VerificationCodePolicy: domain.DefaultVerificationCodePolicy(),
		RateLimitPolicy:        domain.DefaultAuthRateLimitPolicy(),
		SessionPolicy:          domain.SessionPolicy{AccessTokenLifetime: 15 * time.Minute},
		Access:                 access, Limiter: &limitStub{decision: ratelimit.Decision{Allowed: true}},
		Mailer: mailer, MailRenderer: testMailRenderer(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		purpose enums.VerificationPurpose
		subject string
		request func(context.Context, string, string) (uuid.UUID, error)
	}{
		{enums.PurposeSignIn, "Your sign-in code", svc.RequestSignInCode},
	} {
		t.Run(string(tc.purpose), func(t *testing.T) {
			if _, err := tc.request(t.Context(), address.String(), "192.0.2.1"); err != nil {
				t.Fatal(err)
			}
			code := mailer.code(address.String())
			message := mailer.message
			if challenges.challenge.Purpose != tc.purpose || message.Subject != tc.subject || !strings.Contains(message.HTMLBody, code) || !strings.Contains(message.TextBody, code) || strings.Contains(message.Subject, code) {
				t.Fatal("wrong purpose, destination or rendered content", message.Subject)
			}
		})
	}
	mailer.err = errors.New("SMTP password=secret")
	if _, err := svc.RequestSignInCode(t.Context(), address.String(), "192.0.2.1"); !errors.Is(err, domain.ErrVerificationDelivery) || strings.Contains(err.Error(), "secret") {
		t.Fatal("provider details exposed", err)
	}
	calls := mailer.calls
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := svc.RequestSignInCode(ctx, address.String(), "192.0.2.1"); err == nil || mailer.calls != calls {
		t.Fatal("cancellation did not prevent mail", err)
	}
	content, err := mailviews.SignIn("123456", domain.DefaultVerificationCodePolicy().CodeLifetime)
	if err != nil || strings.Contains(content.Subject, "123456") || strings.Contains(content.Preheader, "123456") {
		t.Fatal("security code leaked into preview", err)
	}

}
