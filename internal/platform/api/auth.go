package api

import (
	"context"
	"errors"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/auth"
	"github.com/wisalgroup/labena/pkg/apperror"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (auth.Principal, error)
}

type principalKey struct{}

// Bearer only accepts the Authorization header. Nil authentication fails closed.
// A database outage remains an operational error, never anonymous access.
func Bearer(authenticator Authenticator) func(huma.Context, func(huma.Context)) {
	return func(c huma.Context, next func(huma.Context)) {
		c.SetHeader("Cache-Control", "no-store")
		var values []string
		c.EachHeader(func(name, value string) {
			if strings.EqualFold(name, "Authorization") {
				values = append(values, value)
			}
		})
		if authenticator == nil || len(values) != 1 || len(values[0]) > 4103 {
			denyAuthentication(c)
			return
		}
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			denyAuthentication(c)
			return
		}
		principal, err := authenticator.Authenticate(c.Context(), parts[1])
		if err != nil {
			var public *apperror.Error
			if errors.As(err, &public) && public != nil && public.Code() == apperror.CodeUnauthenticated {
				c.SetHeader("WWW-Authenticate", "Bearer")
			}
			response.WriteError(c, err)
			return
		}
		if principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
			denyAuthentication(c)
			return
		}
		next(huma.WithValue(c, principalKey{}, principal))
	}
}
func denyAuthentication(c huma.Context) {
	c.SetHeader("WWW-Authenticate", "Bearer")
	response.WriteError(c, apperror.Unauthenticated("Authentication is required."))
}
func Principal(ctx context.Context) (auth.Principal, error) {
	if principal, valid := ctx.Value(principalKey{}).(auth.Principal); valid && principal.UserID != uuid.Nil && principal.SessionID != uuid.Nil {
		return principal, nil
	}
	return auth.Principal{}, apperror.Unauthenticated("Authentication is required.")
}

// CurrentUserID returns the authenticated account ID without loading the account.
func CurrentUserID(ctx context.Context) (uuid.UUID, error) {
	principal, err := Principal(ctx)
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

// Authenticated keeps bearer enforcement and its OpenAPI security declaration together.
func Authenticated(a huma.API, authenticator Authenticator) huma.API {
	group := huma.NewGroup(a)
	group.UseSimpleModifier(func(op *huma.Operation) { op.Security = []map[string][]string{{"Bearer": {}}} })
	group.UseMiddleware(Bearer(authenticator))
	return group
}
