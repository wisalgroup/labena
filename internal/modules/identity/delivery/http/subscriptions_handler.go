package http

import (
	"context"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/auth"
)

type subscriptionHandler struct {
	notifications Notifications
	auth          service.AuthService
}

func (h *subscriptionHandler) subscribe(ctx context.Context, in *SubscribeRequest) (*response.JSON[SubscriptionResponse], error) {
	input := in.Body
	principal, err := auth.FromContext(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	if h.auth == nil {
		return nil, response.Error(ctx, domain.ErrUnauthenticated)
	}
	sub, err := h.auth.SubscribePush(ctx, principal, input.InstallationID, input.Registration)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.OK(newSubscriptionResponse(sub)), nil
}

func (h *subscriptionHandler) unsubscribe(ctx context.Context, in *NotificationRequest) (*response.Empty, error) {
	input := *in
	r, err := currentRecipient(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	if err = h.notifications.Unsubscribe(ctx, r, uuid.MustParse(input.ID)); err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.NoContent(), nil
}
