// Package mail sends email immediately or through an optional Asynq queue.
// It borrows clients; application assembly owns their lifetime.
package mail

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/pkg/asynqx"
	"github.com/wisalgroup/labena/pkg/database"
)

var (
	ErrTransportRequired = errors.New("mail: transport is required")
	ErrQueueUnavailable  = errors.New("mail: queue is not configured")
	ErrInvalidMessage    = errors.New("mail: invalid queued message")
	ErrAttachments       = errors.New("mail: queued messages cannot contain attachment readers; use a module job that reopens stored files")
	ErrMessageTooLarge   = errors.New("mail: queued message exceeds 64 KiB")
)

// Address keeps the mailbox separate from its optional display name.
type Address struct {
	Email string
	Name  string
}

// Attachment borrows a reader for immediate sending. Readers cannot be queued.
type Attachment struct {
	Filename    string
	ContentType string

	Body io.Reader
}

func NewAttachment(filename, contentType string, body io.Reader) Attachment {
	return Attachment{
		Filename:    filename,
		ContentType: contentType,
		Body:        body,
	}
}

// Message contains rendered content. An omitted From uses the transport default.
// Send borrows the values and readers for the duration of the call; Enqueue copies
// supported values into its serialized task. Callers must not mutate them in use.
type Message struct {
	From *Address

	To  []Address
	CC  []Address
	BCC []Address

	ReplyTo []Address

	Subject string

	TextBody string
	HTMLBody string

	Headers map[string]string

	Attachments []Attachment
	// CloseAttachments transfers each closable reader to SMTP for this call,
	// including validation/read/send failures. Do not list an owned reader twice.
	// Close errors join the result, even after SMTP acceptance; do not retry blindly.
	// Prefer caller-owned cleanup when the same reader is needed after Send.
	CloseAttachments bool
	Tags             map[string]string
}

// Validate checks required content. Transports and Enqueue also validate the
// formats and limits needed by their delivery mechanism.
func (m Message) Validate() error {
	if len(m.To) == 0 {
		return errors.New("at least one recipient is required")
	}

	if m.Subject == "" {
		return errors.New("subject is required")
	}

	if m.TextBody == "" && m.HTMLBody == "" {
		return errors.New("message body is required")
	}

	return nil
}

// Sender delivers an email immediately. SMTP transports, decorators and test
// senders implement this contract; rendering and queueing are separate operations.
type Sender interface {
	Send(context.Context, Message) error
}

// Mailer preserves the transport's immediate Send contract and adds an explicit
// enqueue operation. Success acknowledges SMTP submission or Redis acceptance,
// respectively; neither proves receipt by the recipient.
type Mailer interface {
	Sender
	Enqueue(context.Context, Message, ...asynq.Option) (*asynq.TaskInfo, error)
}

type mailer struct {
	sender Sender
	queue  asynqx.Client
}

// New borrows a transport and an optional queue without I/O or background work.
// A nil queue permits immediate sends. Instances are safe to share when their
// collaborators are. App registration installs the handler separately.
func New(sender Sender, queue asynqx.Client) (Mailer, error) {
	if sender == nil {
		return nil, ErrTransportRequired
	}
	return &mailer{sender: sender, queue: queue}, nil
}

// Send delegates immediately, preserving attachments and transport errors.
// External effects must run outside SQL transactions. No task is created.
func (m *mailer) Send(ctx context.Context, message Message) error {
	if err := database.RequireNoTransaction(ctx); err != nil {
		return err
	}
	return m.sender.Send(ctx, message)
}

// Enqueue snapshots the rendered message into Redis without sending it. Options
// use native Asynq semantics; defaults allow three retries and a 30-second attempt.
// Repeated attempts can send duplicates after an uncertain SMTP result. Callers
// own cancellation and must not mutate message data during this call.
// This operation rejects SQL transactions: use notification.Enqueue when intent
// must commit atomically with a business change. Attachment readers are rejected.
func (m *mailer) Enqueue(ctx context.Context, message Message, options ...asynq.Option) (*asynq.TaskInfo, error) {
	if err := database.RequireNoTransaction(ctx); err != nil {
		return nil, err
	}
	if m.queue == nil {
		return nil, ErrQueueUnavailable
	}
	if len(message.Attachments) != 0 || message.CloseAttachments {
		return nil, ErrAttachments
	}
	data, err := encode(message)
	if err != nil {
		return nil, err
	}
	task := asynqx.NewTask(SendTask, data, asynq.MaxRetry(3), asynq.Timeout(30*time.Second))
	return m.queue.EnqueueContext(ctx, task, options...)
}
