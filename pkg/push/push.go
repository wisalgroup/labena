// Package push sends provider messages without a database, queue or application bootstrap.
package push

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Sender is the provider boundary, defined once here and reused by notifications.
type Sender interface {
	Send(ctx context.Context, registrationToken string, message Message) (Ticket, error)
	Receipt(ctx context.Context, ticketID string) (Receipt, error)
}

// Ticket acknowledges provider queue acceptance, not device delivery.
type Ticket struct {
	ID string `json:"id"`
}

// Receipt distinguishes a missing receipt from a completed provider handoff.
// Error describes a definitive delivery failure. A lookup/network failure is
// returned separately by Sender.Receipt and must never trigger a new send.
type Receipt struct {
	Found bool
	Error *Error
}

// Message is an OS notification with optional string data. ExpiresAt prevents
// delayed alerts; zero allows provider defaults. Data should contain references,
// never secrets or authoritative application state.
type Message struct {
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Data      map[string]string `json:"data,omitempty"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
}

var ErrInvalid = errors.New("push: invalid message or registration")

func (m Message) Validate() error {
	if strings.TrimSpace(m.Title) == "" || len(m.Title) > 200 || len(m.Body) > 2000 {
		return ErrInvalid
	}
	for k := range m.Data {
		if k == "" || k == "from" || k == "message_type" || strings.HasPrefix(k, "google.") || strings.HasPrefix(k, "gcm.") {
			return ErrInvalid
		}
	}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > 3500 {
		return ErrInvalid
	}
	return nil
}

// Error is safe to log: it never contains a registration or provider body.
// InvalidDestination allows callers to disable the registration. RetryAfter is a
// lower bound; Permanent prevents automatic retries of invalid requests/config.
type Error struct {
	Code               string
	Permanent          bool
	InvalidDestination bool
	RetryAfter         time.Duration
}

func (e *Error) Error() string { return "push: " + e.Code }
