package identity_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	platformauth "github.com/wisalgroup/labena/internal/platform/auth"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/push"
	"github.com/wisalgroup/labena/pkg/token"
)

type lifecyclePush struct{ sends int }

func (p *lifecyclePush) Send(context.Context, string, push.Message) (push.Ticket, error) {
	p.sends++
	return push.Ticket{ID: uuid.NewString()}, nil
}
func (*lifecyclePush) Receipt(context.Context, string) (push.Receipt, error) {
	return push.Receipt{Found: true}, nil
}

func TestPushSessionLifecyclePostgres(t *testing.T) {
	for _, transition := range []string{"replay", "displaced", "suspended", "expired"} {
		t.Run(transition, func(t *testing.T) {
			db := verificationDB(t)
			repo := newRepositories(db)
			pusher := &lifecyclePush{}
			n, err := notification.New(db, notification.Options{Pusher: pusher, PushProject: "lifecycle", SessionActive: pushSessionCheck(repo)})
			if err != nil {
				t.Fatal(err)
			}
			sender := &codeDelivery{codes: map[string]string{}}
			opts := authenticationOptions(t, repo, bytes.Repeat([]byte{1}, 32), sender)
			opts.Access, err = token.NewAccess(bytes.Repeat([]byte{2}, 32), "test", "api")
			if err != nil {
				t.Fatal(err)
			}
			opts.SessionPolicy = domain.SessionPolicy{AccessTokenLifetime: time.Minute}
			opts.Notifier = n
			opts.Transactions, err = database.NewTxManager(db, nil)
			if err != nil {
				t.Fatal(err)
			}
			auth, err := service.NewAuthService(opts)
			if err != nil {
				t.Fatal(err)
			}
			login := func() *service.SignInResult {
				t.Helper()
				if _, err := db.NewRaw("UPDATE verification_challenges SET created_at=LEAST(created_at, ?)", time.Now().Add(-2*time.Minute)).Exec(t.Context()); err != nil {
					t.Fatal(err)
				}
				id, err := auth.RequestSignInCode(t.Context(), "push-lifecycle@example.com", "192.0.2.140")
				if err != nil {
					t.Fatal(err)
				}
				result, err := auth.SignInWithCode(t.Context(), service.SignInInput{Proof: service.ProofInput{ChallengeID: id, Code: sender.code("push-lifecycle@example.com"), ClientIP: "192.0.2.140"}, Device: service.Device{Platform: enums.PlatformWeb}})
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			initial := login()
			principal := platformauth.Principal{UserID: initial.User.ID, SessionID: initial.SessionID}
			sub, err := auth.SubscribePush(t.Context(), principal, uuid.NewString(), "ExpoPushToken[lifecycle]")
			if err != nil {
				t.Fatal(err)
			}
			recipient := notification.Recipient{Type: string(entities.UserEntityType), ID: initial.User.ID.String()}
			message := notification.Message{Type: "lifecycle", Push: &push.Message{Title: "Test"}}
			queued, err := n.Enqueue(t.Context(), recipient, message)
			if err != nil {
				t.Fatal(err)
			}
			switch transition {
			case "replay":
				if _, err = auth.Refresh(t.Context(), initial.RefreshToken); err != nil {
					t.Fatal(err)
				}
				if _, err = auth.Refresh(t.Context(), initial.RefreshToken); !errors.Is(err, domain.ErrUnauthenticated) {
					t.Fatal(err)
				}
			case "displaced":
				login()
			case "suspended":
				users, _ := service.NewUserService(repo.users)
				if _, err = users.ChangeStatus(t.Context(), initial.User.ID, enums.UserSuspended); err != nil {
					t.Fatal(err)
				}
			case "expired":
				if _, err = db.NewRaw("UPDATE sessions SET created_at=now()-interval '2 hours',last_used_at=now()-interval '2 hours',expires_at=now()-interval '1 hour' WHERE id=?", initial.SessionID).Exec(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = auth.Authenticate(t.Context(), initial.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatal("session remained active", err)
			}
			if err = n.Deliver(t.Context(), queued.DeliveryIDs[0]); err != nil {
				t.Fatal(err)
			}
			if _, err = n.Send(t.Context(), recipient, message); err != nil {
				t.Fatal(err)
			}
			if pusher.sends != 0 {
				t.Fatal("sent after session ended")
			}
			if err = db.NewSelect().Model(sub).WherePK().Scan(t.Context()); err != nil || sub.DisabledAt == nil {
				t.Fatal("stale registration not detached", err)
			}
			if transition == "suspended" {
				users, _ := service.NewUserService(repo.users)
				if _, err = users.ChangeStatus(t.Context(), initial.User.ID, enums.UserActive); err != nil {
					t.Fatal(err)
				}
			}
			fresh := login()
			principal.SessionID = fresh.SessionID
			if _, err = auth.SubscribePush(t.Context(), principal, sub.InstallationID, "ExpoPushToken[lifecycle]"); err != nil {
				t.Fatal(err)
			}
			if _, err = n.Send(t.Context(), recipient, message); err != nil {
				t.Fatal(err)
			}
			if pusher.sends != 1 {
				t.Fatal("fresh session did not restore delivery")
			}
		})
	}
}
