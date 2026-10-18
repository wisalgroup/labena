package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	identityjobs "github.com/wisalgroup/labena/internal/modules/identity/jobs"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/internal/platform/notification"
	notificationmigrations "github.com/wisalgroup/labena/internal/platform/notification/migrations"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/mailview"
)

type usersStub struct {
	service.UserService
	user  *entities.User
	err   error
	calls int
}

func (u *usersStub) Get(context.Context, uuid.UUID) (*entities.User, error) {
	u.calls++
	return u.user, u.err
}

type unexpectedMailer struct{}

func (unexpectedMailer) Send(context.Context, mail.Message) error {
	panic("welcome job must persist intent without sending email")
}

func TestWelcomeReloadsAccountAndSelectsChannels(t *testing.T) {
	for _, tc := range []struct {
		name              string
		verified, enabled bool
		status            enums.UserStatus
		missing           bool
		database, email   bool
	}{
		{"verified", true, true, enums.UserActive, false, true, true},
		{"unverified", false, true, enums.UserActive, false, true, false},
		{"mail disabled", true, false, enums.UserActive, false, true, false},
		{"suspended", true, true, enums.UserSuspended, false, false, false},
		{"pending deletion", true, true, enums.UserPendingDeletion, false, false, false},
		{"deleted", true, true, enums.UserDeleted, false, false, false},
		{"missing", true, true, enums.UserActive, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email, _ := valueobjects.NewEmail("current@example.com")
			user, _ := entities.NewUser("Current name", email)
			user.Status = tc.status
			if tc.verified {
				now := time.Now()
				user.EmailVerifiedAt = &now
			}
			repo := &usersStub{user: user}
			if tc.missing {
				repo.err = domain.ErrUserNotFound
			}
			db := testdb.OpenDriver(t, database.DriverPostgres)
			var registry migrate.Registry
			if err := registry.Register("cache", testdb.Migrations(t, database.DriverPostgres)); err != nil {
				t.Fatal(err)
			}
			if err := registry.Register("notifications", notificationmigrations.Files); err != nil {
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
			views, err := mailview.New(mailview.Brand{Name: "Test", WebsiteURL: "https://example.com", AccentColor: "#000000"})
			if err != nil {
				t.Fatal(err)
			}
			notifier, err := notification.New(db, notification.Options{Mailer: unexpectedMailer{}, MailRenderer: views})
			if err != nil {
				t.Fatal(err)
			}
			handler := Welcome{Users: repo, Notifier: notifier, EmailEnabled: tc.enabled}
			task, err := identityjobs.NewWelcome(user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := handler.Handle(t.Context(), task); err != nil {
				t.Fatal(err)
			}
			var deliveries []notification.Delivery
			if err := db.NewSelect().Model(&deliveries).Scan(t.Context()); err != nil {
				t.Fatal(err)
			}
			expected := 0
			if tc.database {
				expected++
			}
			if tc.email {
				expected++
			}
			if repo.calls != 1 || len(deliveries) != expected {
				t.Fatal("wrong account/delivery behavior", repo.calls, len(deliveries))
			}
			for _, delivery := range deliveries {
				if delivery.Key != "welcome" || delivery.RecipientType != string(entities.UserEntityType) || delivery.RecipientID != user.ID.String() {
					t.Fatal("current identity lost", delivery)
				}
				if delivery.Channel == notification.ChannelEmail && (!strings.Contains(string(delivery.Payload), "Welcome, Current name.") || !strings.Contains(string(delivery.Payload), "current@example.com")) {
					t.Fatal("mail ignored current profile")
				}
			}
		})
	}
}
func TestWelcomeRejectsMalformedJobsAndRetriesInfrastructureErrors(t *testing.T) {
	repo := &usersStub{}
	notifier, _ := notification.New(nil, notification.Options{})
	handler := Welcome{Users: repo, Notifier: notifier}
	for _, payload := range []string{`{}`, `null`, `broken`, `{"user_id":"invalid"}`, `{"user_id":"00000000-0000-0000-0000-000000000000"}`} {
		err := handler.Handle(t.Context(), asynq.NewTask(identityjobs.WelcomeType, []byte(payload)))
		if !errors.Is(err, asynq.SkipRetry) {
			t.Fatal("malformed task retries", payload, err)
		}
	}
	if repo.calls != 0 {
		t.Fatal("invalid task reached dependencies")
	}
	task, _ := identityjobs.NewWelcome(uuid.New())
	failure := errors.New("database unavailable")
	repo.err = failure
	if err := handler.Handle(t.Context(), task); !errors.Is(err, failure) || errors.Is(err, asynq.SkipRetry) {
		t.Fatal("lookup failure not retryable", err)
	}
	repo.err = nil
	email, _ := valueobjects.NewEmail("member@example.com")
	repo.user, _ = entities.NewUser("Member", email)
	failure = database.ErrNilDB
	if err := handler.Handle(t.Context(), task); !errors.Is(err, failure) || errors.Is(err, asynq.SkipRetry) {
		t.Fatal("intent failure not retryable", err)
	}
}
