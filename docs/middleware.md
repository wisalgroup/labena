# Middleware

Application middleware uses Huma's `func(huma.Context, func(huma.Context))` shape.
Server-wide policies use standard `func(http.Handler) http.Handler` middleware.

## Request logging is built in

`api.New` installs one request logger around recovery. Completed and aborted
requests produce an `HTTP request` entry with the server, method, route template,
status and duration. Unmatched routes use `unmatched`. The context logger supplies
request/trace correlation. Raw paths, query strings, headers and bodies are excluded.

Responses below 400 use INFO, 4xx WARN and 5xx ERROR. A stream's recorded status is
the status already committed; an aborted transport is marked separately.
Upgrades record handler lifetime rather than individual messages. Redirects and
unmatched routes also pass through the same policies. Logging lives in `api/http.go`. See [logging](logging.md) for output settings.

## Middleware ownership

Reusable HTTP middleware lives in `internal/platform/api/middleware`, with
purpose-named files such as `auth.go`, `cache.go`, `ratelimit.go` and
`multipart.go`. Add shared HTTP policies there; keep module-specific business
checks in the owning module's `delivery/http`. Use the native Huma or standard
`net/http` middleware function signature rather than introducing another interface.

`middleware.BearerHTTP(authenticator)` is the standard `net/http` counterpart
to Huma's `Bearer`, for native routes such as WebSocket handshakes. Both share
credential validation, principal storage and rejection behavior. Native routes
do not automatically inherit middleware from Huma groups.

The API package owns server construction, trusted-proxy resolution and its private
request pipeline. It may import `middleware`; middleware must not import its
parent `api` package. `api.Authenticated` remains the group composition helper
that combines `middleware.Bearer` with the OpenAPI security declaration.
The server's `api.ClientIPKey` and `api.PeerIPKey` can be passed to
`middleware.RateLimit`; trusted proxy configuration remains owned by the server.

Import `github.com/wisalgroup/labena/internal/platform/api/middleware`:

```go
// Apply to sensitive public routes before other middleware that may reject.
signIn := huma.NewGroup(public, "/sign-in")
signIn.UseMiddleware(middleware.NoStore)

// App composition applies the broad rate limit.
public.UseMiddleware(middleware.RateLimit(limiter, limit, api.ClientIPKey))
```

These snippets show registration patterns; register middleware before the affected
operations. `NoStore` sets `Cache-Control: no-store` before calling the next
middleware, so downstream errors receive it too. `Bearer` includes the same
policy even when used directly. Do not disable caching globally merely to cover
sensitive routes: docs, public content and administrative endpoints own their policies.

Principal data and transport-independent helpers live in `internal/platform/auth`:
`Authenticator`, `Principal`, `FromContext`, `CurrentUserID` and
`RequireCurrentUser`. Only trusted authentication code should call
`auth.WithPrincipal`, after credential verification. It copies identity into a
child context without dropping cancellation; it does not authenticate a request.
Lookup rejects missing or incomplete principals. User loading and session/business
rules remain with Identity services.

## Write and scope middleware

A module can add bounded context fields:

```go
func CatalogContext(ctx huma.Context, next func(huma.Context)) {
    request := ctx.Context()
    logger := logging.FromContext(request).With("component", "catalog.http")
    next(huma.WithContext(ctx, logging.WithLogger(request, logger)))
}
```

Imports are Huma and `github.com/wisalgroup/labena/pkg/logging`. Apply a policy once at the intended scope:

```go
// All subsequently registered operations on this API.
public.UseMiddleware(CatalogContext)

// Or only operations on a module-owned group.
catalog := huma.NewGroup(public, "/catalog")
catalog.UseMiddleware(CatalogContext)

// Or use Operation.Middlewares for one operation.
```

These are alternatives, not three layers to install together. Use native Huma
middleware and groups; there is no framework-specific middleware interface.

## Reject a request

Before calling `next`, write a safe failure and return:

```go
response.WriteError(ctx, apperror.Forbidden("This operation is unavailable."))
return
```

`response.WriteError` uses the same problem details and error classification as handler
errors. Typed handlers should instead return `nil, response.Error(ctx, err)` for a service failure. Never write another
response after headers or stream bytes are committed.

## Add a standard HTTP policy

Use `api.Options.Middleware` when a policy must cover infrastructure routes,
redirects, and unmatched paths too. This example sets a static response header:

```go
func serviceHeader(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Service", "catalog")
        next.ServeHTTP(w, r)
    })
}

// During API construction, before registering routes:
server, err := api.New(api.Options{
    Middleware: []func(http.Handler) http.Handler{serviceHeader},
})
```

Import `net/http` and the API package, handle the constructor error, and serve
`server`. The list runs in declaration order on the way in, then unwinds on return.
App already supplies its CORS policy; avoid installing another competing layer.

| Scope | API | Typical purpose |
| --- | --- | --- |
| Every HTTP request | `api.Options.Middleware` | Headers, CORS, proxy-specific policy |
| Business API group | `group.UseMiddleware` | Shared authentication or module context |
| One operation | `huma.Operation.Middlewares` | Multipart parsing or an endpoint-specific check |

## Understand order

The application chain is:

1. Server correlation, access logging, recovery, tracing/metrics and standard middleware (CORS).
2. Huma public API rate limiting.
3. Bearer authentication on protected groups.
4. Operation middleware, including multipart bounds.
5. Huma binding, schema validation, resolvers (including pagination), then the handler.

Authentication and rate-limit rejection happen before body reads. Policies run once;
code after `next(ctx)` runs on return. Use `defer` for cleanup that must also run
when an operation panics or aborts a stream.

`api.Authenticated` combines bearer enforcement and its OpenAPI security declaration.
It fails closed with a nil authenticator, preserves operational authentication errors,
and accepts credentials only from one Authorization header. A trusted principal
lives in `context.Context`; handlers use `auth.FromContext`, `auth.CurrentUserID` or
`auth.RequireCurrentUser`. A principal is not automatic permission to access another user's resource.

Broad limits use the direct connection address, hashed before it becomes a key.
Forwarding headers cannot alter the budget. Identity also has explicit service-level
challenge limits. Configure CORS through `HTTP_CORS_*` and broad limits through
`API_RATE_LIMIT_*`; see the [configuration reference](../deployments/configuration.md).
The API uses bearer headers, not authentication cookies; CSRF middleware is not installed.

## Use the context logger

`huma.WithContext` propagates a derived context through the downstream Huma handler. Derived fields do not mutate the outer
HTTP request or its access logger. Services receive cancellation, tracing and log correlation using the same
standard context. Do not store a resource container or arbitrary dependencies there.

Asynq and Watermill keep their own native middleware contracts: these describe
different execution lifecycles. See [tasks](queue-jobs.md) and [events](events.md).

## Test rejection and propagation

Exercise middleware through an HTTP request to the configured server. Verify that
an early rejection does not call the downstream handler, headers are correct on
both success and failure, and the derived context reaches the service. For
multipart endpoints, confirm authentication rejects an unauthorized request before
file parsing.

Huma middleware only runs for registered operations. To apply a policy to a 404,
health probe, or dashboard, use the standard HTTP layer. Context fields added
inside a Huma group do not retroactively change the outer request access log.

Continue with [request validation](requests.md), [error handling](errors.md), and
[observability](observability.md).
