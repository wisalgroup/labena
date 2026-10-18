package identity_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	identitymigrations "github.com/wisalgroup/labena/internal/modules/identity/migrations"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	shareddomain "github.com/wisalgroup/labena/internal/modules/shared/domain"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/pagination"
)

func TestPaginateUnavailableDatabase(t *testing.T) {
	_, _, err := repository.NewUserRepository(nil).List(t.Context(), pagination.Pagination{})
	if !errors.Is(err, database.ErrNilDB) {
		t.Fatal(err)
	}
}

func TestPaginatePostgres(t *testing.T) {
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
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(db)
	for i := 1; i <= 5; i++ {
		id, _ := uuid.Parse(fmt.Sprintf("00000000-0000-4000-8000-%012d", i))
		email, _ := valueobjects.NewEmail(fmt.Sprintf("user%d@example.com", i))
		user := &entities.User{Status: enums.UserActive, Email: email, Name: "User", Entity: shareddomain.Entity{ID: id, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
		if err := repo.Create(t.Context(), user); err != nil {
			t.Fatal(err)
		}
	}
	id, _ := uuid.Parse("00000000-0000-4000-8000-000000000001")
	user, err := repo.Get(t.Context(), id)
	if err != nil || user.Email.String() != "user1@example.com" || user.UpdatedAt.IsZero() {
		t.Fatal(user, err)
	}
	user.ID = uuid.Must(uuid.NewV7())
	if err := repo.Create(t.Context(), user); !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatal("duplicate email", err)
	}
	if _, err := repo.Get(t.Context(), uuid.Must(uuid.NewV7())); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatal("missing user", err)
	}
	for _, tc := range []struct {
		page int
		ids  []string
	}{
		{1, []string{"00000000-0000-4000-8000-000000000005", "00000000-0000-4000-8000-000000000004"}},
		{2, []string{"00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000002"}},
		{3, []string{"00000000-0000-4000-8000-000000000001"}},
		{4, nil},
	} {
		users, total, err := repo.List(t.Context(), pagination.NewPagination(tc.page, 2))
		if err != nil || total != 5 || len(users) != len(tc.ids) || users == nil {
			t.Fatalf("page %d: %v %d %v", tc.page, users, total, err)
		}
		for i, id := range tc.ids {
			if users[i].ID.String() != id || users[i].Email.String() == "" || users[i].Name != "User" || users[i].CreatedAt.IsZero() {
				t.Fatal(users[i])
			}
		}
	}
}
