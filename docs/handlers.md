# HTTP handlers

A handler translates a validated HTTP request into a service call, then selects
public response fields. Huma supplies typed inputs and outputs; business services
receive ordinary `context.Context` and service-owned inputs.

The usual flow is short:

```text
Bound request → trusted caller context → authorization → service → public response
```

Start with the [Catalog route](routing.md#register-a-route) for a database-free
example. The profile update below shows the same pattern with a real service.

## Register and handle once

Identity keeps its handler private in
`internal/modules/identity/delivery/http/user_handler.go`. This is its profile-update flow:

```go
func (h *userHandler) updateProfile(ctx context.Context, req *UpdateProfileRequest) (*response.JSON[UserResponse], error) {
    id, err := auth.CurrentUserID(ctx)
    if err != nil {
        return nil, response.Error(ctx, err)
    }
    user, err := h.svc.UpdateProfile(ctx, id, req.Input())
    if err != nil {
        return nil, response.Error(ctx, err)
    }
    return response.OK(newUserResponse(user)), nil
}
```

Imports are `context`, `github.com/wisalgroup/labena/internal/platform/auth`, and
`github.com/wisalgroup/labena/internal/platform/api/response`. The receiver holds Identity's existing
`service.UserService`; `UpdateProfileRequest` and `UserResponse` belong to the same
HTTP package. See [request mapping](requests.md#map-to-a-service-input) for `Input`.

Registration in the same package uses the borrowed protected API:

```go
huma.Register(protected, huma.Operation{
    OperationID:  "identity.updateProfile",
    Method:       "PATCH",
    Path:         "/users/me",
    Summary:      "Update your profile",
    MaxBodyBytes: 4096,
}, handler.updateProfile)
```

This describes the existing Identity route; do not register another copy. A request
with `{"name":"Ada"}` updates the signed-in account and returns its public fields
under `data`. The account ID comes from the principal, never the request body.

## Input and output

A typed handler has this shape:

```go
func(ctx context.Context, input *Input) (*Output, error)
```

| Part | Responsibility |
| --- | --- |
| `Input` | HTTP sources, JSON shape, Huma validation and documentation tags |
| `Input.Input(...)` | Straightforward conversion into a service input, where useful |
| Handler | Trusted caller context, transport-specific authorization and response conversion |
| Service | Business decisions and validation shared by HTTP, jobs, events, and CLI |
| `Output` | Public response body, headers, and status |

Use `struct{ Body T }` for a JSON-only request. Use a named input when it has several
sources or a mapping method. Return `response.JSON[T]` for the shared success
envelope. Use `response.Empty` for no body and native Huma output types for streams
or special headers. See [responses](responses.md).

Keep request and response types in the transport package. Returning a Bun entity
directly can expose fields unrelated to the endpoint, including future additions.
An explicit conversion makes that public boundary reviewable.

## Authentication and policy

Protected routes use `targets.AuthenticatedHTTP`, built with `api.Authenticated`.
It combines bearer validation with its OpenAPI security declaration. Inside the
handler:

| Helper | Result |
| --- | --- |
| `auth.FromContext(ctx)` | Authenticated user and session identifiers |
| `auth.CurrentUserID(ctx)` | Account ID, without loading the account |
| `auth.RequireCurrentUser(ctx, id)` | Requires the supplied ID to match the caller; mismatch returns not found |

For a self-service `/users/{id}` route, Identity checks ownership before reading:

```go
id := uuid.MustParse(req.ID) // Huma already validated format:"uuid".
if err := auth.RequireCurrentUser(ctx, id); err != nil {
    return nil, response.Error(ctx, err)
}
user, err := h.svc.Get(ctx, id)
if err != nil {
    return nil, response.Error(ctx, err)
}
return response.OK(newUserResponse(user)), nil
```

This fragment uses `github.com/google/uuid` and Identity's existing handler types.
A validated UUID is only a well-formed identifier. For shared documents, teams,
roles, or other permissions, implement the actual policy in the owning service;
`RequireCurrentUser` is specifically a self-service account check.

Pass trusted values explicitly to services. Do not accept `owner_id`, session IDs,
or privileged flags from a request when they are determined by authentication.

## Keep context and deadlines

Pass the handler's `ctx` into every service, database, storage, and provider call.
It carries cancellation, tracing, and the context logger. Do not replace it with
`context.Background()` to keep work running after the request.

If a call has a shorter deadline than its caller, derive a child context:

```go
lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
defer cancel()
user, err := h.svc.Get(lookupCtx, id)
```

This fragment imports `time` and belongs before the usual error/response mapping.
For work intended to outlive the request, enqueue a task and return a documented
acceptance result. Cancellation does not undo a committed database transaction or
an accepted task. See [transactions](database-transactions.md) and [queues](queues.md).

## Pagination, uploads and streaming

These are native Huma inputs and outputs with a few shared helpers. Use the
smallest shape appropriate to the operation.

### Return a page

Embed `api.PaginationInput`, pass `input.Pagination()` to the service, and convert
only the returned items. The application type remains `pagination.Pagination`.

```go
type ListProductsRequest struct {
    api.PaginationInput
    Search string `query:"search" maxLength:"100"`
}
```

A service returns `*pagination.Page[Product]`; the handler should expose its public
`ProductResponse` shape while preserving page metadata. See
[page responses](responses.md#return-a-page) and [database pagination](database-queries.md#return-a-page).

### Receive an upload

Use `RawBody multipart.Form` and `middleware.Multipart` from
`internal/platform/api/middleware` on the operation. Set an explicit
positive `MaxBodyBytes` for the **whole request**, not just the file contents.
Authentication should run before multipart parsing.

The handler must still validate permitted parts, open the selected file, and close
it. The middleware bounds parsing, spools large files, and removes temporary files
after return or panic. See the [multipart example](requests.md#multipart-bodies).

### Stream a response

Native Huma outputs can supply `Body func(huma.Context)`. This helper is a complete
pattern for an already-authorized, already-opened download reader:

```go
// Imports: context, io, net/http, Huma, and platform/api/response.
type downloadOutput struct {
    Body func(huma.Context)
}

func streamDownload(ctx context.Context, reader io.ReadCloser) *downloadOutput {
    return &downloadOutput{Body: func(c huma.Context) {
        defer func() { response.LogError(ctx, reader.Close()) }()
        c.SetHeader("Content-Type", "application/octet-stream")
        c.SetHeader("Content-Disposition", "attachment")
        c.SetHeader("X-Content-Type-Options", "nosniff")
        c.SetHeader("Cache-Control", "no-store")
        if _, err := io.Copy(c.BodyWriter(), reader); err != nil {
            response.LogError(ctx, err)
            panic(http.ErrAbortHandler)
        }
    }}
}
```

Open the reader using the request context **before** returning this output, so
open/authorization failures can still return `response.Error`. The output callback
owns closing the reader. Declare the binary media type in the operation's
`Responses`; Identity's `registerMedia` in `internal/modules/identity/delivery/http/routes.go`
is the working example.

Once headers or bytes are committed, do not append a JSON problem to the stream.
`http.ErrAbortHandler` tells the server to abort the transport; the shared recovery
preserves that signal. Flush, hijack, and response-controller deadlines remain
available through the underlying HTTP support.

## Independent use

You can construct the platform API without the complete application. This example
belongs inside the repository because `internal/platform/api` is an internal package:

```go
server, err := api.New(api.Options{Title: "Catalog API", Version: "1.0.0"})
if err != nil {
    return err
}
public := huma.NewGroup(server, "/api/v1")
protected := api.Authenticated(public, authenticator)
// Register your operations on public or protected before serving.
```

`authenticator` implements `auth.Authenticator`. A nil authenticator fails closed on
protected routes. `server` is an `http.Handler`: pass it to `pkg/httpserver` or a
standard HTTP server whose listener and graceful shutdown you own. Infrastructure
handlers mount on `server.Mux`, but serve the server object itself.

## Test a handler through HTTP

Use a freshly constructed API and `httptest` to exercise the route, binding,
policies, metadata, and response together. The following assertions fit a test
that already registered its Catalog route on `server`:

```go
request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/hello/Ada", nil)
result := httptest.NewRecorder()
server.ServeHTTP(result, request)
if result.Code != http.StatusOK {
    t.Fatalf("status = %d; body = %s", result.Code, result.Body.String())
}
if result.Header().Get("X-Request-ID") == "" {
    t.Fatal("missing request ID")
}
```

Imports are `net/http` and `net/http/httptest`; `t` is the test's `*testing.T`.
Use the Catalog tests in `tests/modules` as a complete infrastructure-free starting
point. Keep business behavior tests in `tests/<module>`, including authorization,
invalid input, and failure cases. Calling a handler directly bypasses Huma binding
and must not be treated as validation coverage.

Continue with [requests](requests.md), [responses](responses.md), and
[error handling](errors.md).
