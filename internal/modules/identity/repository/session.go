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
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/pagination"
)

// sessionRepository borrows its pool and supports concurrent use.
type sessionRepository struct{ db *bun.DB }

// NewSessionRepository borrows the application pool and performs no I/O.
// A nil pool supports offline assembly; database operations return database.ErrNilDB.
func NewSessionRepository(db *bun.DB) SessionRepository {
	return &sessionRepository{db: db}
}

func (r *sessionRepository) CompleteSignIn(ctx context.Context, proof ChallengeProof, session *entities.Session, refresh *entities.SessionRefreshToken, multiple bool, name string) (*entities.User, bool, error) {
	proof.Purpose = enums.PurposeSignIn
	return completeSignIn(ctx, r.db, proof, session, refresh, multiple, name)
}

// RotateRefresh uses strict reuse detection. Losing a successful response requires
// sign-in again; replaying the consumed credential revokes its whole session.
func (r *sessionRepository) RotateRefresh(ctx context.Context, hash []byte, next *entities.SessionRefreshToken, p domain.SessionPolicy) (*entities.Session, error) {
	if len(hash) != 32 || next == nil || len(next.TokenHash) != 32 || next.ID == uuid.Nil {
		return nil, domain.ErrUnauthenticated
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var session *entities.Session
	var denial error
	err := authenticationTransaction(ctx, r.db, func(ctx context.Context, db bun.IDB) error {
		old := new(entities.SessionRefreshToken)
		if err := db.NewSelect().Model(old).Where("token_hash = ?", hash).Scan(ctx); err != nil {
			return authLookupError(err)
		}
		candidate := new(entities.Session)
		if err := db.NewSelect().Model(candidate).Where("id = ?", old.SessionID).Scan(ctx); err != nil {
			return authLookupError(err)
		}
		user, err := lockSessionUser(ctx, db, candidate.UserID)
		if err != nil {
			return err
		}
		if err := db.NewSelect().Model(candidate).WherePK().For("UPDATE").Scan(ctx); err != nil {
			return authLookupError(err)
		}
		if err := db.NewSelect().Model(old).WherePK().For("UPDATE").Scan(ctx); err != nil {
			return authLookupError(err)
		}
		now := time.Now().UTC()
		if !activeSessionUser(user) || candidate.RevokedAt != nil || (candidate.ExpiresAt != nil && !now.Before(*candidate.ExpiresAt)) {
			return domain.ErrUnauthenticated
		}
		// Check consumption before token expiry; retained history detects old replays too.
		if old.ConsumedAt != nil {
			_, err := db.NewUpdate().Model(candidate).Set("revoked_at = ?", now).WherePK().Exec(ctx)
			denial = domain.ErrUnauthenticated
			return err
		}
		if old.ExpiresAt != nil && !now.Before(*old.ExpiresAt) {
			return domain.ErrUnauthenticated
		}
		next.SessionID = candidate.ID
		next.CreatedAt = now
		next.ExpiresAt = candidate.ExpiresAt
		if p.RefreshTokenLifetime > 0 {
			expires := now.Add(p.RefreshTokenLifetime)
			if next.ExpiresAt == nil || expires.Before(*next.ExpiresAt) {
				next.ExpiresAt = &expires
			}
		}
		if err := next.Validate(); err != nil {
			return err
		}
		if _, err := db.NewUpdate().Model(old).Set("consumed_at = ?", now).WherePK().Exec(ctx); err != nil {
			return err
		}
		if _, err := db.NewInsert().Model(next).Exec(ctx); err != nil {
			return err
		}
		candidate.LastUsedAt = now
		if _, err := db.NewUpdate().Model(candidate).Column("last_used_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		session = candidate
		return nil
	})
	if err != nil {
		return nil, err
	}
	if denial != nil {
		return nil, denial
	}
	return session, nil
}

func activeSessionUser(user *entities.User) bool {
	return user.Status == enums.UserActive && user.DeletedAt == nil && user.EmailVerifiedAt != nil
}
func authLookupError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrUnauthenticated
	}
	return err
}
func lockSessionUser(ctx context.Context, db bun.IDB, id uuid.UUID) (*entities.User, error) {
	user := new(entities.User)
	if err := db.NewSelect().Model(user).Where("id = ?", id).For("UPDATE").Scan(ctx); err != nil {
		return nil, authLookupError(err)
	}
	return user, nil
}

