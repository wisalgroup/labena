package notification

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/asynqx"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/pagination"
	"github.com/wisalgroup/labena/pkg/push"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const DeliveryTask = "notifications.deliver.v1"
const DispatchTask = "notifications.dispatch.v1"

// PruneSubscriptionsTask disables a bounded batch of inactive push registrations.
const PruneSubscriptionsTask = "notifications.prune_subscriptions.v1"

// DeliveryStatus is the persisted lifecycle of one channel. Its zero value is invalid.
type DeliveryStatus string

const (
	// DeliveryPending is waiting for dispatch or its next retry time.
	DeliveryPending DeliveryStatus = "pending"
	// DeliveryAwaitingReceipt is accepted by Expo, pending provider handoff confirmation.
	DeliveryAwaitingReceipt DeliveryStatus = "awaiting_receipt"
	// DeliverySending is held by a worker with a bounded lease.
	DeliverySending DeliveryStatus = "sending"
	// DeliverySent means the channel accepted the operation (Expo confirmed platform
	// handoff for push), not that a person received or read it.
	DeliverySent DeliveryStatus = "sent"
	// DeliveryFailed exhausted its attempts and requires deliberate replay.
	DeliveryFailed DeliveryStatus = "failed"
	// DeliverySkipped had no eligible destination or its content expired.
	DeliverySkipped DeliveryStatus = "skipped"
)

func (s DeliveryStatus) Valid() bool {
	switch s {
	case DeliveryPending, DeliveryAwaitingReceipt, DeliverySending, DeliverySent, DeliveryFailed, DeliverySkipped:
		return true
	default:
		return false
	}
}

var (
	ErrInvalidDeliveryID     = apperror.Validation("Provide a valid delivery ID.")
	ErrInvalidDeliveryStatus = apperror.Validation("Provide a valid delivery status.")
	ErrDeliveryNotFound      = apperror.NotFound("The delivery was not found.")
	ErrDeliveryClaimLost     = apperror.Conflict("The delivery attempt no longer owns its claim.")
	ErrDeliveryNotFailed     = apperror.Conflict("Only a failed delivery can be retried.")
)

// Dispatch scans a bounded batch. Duplicate submissions are harmless to the
// database claim; Redis uniqueness reduces redundant jobs but is not correctness.
func (n *Notifier) Dispatch(ctx context.Context) (int, error) {
	if err := database.RequireNoTransaction(ctx); err != nil {
		return 0, err
	}
	if n.options.Queue == nil {
		return 0, ErrUnavailable
	}
	ids, err := n.store.Due(ctx, 100)
	if err != nil {
		return 0, err
	}
	for i, id := range ids {
		data, _ := json.Marshal(id)
		_, err := n.options.Queue.EnqueueContext(ctx, asynqx.NewTask(DeliveryTask, data), asynq.Unique(time.Minute), asynq.MaxRetry(3), asynq.Timeout(time.Minute))
		if err != nil && !errors.Is(err, asynq.ErrDuplicateTask) {
			return i, err
		}
	}
	return len(ids), nil
}

// Deliver claims one channel. The ledger owns provider retries (eight attempts,
// exponential delay). Asynq retries infrastructure failures only. A lease prevents
// concurrent healthy attempts; a provider acceptance followed by a crash may send
// twice. SMTP cannot provide an exactly-once guarantee.
func (n *Notifier) Deliver(ctx context.Context, id uuid.UUID) error {
	if err := database.RequireNoTransaction(ctx); err != nil {
		return err
	}
	if id == uuid.Nil {
		return ErrInvalidDeliveryID
	}
	row, err := n.store.Claim(ctx, id)
	if err != nil || row == nil {
		return err
	}
	attempt, cancel := context.WithTimeout(ctx, 30*time.Second)
	var sendErr error
	var receiptStatus DeliveryStatus
	var receiptDelay time.Duration
	if row.PushTicketID != "" {
		receiptStatus, receiptDelay, sendErr = n.checkPushReceipt(attempt, row)
	} else {
		sendErr = n.sendChannel(attempt, row)
	}
	cancel()
	if sendErr != nil && n.options.Logger != nil {
		n.options.Logger.WarnContext(ctx, "notification delivery attempt failed", "delivery_id", row.ID, "channel", row.Channel, "attempt", row.Attempts)
	}
	// Persist the outcome even when the task/request deadline has expired.
	finish, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	status := DeliveryPending
	if sendErr == nil {
		status = DeliverySent
		if row.PushTicketID != "" && receiptStatus == "" {
			status = DeliveryAwaitingReceipt
			receiptDelay = 15 * time.Minute
		}
	} else if errors.Is(sendErr, errSkipped) {
		status = DeliverySkipped
	} else if errors.Is(sendErr, ErrInvalidMessage) || errors.Is(sendErr, push.ErrInvalid) {
		status = DeliveryFailed
	}
	var provider *push.Error
	var retryAfter time.Duration
	if errors.As(sendErr, &provider) {
		retryAfter = provider.RetryAfter
		if provider.Permanent {
			status = DeliveryFailed
		}
		if provider.Code == "expired" {
			status = DeliverySkipped
		}
	}
	if receiptStatus != "" {
		status = receiptStatus
	}
	retryAfter = max(retryAfter, receiptDelay)
	if n.attempts != nil {
		outcome := string(status)
		if provider != nil {
			switch provider.Code {
			case "unregistered", "rate_limited", "credentials", "invalid_argument":
				outcome = provider.Code
			}
		}
		n.attempts.Add(finish, 1, metric.WithAttributes(attribute.String("channel", string(row.Channel)), attribute.String("outcome", outcome)))
	}
	return n.store.finishOutcome(finish, row, status, retryAfter)
}

