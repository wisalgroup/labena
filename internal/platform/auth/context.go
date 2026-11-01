package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/pkg/apperror"
)

type principalKey struct{}

// WithPrincipal returns a child context containing a verified principal by value.
// Only trusted authentication code should call it after verifying credentials.
// It does not verify credentials or load users; FromContext rejects incomplete identities.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// FromContext returns the authenticated principal or an unauthenticated error
// when absent or incomplete. It performs no I/O and never loads a user or session.
func FromContext(ctx context.Context) (Principal, error) {
	if principal, valid := ctx.Value(principalKey{}).(Principal); valid && principal.UserID != uuid.Nil && principal.SessionID != uuid.Nil {
		return principal, nil
	}
	return Principal{}, apperror.Unauthenticated("Authentication is required.")
}

// CurrentUserID returns the authenticated account ID without loading the account.
func CurrentUserID(ctx context.Context) (uuid.UUID, error) {
	principal, err := FromContext(ctx)
	return principal.UserID, err
}

// RequireCurrentUser requires id to match the authenticated account.
// A mismatch returns not found; the check grants no administrative rights.
func RequireCurrentUser(ctx context.Context, id uuid.UUID) error {
	userID, err := CurrentUserID(ctx)
	if err != nil {
		return err
	}
	if userID != id {
		return apperror.NotFound("The resource was not found.")
	}
	return nil
}
