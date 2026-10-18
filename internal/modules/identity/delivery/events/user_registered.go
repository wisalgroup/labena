// Package events reacts to Identity events using borrowed delivery clients.
package events

import (
	"encoding/json"
	"errors"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/jobs"
	sharedevents "github.com/wisalgroup/labena/internal/modules/shared/events"
	"github.com/wisalgroup/labena/pkg/asynqx"
)

// UserRegistered queues onboarding work after account creation. GoChannel is local
// and best-effort; Redis acceptance cannot recover an event lost before this handler.
type UserRegistered struct{ Queue asynqx.Client }

func (h UserRegistered) Handle(msg *message.Message) error {
	var event sharedevents.UserRegistered
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return errors.New("identity: invalid user registration event")
	}
	if h.Queue == nil {
		return domain.ErrQueueRequired
	}
	task, err := jobs.NewWelcome(event.UserID)
	if err != nil {
		return err
	}
	_, err = h.Queue.EnqueueContext(msg.Context(), task)
	return err
}
