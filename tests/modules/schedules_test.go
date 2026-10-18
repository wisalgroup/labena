package modules_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/identity"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/internal/platform/dependencies"
	"github.com/wisalgroup/labena/internal/platform/media"
	mediaintegration "github.com/wisalgroup/labena/internal/platform/media/integration"
	"github.com/wisalgroup/labena/internal/platform/notification"
	notificationintegration "github.com/wisalgroup/labena/internal/platform/notification/integration"
	notificationmigrations "github.com/wisalgroup/labena/internal/platform/notification/migrations"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

// Unexpected authentication operations panic through the nil embed.
type maintenanceAuth struct {
	service.AuthService
	sessions, verification int
}

func (a *maintenanceAuth) PruneSessions(context.Context, int) (int64, error) {
	a.sessions++
	return 0, nil
}
func (a *maintenanceAuth) PruneVerification(context.Context, int) (int64, error) {
	a.verification++
	return 0, nil
}

func TestIdentityScheduledCommandFactories(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, commands := range []bool{false, true} {
			t.Run(fmt.Sprintf("auth=%t/commands=%t", enabled, commands), func(t *testing.T) {
				borrowed := &maintenanceAuth{}
				var authentication service.AuthService
				if enabled {
					authentication = borrowed
				}
				targets := dependencies.Targets{}
				if commands {
					targets.ScheduledCommands = &cli.ScheduledCommands{}
				}
				if err := identity.Register(dependencies.Resources{}, targets, authentication, false); err != nil {
					t.Fatal(err)
				}
				if borrowed.sessions != 0 || borrowed.verification != 0 {
					t.Fatal("registration executed maintenance")
				}
				if targets.ScheduledCommands != nil {
					for _, name := range []string{identity.PruneSessionsCommand, identity.PruneVerificationCommand} {
						task, err := targets.ScheduledCommands.Task(name)
						if (err == nil) != enabled {
							t.Fatalf("command %s enabled=%t: %v", name, enabled, err)
						}
						if enabled {
							for range 2 {
								if err := targets.ScheduledCommands.ProcessTask(t.Context(), task); err != nil {
									t.Fatal(err)
								}
							}
						}
					}
				}
				want := 0
				if enabled && commands {
					want = 2
				}
				if borrowed.sessions != want || borrowed.verification != want {
					t.Fatal("factories did not reuse the supplied auth service", borrowed.sessions, borrowed.verification)
				}
			})
		}
	}
}

func TestMaintenanceHandlersRegisterWithoutScheduler(t *testing.T) {
	library, err := media.New(nil, nil, "", media.Config{})
	if err != nil {
		t.Fatal(err)
	}
	targets := dependencies.Targets{TaskRouter: asynq.NewServeMux()}
	if err := mediaintegration.Register(library, targets); err != nil {
		t.Fatal(err)
	}
	notifier, err := notification.New(nil, notification.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := notificationintegration.Register(notifier, targets); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{media.RecoverTask, notification.PruneSubscriptionsTask} {
		if _, pattern := targets.TaskRouter.Handler(asynq.NewTask(name, nil)); pattern == "" {
			t.Fatal("missing handler", name)
		}
	}
}

func TestSubscriptionPruningRejectsMalformedTasks(t *testing.T) {
	notifier, _ := notification.New(nil, notification.Options{})
	mux := asynq.NewServeMux()
	if err := notificationintegration.Register(notifier, dependencies.Targets{TaskRouter: mux}); err != nil {
		t.Fatal(err)
	}
	for _, task := range []*asynq.Task{
		asynq.NewTask(notification.PruneSubscriptionsTask, []byte(`{}`)),
		asynq.NewTask(notification.PruneSubscriptionsTask+".unknown", nil),
	} {
		if err := mux.ProcessTask(t.Context(), task); !errors.Is(err, asynq.SkipRetry) {
			t.Fatal(err)
		}
	}
	if err := mux.ProcessTask(t.Context(), asynq.NewTask(notification.PruneSubscriptionsTask, nil)); !errors.Is(err, database.ErrNilDB) || errors.Is(err, asynq.SkipRetry) {
		t.Fatal("database failure must remain retryable", err)
	}
}

func TestSubscriptionPruningHandlerPostgres(t *testing.T) {
	db := testdb.OpenDriver(t, database.DriverPostgres)
	var registry migrate.Registry
	for name, files := range map[string]fs.FS{"cache": testdb.Migrations(t, database.DriverPostgres), "notification": notificationmigrations.Files} {
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
	notifier, err := notification.New(db, notification.Options{})
	if err != nil {
		t.Fatal(err)
	}
	mux := asynq.NewServeMux()
	if err := notificationintegration.Register(notifier, dependencies.Targets{TaskRouter: mux}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-95 * 24 * time.Hour)
	rows := make([]notification.Subscription, 1003)
	for i := range rows {
		rows[i] = notification.Subscription{ID: uuid.New(), RecipientType: "user", RecipientID: "owner", InstallationID: uuid.NewString(), Project: "test", Registration: "ExpoPushToken[test]", RegistrationHash: uuid.NewString(), Version: 1, UpdatedAt: old}
	}
	rows[1001].UpdatedAt = time.Now()
	rows[1002].DisabledAt = &old
	if _, err := db.NewInsert().Model(&rows).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{1000, 1001, 1001} {
		if err := mux.ProcessTask(t.Context(), asynq.NewTask(notification.PruneSubscriptionsTask, nil)); err != nil {
			t.Fatal(err)
		}
		count, err := db.NewSelect().Model((*notification.Subscription)(nil)).Where("version=2 AND disabled_at IS NOT NULL").Count(t.Context())
		if err != nil || count != want {
			t.Fatal("batch or idempotency", count, want, err)
		}
	}
	count, err := db.NewSelect().Model((*notification.Subscription)(nil)).Where("version=1 AND id IN (?)", bun.In([]uuid.UUID{rows[1001].ID, rows[1002].ID})).Count(t.Context())
	if err != nil || count != 2 {
		t.Fatal("recent/already disabled subscriptions changed", count, err)
	}
}
