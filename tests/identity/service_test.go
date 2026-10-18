package identity_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/integrations"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/pkg/ratelimit"
	"github.com/wisalgroup/labena/pkg/token"
)

// Construction must not call delivery providers. Unexpected calls panic through
// the embedded nil interfaces, keeping these fixtures limited to assembly.
type constructionMailer struct{ mail.Sender }
type constructionProvider struct{ integrations.SocialProvider }

func TestAuthenticationConstruction(t *testing.T) {
	access, err := token.NewAccess(bytes.Repeat([]byte{1}, 32), "test", "api")
	if err != nil {
		t.Fatal(err)
	}
	options := func() service.AuthOptions {
		return service.AuthOptions{
			VerificationRepository: repository.NewVerificationRepository(nil),
			SessionRepository:      repository.NewSessionRepository(nil),
			SocialRepository:       repository.NewSocialRepository(nil),
			VerificationKey:        bytes.Repeat([]byte{2}, 32),
			VerificationCodePolicy: domain.DefaultVerificationCodePolicy(),
			RateLimitPolicy:        domain.DefaultAuthRateLimitPolicy(),
			SessionPolicy:          domain.SessionPolicy{AccessTokenLifetime: time.Minute, RefreshTokenLifetime: time.Hour, AbsoluteSessionLifetime: 24 * time.Hour},
			Limiter:                &limitStub{}, Mailer: constructionMailer{}, MailRenderer: testMailRenderer(t),
		}
	}
	for _, tc := range []struct {
		name             string
		sessions, social bool
	}{
		{name: "cleanup only"},
		{name: "authentication", sessions: true},
		{name: "all capabilities", sessions: true, social: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := options()
			if tc.sessions {
				input.Access = access
			}
			if tc.social {
				input.Providers = map[enums.AuthProvider]integrations.SocialProvider{enums.ProviderGoogle: constructionProvider{}}
			}
			services, err := service.NewAuthService(input)
			if err != nil {
				t.Fatal(err)
			}
			if services == nil {
				t.Fatal("missing auth service")
			}
			if !tc.sessions {
				if _, err := services.Authenticate(t.Context(), "token"); !errors.Is(err, domain.ErrUnauthenticated) {
					t.Fatal("disabled authentication", err)
				}
				if _, err := services.SignInWithCode(t.Context(), service.SignInInput{}); !errors.Is(err, domain.ErrSessionConfig) {
					t.Fatal("disabled OTP sign-in", err)
				}
				if _, err := services.PruneSessions(t.Context(), 1); !errors.Is(err, domain.ErrSessionConfig) {
					t.Fatal("disabled session cleanup", err)
				}
			}
			if !tc.social {
				if _, err := services.SignInWithProvider(t.Context(), enums.ProviderGoogle, "token", service.SocialSignInInput{}); !errors.Is(err, domain.ErrSocialProvider) {
					t.Fatal("disabled provider", err)
				}
			}
			if len(input.Limiter.(*limitStub).keys) != 0 {
				t.Fatal("construction called the limiter")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*service.AuthOptions)
		want   error
	}{
		{"short secret", func(o *service.AuthOptions) { o.VerificationKey = []byte("short") }, domain.ErrVerificationConfig},
		{"missing limiter", func(o *service.AuthOptions) { o.Limiter = nil }, domain.ErrVerificationConfig},
		{"budget expires before proof", func(o *service.AuthOptions) { o.RateLimitPolicy.RefillPeriod = time.Minute }, domain.ErrVerificationConfig},
		{"budget smaller than proof attempts", func(o *service.AuthOptions) { o.RateLimitPolicy.AttemptsPerIdentity = 1 }, domain.ErrVerificationConfig},
		{"authentication without mail", func(o *service.AuthOptions) { o.Access = access; o.Mailer = nil }, domain.ErrSessionConfig},
		{"authentication without views", func(o *service.AuthOptions) { o.Access = access; o.MailRenderer = nil }, domain.ErrSessionConfig},
		{"invalid session policy", func(o *service.AuthOptions) { o.Access = access; o.SessionPolicy.RefreshTokenLifetime = -time.Second }, domain.ErrSessionConfig},
		{"provider without sessions", func(o *service.AuthOptions) {
			o.Providers = map[enums.AuthProvider]integrations.SocialProvider{enums.ProviderGoogle: constructionProvider{}}
		}, domain.ErrSessionConfig},
		{"missing provider", func(o *service.AuthOptions) {
			o.Access = access
			o.Providers = map[enums.AuthProvider]integrations.SocialProvider{enums.ProviderGoogle: nil}
		}, domain.ErrSessionConfig},
		{"unknown provider", func(o *service.AuthOptions) {
			o.Access = access
			o.Providers = map[enums.AuthProvider]integrations.SocialProvider{"unknown": constructionProvider{}}
		}, domain.ErrSessionConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := options()
			tc.change(&input)
			services, err := service.NewAuthService(input)
			if services != nil || !errors.Is(err, tc.want) {
				t.Fatalf("got %+v, %v; want nil, %v", services, err, tc.want)
			}
		})
	}
	input := options()
	input.VerificationRepository = nil
	if services, err := service.NewAuthService(input); services != nil || !errors.Is(err, domain.ErrVerificationConfig) {
		t.Fatal("accepted missing repository", services, err)
	}
	if users, err := service.NewUserService(nil); users != nil || !errors.Is(err, domain.ErrDependenciesRequired) {
		t.Fatal("accepted missing user repository", users, err)
	}
}

type limitStub struct {
	decision ratelimit.Decision
	err      error
	keys     []string
	limits   []ratelimit.Limit
}

func (s *limitStub) Allow(_ context.Context, key string, limit ratelimit.Limit) (ratelimit.Decision, error) {
	s.keys = append(s.keys, key)
	s.limits = append(s.limits, limit)
	return s.decision, s.err
}
