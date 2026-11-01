package middleware

import (
	"context"
	"errors"
	"net/http"
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
		principal, err := authenticateBearer(c.Context(), values, authenticator)
		if err != nil {
			if unauthenticated(err) {
				c.SetHeader("WWW-Authenticate", "Bearer")
			}
			response.WriteError(c, err)
			return
		}
		next(huma.WithContext(c, auth.WithPrincipal(c.Context(), principal)))
	}
}

// BearerHTTP applies the same bearer policy to native HTTP handlers, including
// WebSocket handshakes. Nil authentication fails closed. It does not accept tokens
// from query parameters or cookies; browser applications need an explicit trusted
// session/ticket adapter because browser WebSocket APIs cannot set Authorization.
func BearerHTTP(authenticator auth.Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			principal, err := authenticateBearer(r.Context(), r.Header.Values("Authorization"), authenticator)
			if err != nil {
				if unauthenticated(err) {
					w.Header().Set("WWW-Authenticate", "Bearer")
				}
				response.WriteHTTPError(r.Context(), w, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
		})
	}
}

func authenticateBearer(ctx context.Context, values []string, authenticator auth.Authenticator) (auth.Principal, error) {
	denied := func() (auth.Principal, error) {
		return auth.Principal{}, apperror.Unauthenticated("Authentication is required.")
	}
	if authenticator == nil || len(values) != 1 || len(values[0]) > 4103 {
		return denied()
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return denied()
	}
	principal, err := authenticator.Authenticate(ctx, parts[1])
	if err != nil {
		return auth.Principal{}, err
	}
	if principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
		return denied()
	}
	return principal, nil
}

func unauthenticated(err error) bool {
	var public *apperror.Error
	return errors.As(err, &public) && public != nil && public.Kind() == apperror.KindUnauthenticated
}
