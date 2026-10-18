// Package notification sends channel-specific messages and stores polymorphic
// database notifications. Business modules own content and recipient selection.
package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	netmail "net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/pkg/asynqx"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/mailview"
	"github.com/wisalgroup/labena/pkg/pagination"
	"github.com/wisalgroup/labena/pkg/push"

	"go.opentelemetry.io/otel/metric"
)

// Notifier borrows its dependencies and is safe to share when they are.
// Database notifications, email and queued delivery are independently optional.
type Notifier struct {
	store      *store
	options    Options
	attempts   metric.Int64Counter
	pendingAge metric.Float64Gauge
}

// Options supplies optional delivery clients. Queue is needed only by Dispatch;
// Enqueue persists intent using the database even when this process has no Redis client.
type Options struct {
	Meter        metric.Meter
	Logger       *slog.Logger
	Queue        asynqx.Client
	Mailer       mail.Sender
	MailRenderer *mailview.Renderer
	Pusher       push.Sender
	PushProject  string
	// SessionActive is required for session-bound push registrations. It must
	// return false for revoked/expired sessions and errors for unavailable checks.
	// Sessionless registrations do not call it.
	SessionActive func(context.Context, Recipient, string) (bool, error)
}

// New borrows its dependencies without I/O or background work. A nil database
// supports offline construction and email-only sends; SQL operations return ErrNilDB.
func New(db *bun.DB, options Options) (*Notifier, error) {
	if options.Mailer != nil && options.MailRenderer == nil {
		return nil, ErrMailRendererRequired
	}
	n := &Notifier{store: newStore(db), options: options}
	if options.Meter != nil {
		var err error
		n.attempts, err = options.Meter.Int64Counter("notification.delivery.attempts")
		if err != nil {
			return nil, err
		}
		n.pendingAge, err = options.Meter.Float64Gauge("notification.pending.age", metric.WithUnit("s"))
		if err != nil {
			return nil, err
		}
	}
	return n, nil
}

// Result describes one synchronous channel attempt. Inspect every result on error
// because other channels may have succeeded.
type Result struct {
	// PushTicketID is set when Expo accepted a synchronous push. The caller checks its receipt.
	PushTicketID string
	// Destination is the subscription ID for push, never a provider registration.
	Destination string
	Channel     Channel
	Err         error
}

// Receipt acknowledges persisted intent, subject to the caller's transaction commit.
// It does not imply Redis acceptance or delivery to the recipient.
type Receipt struct {
	DeliveryIDs []uuid.UUID `json:"delivery_ids"`
}

// Send validates all content before attempting each channel once. Database-only
// messages join the caller's SQL transaction. Messages containing external channels must run
// outside transactions. Use Enqueue for durable, independent channel retries.
func (n *Notifier) Send(ctx context.Context, recipient Recipient, message Message) ([]Result, error) {
	if message.Email != nil || message.Push != nil {
		if err := database.RequireNoTransaction(ctx); err != nil {
			return nil, err
		}
	}
	deliveries, err := n.prepare(recipient, message)
	if err != nil {
		return nil, err
	}
	expanded := make([]*Delivery, 0, len(deliveries))
	for _, d := range deliveries {
		if d.Channel == ChannelPush {
			rows, e := n.pushTargets(ctx, d)
			if e != nil {
				return nil, e
			}
			expanded = append(expanded, rows...)
		} else {
			expanded = append(expanded, d)
		}
	}
	deliveries = expanded
	results := make([]Result, 0, len(deliveries))
	var failures error
	for _, delivery := range deliveries {
		err := n.sendChannel(ctx, delivery)
		if errors.Is(err, errSkipped) {
			err = nil
		}
		results = append(results, Result{Channel: delivery.Channel, Destination: delivery.Destination, PushTicketID: delivery.PushTicketID, Err: err})
		if err != nil {
			failures = errors.Join(failures, fmt.Errorf("%s: %w", delivery.Channel, err))
		}
	}
	return results, failures
}

// Enqueue snapshots all channel content and persists the intents atomically. It
// joins the caller's transaction and performs no queue or provider I/O. Dispatch
// later submits committed, due delivery IDs to Asynq.
func (n *Notifier) Enqueue(ctx context.Context, recipient Recipient, message Message) (Receipt, error) {
	deliveries, err := n.prepare(recipient, message)
	if err != nil {
		return Receipt{}, err
	}
	ids, err := n.enqueueDeliveries(ctx, deliveries)
	return Receipt{DeliveryIDs: ids}, err
}

