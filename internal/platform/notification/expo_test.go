package notification

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/push"

	"github.com/google/uuid"
)

type expoCapture struct {
	sends, checks int
	receipt       push.Receipt
	lookupErr     error
}

func (e *expoCapture) Send(context.Context, string, push.Message) (push.Ticket, error) {
	e.sends++
	return push.Ticket{ID: fmt.Sprintf("ticket-%d", e.sends)}, nil
}
func (e *expoCapture) Receipt(context.Context, string) (push.Receipt, error) {
	e.checks++
	return e.receipt, e.lookupErr
}

func TestExpoReceiptLifecyclePostgres(t *testing.T) {
	db, owner := databaseFixture(t)
	sender := &expoCapture{}
	n, _ := New(db, Options{SessionActive: activePushSession, Pusher: sender, PushProject: "expo-project"})
	r := Recipient{Type: "user", ID: owner.String()}
	sub, err := n.Subscribe(t.Context(), r, uuid.NewString(), "session", "ExpoPushToken[phone]")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := n.Enqueue(t.Context(), r, Message{Type: "ready", Push: &push.Message{Title: "Ready"}})
	if err != nil {
		t.Fatal(err)
	}
	id := receipt.DeliveryIDs[0]
	deliver := func() {
		t.Helper()
		if err := n.Deliver(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	due := func() {
		t.Helper()
		if _, err := db.NewRaw("UPDATE notification_deliveries SET available_at=now() WHERE id=?", id).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	row := func() *Delivery {
		t.Helper()
		d, err := n.store.GetDelivery(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	deliver()
	d := row()
	if d.Status != DeliveryAwaitingReceipt || d.PushTicketID != "ticket-1" || d.PushAcceptedAt == nil || d.Attempts != 1 || time.Until(d.AvailableAt) < 14*time.Minute {
		t.Fatal(d)
	}
	// No receipt yet, then network failure: poll the same ticket, never resend.
	due()
	deliver()
	if row().Status != DeliveryAwaitingReceipt || sender.sends != 1 {
		t.Fatal(row(), sender.sends)
	}
	sender.lookupErr = errors.New("lookup offline")
	due()
	deliver()
	if sender.sends != 1 || row().Status != DeliveryAwaitingReceipt {
		t.Fatal(row())
	}
	// Expired worker lease after the final send attempt remains eligible for receipt lookup.
	sender.lookupErr = nil
	sender.receipt = push.Receipt{Found: true}
	if _, err = db.NewRaw("UPDATE notification_deliveries SET status='sending',attempts=8,lease_token=?,lease_until=now()-interval '1 minute' WHERE id=?", uuid.New(), id).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	ids, err := n.store.Due(t.Context(), 100)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	deliver()
	if row().Status != DeliverySent || row().Attempts != 8 || sender.sends != 1 {
		t.Fatal(row(), sender.sends)
	}
	// An invalid receipt for an old registration cannot disable a rotated registration.
	second, err := n.Enqueue(t.Context(), r, Message{Type: "second", Push: &push.Message{Title: "Second"}})
	if err != nil {
		t.Fatal(err)
	}
	id = second.DeliveryIDs[0]
	deliver()
	rotated, err := n.Subscribe(t.Context(), r, sub.InstallationID, "session", "ExpoPushToken[new]")
	if err != nil {
		t.Fatal(err)
	}
	sender.receipt = push.Receipt{Found: true, Error: &push.Error{Code: "unregistered", Permanent: true, InvalidDestination: true}}
	due()
	deliver()
	if row().Status != DeliveryFailed {
		t.Fatal(row())
	}
	if err = db.NewSelect().Model(rotated).WherePK().Scan(t.Context()); err != nil || rotated.DisabledAt != nil {
		t.Fatal("new registration disabled", err)
	}
}
func TestExpoReceiptFailureRetryAndTimeout(t *testing.T) {
	for _, mode := range []string{"retryable", "unregistered", "missing"} {
		t.Run(mode, func(t *testing.T) {
			db, owner := databaseFixture(t)
			sender := &expoCapture{}
			n, _ := New(db, Options{SessionActive: activePushSession, Pusher: sender, PushProject: "project"})
			r := Recipient{Type: "admin", ID: owner.String()}
			sub, err := n.Subscribe(t.Context(), r, uuid.NewString(), "session", "ExpoPushToken[phone]")
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := n.Enqueue(t.Context(), r, Message{Type: "test", Push: &push.Message{Title: "Test"}})
			if err != nil {
				t.Fatal(err)
			}
			id := receipt.DeliveryIDs[0]
			if err = n.Deliver(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "retryable":
				sender.receipt = push.Receipt{Found: true, Error: &push.Error{Code: "rate_limited", RetryAfter: 2 * time.Minute}}
			case "unregistered":
				sender.receipt = push.Receipt{Found: true, Error: &push.Error{Code: "unregistered", Permanent: true, InvalidDestination: true}}
			case "missing":
				_, err = db.NewRaw("UPDATE notification_deliveries SET push_accepted_at=now()-interval '25 hours' WHERE id=?", id).Exec(t.Context())
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err = db.NewRaw("UPDATE notification_deliveries SET available_at=now() WHERE id=?", id).Exec(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err = n.Deliver(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			row, err := n.store.GetDelivery(t.Context(), id)
			if err != nil || sender.sends != 1 {
				t.Fatal(row, err, sender.sends)
			}
			if mode == "retryable" {
				if row.Status != DeliveryPending || row.PushTicketID != "" || time.Until(row.AvailableAt) < 119*time.Second {
					t.Fatal(row)
				}
			} else {
				if row.Status != DeliveryFailed || row.PushTicketID == "" {
					t.Fatal(row)
				}
				if mode == "unregistered" {
					if err = db.NewSelect().Model(sub).WherePK().Scan(t.Context()); err != nil || sub.DisabledAt == nil {
						t.Fatal(sub, err)
					}
				}
			}
		})
	}
}
