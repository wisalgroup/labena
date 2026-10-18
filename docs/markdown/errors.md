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
    code := failure.Code()
    _ = code // Classify without matching human-readable strings.
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
