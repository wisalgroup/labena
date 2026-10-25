package media

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"

	"github.com/wisalgroup/labena/pkg/database"
)

func TestSetVisibilityPersistsAndMovesObject(t *testing.T) {
	db, ownerID := databaseFixture(t)
	bucket, err := fileblob.OpenBucket(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bucket.Close()
	library, err := New(db, map[string]*blob.Bucket{"local": bucket}, "local", Config{})
	if err != nil {
		t.Fatal(err)
	}
	owner := ownerRef(ownerID)
	asset, err := library.Upload(t.Context(), UploadInput{Owner: owner, Filename: "file.txt", Body: strings.NewReader("hello"), Policy: Policy{MaxBytes: 100}})
	if err != nil {
		t.Fatal(err)
	}
	for _, visibility := range []Visibility{Private, Public, Public} {
		previous := *asset
		asset, err = library.SetVisibility(t.Context(), owner, asset.ID, visibility)
		if err != nil {
			t.Fatal(err)
		}
		if asset.ID != previous.ID || asset.Visibility != visibility || asset.Status != AssetReady || asset.Size != previous.Size || asset.Filename != previous.Filename || !asset.CreatedAt.Equal(previous.CreatedAt) {
			t.Fatalf("unexpected result: %+v", asset)
		}
		stored, err := library.Get(t.Context(), owner, asset.ID)
		if err != nil || stored.Visibility != visibility || stored.Key != asset.Key || stored.VisibilityTarget != "" {
			t.Fatal(stored, err)
		}
		if previous.Visibility == visibility {
			if asset.Key != previous.Key || !asset.UpdatedAt.Equal(previous.UpdatedAt) {
				t.Fatal("idempotent call changed the asset")
			}
		} else if exists, err := bucket.Exists(t.Context(), previous.Key); err != nil || exists {
			t.Fatal("old object survived", exists, err)
		}
		_, reader, err := library.Open(t.Context(), owner, asset.ID)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || string(body) != "hello" {
			t.Fatal(string(body), err)
		}
		attrs, err := bucket.Attributes(t.Context(), asset.Key)
		if err != nil || attrs.ContentType != asset.ContentType || !strings.Contains(attrs.ContentDisposition, "file.txt") {
			t.Fatal("lost object metadata", attrs, err)
		}
		_, reader, err = library.OpenContent(t.Context(), asset.ID, 0, "")
		if visibility == Private {
			if !errors.Is(err, ErrAssetNotFound) {
				t.Fatal("anonymous private content", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			reader.Close()
		}
	}
	for _, test := range []struct {
		owner      Owner
		visibility Visibility
		want       error
	}{
		{Owner{Type: "admin", ID: owner.ID}, Private, ErrAssetNotFound},
		{Owner{}, Private, ErrAssetNotFound},
		{owner, "invalid", ErrInvalidVisibility},
		{owner, "", ErrInvalidVisibility},
	} {
		if _, err := library.SetVisibility(t.Context(), test.owner, asset.ID, test.visibility); !errors.Is(err, test.want) {
			t.Fatal(err)
		}
	}
	tx, err := database.NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Execute(t.Context(), func(ctx context.Context) error {
		_, err := library.SetVisibility(ctx, owner, asset.ID, Private)
		return err
	}); !errors.Is(err, database.ErrTransactionActive) {
		t.Fatal(err)
	}
	if err := library.Delete(t.Context(), owner, asset.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := library.SetVisibility(t.Context(), owner, asset.ID, Private); !errors.Is(err, ErrAssetNotFound) {
		t.Fatal(err)
	}
}

// Simulate crashes at each durable boundary using real metadata and file storage.
func TestVisibilityRecovery(t *testing.T) {
	for _, stage := range []string{"before-copy", "after-copy", "after-delete", "missing-both", "incomplete-destination"} {
		t.Run(stage, func(t *testing.T) {
			db, ownerID := databaseFixture(t)
			bucket, err := fileblob.OpenBucket(t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer bucket.Close()
			library, err := New(db, map[string]*blob.Bucket{"local": bucket}, "local", Config{})
			if err != nil {
				t.Fatal(err)
			}
			owner := ownerRef(ownerID)
			asset, err := library.Upload(t.Context(), UploadInput{Owner: owner, Filename: "file.txt", Body: strings.NewReader("hello"), Policy: Policy{MaxBytes: 100}})
			if err != nil {
				t.Fatal(err)
			}
			pending, err := library.repo.BeginVisibilityChange(t.Context(), owner, asset.ID, Private)
			if err != nil || pending.Status != AssetChangingVisibility {
				t.Fatal(pending, err)
			}
			key := strings.TrimPrefix(asset.Key, "public/")
			if stage == "after-copy" || stage == "after-delete" {
				if err := bucket.Copy(t.Context(), key, asset.Key, nil); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "incomplete-destination" {
				if err := bucket.WriteAll(t.Context(), key, []byte("bad"), nil); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "after-delete" || stage == "missing-both" || stage == "incomplete-destination" {
				if err := bucket.Delete(t.Context(), asset.Key); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := library.OpenContent(t.Context(), asset.ID, 0, ""); err == nil {
				t.Fatal("changing asset exposed")
			}
			if err := library.Delete(t.Context(), owner, asset.ID); !errors.Is(err, ErrNotReady) {
				t.Fatal("delete stole pending transition", err)
			}
			if _, err := library.SetVisibility(t.Context(), owner, asset.ID, Private); !errors.Is(err, ErrNotReady) {
				t.Fatal("duplicate transition", err)
			}
			if n, err := library.Recover(t.Context()); n != 0 || err != nil {
				t.Fatal("recovery claimed in-flight operation", n, err)
			}
			if _, err := db.NewRaw("UPDATE media_assets SET updated_at=now()-interval '11 minutes' WHERE id=?", asset.ID).Exec(t.Context()); err != nil {
				t.Fatal(err)
			}
			n, err := library.Recover(t.Context())
			if stage == "missing-both" || stage == "incomplete-destination" {
				if err == nil || n != 0 {
					t.Fatal("missing bytes became ready", n, err)
				}
				stored, err := library.Get(t.Context(), owner, asset.ID)
				if err != nil || stored.Status != AssetChangingVisibility || stored.VisibilityTarget != Private {
					t.Fatal("lost retry intent", stored, err)
				}
				if n, err := library.Recover(t.Context()); err != nil || n != 0 {
					t.Fatal("recovery ignored cooldown", n, err)
				}
				return
			}
			if err != nil || n != 1 {
				t.Fatal(n, err)
			}
			stored, err := library.Get(t.Context(), owner, asset.ID)
			if err != nil || stored.Visibility != Private || stored.Status != AssetReady || stored.Key != key || stored.VisibilityTarget != "" {
				t.Fatal(stored, err)
			}
			if exists, err := bucket.Exists(t.Context(), asset.Key); err != nil || exists {
				t.Fatal("public source survived", exists, err)
			}
			if n, err := library.Recover(t.Context()); err != nil || n != 0 {
				t.Fatal(n, err)
			}
		})
	}
}

func TestVisibilityChangeAndDeleteAreExclusive(t *testing.T) {
	db, ownerID := databaseFixture(t)
	repo, owner := newStore(db), ownerRef(ownerID)
	for range 10 {
		asset := &Asset{ID: uuid.New(), Owner: owner, Collection: DefaultCollection, Visibility: Public, Disk: "local", Key: uuid.NewString(), Filename: "file.txt", ContentType: "text/plain", Status: AssetReady, Size: 5}
		if err := repo.Create(t.Context(), asset); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		wg.Go(func() { _, err := repo.BeginVisibilityChange(t.Context(), owner, asset.ID, Private); results <- err })
		wg.Go(func() { _, err := repo.MarkDeleting(t.Context(), owner, asset.ID); results <- err })
		wg.Wait()
		first, second := <-results, <-results
		if (first == nil) == (second == nil) {
			t.Fatalf("expected one winner, got %v / %v", first, second)
		}
	}
}

type visibilityCompletionFailure struct{ assetStore }

func (r visibilityCompletionFailure) CompleteVisibilityChange(context.Context, *Asset, string) (*Asset, error) {
	return nil, errors.New("database unavailable after object move")
}

func TestVisibilityFailuresRemainRecoverable(t *testing.T) {
	for _, stage := range []string{"copy", "completion"} {
		t.Run(stage, func(t *testing.T) {
			db, ownerID := databaseFixture(t)
			root := t.TempDir()
			bucket, err := fileblob.OpenBucket(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer bucket.Close()
			library, err := New(db, map[string]*blob.Bucket{"local": bucket}, "local", Config{})
			if err != nil {
				t.Fatal(err)
			}
			owner := ownerRef(ownerID)
			asset, err := library.Upload(t.Context(), UploadInput{Owner: owner, Filename: "file.txt", Body: strings.NewReader("hello"), Policy: Policy{MaxBytes: 100}})
			if err != nil {
				t.Fatal(err)
			}
			if stage == "copy" {
				if err := bucket.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				library.repo = visibilityCompletionFailure{library.repo}
			}
			if result, err := library.SetVisibility(t.Context(), owner, asset.ID, Private); result != nil || !errors.Is(err, ErrVisibilityPending) {
				t.Fatal("lost recorded-change outcome", result, err)
			}
			stored, err := library.Get(t.Context(), owner, asset.ID)
			if err != nil || stored.Status != AssetChangingVisibility || stored.VisibilityTarget != Private || stored.Visibility != Public {
				t.Fatal("lost recovery intent", stored, err)
			}
			restored, err := fileblob.OpenBucket(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			recovery, err := New(db, map[string]*blob.Bucket{"local": restored}, "local", Config{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.NewRaw("UPDATE media_assets SET updated_at=now()-interval '11 minutes' WHERE id=?", asset.ID).Exec(t.Context()); err != nil {
				t.Fatal(err)
			}
			if n, err := recovery.Recover(t.Context()); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			stored, err = recovery.Get(t.Context(), owner, asset.ID)
			if err != nil || stored.Visibility != Private || stored.Status != AssetReady {
				t.Fatal(stored, err)
			}
			if exists, err := restored.Exists(t.Context(), asset.Key); err != nil || exists {
				t.Fatal("public source survived recovery", exists, err)
			}
			// Deletion must use the current key, including after recovery moved it.
			if err := recovery.Delete(t.Context(), owner, asset.ID); err != nil {
				t.Fatal(err)
			}
			if exists, err := restored.Exists(t.Context(), stored.Key); err != nil || exists {
				t.Fatal("delete left moved object", exists, err)
			}
		})
	}
}
