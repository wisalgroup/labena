package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/push"

	"github.com/google/uuid"
)

type pushCapture struct {
	tokens []string
	fail   string
}

func (p *pushCapture) Send(_ context.Context, token string, _ push.Message) (push.Ticket, error) {
	p.tokens = append(p.tokens, token)
	if token == p.fail {
		return push.Ticket{}, &push.Error{Code: "unavailable"}
	}
	return push.Ticket{ID: "ticket-" + token}, nil
}
func (p *pushCapture) Receipt(context.Context, string) (push.Receipt, error) {
	return push.Receipt{Found: true}, nil
}

func TestPushTargetsRetriesAndDetachment(t *testing.T) {
	db, owner := databaseFixture(t)
	capture := &pushCapture{fail: "ExpoPushToken[phone]"}
	n, _ := New(db, Options{SessionActive: activePushSession, Pusher: capture, PushProject: "demo"})
	r := Recipient{Type: "admin", ID: owner.String()}
	phone, err := n.Subscribe(t.Context(), r, uuid.NewString(), "session-a", "ExpoPushToken[phone]")
	if err != nil {
		t.Fatal(err)
	}
	_, err = n.Subscribe(t.Context(), r, uuid.NewString(), "session-b", "ExpoPushToken[laptop]")
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{Key: "order:1", Type: "order.shipped", Push: &push.Message{Title: "Shipped"}}
	receipt, err := n.Enqueue(t.Context(), r, msg)
	if err != nil || len(receipt.DeliveryIDs) != 2 {
		t.Fatal(receipt, err)
	}
	for _, id := range receipt.DeliveryIDs {
		if err = n.Deliver(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	var pending int
	pending, err = db.NewSelect().Table("notification_deliveries").Where("status='pending'").Count(t.Context())
	if err != nil || pending != 1 {
		t.Fatal(pending, err)
	}
	_, err = n.Subscribe(t.Context(), r, uuid.NewString(), "session-c", "ExpoPushToken[tablet]")
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := n.Enqueue(t.Context(), r, msg)
	if err != nil || len(repeat.DeliveryIDs) != 2 {
		t.Fatal("target set changed", repeat, err)
	}
	if err = n.Unsubscribe(t.Context(), r, phone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.NewRaw("UPDATE notification_deliveries SET available_at=now() WHERE status='pending'").Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range receipt.DeliveryIDs {
		if err = n.Deliver(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	if len(capture.tokens) != 2 {
		t.Fatal("resent accepted or detached destination", capture.tokens)
	}
	skipped, err := db.NewSelect().Table("notification_deliveries").Where("status='skipped'").Count(t.Context())
	if err != nil || skipped != 1 {
		t.Fatal(skipped, err)
	}
}
func TestDispatcherCancels(t *testing.T) {
	n, _ := New(nil, Options{})
	ctx, cancel := context.WithCancel(t.Context())
	d, err := NewDispatcher(n, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	<-d.Ready()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatcher leaked")
	}
}

func TestDispatcherRejectsInvalidConstruction(t *testing.T) {
	if dispatcher, err := NewDispatcher(nil, 0); !errors.Is(err, ErrNotifierRequired) || dispatcher != nil {
		t.Fatal("nil notifier accepted", dispatcher, err)
	}
	n, err := New(nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, interval := range []time.Duration{-time.Second, time.Millisecond} {
		if dispatcher, err := NewDispatcher(n, interval); !errors.Is(err, ErrInvalidMessage) || dispatcher != nil {
			t.Fatal("invalid polling interval accepted", dispatcher, err)
		}
	}
	if dispatcher, err := NewDispatcher(n, 0); err != nil || dispatcher.interval != time.Second {
		t.Fatal("zero interval lost its default", dispatcher, err)
	}
}
