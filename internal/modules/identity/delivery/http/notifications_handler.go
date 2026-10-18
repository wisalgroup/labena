package http

import (
	"context"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/pagination"
)

// Notifications provides recipient-scoped reads and subscription detachment.
type Notifications interface {
	List(ctx context.Context, recipient notification.Recipient, page pagination.Pagination) (*pagination.Page[*notification.Record], error)
	UnreadCount(ctx context.Context, recipient notification.Recipient) (int, error)
	MarkRead(ctx context.Context, recipient notification.Recipient, notificationID uuid.UUID) error
	Unsubscribe(ctx context.Context, recipient notification.Recipient, subscriptionID uuid.UUID) error
}

type notificationHandler struct{ svc Notifications }

func (h *notificationHandler) list(ctx context.Context, in *api.PaginationInput) (*response.JSON[*pagination.Page[NotificationResponse]], error) {
	recipient, err := currentRecipient(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	page := in.Pagination()
	result, err := h.svc.List(ctx, recipient, page)
	if err != nil {
		return nil, response.Error(ctx, err)
	}

	items := make([]NotificationResponse, len(result.Items))
	for i, n := range result.Items {
		items[i] = newNotificationResponse(n)
	}
	return response.OK(pagination.NewPage(items, page, result.Total)), nil
}

func (h *notificationHandler) unreadCount(ctx context.Context, in *struct{}) (*response.JSON[UnreadCountResponse], error) {
	recipient, err := currentRecipient(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	count, err := h.svc.UnreadCount(ctx, recipient)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.OK(UnreadCountResponse{Count: count}), nil
}

func (h *notificationHandler) markRead(ctx context.Context, in *NotificationRequest) (*response.Empty, error) {
	recipient, err := currentRecipient(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	input := *in
	if err := h.svc.MarkRead(ctx, recipient, uuid.MustParse(input.ID)); err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.NoContent(), nil
}

// Default database notification routes serve Identity users. Other models authorize their own
// recipients before calling the Notifier; request data never selects this scope.
func currentRecipient(ctx context.Context) (notification.Recipient, error) {
	id, err := api.CurrentUserID(ctx)
	if err != nil {
		return notification.Recipient{}, err
	}
	return notification.Recipient{Type: string(entities.UserEntityType), ID: id.String()}, nil
}
