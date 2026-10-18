package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
)

// socialRepository borrows its pool and supports concurrent use.
type socialRepository struct{ db *bun.DB }

// NewSocialRepository borrows the application pool and performs no I/O.
// A nil pool supports offline assembly; database operations return database.ErrNilDB.
func NewSocialRepository(db *bun.DB) SocialRepository {
	return &socialRepository{db: db}
}

func (r *socialRepository) CompleteSocialSignIn(ctx context.Context, provider enums.AuthProvider, claim domain.SocialIdentity, session *entities.Session, refresh *entities.SessionRefreshToken, multiple bool) (*entities.User, bool, error) {
	if !provider.Valid() || claim.Subject == "" || len(claim.Subject) > 255 || session == nil || refresh == nil {
		return nil, false, domain.ErrSocialInvalid
	}
	var account *entities.User
	var created bool
	err := authenticationTransaction(ctx, r.db, func(ctx context.Context, db bun.IDB) error {
		// Serialize first login for a subject, even before its identity row exists.
		// Transaction-scoped lock; hash collisions only serialize unrelated sign-ins.
		if _, err := db.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", string(provider)+":"+claim.Subject); err != nil {
			return err
		}
		identity := new(entities.AuthIdentity)
		err := db.NewSelect().Model(identity).Where("provider = ? AND provider_subject = ?", provider, claim.Subject).Scan(ctx)
		var user *entities.User
		if errors.Is(err, sql.ErrNoRows) {
			if !claim.EmailVerified {
				return domain.ErrSocialEmail
			}
			email, err := valueobjects.NewEmail(claim.Email)
			if err != nil {
				return domain.ErrSocialEmail
			}
			user, err = entities.NewUser(claim.Name, email)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			if err := user.VerifyEmail(email, now); err != nil {
				return err
			}
			result, err := db.NewInsert().Model(user).On("CONFLICT (email) DO NOTHING").Exec(ctx)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if count == 0 {
				return domain.ErrSocialLinkRequired
			}
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			identity = &entities.AuthIdentity{ID: id, UserID: user.ID, Provider: provider, ProviderSubject: claim.Subject, CreatedAt: now}
			if err := identity.Validate(); err != nil {
				return err
			}
			if _, err := db.NewInsert().Model(identity).Exec(ctx); err != nil {
				return err
			}
			created = true
		} else if err != nil {
			return err
		} else {
			user, err = lockSessionUser(ctx, db, identity.UserID)
			if err != nil {
				return err
			}
		}
		account = user
		if !activeSessionUser(user) {
			return domain.ErrUnauthenticated
		}
		now := time.Now().UTC()
		if err := insertSession(ctx, db, user, session, refresh, multiple, now); err != nil {
			return err
		}
		_, err = db.NewUpdate().Model(identity).Set("last_authenticated_at = ?", now).WherePK().Exec(ctx)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return account, created, nil
}
