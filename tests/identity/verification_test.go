package identity_test

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	identitymigrations "github.com/wisalgroup/labena/internal/modules/identity/migrations"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/mail"
	notificationmigrations "github.com/wisalgroup/labena/internal/platform/notification/migrations"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/mailview"
	"github.com/wisalgroup/labena/pkg/token"
)

type codeDelivery struct {
	mu      sync.Mutex
	codes   map[string]string
	err     error
	message mail.Message
	calls   int
}

var deliveredCode = regexp.MustCompile(`(?m): ([0-9]{6})$`)

func (d *codeDelivery) Send(_ context.Context, message mail.Message) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	code := deliveredCode.FindStringSubmatch(message.TextBody)
	if len(message.To) != 1 || len(code) != 2 {
		return errors.New("test: missing rendered code or recipient")
	}
	d.codes[message.To[0].Email] = code[1]
	d.message = message
	d.calls++
	return d.err
}

func testMailRenderer(t *testing.T) *mailview.Renderer {
	t.Helper()
	views, err := mailview.New(mailview.Brand{Name: "Example", AccentColor: "#1D4ED8", WebsiteURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return views
}

func (d *codeDelivery) code(email string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.codes[email]
}

func verificationDB(t *testing.T) *bun.DB {
	t.Helper()
	db := testdb.OpenDriver(t, database.DriverPostgres)
	var registry migrate.Registry
	if err := registry.Register("cache", testdb.Migrations(t, database.DriverPostgres)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("identity", identitymigrations.Files); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("notification", notificationmigrations.Files); err != nil {
		t.Fatal(err)
	}
	files, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := migrate.NewProvider(t.Context(), db, files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestEmailOTPPostgres(t *testing.T) {
	db := verificationDB(t)
	repo := newRepositories(db)
	sender := &codeDelivery{codes: map[string]string{}}
	policy := domain.DefaultVerificationCodePolicy()
	options := authenticationOptions(t, repo, bytes.Repeat([]byte{0x42}, 32), sender)
	options.VerificationCodePolicy = policy
	options.Access, _ = token.NewAccess(bytes.Repeat([]byte{1}, 32), "test", "api")
	options.SessionPolicy = domain.SessionPolicy{AccessTokenLifetime: 15 * time.Minute, AllowConcurrentSessions: true}
	svc, err := service.NewAuthService(options)
	if err != nil {
		t.Fatal(err)
	}
	create := func(email string) *entities.User {
		address, _ := valueobjects.NewEmail(email)
		user, err := entities.NewUser("Test", address)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.users.Create(t.Context(), user); err != nil {
			t.Fatal(err)
		}
		return user
	}
	request := func(email, ip string) uuid.UUID {
		id, err := svc.RequestSignInCode(t.Context(), email, ip)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	resetCooldown := func() {
		if _, err := db.NewRaw("UPDATE verification_challenges SET created_at = LEAST(created_at, ?)", time.Now().Add(-2*time.Minute)).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("single consumption across concurrent attempts", func(t *testing.T) {
		user := create("verified@example.com")
		id := request(user.Email.String(), "192.0.2.1")
		code := sender.code(user.Email.String())
		challenge, err := repo.verification.GetChallenge(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(challenge.CodeDigest) != 32 || bytes.Equal(challenge.CodeDigest, []byte(code)) || challenge.UserID == nil {
			t.Fatal("invalid digest or account binding")
		}
		results := make(chan error, 4)
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() { results <- verifyCode(svc, t.Context(), id, code, "192.0.2.2") })
		}
		wg.Wait()
		close(results)
		success := 0
		for err := range results {
			if err == nil {
				success++
			} else if !errors.Is(err, domain.ErrVerificationInvalid) {
				t.Fatal(err)
			}
		}
		if success != 1 {
			t.Fatalf("consumed %d times", success)
		}
		persisted, err := repo.users.Get(t.Context(), user.ID)
		if err != nil || persisted.EmailVerifiedAt == nil {
			t.Fatal(err)
		}
		if err := verifyCode(svc, t.Context(), id, code, "192.0.2.3"); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal("replay accepted", err)
		}
	})
	t.Run("failed attempts commit and resend cannot reset destination budget", func(t *testing.T) {
		user := create("limited@example.com")
		id := request(user.Email.String(), "192.0.2.10")
		if _, err := svc.RequestSignInCode(t.Context(), user.Email.String(), "192.0.2.11"); !errors.Is(err, domain.ErrVerificationLimited) {
			t.Fatal("cooldown bypass", err)
		}
		for i := 0; i < policy.MaxAttemptsPerCode; i++ {
			if err := verifyCode(svc, t.Context(), id, "wrong", "192.0.2.12"); !errors.Is(err, domain.ErrVerificationInvalid) {
				t.Fatal(err)
			}
		}
		c, _ := repo.verification.GetChallenge(t.Context(), id)
		if c.AttemptCount != policy.MaxAttemptsPerCode {
			t.Fatal("failed attempts rolled back", c.AttemptCount)
		}
		if err := verifyCode(svc, t.Context(), id, sender.code(user.Email.String()), "192.0.2.13"); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal("exhausted challenge accepted", err)
		}
		resetCooldown()
		next := request(user.Email.String(), "192.0.2.14")
		for range domain.DefaultAuthRateLimitPolicy().AttemptsPerIdentity - policy.MaxAttemptsPerCode - 1 {
			if err := verifyCode(svc, t.Context(), next, "wrong", "192.0.2.15"); !errors.Is(err, domain.ErrVerificationInvalid) {
				t.Fatal(err)
			}
		}
		if err := verifyCode(svc, t.Context(), next, sender.code(user.Email.String()), "192.0.2.16"); !errors.Is(err, domain.ErrVerificationLimited) {
			t.Fatal("resend reset budget", err)
		}
	})
	t.Run("new request invalidates previous proof", func(t *testing.T) {
		user := create("resend@example.com")
		old := request(user.Email.String(), "192.0.2.20")
		oldCode := sender.code(user.Email.String())
		resetCooldown()
		next := request(user.Email.String(), "192.0.2.21")
		if err := verifyCode(svc, t.Context(), old, oldCode, "192.0.2.22"); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal(err)
		}
		if err := verifyCode(svc, t.Context(), next, sender.code(user.Email.String()), "192.0.2.22"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("unknown accounts register but suspended and changed contacts cannot sign in", func(t *testing.T) {
		id := request("missing@example.com", "192.0.2.30")
		if err := verifyCode(svc, t.Context(), id, sender.code("missing@example.com"), "192.0.2.31"); err != nil {
			t.Fatal(err)
		}
		user := create("suspended@example.com")
		id = request(user.Email.String(), "192.0.2.32")
		if _, err := repo.users.Edit(t.Context(), user.ID, func(u *entities.User) error { u.Status = enums.UserSuspended; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := verifyCode(svc, t.Context(), id, sender.code(user.Email.String()), "192.0.2.33"); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal(err)
		}
		user = create("changed@example.com")
		id = request(user.Email.String(), "192.0.2.34")
		if _, err := repo.users.Edit(t.Context(), user.ID, func(u *entities.User) error {
			address, _ := valueobjects.NewEmail("new@example.com")
			return u.ChangeEmail(address, time.Now().UTC())
		}); err != nil {
			t.Fatal(err)
		}
		if err := verifyCode(svc, t.Context(), id, sender.code(user.Email.String()), "192.0.2.35"); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal(err)
		}
	})
	t.Run("expiry pruning and nested transaction refusal", func(t *testing.T) {
		user := create("expired@example.com")
		id := request(user.Email.String(), "192.0.2.40")
		if _, err := db.NewRaw("UPDATE verification_challenges SET expires_at = ? WHERE id = ?", time.Now().Add(-time.Minute), id).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := verifyCode(svc, t.Context(), id, sender.code(user.Email.String()), "192.0.2.41"); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal(err)
		}
		tx, _ := database.NewTxManager(db, nil)
		err := tx.Execute(t.Context(), func(ctx context.Context) error { return verifyCode(svc, ctx, id, "000000", "192.0.2.41") })
		if !errors.Is(err, database.ErrTransactionActive) {
			t.Fatal(err)
		}
		count, err := svc.PruneVerification(t.Context(), 1)
		if err != nil || count != 1 {
			t.Fatal(count, err)
		}
		if _, err := repo.verification.GetChallenge(t.Context(), id); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal(err)
		}
	})
	t.Run("delivery failure is safe and budget remains consumed", func(t *testing.T) {
		sender.err = errors.New("SMTP password=secret")
		defer func() { sender.err = nil }()
		_, err := svc.RequestSignInCode(t.Context(), "delivery@example.com", "192.0.2.50")
		if !errors.Is(err, domain.ErrVerificationDelivery) || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
		_, err = svc.RequestSignInCode(t.Context(), "delivery@example.com", "192.0.2.51")
		if !errors.Is(err, domain.ErrVerificationLimited) {
			t.Fatal(err)
		}
	})
}

func authenticationOptions(t *testing.T, repo repositories, key []byte, sender *codeDelivery) service.AuthOptions {
	t.Helper()
	return service.AuthOptions{
		VerificationRepository: repo.verification,
		SessionRepository:      repo.sessions,
		SocialRepository:       repo.social,
		VerificationKey:        key,
		VerificationCodePolicy: domain.DefaultVerificationCodePolicy(),
		RateLimitPolicy:        domain.DefaultAuthRateLimitPolicy(),
		Limiter:                testdb.RateLimiter(t), Mailer: sender, MailRenderer: testMailRenderer(t),
	}
}

// Each integration fixture constructs the public repositories independently,
// using one isolated pool. No combined production store is available to tests.
type repositories struct {
	users        repository.UserRepository
	sessions     repository.SessionRepository
	verification repository.VerificationRepository
	social       repository.SocialRepository
}

func newRepositories(db *bun.DB) repositories {
	return repositories{
		users:        repository.NewUserRepository(db),
		sessions:     repository.NewSessionRepository(db),
		verification: repository.NewVerificationRepository(db),
		social:       repository.NewSocialRepository(db),
	}
}

func verifyCode(svc service.AuthService, ctx context.Context, id uuid.UUID, code, ip string) error {
	_, err := svc.SignInWithCode(ctx, service.SignInInput{Proof: service.ProofInput{ChallengeID: id, Code: code, ClientIP: ip}, Device: service.Device{Platform: enums.PlatformWeb}})
	return err
}
