package media

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"
)

type assetRepo struct {
	assetStore
	asset    *Asset
	readyErr error
	calls    int
}

func (r *assetRepo) Create(_ context.Context, a *Asset) error {
	r.calls++
	copy := *a
	r.asset = &copy
	return nil
}
func (r *assetRepo) MarkReady(_ context.Context, _ uuid.UUID, size int64) error {
	if r.readyErr != nil {
		return r.readyErr
	}
	r.asset.Status = AssetReady
	r.asset.Size = size
	return nil
}
func (r *assetRepo) Get(_ context.Context, owner Owner, id uuid.UUID) (*Asset, error) {
	if r.asset == nil || r.asset.Owner != owner || r.asset.ID != id {
		return nil, ErrAssetNotFound
	}
	copy := *r.asset
	return &copy, nil
}
func (r *assetRepo) MarkDeleting(ctx context.Context, owner Owner, id uuid.UUID) (*Asset, error) {
	a, err := r.Get(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if a.Status != AssetDeleted {
		r.asset.Status = AssetDeleting
	}
	return r.Get(ctx, owner, id)
}
func (r *assetRepo) MarkDeleted(context.Context, uuid.UUID) error {
	r.asset.Status = AssetDeleted
	return nil
}
func fixture(t *testing.T) (*Library, *assetRepo, *blob.Bucket) {
	t.Helper()
	bucket, err := fileblob.OpenBucket(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bucket.Close() })
	repo := &assetRepo{}
	svc, err := newWithStore(repo, map[string]*blob.Bucket{"local": bucket}, "local", Config{})
	if err != nil {
		t.Fatal(err)
	}
	return svc, repo, bucket
}
func TestPrivateAssetUploadDownloadDelete(t *testing.T) {
	svc, repo, bucket := fixture(t)
	owner := Owner{Type: "user", ID: uuid.NewString()}
	a, err := svc.Upload(t.Context(), UploadInput{Owner: owner, Filename: "../../private.txt", Body: strings.NewReader("hello world"), Policy: Policy{MaxBytes: 100, ContentTypes: []string{"text/plain"}}})
	if err != nil || a.Status != AssetReady || a.Size != 11 || a.Filename != "private.txt" {
		t.Fatal(a, err)
	}
	if a.Key == a.Filename || strings.Contains(a.Key, "..") {
		t.Fatal("client selected storage path")
	}
	if _, _, err := svc.Open(t.Context(), Owner{Type: "admin", ID: owner.ID}, a.ID); !errors.Is(err, ErrAssetNotFound) {
		t.Fatal("cross-owner download", err)
	}
	_, reader, err := svc.Open(t.Context(), owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(data) != "hello world" {
		t.Fatal(string(data), err)
	}
	for range 2 {
		if err := svc.Delete(t.Context(), owner, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if exists, err := bucket.Exists(t.Context(), a.Key); err != nil || exists || repo.asset.Status != AssetDeleted {
		t.Fatal("delete did not converge", exists, err)
	}
}
func TestUploadRejectsAndCleansFailedWrites(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		max        int64
		types      []string
		readyErr   error
		want       error
	}{
		{"empty", "", 10, nil, nil, ErrInvalidUpload},
		{"oversize header", "hello", 4, nil, nil, ErrFileTooLarge},
		{"oversize stream", strings.Repeat("x", 2048), 1024, nil, nil, ErrFileTooLarge},
		{"type mismatch", "hello", 10, []string{"image/png"}, nil, ErrContentType},
		{"metadata failure", "hello", 10, nil, io.ErrUnexpectedEOF, io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, bucket := fixture(t)
			repo.readyErr = tc.readyErr
			_, err := svc.Upload(t.Context(), UploadInput{Owner: Owner{Type: "user", ID: uuid.NewString()}, Filename: "input.txt", Body: strings.NewReader(tc.body), Policy: Policy{MaxBytes: tc.max, ContentTypes: tc.types}})
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if repo.asset != nil {
				if exists, err := bucket.Exists(t.Context(), repo.asset.Key); err != nil || exists || repo.asset.Status != AssetDeleted {
					t.Fatal("failed upload leaked", exists, err)
				}
			}
		})
	}
}
func TestUploadRejectsCanceledContextBeforeReading(t *testing.T) {
	svc, repo, _ := fixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := svc.Upload(ctx, UploadInput{Owner: Owner{Type: "user", ID: uuid.NewString()}, Filename: "x", Body: failReader{t}, Policy: Policy{MaxBytes: 10}})
	if !errors.Is(err, context.Canceled) || repo.calls != 0 {
		t.Fatal(err)
	}
}

type failReader struct{ t *testing.T }

func (r failReader) Read([]byte) (int, error) { r.t.Fatal("read inside transaction"); return 0, io.EOF }

func TestNewRejectsInvalidDiskWithoutOpeningResources(t *testing.T) {
	if _, err := New(nil, map[string]*blob.Bucket{"local": nil}, "local", Config{}); !errors.Is(err, ErrDependenciesRequired) {
		t.Fatal(err)
	}
	if _, err := New(nil, nil, "", Config{}); err != nil {
		t.Fatal("offline construction", err)
	}
}

func TestOwnerAndCollectionValidationBeforeUpload(t *testing.T) {
	library, repo, _ := fixture(t)
	for _, input := range []UploadInput{
		{Owner: Owner{}, Collection: "default"},
		{Owner: Owner{Type: "User", ID: "42"}, Collection: "default"},
		{Owner: Owner{Type: "user", ID: "42\n"}, Collection: "default"},
		{Owner: Owner{Type: "user", ID: "42"}, Collection: "../avatar"},
	} {
		input.Filename = "test.txt"
		input.Body = failReader{t}
		input.Policy = Policy{MaxBytes: 10}
		if _, err := library.Upload(t.Context(), input); err == nil {
			t.Fatal("invalid reference accepted", input)
		}
	}
	if repo.calls != 0 {
		t.Fatal("invalid input persisted")
	}
}

func (r *assetRepo) Find(_ context.Context, id uuid.UUID) (*Asset, error) {
	if r.asset == nil || r.asset.ID != id {
		return nil, ErrAssetNotFound
	}
	copy := *r.asset
	return &copy, nil
}
