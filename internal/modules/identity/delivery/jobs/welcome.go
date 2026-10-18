// Package jobs handles Identity's native Asynq tasks.
package jobs

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/wisalgroup/labena/internal/modules/shared/entities"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	identityjobs "github.com/wisalgroup/labena/internal/modules/identity/jobs"
	"github.com/wisalgroup/labena/internal/modules/identity/notifications"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/notification"
)

// Welcome reloads current account state and persists channel intents. Completion
// means notification intent is saved, not that email has reached the recipient.
// Platform notification workers own actual channel delivery and independent retries.
type Welcome struct {
	Users        service.UserService
	Notifier     *notification.Notifier
	EmailEnabled bool
}

func (h Welcome) Handle(ctx context.Context, task *asynq.Task) error {
	var input identityjobs.Welcome
	if task == nil {
		return errors.Join(errors.New("identity: missing welcome job"), asynq.SkipRetry)
	}
	if err := json.Unmarshal(task.Payload(), &input); err != nil || input.UserID == uuid.Nil {
		return errors.Join(errors.New("identity: invalid welcome job"), asynq.SkipRetry)
	}
	user, err := h.Users.Get(ctx, input.UserID)
	if errors.Is(err, domain.ErrUserNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if user == nil {
		return domain.ErrUserRequired
	}
	if user.DeletedAt != nil || user.Status != enums.UserActive {
		return nil
	}
	content := notifications.NewWelcome(user.Name)
	if !h.EmailEnabled || user.EmailVerifiedAt == nil {
		content.Email = nil
	}
	recipient := notification.Recipient{Type: string(entities.UserEntityType), ID: user.ID.String(), Email: user.Email.String()}
	_, err = h.Notifier.Enqueue(ctx, recipient, content)
	return err
}
