// Package service owns Identity account and authentication operations.
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/auth"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/pagination"
)

// UserService manages accounts. Callers must authorize administrative operations.
type UserService interface {
	// UpdateProfile changes only allowlisted profile fields. The caller authorizes userID.
	UpdateProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*entities.User, error)
	Get(ctx context.Context, id uuid.UUID) (*entities.User, error)
	List(ctx context.Context, page pagination.Pagination) (*pagination.Page[*entities.User], error)
	// ChangeStatus atomically revokes sessions when account access is disabled.
	ChangeStatus(ctx context.Context, id uuid.UUID, status enums.UserStatus) (*entities.User, error)
}

// AuthService owns passwordless sign-in, credentials and sessions.
// Disabled sign-in/session operations return
// domain.ErrSessionConfig (Authenticate returns domain.ErrUnauthenticated).
// Unknown providers return domain.ErrSocialProvider.
type AuthService interface {
	PruneVerification(ctx context.Context, limit int) (int64, error)

	RequestSignInCode(ctx context.Context, email, clientIP string) (uuid.UUID, error)
	// SignInWithCode atomically creates a verified account if needed and its session.
	SignInWithCode(ctx context.Context, input SignInInput) (*SignInResult, error)
	// Provider credentials may be presented again until expiry.
	SignInWithProvider(ctx context.Context, provider enums.AuthProvider, credential string, input SocialSignInInput) (*SignInResult, error)

	// Refresh rotates once. Reusing a consumed token revokes its session.
	Refresh(ctx context.Context, token string) (*Tokens, error)
	Authenticate(ctx context.Context, token string) (auth.Principal, error)
	ListSessions(ctx context.Context, userID uuid.UUID, page pagination.Pagination) (*pagination.Page[*entities.Session], error)
	// SubscribePush validates and locks the active session until registration commits.
	SubscribePush(ctx context.Context, principal auth.Principal, installationID, registrationToken string) (*notification.Subscription, error)
	RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error
	RevokeSessions(ctx context.Context, userID uuid.UUID) error
	PruneSessions(ctx context.Context, limit int) (int64, error)
}

// SignInResult contains the authenticated account and credentials after commit.
// Created is true only when this operation created the account. Callers can inspect
// User.ProfileComplete for both new and returning users.
type SignInResult struct {
	*Tokens
	User    *entities.User
	Created bool
}
