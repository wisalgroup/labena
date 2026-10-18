// Package media manages public and private polymorphic assets using database metadata and borrowed blob disks.
package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"

	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/pagination"
)

const RecoverTask = "media.recover.v1"

// Library borrows all disks. It copies the disk map and never closes buckets.
// All operations support concurrent use; a single upload's reader is caller-owned.
type Library struct {
	repo        assetStore
	disks       map[string]*blob.Bucket
	defaultDisk string
	options     Config
}

func newWithStore(repo assetStore, disks map[string]*blob.Bucket, defaultDisk string, options Config) (*Library, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	options = options.normalized()
	if repo == nil {
		return nil, ErrDependenciesRequired
	}
	for name, bucket := range disks {
		if name == "" || bucket == nil {
			return nil, ErrDependenciesRequired
		}
	}
	if len(disks) > 0 && defaultDisk != "disabled" && disks[defaultDisk] == nil {
		return nil, ErrDependenciesRequired
	}
	return &Library{repo: repo, disks: maps.Clone(disks), defaultDisk: defaultDisk, options: options}, nil
}
func (s *Library) Upload(ctx context.Context, in UploadInput) (asset *Asset, err error) {
	disk := in.Policy.Disk
	if disk == "" {
		disk = s.defaultDisk
	}
	if in.Visibility == "" {
		in.Visibility = s.options.Disks[disk].Visibility
	}
	if in.Visibility == "" {
		in.Visibility = Public
	}
	if !in.Visibility.Valid() {
		return nil, ErrInvalidUpload
	}
	if in.Collection == "" {
		in.Collection = DefaultCollection
	}
	if !validCollection(in.Collection) {
		return nil, ErrInvalidCollection
	}
	name := path.Base(strings.ReplaceAll(in.Filename, "\\", "/"))
	if !in.Owner.Valid() || in.Body == nil || name == "." || name == "/" || strings.TrimSpace(name) == "" || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) || in.Policy.MaxBytes < 1 || in.Policy.MaxBytes > 1<<30 {
		return nil, ErrInvalidUpload
	}
	bucket := s.disks[disk]
	if bucket == nil {
		return nil, ErrUnavailable
	}
	if err := database.RequireNoTransaction(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	reader := io.LimitReader(in.Body, in.Policy.MaxBytes+1)
	header := make([]byte, 512)
	n, readErr := io.ReadFull(reader, header)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return nil, readErr
	}
	if n == 0 {
		return nil, ErrInvalidUpload
	}
	if int64(n) > in.Policy.MaxBytes {
		return nil, ErrFileTooLarge
	}
	header = header[:n]
	kind, _, _ := mime.ParseMediaType(http.DetectContentType(header))
	if len(in.Policy.ContentTypes) > 0 && !slices.Contains(in.Policy.ContentTypes, kind) {
		return nil, ErrContentType
	}
	now := time.Now().UTC()
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	asset = &Asset{ID: id, Owner: in.Owner, Collection: in.Collection, Visibility: in.Visibility, Disk: disk, Key: "assets/" + id.String(), Filename: name, ContentType: kind, Status: AssetPending, CreatedAt: now, UpdatedAt: now}
	if asset.Visibility == Public {
		asset.Key = "public/" + asset.Key
	}
	if err := s.repo.Create(ctx, asset); err != nil {
		return nil, err
	}
	// A failed or canceled operation leaves recoverable metadata even if cleanup
	// also fails. Never delete metadata before successfully removing its object.
	defer func() {
		if err != nil {
			cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer done()
			err = errors.Join(err, s.Delete(cleanup, asset.Owner, asset.ID))
		}
	}()
	writeCtx, abort := context.WithCancel(ctx)
	defer abort()
	writer, err := bucket.NewWriter(writeCtx, asset.Key, &blob.WriterOptions{ContentType: kind, ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": name})})
	if err != nil {
		return asset, err
	}
	size, copyErr := io.Copy(writer, io.MultiReader(bytes.NewReader(header), reader))
	if size > in.Policy.MaxBytes {
		copyErr = ErrFileTooLarge
	}
	if copyErr != nil {
		abort()
	}
	closeErr := writer.Close()
	if err = errors.Join(copyErr, closeErr); err != nil {
		return asset, err
	}
	if err = ctx.Err(); err != nil {
		return asset, err
	}
	if err = s.repo.MarkReady(ctx, asset.ID, size); err != nil {
		return asset, err
	}
	asset.Size = size
	asset.Status = AssetReady
	asset.UpdatedAt = time.Now().UTC()
	return asset, nil
}
func (s *Library) Get(ctx context.Context, owner Owner, id uuid.UUID) (*Asset, error) {
	if !owner.Valid() || id == uuid.Nil {
		return nil, ErrAssetNotFound
	}
	a, err := s.repo.Get(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if a.Status == AssetDeleted || a.Status == AssetDeleting {
		return nil, ErrAssetNotFound
	}
	return a, nil
}
func (s *Library) List(ctx context.Context, owner Owner, collection string, p pagination.Pagination) (*pagination.Page[*Asset], error) {
	if !owner.Valid() {
		return nil, ErrAssetNotFound
	}
	if collection != "" && !validCollection(collection) {
		return nil, ErrInvalidCollection
	}
	p = pagination.NewPagination(p.Page, p.PerPage)
	rows, count, err := s.repo.List(ctx, owner, collection, p)
	if err != nil {
		return nil, err
	}
	return pagination.NewPage(rows, p, count), nil
}

// Open authorizes by owner before opening the object. The caller must close the
// reader. Downloads remain private; routes use attachment disposition and no-store.
func (s *Library) Open(ctx context.Context, owner Owner, id uuid.UUID) (*Asset, *blob.Reader, error) {
	a, err := s.Get(ctx, owner, id)
	if err != nil {
		return nil, nil, err
	}
	return s.open(ctx, a)
}

func (s *Library) open(ctx context.Context, a *Asset) (*Asset, *blob.Reader, error) {
	if a.Status != AssetReady {
		return nil, nil, ErrNotReady
	}
	bucket := s.disks[a.Disk]
	if bucket == nil {
		return nil, nil, ErrUnavailable
	}
	reader, err := bucket.NewReader(ctx, a.Key, nil)
	if err != nil {
		return nil, nil, err
	}
	return a, reader, nil
}

// Delete is idempotent and owner-scoped. A durable tombstone precedes blob I/O;
// failures leave a deleting row for Recover. Call outside SQL transactions.
func (s *Library) Delete(ctx context.Context, owner Owner, id uuid.UUID) error {
	if !owner.Valid() || id == uuid.Nil {
		return ErrAssetNotFound
	}
	if err := database.RequireNoTransaction(ctx); err != nil {
		return err
	}
	a, err := s.repo.Get(ctx, owner, id)
	if err != nil {
		return err
	}
	if a.Status == AssetDeleted {
		return nil
	}
	if err := s.repo.MarkDeleting(ctx, owner, id); err != nil {
		return err
	}
	return s.remove(ctx, a)
}
func (s *Library) remove(ctx context.Context, a *Asset) error {
	bucket := s.disks[a.Disk]
	if bucket == nil {
		return ErrUnavailable
	}
	err := bucket.Delete(ctx, a.Key)
	if err != nil && gcerrors.Code(err) != gcerrors.NotFound {
		return err
	}
	return s.repo.MarkDeleted(ctx, a.ID)
}

// Recover repairs bounded batches of interrupted uploads and deletions. It is
// safe to repeat and run concurrently; it never scans arbitrary bucket objects.
func (s *Library) Recover(ctx context.Context) (int, error) {
	if err := database.RequireNoTransaction(ctx); err != nil {
		return 0, err
	}
	rows, err := s.repo.Recoverable(ctx)
	if err != nil {
		return 0, err
	}
	var failures error
	completed := 0
	for _, a := range rows {
		if err := ctx.Err(); err != nil {
			return completed, errors.Join(failures, err)
		}
		if err := s.remove(ctx, a); err != nil {
			failures = errors.Join(failures, err)
		} else {
			completed++
		}
	}
	return completed, failures
}

// New borrows the pool and named disks without opening connections or starting work.
// The library copies the disk map, and never closes caller-owned buckets.
func New(db *bun.DB, disks map[string]*blob.Bucket, defaultDisk string, options Config) (*Library, error) {
	return newWithStore(newStore(db), disks, defaultDisk, options)
}
