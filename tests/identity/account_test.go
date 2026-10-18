package identity_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	identitymigrations "github.com/wisalgroup/labena/internal/modules/identity/migrations"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/pagination"
)

func TestAccountMigrationAndEditsPostgres(t *testing.T) {
	db := testdb.OpenDriver(t, database.DriverPostgres)
	var registry migrate.Registry
	if err := registry.Register("test/cache", testdb.Migrations(t, database.DriverPostgres)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("identity", identitymigrations.Files); err != nil {
		t.Fatal(err)
	}
	history, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := migrate.NewProvider(t.Context(), db, history)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(t.Context(), 20261003200000); err != nil {
		t.Fatal(err)
	}
	var passwordColumns int
	if err := db.NewRaw(`SELECT count(*) FROM information_schema.columns
  WHERE table_schema = current_schema() AND table_name IN ('users', 'verification_challenges')
  AND column_name IN ('password_hash', 'password_changed_at', 'credential_version')`).Scan(t.Context(), &passwordColumns); err != nil {
		t.Fatal(err)
	}
	if passwordColumns != 0 {
		t.Fatal("password storage remains in fresh Identity schema")
	}
	id := uuid.New()
	now := time.Now().UTC()
	if _, err = db.NewRaw("INSERT INTO users (id,email,name,created_at,updated_at) VALUES (?, ?, ?, ?, ?)", id, "legacy@example.com", "Legacy", now, now).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(db)
	u, err := repo.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != enums.UserActive || u.Phone != nil || u.EmailVerifiedAt != nil || u.PhoneVerifiedAt != nil {
		t.Fatal("migration invented verification")
	}
	phone := "+201001234567"
	_, err = repo.Edit(t.Context(), id, func(u *entities.User) error {
		if err := u.ChangePhone(&phone, time.Now().UTC()); err != nil {
			return err
		}
		return u.VerifyPhone(phone, time.Now().UTC())
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err = repo.Get(t.Context(), id)
	if err != nil || u.Phone == nil || *u.Phone != phone || u.PhoneVerifiedAt == nil {
		t.Fatal("contact round trip", err)
	}
	email, _ := valueobjects.NewEmail("second@example.com")
	second, err := entities.NewUser("Second", email)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Create(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	_, err = repo.Edit(t.Context(), second.ID, func(u *entities.User) error { return u.ChangePhone(&phone, time.Now().UTC()) })
	if !errors.Is(err, domain.ErrPhoneTaken) {
		t.Fatal("phone uniqueness", err)
	}
	// A callback error and a later outer-transaction failure both roll back changes.
	rollback := errors.New("rollback")
	_, err = repo.Edit(t.Context(), id, func(u *entities.User) error { u.Name = "Rejected"; return rollback })
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	tx, err := database.NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.Execute(t.Context(), func(ctx context.Context) error {
		if _, err := repo.Edit(ctx, id, func(u *entities.User) error { return u.Rename("Outer rollback") }); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	u, err = repo.Get(t.Context(), id)
	if err != nil || u.Name != "Legacy" {
		t.Fatal("rollback escaped", err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			<-start
			_, err := repo.Edit(t.Context(), id, func(u *entities.User) error { return u.Rename(u.Name + "x") })
			if err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	u, err = repo.Get(t.Context(), id)
	if err != nil || u.Name != "Legacy"+strings.Repeat("x", 8) {
		t.Fatal("lost concurrent edits", u, err)
	}
	for _, status := range []enums.UserStatus{enums.UserPendingDeletion, enums.UserDeleted} {
		if _, err = repo.Edit(t.Context(), id, func(u *entities.User) error { return u.ChangeStatus(status, time.Now().UTC()) }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = repo.Get(t.Context(), id); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatal("deleted user returned", err)
	}
	users, total, err := repo.List(t.Context(), pagination.NewPagination(1, 10))
	if err != nil || total != 1 || len(users) != 1 {
		t.Fatal("deleted user listed", total, err)
	}
	_, err = repo.Edit(t.Context(), second.ID, func(u *entities.User) error { return u.ChangePhone(&phone, time.Now().UTC()) })
	if !errors.Is(err, domain.ErrPhoneTaken) {
		t.Fatal("deleted contact reservation lost", err)
	}
}
