// Package auth defines authenticated identity without token implementations or transport dependencies.
package auth

import (
	"context"

	"github.com/google/uuid"
)

// Principal identifies the authenticated user and active session.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

// Authenticator verifies a credential and returns the authenticated user/session.
// Implementations must honor cancellation and return errors for invalid credentials
// or unavailable authentication infrastructure.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (Principal, error)
}