// List returns a bounded, newest-first page of a recipient's database notifications.
func (n *Notifier) List(ctx context.Context, recipient Recipient, p pagination.Pagination) (*pagination.Page[*Record], error) {
	p = pagination.NewPagination(p.Page, p.PerPage)
	records, total, err := n.store.List(ctx, recipient, p)
	if err != nil {
		return nil, err
	}
	return pagination.NewPage(records, p, total), nil
}

func (n *Notifier) UnreadCount(ctx context.Context, recipient Recipient) (int, error) {
	return n.store.UnreadCount(ctx, recipient)
}

// MarkRead preserves the first read time and never crosses recipient boundaries.
func (n *Notifier) MarkRead(ctx context.Context, recipient Recipient, id uuid.UUID) error {
	return n.store.MarkRead(ctx, recipient, id)
}

// deliveryPayload contains only the selected channel's immutable content. Routing
// identity, occurrence key and notification type belong to Delivery, not this JSON.
type deliveryPayload struct {
	Data    json.RawMessage   `json:"data,omitempty"`
	Email   *mailview.Content `json:"email,omitempty"`
	Address string            `json:"address,omitempty"`
	Push    *push.Message     `json:"push,omitempty"`
}

func (n *Notifier) prepare(recipient Recipient, message Message) ([]*Delivery, error) {
	if !recipient.Valid() {
		return nil, ErrInvalidRecipient
	}
	if err := message.validate(); err != nil {
		return nil, err
	}
	if message.Key == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		message.Key = id.String()
	}
	channels := message.Channels()
	deliveries := make([]*Delivery, 0, len(channels))
	for _, channel := range channels {
		var content deliveryPayload
		switch channel {
		case ChannelDatabase:
			content.Data = message.Database
		case ChannelPush:
			if n.options.Pusher == nil || n.options.PushProject == "" {
				return nil, ErrUnavailable
			}
			snapshot := *message.Push
			snapshot.Data = maps.Clone(message.Push.Data)
			if snapshot.Data == nil {
				snapshot.Data = map[string]string{}
			}
			snapshot.Data["notification_key"] = message.Key
			snapshot.Data["notification_type"] = message.Type
			if err := snapshot.Validate(); err != nil {
				return nil, err
			}
			content.Push = &snapshot
		case ChannelEmail:
			if n.options.Mailer == nil || n.options.MailRenderer == nil {
				return nil, ErrUnavailable
			}
			if !validEmail(recipient.Email) {
				return nil, ErrInvalidMessage
			}
			rendered, err := n.options.MailRenderer.Render(*message.Email)
			if err != nil {
				return nil, err
			}
			content.Email, content.Address = &rendered, recipient.Email
		}
		data, err := json.Marshal(content)
		if err != nil {
			return nil, err
		}
		if len(data) > 64<<10 {
			return nil, ErrInvalidMessage
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, &Delivery{ID: id, RecipientType: recipient.Type, RecipientID: recipient.ID, Type: message.Type, Key: message.Key, Channel: channel, Payload: data})
	}
	return deliveries, nil
}

func (n *Notifier) sendChannel(ctx context.Context, delivery *Delivery) error {
	var content deliveryPayload
	if err := json.Unmarshal(delivery.Payload, &content); err != nil {
		return ErrInvalidMessage
	}
	switch delivery.Channel {
	case ChannelDatabase:
		record, err := newRecord(Recipient{Type: delivery.RecipientType, ID: delivery.RecipientID}, delivery.Type, delivery.Key, content.Data)
		if err != nil {
			return err
		}
		return n.store.Create(ctx, record)
	case ChannelPush:
		return n.sendPush(ctx, delivery, content)
	case ChannelEmail:
		if content.Email == nil || !validEmail(content.Address) {
			return ErrInvalidMessage
		}
		if n.options.Mailer == nil {
			return ErrUnavailable
		}
		return n.options.Mailer.Send(ctx, mail.Message{To: []mail.Address{{Email: content.Address}}, Subject: content.Email.Subject, TextBody: content.Email.Text, HTMLBody: content.Email.HTML})
	default:
		return ErrInvalidMessage
	}
}

func validEmail(value string) bool {
	address, err := netmail.ParseAddress(value)
	return err == nil && address.Address == value && !strings.ContainsAny(value, "\r\n")
}
