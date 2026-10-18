package modules_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/wisalgroup/labena/examples/module/profile"
	identitymigrations "github.com/wisalgroup/labena/internal/modules/identity/migrations"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/platform/notification"
	notificationmigrations "github.com/wisalgroup/labena/internal/platform/notification/migrations"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/pagination"
)

func TestProfileNotificationWorkflowPostgres(t *testing.T) {
	db := testdb.OpenDriver(t, database.DriverPostgres)
	var registry migrate.Registry
	for name, files := range map[string]fs.FS{
		"cache":    testdb.Migrations(t, database.DriverPostgres),
		"identity": identitymigrations.Files, "notification": notificationmigrations.Files,
	} {
		if err := registry.Register(name, files); err != nil {
			t.Fatal(err)
		}
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
	transactions, err := database.NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	users, err := service.NewUserService(repository.NewUserRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	notifier, err := notification.New(db, notification.Options{})
	if err != nil {
		t.Fatal(err)
	}
	email, _ := valueobjects.NewEmail("profile@example.com")
	created, err := entities.NewUser("Original", email)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.NewUserRepository(db).Create(t.Context(), created); err != nil {
		t.Fatal(err)
	}
	userID := created.ID
	assertState := func(name string, intents int) {
		t.Helper()
		user, err := users.Get(t.Context(), userID)
		if err != nil || user.Name != name {
			t.Fatal("unexpected profile", user, err)
		}
		var count int
		if err := db.NewRaw("SELECT count(*) FROM notification_deliveries").Scan(t.Context(), &count); err != nil || count != intents {
			t.Fatal("unexpected intents", count, err)
		}
	}
	abort := errors.New("abort enclosing operation")
	err = transactions.Execute(t.Context(), func(ctx context.Context) error {
		if err := profile.Update(ctx, transactions, users, notifier, userID, "Rolled back"); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	assertState("Original", 0)
	if err := profile.Update(t.Context(), transactions, users, notifier, userID, "Updated"); err != nil {
		t.Fatal(err)
	}
	assertState("Updated", 1)
	recipient := notification.Recipient{Type: string(entities.UserEntityType), ID: userID.String()}
	if count, err := notifier.UnreadCount(t.Context(), recipient); err != nil || count != 0 {
		t.Fatal("enqueue delivered before processing", count, err)
	}
	page, err := notifier.ListDeliveries(t.Context(), notification.DeliveryPending, pagination.NewPagination(1, 20))
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	for range 2 {
		if err := notifier.Deliver(t.Context(), page.Items[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := notifier.UnreadCount(t.Context(), recipient); err != nil || count != 1 {
		t.Fatal("replay duplicated the database notification", count, err)
	}
	// A failure after UpdateProfile must roll back the earlier business write.
	if _, err := db.ExecContext(t.Context(), "ALTER TABLE notification_deliveries ADD CONSTRAINT reject_example_intent CHECK (type <> 'identity.profile_updated') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	if err := profile.Update(t.Context(), transactions, users, notifier, userID, "Must roll back"); err == nil {
		t.Fatal("failed intent accepted")
	}
	assertState("Updated", 1)
}
