package http

import (
	"context"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/auth"
)

type userHandler struct{ svc service.UserService }

func newUserHandler(svc service.UserService) *userHandler { return &userHandler{svc: svc} }

func (h *userHandler) getUser(ctx context.Context, req *GetUserRequest) (*response.JSON[UserResponse], error) {
	id := uuid.MustParse(req.ID)
	if err := auth.RequireCurrentUser(ctx, id); err != nil {
		return nil, response.Error(ctx, err)
	}
	user, err := h.svc.Get(ctx, id)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.OK(newUserResponse(user)), nil
}

func (h *userHandler) currentUser(ctx context.Context, req *struct{}) (*response.JSON[UserResponse], error) {
	id, err := auth.CurrentUserID(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	user, err := h.svc.Get(ctx, id)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.OK(newUserResponse(user)), nil
}

func (h *userHandler) updateProfile(ctx context.Context, req *UpdateProfileRequest) (*response.JSON[UserResponse], error) {
	id, err := auth.CurrentUserID(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	user, err := h.svc.UpdateProfile(ctx, id, req.Input())
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.OK(newUserResponse(user)), nil
}
