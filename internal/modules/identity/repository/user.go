package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/pagination"
)

// userRepository borrows its pool and supports concurrent use.
type userRepository struct{ db *bun.DB }

// NewUserRepository borrows the application pool and performs no I/O.
// A nil pool supports offline assembly; database operations return database.ErrNilDB.
func NewUserRepository(db *bun.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, u *entities.User) error {
	if u == nil {
		return domain.ErrUserRequired
	}
	if err := u.Validate(); err != nil {
		return err
	}
	db, err := executor(ctx, r.db)
	if err != nil {
		return err
	}
	_, err = db.NewInsert().Model(u).Exec(ctx)
	return userWriteError(err)
}

// List uses a deterministic order. The count and rows are separate reads;
// concurrent writes may change the total between them.
func (r *userRepository) List(ctx context.Context, p pagination.Pagination) ([]*entities.User, int, error) {
	db, err := executor(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	p = pagination.NewPagination(p.Page, p.PerPage)
	users := make([]*entities.User, 0)
	total, err := db.NewSelect().Model(&users).Where("deleted_at IS NULL").Order("created_at DESC", "id DESC").Limit(p.PerPage).Offset(p.Offset()).ScanAndCount(ctx)
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// Edit serializes concurrent account changes. A successful callback is not committed
// until Execute succeeds; when joining a transaction the caller owns the final commit.
func (r *userRepository) Edit(ctx context.Context, id uuid.UUID, change func(*entities.User) error) (*entities.User, error) {
	if change == nil {
		return nil, domain.ErrUserChangeRequired
	}
	tx, err := database.NewTxManager(r.db, nil)
	if err != nil {
		return nil, err
	}
	var result *entities.User
	err = tx.Execute(ctx, func(ctx context.Context) error {
		db, err := executor(ctx, r.db)
		if err != nil {
			return err
		}
		user := new(entities.User)
		if err = db.NewSelect().Model(user).Where("id = ?", id).For("UPDATE").Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrUserNotFound
			}
			return err
		}
		createdAt := user.CreatedAt
		originalEmail := user.Email.String()
		originalPhone := ""
		if user.Phone != nil {
			originalPhone = *user.Phone
		}
		if err = change(user); err != nil {
			return err
		}
		if user.ID != id || !user.CreatedAt.Equal(createdAt) {
			return entities.ErrInvalidUserState
		}
		if err = user.Validate(); err != nil {
			return err
		}
		_, err = db.NewUpdate().Model(user).Column("email", "email_verified_at", "phone", "phone_verified_at", "status", "last_active_at", "deleted_at", "name", "updated_at").WherePK().Exec(ctx)
		if err != nil {
			return userWriteError(err)
		}
		phone := ""
		if user.Phone != nil {
			phone = *user.Phone
		}
		if user.Status != enums.UserActive || user.DeletedAt != nil || user.Email.String() != originalEmail || phone != originalPhone || user.EmailVerifiedAt == nil {
			if _, err := db.NewUpdate().Model((*entities.Session)(nil)).Set("revoked_at = ?", time.Now().UTC()).Where("user_id = ?", user.ID).Where("revoked_at IS NULL").Exec(ctx); err != nil {
				return err
			}
		}
		result = user
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func userWriteError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		switch pg.ConstraintName {
		case "users_email_key":
			return domain.ErrEmailTaken
		case "users_phone_key":
			return domain.ErrPhoneTaken
		}
	}
	return err
}

func (r *userRepository) Get(ctx context.Context, id uuid.UUID) (*entities.User, error) {
	return getUser(ctx, r.db, id)
}

func getUser(ctx context.Context, pool *bun.DB, id uuid.UUID) (*entities.User, error) {
	db, err := executor(ctx, pool)
	if err != nil {
		return nil, err
	}
	var user entities.User
	if err = db.NewSelect().Model(&user).Where("id = ?", id).Where("deleted_at IS NULL").Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}
