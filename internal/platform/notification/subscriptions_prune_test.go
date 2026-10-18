package notification

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPruneSkipsConcurrentRegistrationRefresh(t *testing.T) {
	db, _ := databaseFixture(t)
	fresh, stale := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{fresh, stale} {
		_, err := db.ExecContext(t.Context(), `INSERT INTO notification_subscriptions
 (id,recipient_type,recipient_id,installation_id,session_id,project,registration,registration_hash,version,updated_at)
 VALUES (?,'user','prune-test',?,'','prune-test',?,?,1,now()-interval '100 days')`, id, uuid.NewString(), "ExpoPushToken["+id.String()+"]", id.String())
		if err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), "UPDATE notification_subscriptions SET updated_at=now() WHERE id=?", fresh); err != nil {
		t.Fatal(err)
	}
	notifier, err := New(db, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	count, err := notifier.PruneSubscriptions(ctx, time.Now().Add(-90*24*time.Hour))
	if err != nil || count != 1 {
		t.Fatalf("cleanup must skip the locked registration and disable the other: count=%d err=%v", count, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{fresh, stale} {
		row := new(Subscription)
		if err := db.NewSelect().Model(row).Where("id=?", id).Scan(t.Context()); err != nil {
			t.Fatal(err)
		}
		if (row.DisabledAt != nil) != (id == stale) {
			t.Fatalf("registration %s disabled=%v", id, row.DisabledAt != nil)
		}
	}
	if count, err := notifier.PruneSubscriptions(t.Context(), time.Now().Add(-90*24*time.Hour)); err != nil || count != 0 {
		t.Fatalf("fresh registration pruned: count=%d err=%v", count, err)
	}
}
