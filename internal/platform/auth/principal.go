// Package auth defines authenticated identity without tokens or transport dependencies.
package auth

import "github.com/google/uuid"

// Principal identifies the authenticated user and active session.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}
