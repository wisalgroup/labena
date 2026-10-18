package notification

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/wisalgroup/labena/pkg/database"
)

func TestDatabaseNotificationRecipientIsolationAndReadStatePostgres(t *testing.T) {
	db, _ := databaseFixture(t)
	repo := newStore(db)
	user := Recipient{Type: "user", ID: "42"}

	first, _ := newRecord(user, "media.ready", "media.ready:first", json.RawMessage(`{"title":"First"}`))
	second, _ := newRecord(user, "media.ready", "media.ready:second", json.RawMessage(`{"title":"Second"}`))
	for _, n := range []*Record{first, second} {
		if err := repo.Create(t.Context(), n); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := repo.UnreadCount(t.Context(), user); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	other := Recipient{Type: "admin", ID: user.ID}
	if err := repo.MarkRead(t.Context(), other, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("another recipient read item", err)
	}
	if count, err := repo.UnreadCount(t.Context(), other); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err := repo.MarkRead(t.Context(), user, first.ID); err != nil {
		t.Fatal(err)
	}
	var saved Record
	if err := db.NewSelect().Model(&saved).Where("id = ?", first.ID).Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	if saved.ReadAt == nil {
		t.Fatal("missing read time")
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if err := repo.MarkRead(t.Context(), user, first.ID); err != nil {
				t.Error(err)
			}
			duplicate, _ := newRecord(user, "media.ready", "media.ready:first", json.RawMessage(`{"title":"Replacement"}`))
			if err := repo.Create(t.Context(), duplicate); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var after Record
	if err := db.NewSelect().Model(&after).Where("id = ?", first.ID).Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	if after.ReadAt == nil || !after.ReadAt.Equal(*saved.ReadAt) || !after.UpdatedAt.Equal(saved.UpdatedAt) || !strings.Contains(string(after.Data), "First") {
		t.Fatal("retry changed original", after)
	}
	if count, err := repo.UnreadCount(t.Context(), user); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	tx, _ := database.NewTxManager(db, nil)
	abort := errors.New("abort")
	err := tx.Execute(t.Context(), func(ctx context.Context) error {
		if err := repo.MarkRead(ctx, user, second.ID); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if count, err := repo.UnreadCount(t.Context(), user); err != nil || count != 1 {
		t.Fatal("mark read escaped rollback", count, err)
	}
}