func (r *sessionRepository) ListSessions(ctx context.Context, userID uuid.UUID, p pagination.Pagination) ([]*entities.Session, int, error) {
	if userID == uuid.Nil {
		return nil, 0, domain.ErrUnauthenticated
	}
	db, err := executor(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	p = pagination.NewPagination(p.Page, p.PerPage)
	rows := make([]*entities.Session, 0)
	total, err := db.NewSelect().Model(&rows).Where("user_id = ?", userID).Where("revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", time.Now().UTC()).Order("created_at DESC", "id DESC").Limit(p.PerPage).Offset(p.Offset()).ScanAndCount(ctx)
	return rows, total, err
}
func (r *sessionRepository) RevokeSession(ctx context.Context, userID, id uuid.UUID) error {
	if id == uuid.Nil {
		return domain.ErrSessionNotFound
	}
	return r.revokeSessions(ctx, userID, &id)
}
func (r *sessionRepository) RevokeSessions(ctx context.Context, userID uuid.UUID) error {
	return r.revokeSessions(ctx, userID, nil)
}
func (r *sessionRepository) revokeSessions(ctx context.Context, userID uuid.UUID, id *uuid.UUID) error {
	if userID == uuid.Nil {
		return domain.ErrUnauthenticated
	}
	operation := func(ctx context.Context, db bun.IDB) error {
		if _, err := lockSessionUser(ctx, db, userID); err != nil {
			return err
		}
		query := db.NewUpdate().Model((*entities.Session)(nil)).Set("revoked_at = COALESCE(revoked_at, ?)", time.Now().UTC()).Where("user_id = ?", userID)
		if id != nil {
			query.Where("id = ?", *id)
		}
		result, err := query.Exec(ctx)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if id != nil && count == 0 {
			return domain.ErrSessionNotFound
		}
		return nil
	}
	// Revocation has no failed-proof counters to commit independently and can join
	// a transaction which also detaches session-owned push registrations.
	if tx, err := database.TxFromContext(ctx, r.db); err == nil {
		return operation(ctx, tx)
	} else if !errors.Is(err, database.ErrNoTransaction) {
		return err
	}
	return authenticationTransaction(ctx, r.db, operation)
}

// History is retained while a session can still be used, including unlimited sessions.
// Revoked or absolutely expired sessions and their tokens can be removed. Delete
// tokens in bounded batches first; parent deletion never cascades unbounded history.
func (r *sessionRepository) PruneSessions(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, domain.ErrSessionConfig
	}
	db, err := executor(ctx, r.db)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	result, err := db.NewRaw(`DELETE FROM session_refresh_tokens WHERE id IN (
 SELECT rt.id FROM session_refresh_tokens rt JOIN sessions s ON s.id = rt.session_id
 WHERE s.revoked_at IS NOT NULL OR s.expires_at < ?
 ORDER BY s.expires_at NULLS LAST, rt.id LIMIT ? FOR UPDATE OF rt SKIP LOCKED)`, now, limit).Exec(ctx)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return count, err
	}
	result, err = db.NewRaw(`DELETE FROM sessions WHERE id IN (SELECT s.id FROM sessions s
 WHERE (s.revoked_at IS NOT NULL OR s.expires_at < ?) AND NOT EXISTS (SELECT 1 FROM session_refresh_tokens rt WHERE rt.session_id = s.id)
 ORDER BY s.expires_at NULLS LAST, s.id LIMIT ? FOR UPDATE OF s SKIP LOCKED)`, now, limit).Exec(ctx)
	if err != nil {
		return count, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return count + n, err
	}
	return count + n, nil
}

