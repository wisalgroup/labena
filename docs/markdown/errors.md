# Error handling

Return errors from the operation that knows what failed. Preserve their identity
through intermediate layers, then translate them once at the delivery boundary.
Business services use ordinary Go errors and `pkg/apperror`; they do not import HTTP.

## Choose an error kind

| Situation | Return | Caller behavior |
| --- | --- | --- |
| A public, expected business failure | An owning `apperror` value | Show its safe message and classify it |
| Several invalid fields | `apperror.InvalidFields` | Present all field messages |
| Database, storage, or provider failure | An error wrapped with `%w` | Preserve the cause; avoid exposing raw details |
| Context canceled or deadline expired | Preserve the original cause | Stop work; assess whether effects already happened |
| Invalid static programmer setup | Follow the API's documented constructor error or panic contract | Fix construction or registration |

Use the same classification for HTTP, CLI, and background callers. The transport
chooses how to present it.

## Define errors beside their owner

For a new Catalog capability, `internal/modules/catalog/domain/errors.go` could contain:

```go
package domain

import "github.com/wisalgroup/labena/pkg/apperror"

var (
	ErrProductNotFound = apperror.NotFound("The product was not found.")
	ErrProductArchived = apperror.Conflict("Archived products cannot be changed.")
)
```

These are illustrative additions, not shipped Catalog errors. The package-level
values provide stable identity for callers using `errors.Is`. An entity invariant
may instead own its error beside the entity; a platform capability owns its errors
in that capability. Do not create a global application error catalog.

`apperror.Error` keeps its fields private and copies validation maps, so shared
sentinels cannot be mutated accidentally. Its code and message are explicitly
public. Never put submitted secrets, SQL text, credentials, or raw provider
responses into an application message.

## Customize public errors

Use `Kind` for transport classification and `Code` for a public identifier.
The kind determines HTTP status; changing code, type or title never changes it.
Generic constructors need only a safe detail, which can be empty:

```go
return apperror.NotFound("")
return apperror.NotFound("The user does not exist.")
```

For a reusable domain definition in `internal/modules/identity/domain/errors.go`:

```go
var ErrUserNotFound = apperror.Define(apperror.Definition{
    Kind:  apperror.KindNotFound,
    Code:  "USER_NOT_FOUND",
    Type:  "https://api.example.com/problems/user-not-found",
    Title: "User Not Found",
})
```

The equivalent fluent definition is:

```go
var ErrUserNotFound = apperror.NotFound("").
    WithCode("USER_NOT_FOUND").
    WithType("https://api.example.com/problems/user-not-found").
    WithTitle("User Not Found")
```

These are alternatives; choose one definition. `Define` requires all four fields
and a known kind. Incomplete definitions and invalid nonempty type URIs panic as
static programmer mistakes. Fluent methods allow individual overrides in any order.
Use stable developer-defined codes, titles and absolute type URIs, never secrets
or occurrence identifiers. A URN such as `urn:myapp:error:user-not-found` also works.

A service returns the shared sentinel or adds safe occurrence detail:

```go
return domain.ErrUserNotFound
return domain.ErrUserNotFound.WithDetail("User 123 does not exist")
```

The HTTP handler continues to return `response.Error(ctx, err)`. The second
service return produces HTTP 404 with `application/problem+json`:

```json
{
  "type": "https://api.example.com/problems/user-not-found",
  "title": "User Not Found",
  "status": 404,
  "detail": "User 123 does not exist",
  "instance": "urn:uuid:8f3a4d62-8c73-4e9d-96a2-32bf970e765f",
  "code": "USER_NOT_FOUND"
}
```

Returning the sentinel directly produces the same shape without `detail`.
The UUID above is illustrative: the HTTP boundary uses the server-generated
`X-Request-ID` UUID from context, or generates a UUID if no valid nonzero request
UUID exists. Domain errors never store request instances.

### Defaults and partial overrides

All examples below return status 404. Each also receives a fresh request instance.

| Error expression | code | type | title | detail |
| --- | --- | --- | --- | --- |
| `NotFound("")` | `not_found` | `urn:labena:error:not_found` | Not Found | omitted |
| `NotFound("Missing user.")` | `not_found` | `urn:labena:error:not_found` | Not Found | Missing user. |
| `NotFound("").WithCode("USER_NOT_FOUND")` | `USER_NOT_FOUND` | `urn:labena:error:not_found` | Not Found | omitted |
| `NotFound("").WithType("urn:myapp:user-missing")` | `not_found` | `urn:myapp:user-missing` | Not Found | omitted |
| `NotFound("").WithTitle("User Not Found")` | `not_found` | `urn:labena:error:not_found` | User Not Found | omitted |

Expressions omit the `apperror.` prefix for readability. The default type derives
from **kind**, not an overridden code. Empty `WithCode("")`, `WithType("")` and
`WithTitle("")` restore their individual defaults. `WithDetail("")` omits detail.
`WithProblemCode` is unnecessary: use the consistent `WithCode` name.

All `With` methods return immutable copies and preserve `errors.Is` identity
with the original. They are safe to call concurrently on a shared definition.
`errors.As` finds the outermost decorated error; `Kind()` classifies it,
`Code()` reads its public identifier, and `Detail()` reads its description.
`Message()` remains an alias for detail for existing callers.

### Validation and internal failures

```go
return apperror.Validation("").
    WithCode("INVALID_PROFILE").
    WithFields(map[string][]string{"email": {"Enter a valid email."}})
```

