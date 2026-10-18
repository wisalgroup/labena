// Package profile demonstrates a business change with transactional notification intent.
package profile

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/database"
)

// Update borrows already constructed collaborators sharing the same database.
// The caller must authorize userID before invoking it. No new route is installed.
// Success commits the profile and notification intent together unless this call
// joins an outer transaction, in which case that caller still owns the commit.
func Update(ctx context.Context, transactions *database.TxManager, users service.UserService, notifier *notification.Notifier, userID uuid.UUID, name string) error {
	return transactions.Execute(ctx, func(ctx context.Context) error {
		user, err := users.UpdateProfile(ctx, userID, service.UpdateProfileInput{Name: &name})
		if err != nil {
			return err
		}
		_, err = notifier.Enqueue(ctx, notification.Recipient{
			Type: string(entities.UserEntityType), ID: user.ID.String(),
		}, notification.Message{
			Type:     "identity.profile_updated",
			Database: json.RawMessage(`{"title":"Profile updated","body":"Your profile changes were saved."}`),
		})
		return err
	})
}
