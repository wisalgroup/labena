package media

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"

	modulemigrations "github.com/wisalgroup/labena/internal/platform/media/migrations"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/pagination"
	"github.com/wisalgroup/labena/pkg/storage"
)

func databaseFixture(t *testing.T) (*bun.DB, uuid.UUID) {
	t.Helper()
	db := testdb.OpenDriver(t, database.DriverPostgres)
	var registry migrate.Registry
	for name, files := range map[string]fs.FS{"cache": testdb.Migrations(t, database.DriverPostgres), "capability": modulemigrations.Files} {
		if err := registry.Register(name, files); err != nil {
			t.Fatal(err)
		}
	}
	files, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := migrate.NewProvider(t.Context(), db, files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	if applied, err := provider.Up(t.Context()); err != nil || len(applied) != 0 {
		t.Fatal("non-idempotent baseline", err)
	}
	return db, uuid.New()
}

func TestMediaPostgresStorageRecovery(t *testing.T) {
	db, owner := databaseFixture(t)
	repo := newStore(db)
	root := t.TempDir()
	bucket, err := fileblob.OpenBucket(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bucket.Close() })
	svc, err := New(db, map[string]*blob.Bucket{"local": bucket}, "local", Config{})
	if err != nil {
		t.Fatal(err)
	}
	upload := func() *Asset {
		t.Helper()
		a, err := svc.Upload(t.Context(), UploadInput{Owner: ownerRef(owner), Collection: DefaultCollection, Filename: "document.txt", Body: strings.NewReader("private content"), Policy: Policy{MaxBytes: 100}})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	asset := upload()
	if _, err := svc.Get(t.Context(), Owner{Type: "admin", ID: owner.String()}, asset.ID); !errors.Is(err, ErrAssetNotFound) {
		t.Fatal("wrong owner", err)
	}
	page, err := svc.List(t.Context(), ownerRef(owner), "", pagination.NewPagination(1, 20))
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != asset.ID {
		t.Fatal(page, err)
	}
	tx, _ := database.NewTxManager(db, nil)
	err = tx.Execute(t.Context(), func(ctx context.Context) error { return svc.Delete(ctx, ownerRef(owner), asset.ID) })
	if !errors.Is(err, database.ErrTransactionActive) {
		t.Fatal("blob I/O entered transaction", err)
	}
	// A missing disk remains operationally visible without losing object identity.
	if err := bucket.Close(); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(t.Context(), ownerRef(owner), asset.ID); err == nil {
		t.Fatal("closed storage accepted delete")
	}
	saved, err := repo.Get(t.Context(), ownerRef(owner), asset.ID)
	if err != nil || saved.Status != AssetDeleting {
		t.Fatal("lost cleanup intent", saved, err)
	}
	restored, err := fileblob.OpenBucket(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	recovery, _ := New(db, map[string]*blob.Bucket{"local": restored}, "local", Config{})
	if n, err := recovery.Recover(t.Context()); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if exists, err := restored.Exists(t.Context(), asset.Key); err != nil || exists {
		t.Fatal("object survived recovery", exists, err)
	}
	if err := recovery.Delete(t.Context(), ownerRef(owner), asset.ID); err != nil {
		t.Fatal("delete retry", err)
	}
	// An upload crash after object creation but before MarkReady is recoverable; recent
	// in-flight uploads are left alone.
	now := time.Now().UTC()
	pending := &Asset{ID: uuid.New(), Owner: ownerRef(owner), Collection: DefaultCollection, Visibility: Private, Disk: "local", Key: "assets/abandoned", Filename: "abandoned.txt", ContentType: "text/plain", Status: AssetPending, CreatedAt: now.Add(-11 * time.Minute), UpdatedAt: now.Add(-11 * time.Minute)}
	if err := repo.Create(t.Context(), pending); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewRaw("UPDATE media_assets SET created_at=now()-interval '11 minutes' WHERE id=?", pending.ID).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := restored.WriteAll(t.Context(), pending.Key, []byte("orphan"), nil); err != nil {
		t.Fatal(err)
	}
	fresh := *pending
	fresh.ID = uuid.New()
	fresh.Key = "assets/fresh"
	fresh.CreatedAt = now
	fresh.UpdatedAt = now
	if err := repo.Create(t.Context(), &fresh); err != nil {
		t.Fatal(err)
	}
	if n, err := recovery.Recover(t.Context()); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if exists, err := restored.Exists(t.Context(), pending.Key); err != nil || exists {
		t.Fatal("abandoned upload survived", exists, err)
	}
	saved, err = repo.Get(t.Context(), ownerRef(owner), fresh.ID)
	if err != nil || saved.Status != AssetPending {
		t.Fatal("active upload touched", saved, err)
	}
}

func TestInterruptedUploadCleanupRechecksLateObjectPostgres(t *testing.T) {
	db, owner := databaseFixture(t)
	repo := newStore(db)
	bucket, err := fileblob.OpenBucket(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bucket.Close()
	svc, err := New(db, map[string]*blob.Bucket{"local": bucket}, "local", Config{})
	if err != nil {
		t.Fatal(err)
	}
	a := &Asset{ID: uuid.New(), Owner: ownerRef(owner), Collection: DefaultCollection, Visibility: Private, Disk: "local", Key: "assets/interrupted", Filename: "upload.txt", ContentType: "text/plain", Status: AssetPending}
	if err := repo.Create(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(t.Context(), ownerRef(owner), a.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.Get(t.Context(), ownerRef(owner), a.ID)
	if err != nil || saved.Status != AssetDeleting {
		t.Fatal("cleanup finalized during late-write grace", saved, err)
	}
	// Simulate an already-submitted provider write completing after our first delete.
	if err := bucket.WriteAll(t.Context(), a.Key, []byte("late content"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if exists, err := bucket.Exists(t.Context(), a.Key); err != nil || exists {
		t.Fatal("late object survived", exists, err)
	}
	if _, err := db.NewRaw("UPDATE media_assets SET created_at=now()-interval '11 minutes' WHERE id=?", a.ID).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	saved, err = repo.Get(t.Context(), ownerRef(owner), a.ID)
	if err != nil || saved.Status != AssetDeleted {
		t.Fatal("grace expired without finalizing cleanup", saved, err)
	}
}

// Use only disposable S3-compatible infrastructure: this test creates and drops
// its own bucket, alongside its isolated PostgreSQL schema.
func TestMediaPostgresS3Lifecycle(t *testing.T) {
	endpoint := os.Getenv("STORAGE_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_TEST_S3_ENDPOINT not set (disposable MinIO)")
	}
	db, owner := databaseFixture(t)
	repo := newStore(db)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	cfg := storage.Config{Driver: "s3", Bucket: "media-test-" + uuid.NewString(), S3: storage.S3Config{Region: "us-east-1", Endpoint: endpoint, UsePathStyle: true, AccessKeyID: os.Getenv("STORAGE_TEST_S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("STORAGE_TEST_S3_SECRET_ACCESS_KEY")}}
	connection, err := storage.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	bucket := connection.Bucket
	var native *s3.Client
	if !bucket.As(&native) {
		t.Fatal("native S3 client missing")
	}
	if _, err := native.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(cfg.Bucket)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		iterator := bucket.List(nil)
		for {
			object, err := iterator.Next(cleanup)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Error(err)
				return
			}
			if err := bucket.Delete(cleanup, object.Key); err != nil {
				t.Error(err)
				return
			}
		}
		if _, err := native.DeleteBucket(cleanup, &s3.DeleteBucketInput{Bucket: aws.String(cfg.Bucket)}); err != nil {
			t.Error(err)
		}
	})
	svc, err := New(db, map[string]*blob.Bucket{"s3": bucket}, "s3", Config{})
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.Upload(ctx, UploadInput{Owner: ownerRef(owner), Collection: DefaultCollection, Filename: "test.txt", Body: strings.NewReader("stored in S3"), Policy: Policy{MaxBytes: 1024}})
	if err != nil {
		t.Fatal(err)
	}
	_, reader, err := svc.Open(ctx, ownerRef(owner), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(body) != "stored in S3" {
		t.Fatal(readErr, closeErr)
	}
	if err := svc.Delete(ctx, ownerRef(owner), a.ID); err != nil {
		t.Fatal(err)
	}
	if exists, err := bucket.Exists(ctx, a.Key); err != nil || exists {
		t.Fatal(exists, err)
	}
	failed, err := svc.Upload(ctx, UploadInput{Owner: ownerRef(owner), Collection: DefaultCollection, Filename: "large.txt", Body: strings.NewReader(strings.Repeat("x", 2048)), Policy: Policy{MaxBytes: 1024}})
	if !errors.Is(err, ErrFileTooLarge) || failed == nil {
		t.Fatal("oversize upload succeeded", err)
	}
	if exists, err := bucket.Exists(ctx, failed.Key); err != nil || exists {
		t.Fatal("aborted upload leaked an object", exists, err)
	}
	if _, err := db.NewRaw("UPDATE media_assets SET created_at=now()-interval '11 minutes' WHERE id=?", failed.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.Get(ctx, ownerRef(owner), failed.ID)
	if err != nil || saved.Status != AssetDeleted {
		t.Fatal("aborted upload did not converge", saved, err)
	}
}

func ownerRef(id uuid.UUID) Owner { return Owner{Type: "user", ID: id.String()} }

func TestPolymorphicOwnersAndCollections(t *testing.T) {
	db, _ := databaseFixture(t)
	bucket, err := fileblob.OpenBucket(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bucket.Close()
	library, err := New(db, map[string]*blob.Bucket{"local": bucket}, "local", Config{})
	if err != nil {
		t.Fatal(err)
	}
	user, admin, product := Owner{Type: "user", ID: "42"}, Owner{Type: "admin", ID: "42"}, Owner{Type: "product", ID: "sku-A"}
	upload := func(owner Owner, collection string) *Asset {
		t.Helper()
		a, err := library.Upload(t.Context(), UploadInput{Owner: owner, Collection: collection, Filename: "private.txt", Body: strings.NewReader("private"), Policy: Policy{MaxBytes: 100}})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	first := upload(user, "avatar")
	upload(user, "documents")
	upload(admin, "avatar")
	upload(product, "gallery")
	for _, call := range []func() error{
		func() error { _, err := library.Get(t.Context(), admin, first.ID); return err },
		func() error { _, _, err := library.Open(t.Context(), admin, first.ID); return err },
		func() error { return library.Delete(t.Context(), admin, first.ID) },
	} {
		if err := call(); !errors.Is(err, ErrAssetNotFound) {
			t.Fatal("cross-type ownership", err)
		}
	}
	for _, tc := range []struct {
		owner      Owner
		collection string
		count      int
	}{{user, "", 2}, {user, "avatar", 1}, {user, "gallery", 0}, {admin, "", 1}, {product, "gallery", 1}} {
		page, err := library.List(t.Context(), tc.owner, tc.collection, pagination.NewPagination(1, 20))
		if err != nil || page.Total != tc.count || len(page.Items) != tc.count {
			t.Fatal(tc, page, err)
		}
	}
	if _, err := library.List(t.Context(), user, "../bad", pagination.NewPagination(1, 20)); !errors.Is(err, ErrInvalidCollection) {
		t.Fatal(err)
	}
	if err := library.Delete(t.Context(), user, first.ID); err != nil {
		t.Fatal(err)
	}
	if page, err := library.List(t.Context(), admin, "avatar", pagination.NewPagination(1, 20)); err != nil || page.Total != 1 {
		t.Fatal("delete leaked across types", page, err)
	}
}

func TestDeletePersistsIntentWhenDiskIsDisabled(t *testing.T) {
	db, id := databaseFixture(t)
	bucket, err := fileblob.OpenBucket(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bucket.Close()
	library, err := New(db, map[string]*blob.Bucket{"local": bucket}, "local", Config{})
	if err != nil {
		t.Fatal(err)
	}
	owner := ownerRef(id)
	asset, err := library.Upload(t.Context(), UploadInput{Owner: owner, Filename: "file.txt", Body: strings.NewReader("data"), Policy: Policy{MaxBytes: 10}})
	if err != nil {
		t.Fatal(err)
	}
	offline, err := New(db, nil, "", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := offline.Delete(t.Context(), owner, asset.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	saved, err := newStore(db).Get(t.Context(), owner, asset.ID)
	if err != nil || saved.Status != AssetDeleting {
		t.Fatal("missing disk lost deletion intent", saved, err)
	}
	if n, err := library.Recover(t.Context()); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if exists, err := bucket.Exists(t.Context(), asset.Key); err != nil || exists {
		t.Fatal("re-enabled disk retained deleted asset", exists, err)
	}
}

func TestMediaVisibilityPersists(t *testing.T) {
	db, ownerID := databaseFixture(t)
	bucket, err := fileblob.OpenBucket(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bucket.Close()
	options := Config{SigningKey: []byte(strings.Repeat("k", 32))}
	library, err := New(db, map[string]*blob.Bucket{"local": bucket}, "local", options)
	if err != nil {
		t.Fatal(err)
	}
	owner := ownerRef(ownerID)
	for _, visibility := range []Visibility{Public, Private} {
		asset, err := library.Upload(t.Context(), UploadInput{Owner: owner, Visibility: visibility, Filename: "file.txt", Body: strings.NewReader("hello"), Policy: Policy{MaxBytes: 20}})
		if err != nil {
			t.Fatal(err)
		}
		stored, err := library.Get(t.Context(), owner, asset.ID)
		if err != nil || stored.Visibility != visibility {
			t.Fatal("visibility persistence", err)
		}
		_, reader, err := library.OpenContent(t.Context(), asset.ID, 0, "")
		if visibility == Private {
			if !errors.Is(err, ErrAssetNotFound) {
				t.Fatal("private asset exposed", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			reader.Close()
		}
		link, err := library.TemporaryURL(t.Context(), owner, asset.ID, time.Minute)
		if err != nil || link.URL == "" {
			t.Fatal("signed link", err)
		}
	}
}

func TestVisibilityMigrationKeepsExistingAssetsPrivate(t *testing.T) {
	db := testdb.OpenDriver(t, database.DriverPostgres)
	for _, name := range []string{"20261001120200_media_assets.sql", "20261012130000_media_visibility.sql"} {
		source, err := modulemigrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		up, _, _ := strings.Cut(string(source), "-- +goose Down")
		if _, err := db.ExecContext(t.Context(), up); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(name, "120200") {
			_, err := db.ExecContext(t.Context(), `INSERT INTO media_assets (id,owner_type,owner_id,disk,key,filename,content_type,status,created_at,updated_at) VALUES (?, 'user', 'owner', 'local', 'legacy', 'legacy.txt', 'text/plain', 'pending', now(), now())`, uuid.New())
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	var visibility string
	if err := db.NewRaw("SELECT visibility FROM media_assets WHERE key='legacy'").Scan(t.Context(), &visibility); err != nil || visibility != "private" {
		t.Fatal("migration exposed old asset", visibility, err)
	}
}
