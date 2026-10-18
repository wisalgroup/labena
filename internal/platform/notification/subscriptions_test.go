package notification

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/wisalgroup/labena/pkg/push"
)

func activePushSession(context.Context, Recipient, string) (bool, error) { return true, nil }

func TestInstallationAdmissionPostgres(t *testing.T) {
	db, owner := databaseFixture(t)
	sender := &expoCapture{}
	n, err := New(db, Options{Pusher: sender, PushProject: "capacity"})
	if err != nil {
		t.Fatal(err)
	}
	r := Recipient{Type: "user", ID: owner.String()}
	var first *Subscription
	for i := range MaxInstallations - 1 {
		sub, err := n.Subscribe(t.Context(), r, uuid.NewString(), "", fmt.Sprintf("ExpoPushToken[capacity-%d]", i))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = sub
		}
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 8)
	for i := range 8 {
		wg.Go(func() {
			<-start
			_, err := n.Subscribe(t.Context(), r, uuid.NewString(), "", fmt.Sprintf("ExpoPushToken[race-%d]", i))
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, ErrInstallationLimit) {
			t.Fatal(err)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d registrations at boundary", accepted)
	}
	if _, err := n.Subscribe(t.Context(), r, first.InstallationID, "", "ExpoPushToken[rotated]"); err != nil {
		t.Fatal("rotation at capacity", err)
	}
	if _, err := n.Send(t.Context(), r, Message{Type: "capacity", Push: &push.Message{Title: "Test"}}); err != nil {
		t.Fatal(err)
	}
	if sender.sends != MaxInstallations {
		t.Fatalf("sent %d", sender.sends)
	}
	if err := n.Unsubscribe(t.Context(), r, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Subscribe(t.Context(), r, uuid.NewString(), "", "ExpoPushToken[replacement]"); err != nil {
		t.Fatal(err)
	}
	// Existing excess data has an explicit recovery path and a specific error.
	row := &Subscription{ID: uuid.New(), RecipientType: r.Type, RecipientID: r.ID, InstallationID: uuid.NewString(), Project: "capacity", Registration: "ExpoPushToken[legacy]", RegistrationHash: "legacy", Version: 1}
	if _, err := db.NewInsert().Model(row).Value("updated_at", "now()").Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Enqueue(t.Context(), r, Message{Type: "legacy", Push: &push.Message{Title: "Test"}}); !errors.Is(err, ErrInstallationLimit) {
		t.Fatal(err)
	}
	if err := n.Unsubscribe(t.Context(), r, row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Enqueue(t.Context(), r, Message{Type: "recovered", Push: &push.Message{Title: "Test"}}); err != nil {
		t.Fatal(err)
	}
}

func TestPushSessionChecksFailClosedPostgres(t *testing.T) {
	db, owner := databaseFixture(t)
	sender := &expoCapture{}
	n, _ := New(db, Options{Pusher: sender, PushProject: "session"})
	r := Recipient{Type: "user", ID: owner.String()}
	if _, err := n.Subscribe(t.Context(), r, uuid.NewString(), "session", "ExpoPushToken[session]"); !errors.Is(err, ErrSessionCheckRequired) {
		t.Fatal(err)
	}
	n.options.SessionActive = activePushSession
	sub, err := n.Subscribe(t.Context(), r, uuid.NewString(), "session", "ExpoPushToken[session]")
	if err != nil {
		t.Fatal(err)
	}
	message := Message{Type: "session", Push: &push.Message{Title: "Test"}}
	backendErr := errors.New("backend unavailable")
	n.options.SessionActive = func(context.Context, Recipient, string) (bool, error) { return false, backendErr }
	if _, err := n.Send(t.Context(), r, message); !errors.Is(err, backendErr) {
		t.Fatal(err)
	}
	if err := db.NewSelect().Model(sub).WherePK().Scan(t.Context()); err != nil || sub.DisabledAt != nil {
		t.Fatal("transient failure detached registration", err)
	}
	n.options.SessionActive = nil
	if _, err := n.Send(t.Context(), r, message); !errors.Is(err, ErrSessionCheckRequired) {
		t.Fatal(err)
	}
	if sender.sends != 0 {
		t.Fatal("sent without authorization")
	}
}

func TestStaleSessionCheckPreservesReregistrationPostgres(t *testing.T) {
	db, owner := databaseFixture(t)
	sender := &expoCapture{}
	n, _ := New(db, Options{Pusher: sender, PushProject: "rotation", SessionActive: activePushSession})
	r := Recipient{Type: "user", ID: owner.String()}
	sub, err := n.Subscribe(t.Context(), r, uuid.NewString(), "old", "ExpoPushToken[rotation]")
	if err != nil {
		t.Fatal(err)
	}
	n.options.SessionActive = func(ctx context.Context, r Recipient, session string) (bool, error) {
		if session == "new" {
			return true, nil
		}
		_, err := n.Subscribe(ctx, r, sub.InstallationID, "new", "ExpoPushToken[rotation]")
		return false, err
	}
	message := Message{Type: "rotation", Push: &push.Message{Title: "Test"}}
	if _, err = n.Send(t.Context(), r, message); err != nil {
		t.Fatal(err)
	}
	if err = db.NewSelect().Model(sub).WherePK().Scan(t.Context()); err != nil || sub.DisabledAt != nil || sub.SessionID != "new" {
		t.Fatal("new registration disabled", err)
	}
	if _, err = n.Send(t.Context(), r, message); err != nil {
		t.Fatal(err)
	}
	if sender.sends != 1 {
		t.Fatal("stale check sent or new registration suppressed", sender.sends)
	}
}