func insertSession(ctx context.Context, db bun.IDB, user *entities.User, session *entities.Session, refresh *entities.SessionRefreshToken, multiple bool, now time.Time) error {
	session.UserID = user.ID
	// The user row is already locked. Concurrent logins, revocations and refreshes
	// take this lock before any session lock, including under the single-session policy.
	if err := session.Validate(); err != nil {
		return err
	}
	if err := refresh.Validate(); err != nil {
		return err
	}
	if refresh.SessionID != session.ID ||
		(session.ExpiresAt != nil && (refresh.ExpiresAt == nil || refresh.ExpiresAt.After(*session.ExpiresAt))) ||
		(refresh.ExpiresAt != nil && !now.Before(*refresh.ExpiresAt)) {
		return domain.ErrSessionConfig
	}
	if !multiple {
		if _, err := db.NewUpdate().Model((*entities.Session)(nil)).Set("revoked_at = ?", now).Where("user_id = ?", user.ID).Where("revoked_at IS NULL").Exec(ctx); err != nil {
			return err
		}
	}
	if _, err := db.NewInsert().Model(session).Exec(ctx); err != nil {
		return err
	}
	if _, err := db.NewInsert().Model(refresh).Exec(ctx); err != nil {
		return err
	}
	_, err := db.NewUpdate().Model(user).Set("last_active_at = ?", now).Set("updated_at = GREATEST(updated_at, ?)", now).WherePK().Exec(ctx)
	return err
}

func completeSignIn(ctx context.Context, pool *bun.DB, proof ChallengeProof, session *entities.Session, refresh *entities.SessionRefreshToken, multiple bool, name string) (*entities.User, bool, error) {
	if session == nil || refresh == nil {
		return nil, false, domain.ErrSessionConfig
	}
	var account *entities.User
	var created bool
	err := consumeEmailChallenge(ctx, pool, proof, func(ctx context.Context, db bun.IDB, challenge *entities.VerificationChallenge, user *entities.User, now time.Time) error {

		if user == nil {
			email, err := valueobjects.NewEmail(challenge.Destination)
			if err != nil {
				return err
			}
			user, err = entities.NewUser(name, email)
			if err != nil {
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
			if count == 1 {
				created = true
			} else {
				// A concurrent social sign-in or internal account creation may win.

				if err := db.NewSelect().Model(user).Where("email = ?", challenge.Destination).For("UPDATE").Scan(ctx); err != nil {
					return err
				}
			}
		}
		now = time.Now().UTC()
		if !now.Before(challenge.ExpiresAt) || user.Status != enums.UserActive || user.DeletedAt != nil || user.Email.String() != challenge.Destination {
			return domain.ErrVerificationInvalid
		}
		if user.EmailVerifiedAt == nil {
			if err := user.VerifyEmail(user.Email, now); err != nil {
				return err
			}
			if _, err := db.NewUpdate().Model(user).Column("email_verified_at", "updated_at").WherePK().Exec(ctx); err != nil {
				return err
			}
		}

		challenge.UserID = &user.ID
		account = user

		return insertSession(ctx, db, user, session, refresh, multiple, now)
	})
	if err != nil {
		return nil, false, err
	}
	return account, created, nil
}

func (r *sessionRepository) AuthenticateSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	// An explicit transaction requests a stable authorization decision for a
	// related SQL mutation (for example registering a session-owned push device).
	// Match revocation's account-first lock order; ordinary bearer checks stay reads.
	if tx, err := database.TxFromContext(ctx, r.db); err == nil {
		if _, err := lockSessionUser(ctx, tx, userID); err != nil {
			return err
		}
	} else if !errors.Is(err, database.ErrNoTransaction) {
		return err
	}
	return authenticateSession(ctx, r.db, userID, sessionID)
}

// Every authenticated request checks authoritative session and account state.
func authenticateSession(ctx context.Context, pool *bun.DB, userID, sessionID uuid.UUID) error {
	db, err := executor(ctx, pool)
	if err != nil {
		return err
	}
	exists, err := db.NewSelect().TableExpr("sessions AS s").Join("JOIN users AS u ON u.id = s.user_id").
		Where("s.id = ? AND s.user_id = ?", sessionID, userID).
		Where("s.revoked_at IS NULL AND (s.expires_at IS NULL OR s.expires_at > ?)", time.Now().UTC()).
		Where("u.status = ? AND u.deleted_at IS NULL AND u.email_verified_at IS NOT NULL", enums.UserActive).Exists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return domain.ErrUnauthenticated
	}
	return nil
}
