package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/push"

	"github.com/google/uuid"
)

func (n *Notifier) sendPush(ctx context.Context, d *Delivery, p deliveryPayload) error {
	if p.Push == nil {
		return ErrInvalidMessage
	}
	if p.Push.ExpiresAt != nil && !p.Push.ExpiresAt.After(time.Now()) {
		return errSkipped
	}
	if d.Destination == "" {
		return errSkipped
	}
	if n.options.Pusher == nil {
		return ErrUnavailable
	}
	db, err := n.store.executor(ctx)
	if err != nil {
		return err
	}
	var sub Subscription
	err = db.NewSelect().Model(&sub).Where("id=? AND version=? AND recipient_type=? AND recipient_id=? AND project=? AND disabled_at IS NULL", d.Destination, d.SubscriptionVersion, d.RecipientType, d.RecipientID, n.options.PushProject).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return errSkipped
	}
	if err != nil {
		return err
	}
	if sub.SessionID != "" {
		if n.options.SessionActive == nil {
			return ErrSessionCheckRequired
		}
		active, err := n.options.SessionActive(ctx, Recipient{Type: sub.RecipientType, ID: sub.RecipientID}, sub.SessionID)
		if err != nil {
			return err
		}
		if !active {
			// Compare the version so a concurrent new-session registration survives.
			_, err := db.NewRaw(`UPDATE push_subscriptions SET disabled_at=now(),version=version+1 WHERE id=? AND version=? AND disabled_at IS NULL`, sub.ID, sub.Version).Exec(ctx)
			if err != nil {
				return err
			}
			return errSkipped
		}
	}
	ticket, sendErr := n.options.Pusher.Send(ctx, sub.Registration, *p.Push)
	err = sendErr
	if err == nil {
		if ticket.ID == "" {
			return ErrInvalidMessage
		}
		d.PushTicketID = ticket.ID
		now := time.Now().UTC()
		d.PushAcceptedAt = &now
	}
	var failure *push.Error
	if errors.As(err, &failure) && failure.InvalidDestination {
		_, disableErr := db.NewRaw(`UPDATE push_subscriptions SET disabled_at=now(),version=version+1 WHERE id=? AND version=?`, sub.ID, sub.Version).Exec(ctx)
		if disableErr != nil {
			return disableErr
		}
	}
	return err
}

func (n *Notifier) pushTargets(ctx context.Context, d *Delivery) ([]*Delivery, error) {
	db, err := n.store.executor(ctx)
	if err != nil {
		return nil, err
	}
	var subs []Subscription
	err = db.NewSelect().Model(&subs).Where("recipient_type=? AND recipient_id=? AND project=? AND disabled_at IS NULL", d.RecipientType, d.RecipientID, n.options.PushProject).Order("id").Limit(MaxInstallations + 1).Scan(ctx)
	if err != nil {
		return nil, err
	}
	if len(subs) > MaxInstallations {
		return nil, ErrInstallationLimit
	}
	if len(subs) == 0 {
		return []*Delivery{d}, nil
	}
	rows := make([]*Delivery, 0, len(subs))
	for _, sub := range subs {
		row := *d
		row.ID = uuid.New()
		row.Destination = sub.ID.String()
		row.SubscriptionVersion = sub.Version
		rows = append(rows, &row)
	}
	return rows, nil
}

// enqueueDeliveries serializes the first expansion for an occurrence. Re-enqueue
// returns the original push target set even if registrations have since changed.
func (n *Notifier) enqueueDeliveries(ctx context.Context, rows []*Delivery) ([]uuid.UUID, error) {
	manager, err := database.NewTxManager(n.store.db, nil)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	err = manager.Execute(ctx, func(ctx context.Context) error {
		db, err := n.store.executor(ctx)
		if err != nil {
			return err
		}
		expanded := make([]*Delivery, 0, len(rows))
		for _, row := range rows {
			if row.Channel != ChannelPush {
				expanded = append(expanded, row)
				continue
			}
			lock, _ := json.Marshal([]string{row.RecipientType, row.RecipientID, row.Key})
			if _, err = db.NewRaw("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", string(lock)).Exec(ctx); err != nil {
				return err
			}
			var existing []uuid.UUID
			if err = db.NewSelect().Table("notification_deliveries").Column("id").Where("recipient_type=? AND recipient_id=? AND key=? AND channel='push'", row.RecipientType, row.RecipientID, row.Key).Order("id").Scan(ctx, &existing); err != nil {
				return err
			}
			if len(existing) > 0 {
				ids = append(ids, existing...)
				continue
			}
			targets, err := n.pushTargets(ctx, row)
			if err != nil {
				return err
			}
			expanded = append(expanded, targets...)
		}
		if len(expanded) == 0 {
			return nil
		}
		stored, err := n.store.StoreDeliveries(ctx, expanded)
		ids = append(ids, stored...)
		return err
	})
	return ids, err
}

// Receipt lookup never sends. API failures and missing receipts preserve the
// original ticket. Only an explicit retryable receipt failure permits a new send.
func (n *Notifier) checkPushReceipt(ctx context.Context, d *Delivery) (DeliveryStatus, time.Duration, error) {
	if d.PushAcceptedAt == nil || time.Since(*d.PushAcceptedAt) >= 24*time.Hour {
		return DeliveryFailed, 0, &push.Error{Code: "receipt_unconfirmed", Permanent: true}
	}
	if n.options.Pusher == nil {
		return DeliveryAwaitingReceipt, 5 * time.Minute, ErrUnavailable
	}
	receipt, err := n.options.Pusher.Receipt(ctx, d.PushTicketID)
	if err != nil {
		var failure *push.Error
		delay := 5 * time.Minute
		if errors.As(err, &failure) {
			delay = max(delay, failure.RetryAfter)
		}
		return DeliveryAwaitingReceipt, delay, err
	}
	if !receipt.Found {
		return DeliveryAwaitingReceipt, 5 * time.Minute, nil
	}
	if receipt.Error == nil {
		return DeliverySent, 0, nil
	}
	failure := receipt.Error
	if failure.InvalidDestination {
		db, err := n.store.executor(ctx)
		if err != nil {
			return DeliveryAwaitingReceipt, 5 * time.Minute, err
		}
		_, err = db.NewRaw(`UPDATE push_subscriptions SET disabled_at=now(),version=version+1 WHERE id=? AND version=? AND recipient_type=? AND recipient_id=?`, d.Destination, d.SubscriptionVersion, d.RecipientType, d.RecipientID).Exec(ctx)
		if err != nil {
			return DeliveryAwaitingReceipt, 5 * time.Minute, err
		}
	}
	if failure.Permanent {
		return DeliveryFailed, 0, failure
	}
	d.PushTicketID = ""
	d.PushAcceptedAt = nil
	return DeliveryPending, failure.RetryAfter, failure
}
