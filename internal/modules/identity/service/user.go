package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/pagination"
)

// userService borrows its repository and owns profile/account operations.
type userService struct{ repo repository.UserRepository }

// NewUserService borrows its repository without I/O or background work.
func NewUserService(repo repository.UserRepository) (UserService, error) {
	if repo == nil {
		return nil, domain.ErrDependenciesRequired
	}
	return &userService{repo: repo}, nil
}

func (s *userService) Get(ctx context.Context, id uuid.UUID) (*entities.User, error) {
	return s.repo.Get(ctx, id)
}

func (s *userService) List(ctx context.Context, p pagination.Pagination) (*pagination.Page[*entities.User], error) {
	p = pagination.NewPagination(p.Page, p.PerPage)
	users, total, err := s.repo.List(ctx, p)
	if err != nil {
		return nil, err
	}

	return pagination.NewPage(users, p, total), nil
}

// ChangeStatus is an internal account operation, not a public route. Callers must
// authorize it. The repository revokes sessions atomically when account access is disabled.
func (s *userService) ChangeStatus(ctx context.Context, id uuid.UUID, status enums.UserStatus) (*entities.User, error) {
	return s.repo.Edit(ctx, id, func(user *entities.User) error {
		return user.ChangeStatus(status, time.Now().UTC())
	})
}

func (s *userService) UpdateProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*entities.User, error) {
	if input.Name == nil {
		return nil, apperror.Validation("Provide at least one profile field.")
	}
	return s.repo.Edit(ctx, userID, func(user *entities.User) error { return user.Rename(*input.Name) })
}
