package notification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/push"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// MaxInstallations bounds active destinations per recipient and push project.
const MaxInstallations = 100

var (
	ErrInstallationLimit    = apperror.Conflict("The active push installation limit has been reached. Unsubscribe an installation before adding another.")
	ErrSessionCheckRequired = apperror.Unavailable("Session-bound push requires a session eligibility check.")
)

// Subscription is one push installation owned by a recipient. Registration is
// private provider data; SessionID optionally ties it to an authenticated session.
type Subscription struct {
	bun.BaseModel    `bun:"table:notification_subscriptions,alias:s"`
	ID               uuid.UUID  `bun:",pk,type:uuid" json:"id"`
	RecipientType    string     `json:"-"`
	RecipientID      string     `json:"-"`
	InstallationID   string     `json:"installation_id"`
	SessionID        string     `json:"-"`
	Project          string     `json:"-"`
	Registration     string     `json:"-"`
	RegistrationHash string     `json:"-"`
	Version          int64      `json:"-"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DisabledAt       *time.Time `json:"-"`
}

// Subscribe registers or rotates an installation for an authorized recipient.
// InstallationID must be a client-generated UUID, persisted by that installation.
// An active registration owned by somebody else must first be detached by its owner.
func (n *Notifier) Subscribe(ctx context.Context, r Recipient, installation, session, registration string) (*Subscription, error) {
	if !r.Valid() {
		return nil, ErrInvalidRecipient
	}
	if _, err := uuid.Parse(installation); err != nil || !push.ValidToken(registration) || (session != "" && !validLabel(session, 200)) {
		return nil, ErrInvalidMessage
	}
	if n.options.Pusher == nil || n.options.PushProject == "" {
		return nil, ErrUnavailable
	}
	if session != "" && n.options.SessionActive == nil {
		return nil, ErrSessionCheckRequired
	}
	hash := sha256.Sum256([]byte(registration))
	row := &Subscription{ID: uuid.New(), RecipientType: r.Type, RecipientID: r.ID, InstallationID: installation, SessionID: session, Project: n.options.PushProject, Registration: registration, RegistrationHash: hex.EncodeToString(hash[:]), Version: 1}
	return n.store.subscribe(ctx, row)
}

func (s *store) subscribe(ctx context.Context, row *Subscription) (*Subscription, error) {
	manager, err := database.NewTxManager(s.db, nil)
	if err != nil {
		return nil, err
	}
	err = manager.Execute(ctx, func(ctx context.Context) error {
		db, err := s.executor(ctx)
		if err != nil {
			return err
		}
		// Every registration for this recipient/project shares the same lock,
		// including standalone callers and callers already in a transaction.
		key, _ := json.Marshal([]string{"notification-subscribe", row.Project, row.RecipientType, row.RecipientID})
		if _, err = db.NewRaw("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", string(key)).Exec(ctx); err != nil {
			return err
		}
		active := func() *bun.SelectQuery {
			return db.NewSelect().Model((*Subscription)(nil)).Where("project=? AND recipient_type=? AND recipient_id=? AND disabled_at IS NULL", row.Project, row.RecipientType, row.RecipientID)
		}
		count, err := active().Count(ctx)
		if err != nil {
			return err
		}
		if count >= MaxInstallations {
			existing, err := active().Where("installation_id=?", row.InstallationID).Exists(ctx)
			if err != nil {
				return err
			}
			if !existing {
				return ErrInstallationLimit
			}
		}
		// Keep identity stable across rotation. Only routing changes invalidate queued
		// versions; refreshing an unchanged registration updates its freshness timestamp.
		err = db.NewRaw(`INSERT INTO notification_subscriptions (id,recipient_type,recipient_id,installation_id,session_id,project,registration,registration_hash,version,updated_at)
 VALUES (?,?,?,?,?,?,?,?,1,now()) ON CONFLICT (project,installation_id) DO UPDATE SET recipient_type=EXCLUDED.recipient_type,recipient_id=EXCLUDED.recipient_id,session_id=EXCLUDED.session_id,registration=EXCLUDED.registration,registration_hash=EXCLUDED.registration_hash,version=notification_subscriptions.version+CASE WHEN notification_subscriptions.registration<>EXCLUDED.registration OR notification_subscriptions.session_id<>EXCLUDED.session_id OR notification_subscriptions.disabled_at IS NOT NULL THEN 1 ELSE 0 END,updated_at=now(),disabled_at=NULL
 WHERE notification_subscriptions.disabled_at IS NOT NULL OR (notification_subscriptions.recipient_type=EXCLUDED.recipient_type AND notification_subscriptions.recipient_id=EXCLUDED.recipient_id)
 RETURNING *`, row.ID, row.RecipientType, row.RecipientID, row.InstallationID, row.SessionID, row.Project, row.Registration, row.RegistrationHash).Scan(ctx, row)
		var state interface{ SQLState() string }
		if err != nil && !errors.Is(err, sql.ErrNoRows) && !(errors.As(err, &state) && state.SQLState() == "23505") {
			return err
		}
		if err != nil {
			return apperror.Conflict("Push registration could not be assigned to this installation.")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}

// Unsubscribe is recipient-scoped and idempotent. Disabling increments the version
// so already queued messages cannot follow a later reassignment of the installation.
func (n *Notifier) Unsubscribe(ctx context.Context, r Recipient, id uuid.UUID) error {
	if !r.Valid() || id == uuid.Nil {
		return ErrInvalidRecipient
	}
	return n.store.unsubscribe(ctx, r, id)
}

func (s *store) unsubscribe(ctx context.Context, r Recipient, id uuid.UUID) error {
	db, err := s.executor(ctx)
	if err != nil {
		return err
	}
	_, err = db.NewRaw(`UPDATE notification_subscriptions SET disabled_at=now(),version=version+1 WHERE id=? AND recipient_type=? AND recipient_id=? AND disabled_at IS NULL`, id, r.Type, r.ID).Exec(ctx)
	return err
}

// DetachSession disables destinations on logout. An empty session detaches all
// destinations of this recipient. It joins the caller's database transaction.
func (n *Notifier) DetachSession(ctx context.Context, r Recipient, session string) error {
	if !r.Valid() {
		return ErrInvalidRecipient
	}
	return n.store.detachSession(ctx, r, session)
}

func (s *store) detachSession(ctx context.Context, r Recipient, session string) error {
	db, err := s.executor(ctx)
	if err != nil {
		return err
	}
	q := db.NewUpdate().Model((*Subscription)(nil)).Set("disabled_at=now(),version=version+1").Where("recipient_type=? AND recipient_id=? AND disabled_at IS NULL", r.Type, r.ID)
	if session != "" {
		q = q.Where("session_id=?", session)
	}
	_, err = q.Exec(ctx)
	return err
}

// PruneSubscriptions disables stale destinations in bounded batches. Clients
// refresh registration timestamps on startup; app schedules this hourly when push is enabled.
func (n *Notifier) PruneSubscriptions(ctx context.Context, before time.Time) (int64, error) {
	if before.IsZero() || !before.Before(time.Now()) {
		return 0, ErrInvalidMessage
	}
	return n.store.pruneSubscriptions(ctx, before)
}

func (s *store) pruneSubscriptions(ctx context.Context, before time.Time) (int64, error) {
	db, err := s.executor(ctx)
	if err != nil {
		return 0, err
	}
	// Lock candidates before deciding they are stale. A registration being
	// refreshed is skipped and reconsidered by a later maintenance run.
	// Materialize once so re-evaluation cannot expand the bounded batch.
	res, err := db.NewRaw(`WITH stale AS MATERIALIZED (
 SELECT id FROM notification_subscriptions WHERE disabled_at IS NULL AND updated_at<?
 ORDER BY updated_at,id LIMIT 1000 FOR UPDATE SKIP LOCKED)
 UPDATE notification_subscriptions AS s SET disabled_at=now(),version=s.version+1
 FROM stale WHERE s.id=stale.id AND s.disabled_at IS NULL AND s.updated_at<?`, before, before).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

var errSkipped = errors.New("notification: destination detached, absent or expired")
