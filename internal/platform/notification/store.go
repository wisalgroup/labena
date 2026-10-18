package notification

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/pagination"
)

// store borrows its pool and supports concurrent use.
type store struct{ db *bun.DB }

// newStore performs no I/O. A nil pool supports offline app assembly; operations return
// database.ErrNilDB until the application is configured with a database.
func newStore(db *bun.DB) *store { return &store{db: db} }
func (r *store) executor(ctx context.Context) (bun.IDB, error) {
	if r.db == nil {
		return nil, database.ErrNilDB
	}
	return database.Executor(ctx, r.db)
}

// Create is idempotent per recipient and occurrence key, enforced by a unique constraint.
func (r *store) Create(ctx context.Context, n *Record) error {
	if n == nil {
		return errInvalidRecord
	}
	if err := n.validate(); err != nil {
		return err
	}
	db, err := r.executor(ctx)
	if err != nil {
		return err
	}
	_, err = db.NewInsert().Model(n).Value("created_at", "now()").Value("updated_at", "now()").On("CONFLICT (recipient_type, recipient_id, key) DO NOTHING").Exec(ctx)
	return err
}

// List uses stable ordering and bounded pages. Count and rows are separate
// reads, so concurrent writes may change the total between them.
func (r *store) List(ctx context.Context, recipient Recipient, p pagination.Pagination) ([]*Record, int, error) {
	if !recipient.Valid() {
		return nil, 0, ErrInvalidRecipient
	}
	db, err := r.executor(ctx)
	if err != nil {
		return nil, 0, err
	}
	p = pagination.NewPagination(p.Page, p.PerPage)
	rows := make([]*Record, 0)
	total, err := db.NewSelect().Model(&rows).Where("recipient_type = ? AND recipient_id = ?", recipient.Type, recipient.ID).Order("created_at DESC", "id DESC").Limit(p.PerPage).Offset(p.Offset()).ScanAndCount(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// UnreadCount scopes the query to the recipient and uses the partial unread index.
func (r *store) UnreadCount(ctx context.Context, recipient Recipient) (int, error) {
	if !recipient.Valid() {
		return 0, ErrInvalidRecipient
	}
	db, err := r.executor(ctx)
	if err != nil {
		return 0, err
	}
	return db.NewSelect().Model((*Record)(nil)).Where("recipient_type = ? AND recipient_id = ?", recipient.Type, recipient.ID).Where("read_at IS NULL").Count(ctx)
}

// MarkRead atomically preserves the first read time. Missing and other recipients'
// notifications are indistinguishable. Callers must supply an authorized recipient.
func (r *store) MarkRead(ctx context.Context, recipient Recipient, id uuid.UUID) error {
	if !recipient.Valid() {
		return ErrInvalidRecipient
	}
	if id == uuid.Nil {
		return ErrInvalidID
	}
	db, err := r.executor(ctx)
	if err != nil {
		return err
	}
	result, err := db.NewUpdate().Model((*Record)(nil)).
		Set("updated_at = CASE WHEN read_at IS NULL THEN now() ELSE updated_at END").
		Set("read_at = COALESCE(read_at, now())").
		Where("id = ? AND recipient_type = ? AND recipient_id = ?", id, recipient.Type, recipient.ID).Exec(ctx)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *store) StoreDeliveries(ctx context.Context, rows []*Delivery) ([]uuid.UUID, error) {
	db, err := r.executor(ctx)
	if err != nil {
		return nil, err
	}
	// Use the same database clock as Due/Claim, including when application and
	// database hosts disagree. This statement also remains atomic inside caller TXs.
	_, err = db.NewInsert().Model(&rows).
		Value("status", "?", DeliveryPending).Value("available_at", "now()").
		Value("created_at", "now()").Value("updated_at", "now()").
		On("CONFLICT (recipient_type, recipient_id, key, channel, destination) DO UPDATE").Set("key = EXCLUDED.key").
		Returning("id").Exec(ctx)

	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids, nil
}
func (r *store) Due(ctx context.Context, limit int) ([]uuid.UUID, error) {
	db, err := r.executor(ctx)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	// Exhausted attempts whose worker disappeared are retained for operator replay.
	_, err = db.NewRaw(`UPDATE notification_deliveries SET status='failed',lease_token=NULL,lease_until=NULL,updated_at=now()
 WHERE id IN (SELECT id FROM notification_deliveries WHERE status='sending' AND lease_until <= now() AND attempts >= 8 AND push_ticket_id='' ORDER BY lease_until,id LIMIT 100)`).Exec(ctx)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	err = db.NewSelect().Table("notification_deliveries").Column("id").Where("(status IN ('pending','awaiting_receipt') AND available_at <= now()) OR (status = 'sending' AND lease_until <= now() AND (attempts < 8 OR push_ticket_id<>''))").Order("available_at", "id").Limit(limit).Scan(ctx, &ids)
	return ids, err
}
func (r *store) Claim(ctx context.Context, id uuid.UUID) (*Delivery, error) {
	db, err := r.executor(ctx)
	if err != nil {
		return nil, err
	}
	row := new(Delivery)
	err = db.NewRaw(`UPDATE notification_deliveries SET status='sending',attempts=attempts+CASE WHEN push_ticket_id='' THEN 1 ELSE 0 END,lease_token=?,lease_until=now()+interval '2 minutes',updated_at=now()
 WHERE id=? AND (attempts<8 OR push_ticket_id<>'') AND ((status IN ('pending','awaiting_receipt') AND available_at<=now()) OR (status='sending' AND lease_until<=now())) RETURNING *`, uuid.New(), id).Scan(ctx, row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return row, err
}
func (r *store) Finish(ctx context.Context, row *Delivery, sent bool) error {
	status := DeliveryPending
	if sent {
		status = DeliverySent
	}
	return r.finishOutcome(ctx, row, status, 0)
}
func (r *store) finishOutcome(ctx context.Context, row *Delivery, status DeliveryStatus, retryAfter time.Duration) error {
	db, err := r.executor(ctx)
	if err != nil {
		return err
	}
	if status == DeliveryPending && row.Attempts >= 8 {
		status = DeliveryFailed
	}
	delay := time.Minute * time.Duration(1<<min(max(row.Attempts-1, 0), 7))
	delay += time.Duration(rand.Int64N(int64(delay / 5)))
	if status == DeliveryAwaitingReceipt {
		delay = retryAfter
	}
	if retryAfter > delay {
		delay = retryAfter
	}
	result, err := db.NewRaw(`UPDATE notification_deliveries SET status=?,push_ticket_id=?,push_accepted_at=?,available_at=now()+(? * interval '1 second'),lease_token=NULL,lease_until=NULL,updated_at=now() WHERE id=? AND status='sending' AND lease_token=?`, status, row.PushTicketID, row.PushAcceptedAt, int(delay.Seconds()), row.ID, row.LeaseToken).Exec(ctx)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrDeliveryClaimLost
	}
	return err
}

func (r *store) GetDelivery(ctx context.Context, id uuid.UUID) (*Delivery, error) {
	db, err := r.executor(ctx)
	if err != nil {
		return nil, err
	}
	row := new(Delivery)
	err = db.NewSelect().Model(row).Where("id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeliveryNotFound
	}
	return row, err
}
func (r *store) RetryDelivery(ctx context.Context, id uuid.UUID) error {
	db, err := r.executor(ctx)
	if err != nil {
		return err
	}
	result, err := db.NewRaw(`UPDATE notification_deliveries SET status='pending',attempts=0,push_ticket_id='',push_accepted_at=NULL,available_at=now(),updated_at=now() WHERE id=? AND status='failed'`, id).Exec(ctx)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrDeliveryNotFailed
	}
	return err
}

// ListDeliveries exposes a bounded operator view; payloads are omitted from JSON.
func (r *store) ListDeliveries(ctx context.Context, status DeliveryStatus, p pagination.Pagination) ([]*Delivery, int, error) {
	db, err := r.executor(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]*Delivery, 0)
	query := db.NewSelect().Model(&rows).ExcludeColumn("payload").Order("created_at DESC", "id DESC").Limit(p.PerPage).Offset(p.Offset())
	if status != "" {
		query.Where("status = ?", status)
	}
	count, err := query.ScanAndCount(ctx)
	return rows, count, err
}

// pendingAge reports the oldest unsettled intent without exposing SQL to the runner.
func (s *store) pendingAge(ctx context.Context) (float64, error) {
	db, err := s.executor(ctx)
	if err != nil {
		return 0, err
	}
	var age float64
	err = db.NewRaw("SELECT COALESCE(EXTRACT(EPOCH FROM now()-min(created_at)),0)::double precision FROM notification_deliveries WHERE status IN ('pending','sending','awaiting_receipt')").Scan(ctx, &age)
	return max(0, age), err
}
