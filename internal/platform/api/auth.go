package api

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/internal/platform/api/middleware"
	"github.com/wisalgroup/labena/internal/platform/auth"
)

// Authenticated keeps bearer enforcement and its OpenAPI security declaration together.
func Authenticated(a huma.API, authenticator auth.Authenticator) huma.API {
	group := huma.NewGroup(a)
	group.UseSimpleModifier(func(op *huma.Operation) { op.Security = []map[string][]string{{"Bearer": {}}} })
	group.UseMiddleware(middleware.Bearer(authenticator))
	return group
}
