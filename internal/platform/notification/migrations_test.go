package notification

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	modulemigrations "github.com/wisalgroup/labena/internal/platform/notification/migrations"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func TestPushSubscriptionMigrationPreservesRegistrationsAndQueuedDelivery(t *testing.T) {
	db := testdb.OpenDriver(t, database.DriverPostgres)
	var registry migrate.Registry
	for name, files := range map[string]fs.FS{"cache": testdb.Migrations(t, database.DriverPostgres), "notification": modulemigrations.Files} {
		if err := registry.Register(name, files); err != nil {
			t.Fatal(err)
		}
	}
	history, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := migrate.NewProvider(t.Context(), db, history)
	if err != nil {
		t.Fatal(err)
	}
	// Apply the original, unchanged migration, as an existing installation would.
	if _, err := provider.UpTo(t.Context(), 20261001120100); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	var subscriptions []*Subscription
	var deliveries []*Delivery
	for i := range 2 {
		registration := fmt.Sprintf("ExpoPushToken[migration-%d]", i)
		sub := &Subscription{
			ID: uuid.New(), RecipientType: "user", RecipientID: "migration-user",
			InstallationID: uuid.NewString(), SessionID: "migration-session", Project: "migration-project",
			Registration: registration, RegistrationHash: fmt.Sprintf("%x", sha256.Sum256([]byte(registration))),
			Version: 7, UpdatedAt: now,
		}
		if i == 1 {
			sub.DisabledAt = &now
		}
		if _, err := db.NewInsert().Model(sub).ModelTableExpr("notification_subscriptions").Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		delivery := &Delivery{
			ID: uuid.New(), RecipientType: sub.RecipientType, RecipientID: sub.RecipientID,
			Type: "migration.ready", Key: "migration:1", Channel: ChannelPush,
			Destination: sub.ID.String(), SubscriptionVersion: sub.Version,
			Payload: []byte(`{"push":{"title":"Ready"}}`), Status: DeliveryPending,
			AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := db.NewInsert().Model(delivery).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		subscriptions = append(subscriptions, sub)
		deliveries = append(deliveries, delivery)
	}

	// Compare every stored column, including private registration data and timestamps.
	snapshot := func(table string) string {
		t.Helper()
		var rows string
		if err := db.NewRaw("SELECT jsonb_agg(to_jsonb(s) ORDER BY id)::text FROM ? AS s", bun.Ident(table)).Scan(t.Context(), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	wantSubscriptions := snapshot("notification_subscriptions")
	wantDeliveries := snapshot("notification_deliveries")
	assertState := func(table, absent string) {
		t.Helper()
		if snapshot(table) != wantSubscriptions || snapshot("notification_deliveries") != wantDeliveries {
			t.Fatal("migration changed retained registrations or delivery intent")
		}
		var removed bool
		if err := db.NewRaw("SELECT to_regclass(?) IS NULL", absent).Scan(t.Context(), &removed); err != nil || !removed {
			t.Fatalf("old table name still resolves: %s, err=%v", absent, err)
		}
	}
	if _, err := provider.UpByOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertState("push_subscriptions", "notification_subscriptions")
	if _, err := provider.Down(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertState("notification_subscriptions", "push_subscriptions")
	if _, err := provider.UpByOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertState("push_subscriptions", "notification_subscriptions")

	capture := &pushCapture{}
	n, err := New(db, Options{SessionActive: activePushSession, Pusher: capture, PushProject: "migration-project"})
	if err != nil {
		t.Fatal(err)
	}
	// Existing queued IDs and versions still select the original eligible device.
	for i, delivery := range deliveries {
		if err := n.Deliver(t.Context(), delivery.ID); err != nil {
			t.Fatal(err)
		}
		row, err := n.GetDelivery(t.Context(), delivery.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := DeliveryAwaitingReceipt
		if i == 1 {
			want = DeliverySkipped
		}
		if row.Status != want {
			t.Fatalf("delivery %d: status=%s, want %s", i, row.Status, want)
		}
	}
	if !slices.Equal(capture.tokens, []string{subscriptions[0].Registration}) {
		t.Fatalf("sent to wrong destinations: %v", capture.tokens)
	}
	active := subscriptions[0]
	recipient := Recipient{Type: active.RecipientType, ID: active.RecipientID}
	refreshed, err := n.Subscribe(t.Context(), recipient, active.InstallationID, active.SessionID, active.Registration)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ID != active.ID || refreshed.Version != active.Version {
		t.Fatal("refresh changed an existing registration's identity or version")
	}
	if _, err := n.Subscribe(t.Context(), recipient, uuid.NewString(), active.SessionID, active.Registration); err == nil {
		t.Fatal("rename lost active registration uniqueness")
	}
}
