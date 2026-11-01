// Package middleware provides reusable HTTP policies for Huma operations.
// Register policies on the API, a route group or an operation before serving.
package middleware

import "github.com/danielgtaylor/huma/v2"

// NoStore prevents caches from storing responses, including downstream rejections.
// Register it before middleware that may reject requests.
func NoStore(ctx huma.Context, next func(huma.Context)) {
	noStore(ctx)
	next(ctx)
}

func noStore(ctx huma.Context) {
	ctx.SetHeader("Cache-Control", "no-store")
}