This produces HTTP 422, code `INVALID_PROFILE`, type
`urn:labena:error:validation_failed`, title `Unprocessable Entity`, no detail,
and `errors: [{"location":"body.email","message":"Enter a valid email."}]`.
`WithFields` copies its map and slices; it panics for non-validation kinds.
`InvalidFields(fields)` remains the shortcut with detail
`The given data was invalid.`.

Internal and unknown kinds, and ordinary Go errors, always produce safe HTTP 500
responses with code `internal_error`, type `urn:labena:error:internal_error`,
title `Internal Server Error`, and detail `An unexpected error occurred.`.
Custom metadata is redacted. Preserve diagnostic causes with `%w`; never copy
database or provider messages into public details.

Native Huma status errors retain their status and headers. Their public code/type
are derived from HTTP status, title is HTTP status text, and instance is generated
at the same boundary. Native 4xx detail is omitted and validation diagnostics are
sanitized; native 5xx detail is generic. Application definitions belong in their
owner; use native errors for HTTP-specific behavior such as protocol headers.

## Preserve error identity

Wrap an operational failure with meaningful context:

```go
if err := s.products.Create(ctx, product); err != nil {
    return fmt.Errorf("create catalog product: %w", err)
}
```

This service fragment imports `fmt` and uses its injected repository. `%w` keeps
the underlying error available. Callers inspect it with `errors.Is` or `errors.As`:

```go
if errors.Is(err, domain.ErrProductNotFound) {
    // Apply the caller's explicit missing-product behavior.
}

var failure *apperror.Error
if errors.As(err, &failure) {
    kind := failure.Kind()
    _ = kind // Classify without matching human-readable strings.
}
```

Do not wrap an operational error by copying its text into `apperror.Validation`.
That would expose private diagnostics and incorrectly turn a server failure into
a client mistake. Unknown errors become a generic 500 at the HTTP boundary.

## Return several validation messages

Collect independent business validation results into one value:

```go
fields := map[string][]string{}
if name == "" {
    fields["name"] = append(fields["name"], "Choose a display name.")
}
if !eligible {
    fields["email"] = append(fields["email"], "This address is not eligible.")
}
if len(fields) != 0 {
    return apperror.InvalidFields(fields)
}
```

This is a service fragment: `name` and `eligible` are values determined by the
operation. Keys name public inputs; values are safe messages. Use dot paths for
nested fields. HTTP adds the `body.` source prefix to ordinary keys.

`errors.Join` combines independent error causes, often during cleanup. It is not
the field-validation collection API. `InvalidFields` preserves every message and
produces predictable ordering at the HTTP boundary.

## Translate at the boundary

A typed HTTP handler returns:

```go
if err != nil {
    return nil, response.Error(ctx, err)
}
return response.OK(result), nil
```

Import `github.com/wisalgroup/labena/internal/platform/api/response`. It recognizes wrapped
application errors, preserves their causes, maps public codes, and hides unknown
operational details. Native Huma status errors can also be returned directly.
See [responses](responses.md#return-an-error) for the RFC 9457 body and metadata.

| Where the failure occurs | API |
| --- | --- |
| Typed Huma handler | Return `nil, response.Error(ctx, err)` |
| Huma middleware before calling `next` | `response.WriteError(ctx, err)`, then return |
| Standard `net/http` handler before commitment | `response.WriteHTTPError(ctx, w, err)`, then return |
| Stream after commitment | `response.LogError(ctx, err)`, then abort the transport |
| CLI or task | Return an error to that execution boundary; apply its retry/output policy |

The error writers translate application failures; they are not a replacement for
arbitrary native Huma response/header handling. Do not attempt a second response
after headers or body bytes are committed.

## Log at the responsible boundary

Avoid logging the same error in the repository, service, and handler. The API
response boundary records operational failures with bounded `error_category` and
safe `error_code` fields. Its request logger records method, route, status,
duration, and correlation separately.

Use [the context logger](logging.md) when your operation owns a meaningful event or
handles a failure without returning it. Prefer stable fields over arbitrary error
text. Provider/library errors can contain secrets even when the public response is
safe. Background retry attempts and final delivery failures have different logging
owners; see [queues](queues.md) and [notifications](notifications.html).

## Cancellation and uncertain outcomes

Propagate the caller's context and preserve cancellation causes:

```go
if err := ctx.Err(); err != nil {
    return err
}
```

Cancellation tells work to stop. It does not prove a transaction failed to commit,
a provider rejected a message, or a task was never accepted. Before retrying,
determine whether the operation is idempotent and whether its durable result can
be inspected. See [transaction outcomes](database-transactions.md#deadlines-and-uncertain-commits).

## Panics and streaming failures

Use errors for expected failures. The API recovers programming panics and writes a
safe 500 problem if the response has not started. After commitment it must abort
the transport. A broken stream should explicitly signal `http.ErrAbortHandler`;
recovery preserves this signal so clients can detect incomplete delivery.

Recovery cannot roll back external side effects or repair partial application
state. Test cleanup and transaction behavior around panics where those boundaries
matter. See [streaming](handlers.md#stream-a-response).

## Verify error behavior

Check both the service outcome and its delivery representation:

- `errors.Is` still identifies an owned error after wrapping.
- Missing resources and authorization failures reveal only intended information.
- Multiple application messages appear in the HTTP `errors` list.
- Driver/provider error text and submitted secrets do not appear in responses.
- Cancellation stops dependent work, with no background goroutines left behind.
- Retries respect the operation's commit and delivery guarantees.

Business tests belong in `tests/<module>`; reusable error-model and platform tests
may live beside their owners. Continue with [responses](responses.md),
[logging](logging.md), and [observability](observability.md).
