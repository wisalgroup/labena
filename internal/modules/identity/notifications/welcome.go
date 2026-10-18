// Package notifications defines Identity's named notifications and channel content.
package notifications

import (
	"encoding/json"

	"github.com/wisalgroup/labena/internal/modules/identity/mailviews"
	"github.com/wisalgroup/labena/internal/platform/notification"
)

// NewWelcome describes one occurrence per account. Its stable key lets duplicate
// events, jobs and operator replays converge on the same per-channel intents.
// The caller selects eligible channels before enqueueing (for example, omitting
// email when the address is unverified or no mailer is provided).
func NewWelcome(name string) notification.Message {
	email := mailviews.NewWelcome(name)
	return notification.Message{
		Key:      "welcome",
		Type:     "identity.welcome",
		Database: json.RawMessage(`{"title":"Welcome!","body":"Your account is ready."}`),
		Email:    &email,
	}
}
