package middleware

import (
	"errors"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/auth"
	"github.com/wisalgroup/labena/pkg/apperror"
)

// Bearer only accepts the Authorization header. Nil authentication fails closed.
// A database outage remains an operational error, never anonymous access.
func Bearer(authenticator auth.Authenticator) func(huma.Context, func(huma.Context)) {
	return func(c huma.Context, next func(huma.Context)) {
		noStore(c)
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
			if errors.As(err, &public) && public != nil && public.Kind() == apperror.KindUnauthenticated {
				c.SetHeader("WWW-Authenticate", "Bearer")
			}
			response.WriteError(c, err)
			return
		}
		if principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
			denyAuthentication(c)
			return
		}
		next(huma.WithContext(c, auth.WithPrincipal(c.Context(), principal)))
	}
}
func denyAuthentication(c huma.Context) {
	c.SetHeader("WWW-Authenticate", "Bearer")
	response.WriteError(c, apperror.Unauthenticated("Authentication is required."))
}
