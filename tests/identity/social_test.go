package identity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	modulehttp "github.com/wisalgroup/labena/internal/modules/identity/delivery/http"
	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/integrations"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/messaging"
	"github.com/wisalgroup/labena/pkg/token"
)

type socialVerifier func(context.Context, string) (domain.SocialIdentity, error)

func (f socialVerifier) Verify(ctx context.Context, raw string) (domain.SocialIdentity, error) {
	return f(ctx, raw)
}

func TestSocialPostgres(t *testing.T) {
	db := verificationDB(t)
	repo := newRepositories(db)
	key := bytes.Repeat([]byte{2}, 32)
	access, err := token.NewAccess(bytes.Repeat([]byte{1}, 32), "test", "api")
	if err != nil {
		t.Fatal(err)
	}
	sender := &codeDelivery{codes: map[string]string{}}
	published := &registrationRecorder{}
	verifier := socialVerifier(func(_ context.Context, raw string) (domain.SocialIdentity, error) {
		parts := strings.Split(raw, "|")
		if len(parts) != 2 {
			return domain.SocialIdentity{}, domain.ErrSocialInvalid
		}
		name := "Google User"
		if parts[0] == "nameless" {
			name = ""
		}
		return domain.SocialIdentity{Subject: parts[0], Email: parts[1], Name: name, EmailVerified: true}, nil
	})
	options := authenticationOptions(t, repo, key, sender)
	options.Access = access
	options.SessionPolicy = domain.SessionPolicy{AccessTokenLifetime: time.Minute, RefreshTokenLifetime: time.Hour, AbsoluteSessionLifetime: 24 * time.Hour, AllowConcurrentSessions: false}
	options.EventPublisher = published
	options.Providers = map[enums.AuthProvider]integrations.SocialProvider{enums.ProviderGoogle: verifier}
	svc, err := service.NewAuthService(options)
	if err != nil {
		t.Fatal(err)
	}
	input := service.SocialSignInInput{ClientIP: "192.0.2.1", Device: service.Device{Platform: enums.PlatformWeb}}
	login := func(sub, email string) (*service.SignInResult, error) {
		return svc.SignInWithProvider(t.Context(), enums.ProviderGoogle, sub+"|"+email, input)
	}
	first, err := login("subject", "person@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := svc.Authenticate(t.Context(), first.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	// Reusing a still-valid provider credential is now explicitly supported.
	second, err := login("subject", "person@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	next, err := svc.Authenticate(t.Context(), second.AccessToken)
	if err != nil || next.UserID != principal.UserID {
		t.Fatal(next, err)
	}
	if _, err := svc.Authenticate(t.Context(), first.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("single session", err)
	}
	if _, err := login("subject", "changed@gmail.com"); err != nil {
		t.Fatal(err)
	}
	user, err := repo.users.Get(t.Context(), principal.UserID)
	if err != nil || user.Email.String() != "person@gmail.com" || user.EmailVerifiedAt == nil {
		t.Fatal(user, err)
	}
	if published.calls != 1 {
		t.Fatalf("registration events after repeated social login = %d", published.calls)
	}
	if !first.Created || second.Created || first.User.ID != principal.UserID || second.User.ID != principal.UserID || !second.User.ProfileComplete() {
		t.Fatal("social sign-in account result")
	}
	t.Run("missing provider name stays incomplete on returning sign-in", func(t *testing.T) {
		for _, created := range []bool{true, false} {
			result, err := login("nameless", "nameless@gmail.com")
			if err != nil {
				t.Fatal(err)
			}
			if result.Created != created || result.User.Name != "" || result.User.ProfileComplete() {
				t.Fatal("missing provider name became a completed profile")
			}
		}
	})
	t.Run("never links by email", func(t *testing.T) {
		if _, err := login("different-subject", "person@gmail.com"); !errors.Is(err, domain.ErrSocialLinkRequired) {
			t.Fatal(err)
		}
		email, _ := valueobjects.NewEmail("existing@gmail.com")
		u, _ := entities.NewUser("Existing", email)
		if err := repo.users.Create(t.Context(), u); err != nil {
			t.Fatal(err)
		}
		if _, err := login("new-subject", "existing@gmail.com"); !errors.Is(err, domain.ErrSocialLinkRequired) {
			t.Fatal(err)
		}
	})
	t.Run("concurrent first login", func(t *testing.T) {
		const n = 4
		results := make(chan *service.SignInResult, n)
		errs := make(chan error, n)
		var wg sync.WaitGroup
		for range n {
			wg.Go(func() { result, err := login("concurrent", "concurrent@gmail.com"); results <- result; errs <- err })
		}
		wg.Wait()
		close(errs)
		close(results)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		active, created := 0, 0
		for result := range results {
			if result.Created {
				created++
			}
			if _, err := svc.Authenticate(t.Context(), result.AccessToken); err == nil {
				active++
			}
		}
		if created != 1 {
			t.Fatal("new accounts", created)
		}
		if active != 1 {
			t.Fatal("active sessions", active)
		}
		count, err := db.NewSelect().Model((*entities.User)(nil)).Where("email = ?", "concurrent@gmail.com").Count(t.Context())
		if err != nil || count != 1 {
			t.Fatal(count, err)
		}
	})
	t.Run("inactive returning account", func(t *testing.T) {
		if _, err := db.NewUpdate().Model((*entities.User)(nil)).Set("status = ?", enums.UserSuspended).Where("id = ?", principal.UserID).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := login("subject", "person@gmail.com"); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal(err)
		}
	})
	t.Run("HTTP direct JSON exchange", func(t *testing.T) {
		router := newAPI(t)

		modulehttp.RegisterRoutes(router, nil, modulehttp.Routes{Auth: svc, SignIn: true, Social: true})
		call := func(path, body, contentType string) *httptest.ResponseRecorder {
			r := httptest.NewRequest("POST", path, strings.NewReader(body))
			r.Header.Set("Content-Type", contentType)
			r.RemoteAddr = "192.0.2.99:5000"
			out := httptest.NewRecorder()
			router.ServeHTTP(out, r)
			return out
		}
		payload, _ := json.Marshal(map[string]string{"id_token": "http-subject|http@gmail.com", "platform": "web"})
		for _, status := range []int{201, 200} {
			out := call("/auth/social/google", string(payload), "application/json")
			if out.Code != status || out.Header().Get("Cache-Control") != "no-store" || !strings.Contains(out.Body.String(), "access_token") {
				t.Fatal(out.Code, out.Body.String())
			}
		}
		if out := call("/auth/social/apple", string(payload), "application/json"); out.Code != 422 {
			t.Fatal(out.Code)
		}
		if out := call("/auth/social/google", string(payload), "text/plain"); out.Code != 415 {
			t.Fatal("expected unsupported media type", out.Code)
		}
		if out := call("/auth/social/google/challenge", "", "application/json"); out.Code != 404 {
			t.Fatal("challenge endpoint retained", out.Code)
		}
	})
	t.Run("rollback account on invalid session", func(t *testing.T) {
		c := domain.SocialIdentity{Subject: "rollback", Email: "rollback@gmail.com", EmailVerified: true, Name: "Rollback"}
		if _, _, err := repo.social.CompleteSocialSignIn(t.Context(), enums.ProviderGoogle, c, &entities.Session{}, &entities.SessionRefreshToken{}, true); err == nil {
			t.Fatal("invalid session accepted")
		}
		count, err := db.NewSelect().Model((*entities.User)(nil)).Where("email = ?", c.Email).Count(t.Context())
		if err != nil || count != 0 {
			t.Fatal(count, err)
		}
	})
	for _, table := range []string{"social_login_challenges", "verification_limits"} {
		var exists bool
		if err := db.NewRaw("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=?)", table).Scan(t.Context(), &exists); err != nil || exists {
			t.Fatal("obsolete table", table, exists, err)
		}
	}
}

// Records service publication, not repository persistence.
type registrationRecorder struct {
	messaging.Publisher
	calls int
	check func(context.Context) error
}

func (p *registrationRecorder) PublishWithID(ctx context.Context, _ string, _ string, _ any) error {
	p.calls++
	if p.check != nil {
		return p.check(ctx)
	}
	return nil
}

func newAPI(t testing.TB, options ...api.Options) *api.Server {
	t.Helper()
	var opts api.Options
	if len(options) > 0 {
		opts = options[0]
	}
	server, err := api.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}
