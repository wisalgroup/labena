package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	netmail "net/mail"
	"strings"

	"github.com/hibiken/asynq"
)

// SendTask is the persisted task contract. Change its version when changing the
// payload incompatibly; workers must understand all pending task versions.
const SendTask = "mail.send.v1"

// MaxPayloadSize bounds the complete serialized message, including addresses.
const MaxPayloadSize = 64 << 10

// queuedMessage contains values only. SMTP credentials and attachment readers
// never enter the queue. The explicit shape decouples it from transport changes.
type queuedMessage struct {
	From     *Address          `json:"from,omitempty"`
	To       []Address         `json:"to"`
	CC       []Address         `json:"cc,omitempty"`
	BCC      []Address         `json:"bcc,omitempty"`
	ReplyTo  []Address         `json:"reply_to,omitempty"`
	Subject  string            `json:"subject"`
	TextBody string            `json:"text_body,omitempty"`
	HTMLBody string            `json:"html_body,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Tags     map[string]string `json:"tags,omitempty"`
}

func (p queuedMessage) message() Message {
	return Message{From: p.From, To: p.To, CC: p.CC, BCC: p.BCC,
		ReplyTo: p.ReplyTo, Subject: p.Subject, TextBody: p.TextBody,
		HTMLBody: p.HTMLBody, Headers: p.Headers, Tags: p.Tags}
}

func encode(message Message) ([]byte, error) {
	if err := validate(message); err != nil {
		return nil, err
	}
	data, err := json.Marshal(queuedMessage{From: message.From, To: message.To,
		CC: message.CC, BCC: message.BCC, ReplyTo: message.ReplyTo,
		Subject: message.Subject, TextBody: message.TextBody, HTMLBody: message.HTMLBody,
		Headers: message.Headers, Tags: message.Tags})
	if err != nil {
		return nil, ErrInvalidMessage
	}
	if len(data) > MaxPayloadSize {
		return nil, ErrMessageTooLarge
	}
	return data, nil
}

func validate(message Message) error {
	if message.Validate() != nil || strings.ContainsAny(message.Subject, "\r\n") {
		return ErrInvalidMessage
	}
	if len(message.TextBody) > MaxPayloadSize || len(message.HTMLBody) > MaxPayloadSize {
		return ErrMessageTooLarge
	}
	validAddress := func(address Address) bool {
		parsed, err := netmail.ParseAddress(address.Email)
		return err == nil && parsed.Address == address.Email && !strings.ContainsAny(address.Name, "\r\n")
	}
	if message.From != nil && !validAddress(*message.From) {
		return ErrInvalidMessage
	}
	for _, addresses := range [][]Address{message.To, message.CC, message.BCC, message.ReplyTo} {
		for _, address := range addresses {
			if !validAddress(address) {
				return ErrInvalidMessage
			}
		}
	}
	for name, value := range message.Headers {
		if name == "" || strings.ContainsAny(value, "\r\n") || strings.ContainsFunc(name, func(r rune) bool { return r < 33 || r > 126 || r == ':' }) {
			return ErrInvalidMessage
		}
	}
	return nil
}

// NewHandler borrows an immediate transport for use with a native Asynq mux.
// It does not enqueue recursively or start a worker. Malformed tasks skip retries;
// transport failures remain retryable and retain their cause for error policies.
func NewHandler(sender Sender) (asynq.Handler, error) {
	if sender == nil {
		return nil, ErrTransportRequired
	}
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		if task == nil || task.Type() != SendTask || len(task.Payload()) > MaxPayloadSize {
			return errors.Join(ErrInvalidMessage, asynq.SkipRetry)
		}
		var payload *queuedMessage
		decoder := json.NewDecoder(bytes.NewReader(task.Payload()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil || payload == nil {
			return errors.Join(ErrInvalidMessage, asynq.SkipRetry)
		}
		if err := decoder.Decode(new(any)); err != io.EOF || validate(payload.message()) != nil {
			return errors.Join(ErrInvalidMessage, asynq.SkipRetry)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sender.Send(ctx, payload.message()); err != nil {
			return deliveryError{cause: err}
		}
		return nil
	}), nil
}

// Asynq persists error text. Preserve the cause without recording provider
// responses, which may contain recipient addresses or message content.
type deliveryError struct{ cause error }

func (e deliveryError) Error() string { return "mail: delivery failed" }
func (e deliveryError) Unwrap() error { return e.cause }
