package repository

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
)

// verificationRepository borrows its pool and supports concurrent use.
type verificationRepository struct{ db *bun.DB }

// NewVerificationRepository borrows the application pool and performs no I/O.
// A nil pool supports offline assembly; database operations return database.ErrNilDB.
func NewVerificationRepository(db *bun.DB) VerificationRepository {
	return &verificationRepository{db: db}
}

func (r *verificationRepository) IssueEmailChallenge(ctx context.Context, c *entities.VerificationChallenge, p domain.VerificationCodePolicy) error {
	if c == nil || c.Purpose != enums.PurposeSignIn || c.Channel != enums.ChannelEmail || c.MaxAttempts != p.MaxAttemptsPerCode {
		return domain.ErrVerificationConfig
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	return authenticationTransaction(ctx, r.db, func(ctx context.Context, db bun.IDB) error {
		now := time.Now().UTC()
		// Serialize resends for this destination without coupling rate limits to SQL.
		if _, err := db.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "identity:otp:"+c.Destination); err != nil {
			return err
		}
		// SQL enforces resend cooldown in addition to shared Redis budgets.
		recent, err := db.NewSelect().Model((*entities.VerificationChallenge)(nil)).Where("destination = ? AND purpose = ? AND created_at > ?", c.Destination, c.Purpose, now.Add(-p.ResendCooldown)).Exists(ctx)
		if err != nil {
			return err
		}
		if recent {
			return domain.ErrVerificationLimited
		}
		var user entities.User
		query := db.NewSelect().Model(&user).Where("email = ?", c.Destination)
		// Bind sign-in proofs to existing accounts, including disabled accounts.
		// A later email change must not turn their old proof into a new account.
		err = query.Scan(ctx)
		c.UserID = nil
		if err == nil {
			id := user.ID
			c.UserID = &id
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = db.NewUpdate().Model((*entities.VerificationChallenge)(nil)).Set("invalidated_at = ?", now).Where("destination = ?", c.Destination).Where("purpose = ?", c.Purpose).Where("consumed_at IS NULL AND invalidated_at IS NULL").Exec(ctx)
		if err != nil {
			return err
		}
		_, err = db.NewInsert().Model(c).Exec(ctx)
		return err
	})
}

func (r *verificationRepository) GetChallenge(ctx context.Context, id uuid.UUID) (*entities.VerificationChallenge, error) {
	db, err := executor(ctx, r.db)
	if err != nil {
		return nil, err
	}
	c := &entities.VerificationChallenge{ID: id}
	if err = db.NewSelect().Model(c).WherePK().Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrVerificationInvalid
		}
		return nil, err
	}
	return c, nil
}

// PruneVerification deletes expired OTP records in bounded batches. Redis owns budget expiry.
func (r *verificationRepository) PruneVerification(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, domain.ErrVerificationConfig
	}
	db, err := executor(ctx, r.db)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	result, err := db.NewRaw("DELETE FROM verification_challenges WHERE id IN (SELECT id FROM verification_challenges WHERE expires_at < ? ORDER BY expires_at LIMIT ? FOR UPDATE SKIP LOCKED)", now, limit).Exec(ctx)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return count, err
	}
	return count, nil
}

// The callback only writes through db. Proof consumption, per-code attempts and its effect
// commit together. A denial is returned after commit so failed attempts persist.
func consumeEmailChallenge(ctx context.Context, pool *bun.DB, proof ChallengeProof, apply func(context.Context, bun.IDB, *entities.VerificationChallenge, *entities.User, time.Time) error) error {
	p := proof.Policy
	purpose := proof.Purpose
	if err := p.Validate(); err != nil {
		return err
	}
	var denial error
	err := authenticationTransaction(ctx, pool, func(ctx context.Context, db bun.IDB) error {
		now := time.Now().UTC()
		c := &entities.VerificationChallenge{ID: proof.ID}
		if err := db.NewSelect().Model(c).WherePK().For("UPDATE").Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				denial = domain.ErrVerificationInvalid
				return nil
			}
			return err
		}
		// Locks may have waited past expiry; validate against the time after acquisition.
		now = time.Now().UTC()
		if c.Purpose != purpose || c.Channel != enums.ChannelEmail || c.ConsumedAt != nil || c.InvalidatedAt != nil || !now.Before(c.ExpiresAt) || c.AttemptCount >= c.MaxAttempts {
			denial = domain.ErrVerificationInvalid
			return nil
		}
		c.AttemptCount++
		if _, err := db.NewUpdate().Model(c).Column("attempt_count").WherePK().Exec(ctx); err != nil {
			return err
		}
		if !hmac.Equal(c.CodeDigest, proof.Digest) {
			denial = domain.ErrVerificationInvalid
			return nil
		}
		user := &entities.User{}
		query := db.NewSelect().Model(user).For("UPDATE")
		if c.UserID != nil {
			query.Where("id = ?", *c.UserID)
		} else {
			query.Where("email = ?", c.Destination)
		}
		if err := query.Scan(ctx); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if c.UserID != nil {
				denial = domain.ErrVerificationInvalid
				return nil
			}
			user = nil // Account creation is permitted only after a valid sign-in proof.
		}
		now = time.Now().UTC()
		if !now.Before(c.ExpiresAt) || (user != nil && (user.DeletedAt != nil || user.Status != enums.UserActive || user.Email.String() != c.Destination)) {
			denial = domain.ErrVerificationInvalid
			return nil
		}
		if err := apply(ctx, db, c, user, now); err != nil {
			return err
		}
		c.ConsumedAt = &now
		_, err := db.NewUpdate().Model(c).Column("consumed_at", "user_id").WherePK().Exec(ctx)
		return err
	})
	if err != nil {
		return err
	}
	return denial
}
