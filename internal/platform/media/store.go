package media

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/pagination"
)

type assetStore interface {
	Create(context.Context, *Asset) error
	MarkReady(context.Context, uuid.UUID, int64) error
	Get(context.Context, Owner, uuid.UUID) (*Asset, error)
	Find(context.Context, uuid.UUID) (*Asset, error)
	List(context.Context, Owner, string, pagination.Pagination) ([]*Asset, int, error)
	MarkDeleting(context.Context, Owner, uuid.UUID) error
	MarkDeleted(context.Context, uuid.UUID) error
	Recoverable(context.Context) ([]*Asset, error)
}

type store struct{ db *bun.DB }

// New borrows the pool; nil allows offline assembly only.
func newStore(db *bun.DB) *store { return &store{db: db} }
func (r *store) executor(ctx context.Context) (bun.IDB, error) {
	if r.db == nil {
		return nil, database.ErrNilDB
	}
	return database.Executor(ctx, r.db)
}

func (r *store) Create(ctx context.Context, a *Asset) error {
	db, err := r.executor(ctx)
	if err != nil {
		return err
	}
	// Recovery compares against the database clock, never an application's clock.
	_, err = db.NewInsert().Model(a).Value("created_at", "now()").Value("updated_at", "now()").Returning("created_at,updated_at").Exec(ctx)
	return err
}
func (r *store) MarkReady(ctx context.Context, id uuid.UUID, size int64) error {
	db, err := r.executor(ctx)
	if err != nil {
		return err
	}
	result, err := db.NewRaw(`UPDATE media_assets SET status='ready',size=?,updated_at=now() WHERE id=? AND status='pending'`, size, id).Exec(ctx)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotReady
	}
	return err
}
func (r *store) Get(ctx context.Context, owner Owner, id uuid.UUID) (*Asset, error) {
	db, err := r.executor(ctx)
	if err != nil {
		return nil, err
	}
	row := new(Asset)
	err = db.NewSelect().Model(row).Where("id = ? AND owner_type = ? AND owner_id = ?", id, owner.Type, owner.ID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssetNotFound
	}
	return row, err
}
func (r *store) List(ctx context.Context, owner Owner, collection string, p pagination.Pagination) ([]*Asset, int, error) {
	db, err := r.executor(ctx)
	if err != nil {
		return nil, 0, err
	}
	p = pagination.NewPagination(p.Page, p.PerPage)
	rows := make([]*Asset, 0)
	query := db.NewSelect().Model(&rows).Where("owner_type = ? AND owner_id = ? AND status = 'ready'", owner.Type, owner.ID)
	if collection != "" {
		query.Where("collection = ?", collection)
	}
	count, err := query.Order("created_at DESC", "id DESC").Limit(p.PerPage).Offset(p.Offset()).ScanAndCount(ctx)
	return rows, count, err
}
func (r *store) MarkDeleting(ctx context.Context, owner Owner, id uuid.UUID) error {
	db, err := r.executor(ctx)
	if err != nil {
		return err
	}
	// Keep timestamps of existing deletion work so a busy resource is not starved.
	result, err := db.NewRaw(`UPDATE media_assets SET status=CASE WHEN status='deleted' THEN status ELSE 'deleting' END,
 updated_at=CASE WHEN status IN ('ready','pending') THEN now() ELSE updated_at END WHERE id=? AND owner_type=? AND owner_id=?`, id, owner.Type, owner.ID).Exec(ctx)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrAssetNotFound
	}
	return err
}
func (r *store) MarkDeleted(ctx context.Context, id uuid.UUID) error {
	db, err := r.executor(ctx)
	if err != nil {
		return err
	}
	// An interrupted writer may finish remotely after cancellation. Keep unready
	// uploads eligible for repeated deletion throughout the upload crash grace.
	_, err = db.NewRaw(`UPDATE media_assets SET status='deleted',updated_at=now()
 WHERE id=? AND status='deleting' AND (size>0 OR created_at<now()-interval '10 minutes')`, id).Exec(ctx)
	return err
}
func (r *store) Recoverable(ctx context.Context) ([]*Asset, error) {
	db, err := r.executor(ctx)
	if err != nil {
		return nil, err
	}
	// Uploads have a two-minute deadline; ten minutes is a conservative crash grace.
	// Bound both the state change and scan so recovery never locks the full backlog.
	_, err = db.NewRaw(`UPDATE media_assets SET status='deleting',updated_at=now() WHERE id IN
 (SELECT id FROM media_assets WHERE status='pending' AND created_at<now()-interval '10 minutes' ORDER BY created_at,id LIMIT 100)`).Exec(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]*Asset, 0)
	// Rotate attempted rows so a missing disk cannot starve the rest of the backlog.
	err = db.NewRaw(`WITH batch AS (
 SELECT id FROM media_assets WHERE status='deleting' ORDER BY updated_at,id LIMIT 100 FOR UPDATE SKIP LOCKED
 ) UPDATE media_assets SET updated_at=now() FROM batch WHERE media_assets.id=batch.id RETURNING media_assets.*`).Scan(ctx, &rows)
	return rows, err
}

// Find is used only after public/signed access checks in Library. Management stays owner-scoped.
func (r *store) Find(ctx context.Context, id uuid.UUID) (*Asset, error) {
	db, err := r.executor(ctx)
	if err != nil {
		return nil, err
	}
	row := new(Asset)
	err = db.NewSelect().Model(row).Where("id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssetNotFound
	}
	return row, err
}
