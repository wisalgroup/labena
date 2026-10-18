package identity_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	platformauth "github.com/wisalgroup/labena/internal/platform/auth"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/push"
	"github.com/wisalgroup/labena/pkg/token"

	"github.com/google/uuid"
)

type sessionPush struct{}

func (sessionPush) Send(context.Context, string, push.Message) (push.Ticket, error) {
	panic("revocation must not send")
}
func (sessionPush) Receipt(context.Context, string) (push.Receipt, error) {
	panic("revocation must not poll receipts")
}
func TestLogoutDetachesPushAtomically(t *testing.T) {
	db := verificationDB(t)
	repo := newRepositories(db)
	email, _ := valueobjects.NewEmail("push-session@example.com")
	user, err := entities.NewUser("Push", email)
	if err != nil {
		t.Fatal(err)
	}
	if err = user.VerifyEmail(email, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = repo.users.Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	session := &entities.Session{ID: uuid.New(), UserID: user.ID, Platform: enums.PlatformWeb, CreatedAt: time.Now().UTC(), LastUsedAt: time.Now().UTC()}
	if _, err = db.NewInsert().Model(session).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	n, _ := notification.New(db, notification.Options{SessionActive: pushSessionCheck(repo), Pusher: sessionPush{}, PushProject: "demo"})
	installation := uuid.NewString()
	options := authenticationOptions(t, repo, bytes.Repeat([]byte{1}, 32), &codeDelivery{codes: map[string]string{}})
	options.Access, err = token.NewAccess(bytes.Repeat([]byte{2}, 32), "test", "api")
	if err != nil {
		t.Fatal(err)
	}
	options.SessionPolicy = domain.SessionPolicy{AccessTokenLifetime: time.Minute}
	options.Notifier = n
	options.Transactions, err = database.NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := service.NewAuthService(options)
	if err != nil {
		t.Fatal(err)
	}
	principal := platformauth.Principal{UserID: user.ID, SessionID: session.ID}
	sub, err := auth.SubscribePush(t.Context(), principal, installation, "ExpoPushToken[registration]")
	if err != nil {
		t.Fatal(err)
	}
	// Hold logout open. Registration on another connection must wait for the
	// authorization lock instead of committing a stale session attachment.
	err = options.Transactions.Execute(t.Context(), func(ctx context.Context) error {
		if err := auth.RevokeSession(ctx, user.ID, session.ID); err != nil {
			return err
		}
		waiting, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		_, err := auth.SubscribePush(waiting, principal, installation, "ExpoPushToken[registration]")
		if err == nil || waiting.Err() == nil {
			t.Fatalf("registration did not wait for logout: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.SubscribePush(t.Context(), principal, installation, "ExpoPushToken[registration]"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("revoked session registered: %v", err)
	}
	if err = db.NewSelect().Model(sub).WherePK().Scan(t.Context()); err != nil || sub.DisabledAt == nil {
		t.Fatal("registration not detached", err)
	}
	if err = repo.sessions.AuthenticateSession(t.Context(), user.ID, session.ID); err == nil {
		t.Fatal("session still active")
	}
	// Force detachment failure and prove session revocation rolls back with it.
	session.ID = uuid.New()
	session.RevokedAt = nil
	if _, err = db.NewInsert().Model(session).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.NewRaw("ALTER TABLE notification_subscriptions RENAME TO unavailable_subscriptions").Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = auth.RevokeSession(t.Context(), user.ID, session.ID); err == nil {
		t.Fatal("expected failure")
	}
	if err = repo.sessions.AuthenticateSession(t.Context(), user.ID, session.ID); err != nil {
		t.Fatal("partial logout committed", err)
	}
}

func pushSessionCheck(repo repositories) func(context.Context, notification.Recipient, string) (bool, error) {
	return func(ctx context.Context, r notification.Recipient, session string) (bool, error) {
		err := repo.sessions.AuthenticateSession(ctx, uuid.MustParse(r.ID), uuid.MustParse(session))
		if errors.Is(err, domain.ErrUnauthenticated) {
			return false, nil
		}
		return err == nil, err
	}
}