// GetDelivery returns operator metadata only; destination and message content stay private.
func (n *Notifier) GetDelivery(ctx context.Context, id uuid.UUID) (*Delivery, error) {
	if id == uuid.Nil {
		return nil, ErrInvalidDeliveryID
	}
	row, err := n.store.GetDelivery(ctx, id)
	if err != nil {
		return nil, err
	}
	metadata := *row
	metadata.Payload = nil
	metadata.LeaseToken = nil
	return &metadata, nil
}

// RetryDelivery resets the attempt budget of a failed delivery for deliberate replay.
func (n *Notifier) RetryDelivery(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return ErrInvalidDeliveryID
	}
	return n.store.RetryDelivery(ctx, id)
}

// ListDeliveries returns operator metadata. An empty status includes all states.
func (n *Notifier) ListDeliveries(ctx context.Context, status DeliveryStatus, p pagination.Pagination) (*pagination.Page[*Delivery], error) {
	if status != "" && !status.Valid() {
		return nil, ErrInvalidDeliveryStatus
	}
	p = pagination.NewPagination(p.Page, p.PerPage)
	rows, total, err := n.store.ListDeliveries(ctx, status, p)
	if err != nil {
		return nil, err
	}
	return pagination.NewPage(rows, p, total), nil
}

// Delivery tracks one channel attempt for a recipient and occurrence.
// Payload contains recipient data and is never serialized by operator endpoints.
type Delivery struct {
	bun.BaseModel       `bun:"table:notification_deliveries,alias:d"`
	ID                  uuid.UUID       `bun:",pk,type:uuid" json:"id"`
	RecipientType       string          `json:"recipient_type"`
	RecipientID         string          `json:"recipient_id"`
	Type                string          `json:"type"`
	Key                 string          `json:"key"`
	Channel             Channel         `json:"channel"`
	Destination         string          `json:"-"`
	PushTicketID        string          `json:"-"`
	PushAcceptedAt      *time.Time      `json:"-"`
	SubscriptionVersion int64           `json:"-"`
	Payload             json.RawMessage `bun:",type:jsonb" json:"-"`
	Status              DeliveryStatus  `json:"status"`
	Attempts            int             `json:"attempts"`
	AvailableAt         time.Time       `json:"available_at"`
	LeaseToken          *uuid.UUID      `bun:",type:uuid" json:"-"`
	LeaseUntil          *time.Time      `json:"-"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// RunDispatcher polls committed intents without requiring the cron scheduler.
// Run in worker processes; replicas coordinate through delivery claims.
func (n *Notifier) RunDispatcher(ctx context.Context, interval time.Duration) error {
	if interval < 100*time.Millisecond {
		return ErrInvalidMessage
	}
	timer := time.NewTicker(interval)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if _, err := n.Dispatch(ctx); err != nil && ctx.Err() == nil && n.options.Logger != nil {
			n.options.Logger.ErrorContext(ctx, "notification dispatch failed")
		}

		if n.pendingAge != nil {
			if age, err := n.store.pendingAge(ctx); err == nil {
				n.pendingAge.Record(ctx, age)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
	}
}

// Dispatcher is an explicitly owned lifecycle runner. Construction starts no work.
type Dispatcher struct {
	notifier *Notifier
	interval time.Duration
	ready    chan struct{}
}

// NewDispatcher borrows a notifier and validates the polling interval. Zero uses
// one second; positive intervals must be at least 100 ms. Only Run starts work.
func NewDispatcher(n *Notifier, interval time.Duration) (*Dispatcher, error) {
	if n == nil {
		return nil, ErrNotifierRequired
	}
	if interval == 0 {
		interval = time.Second
	}
	if interval < 100*time.Millisecond {
		return nil, ErrInvalidMessage
	}
	return &Dispatcher{notifier: n, interval: interval, ready: make(chan struct{})}, nil
}
func (d *Dispatcher) Ready() <-chan struct{} { return d.ready }
func (d *Dispatcher) Run(ctx context.Context) error {
	close(d.ready)
	return d.notifier.RunDispatcher(ctx, d.interval)
}
