package notification

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/platform/internal/reference"
	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/mailview"
	"github.com/wisalgroup/labena/pkg/push"
)

// Channel identifies an implemented delivery mechanism. Its zero value is invalid.
type Channel string

const (
	ChannelDatabase Channel = "database"
	ChannelEmail    Channel = "email"
	ChannelPush     Channel = "push"
)

func (c Channel) Valid() bool {
	return c == ChannelDatabase || c == ChannelEmail || c == ChannelPush
}

// Message describes one business occurrence. Non-nil channel content selects its
// channels; there is no separate channel list to keep in sync.
type Message struct {
	// Key optionally identifies an occurrence per recipient, such as "shipment:42".
	// Empty generates a fresh key per Send/Enqueue call, shared by its channels.
	// Enqueue reuses an explicit key to preserve persisted content and delivery state.
	Key string
	// Type is a module-owned name such as "order.shipped", shared by all channels.
	// Types need no central registration; clients may use them to select presentation.
	Type string
	// Database is a JSON object for an in-app notification. Nil omits this channel.
	// The caller owns its schema; titles, links and other content belong inside it.
	Database json.RawMessage
	// Email is rendered through the application's mail views before delivery or
	// persistence, so queued retries use the same content. Nil omits this channel.
	Email *mailview.Email
	// Push selects delivery to registered installations. Nil omits this channel.
	Push *push.Message
}

// Channels returns selected channels in delivery order. It does not validate content.
func (m Message) Channels() []Channel {
	channels := make([]Channel, 0, 2)
	if m.Database != nil {
		channels = append(channels, ChannelDatabase)
	}
	if m.Email != nil {
		channels = append(channels, ChannelEmail)
	}
	if m.Push != nil {
		channels = append(channels, ChannelPush)
	}
	return channels
}

// MaxDataSize bounds a database notification's JSON input in bytes.
const MaxDataSize = 32 << 10

var (
	ErrInvalidRecipient     = apperror.Validation("Provide a recipient type and ID.")
	ErrInvalidType          = apperror.Validation("Provide a notification type of 1 to 100 bytes without surrounding whitespace or control characters.")
	ErrInvalidKey           = apperror.Validation("Provide an occurrence key of 1 to 200 bytes without surrounding whitespace or control characters.")
	ErrInvalidData          = apperror.Validation("Notification data must be a JSON object of at most 32 KiB.")
	ErrInvalidMessage       = apperror.Validation("Invalid notification content or destination.")
	ErrInvalidID            = apperror.Validation("Provide a valid notification ID.")
	ErrNotFound             = apperror.NotFound("The notification was not found.")
	ErrUnavailable          = apperror.Unavailable("The notification channel or queue is not configured.")
	ErrMailRendererRequired = apperror.Internal("notification: mail renderer required when a mailer is configured")
	ErrNotifierRequired     = apperror.Internal("notification: notifier required")
	errInvalidRecord        = apperror.Internal("notification: invalid record identity or timestamps")
)

func (m Message) validate() error {
	if m.Key != "" && !validLabel(m.Key, 200) {
		return ErrInvalidKey
	}
	if !validLabel(m.Type, 100) {
		return ErrInvalidType
	}
	if m.Database == nil && m.Email == nil && m.Push == nil {
		return ErrInvalidMessage
	}
	if m.Database != nil && (len(m.Database) > MaxDataSize || !validData(m.Database)) {
		return ErrInvalidData
	}
	if m.Push != nil {
		return m.Push.Validate()
	}
	return nil
}

func validData(data json.RawMessage) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '{' && utf8.Valid(data) && json.Valid(data)
}

func validLabel(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

// Recipient identifies any model by a stable logical type and canonical ID.
// Email is an explicit destination; it is never part of database notification identity.
// The caller authorizes the recipient and resolves its destination before notifying.
type Recipient struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Email string `bun:"-" json:"email,omitempty"`
}

func (r Recipient) Valid() bool {
	return reference.Valid(r.Type, r.ID)
}

// Record is a persisted database notification with recipient-scoped read state.
// Data is returned as JSON, never as an encoded JSON string or fixed text body.
type Record struct {
	bun.BaseModel `bun:"table:notifications,alias:n"`
	ID            uuid.UUID       `bun:",pk,type:uuid" json:"id"`
	Recipient     Recipient       `bun:"embed:recipient_" json:"recipient"`
	Type          string          `bun:",notnull" json:"type"`
	Key           string          `bun:",notnull" json:"-"`
	Data          json.RawMessage `bun:",type:jsonb,notnull" json:"data"`
	ReadAt        *time.Time      `json:"read_at"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func newRecord(recipient Recipient, messageType, key string, data json.RawMessage) (*Record, error) {
	record := &Record{Recipient: Recipient{Type: recipient.Type, ID: recipient.ID}, Type: messageType, Key: key, Data: bytes.Clone(data)}
	if err := record.validateContent(); err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	record.ID = id
	record.CreatedAt = time.Now().UTC()
	record.UpdatedAt = record.CreatedAt
	return record, nil
}

func (r Record) validateContent() error {
	if !r.Recipient.Valid() {
		return ErrInvalidRecipient
	}
	if !validLabel(r.Key, 200) {
		return ErrInvalidKey
	}
	if !validLabel(r.Type, 100) {
		return ErrInvalidType
	}
	// Input size is checked once at Send/Enqueue. PostgreSQL normalizes JSONB,
	// adding whitespace and expanding numbers; do not reapply the input byte limit.
	if !validData(r.Data) {
		return ErrInvalidData
	}
	return nil
}

func (r Record) validate() error {
	if err := r.validateContent(); err != nil {
		return err
	}
	if r.ID == uuid.Nil || r.CreatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) || (r.ReadAt != nil && r.ReadAt.Before(r.CreatedAt)) {
		return errInvalidRecord
	}
	return nil
}
