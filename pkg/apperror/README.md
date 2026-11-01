# Application errors

`apperror` describes safe failures without depending on HTTP, gRPC, or a broker.
Use a typed kind for classification and optional public code, type, title and detail. Validation errors may also contain field
messages. Keep diagnostic causes in wrapped or joined errors, not public messages.

```go
var ErrEmailTaken = apperror.Conflict("This email address is already registered.")

return apperror.InvalidFields(map[string][]string{
    "email": {"Use your work email.", "This address is not eligible."},
    "profile.name": {"Choose a display name."},
})
```

Use `errors.Is(err, ErrEmailTaken)` for sentinel identity and `errors.As` to inspect
an application error through wrapping. Two separately constructed errors with the
same code are not the same sentinel. Fields are copied on input and output so shared
errors cannot be mutated by callers. Do not compare message text.

Transport adapters own translation. The application's HTTP adapter maps:

| Kind | HTTP status |
| --- | --- |
| `invalid_input` | 400 |
| `validation_failed` | 422 |
| `unauthenticated` | 401 |
| `forbidden` | 403 |
| `not_found` | 404 |
| `conflict` | 409 |
| `unavailable` | 503 |
| `rate_limited` | 429 |
| `internal_error`, unknown, or empty | 500 with a generic message |

At the HTTP boundary, return `response.Error(ctx, err)` from the handler. Each field
message becomes a Huma-style detail in the problem's `errors` array. Bare field
names become body locations (`email` → `body.email`); nested dot/index paths are
preserved, and already qualified HTTP locations such as `query.page` remain as
supplied. Keys are sorted and message order is retained. Aggregate validation
messages in one `InvalidFields` call, rather than joining separate field errors.
See [HTTP responses](../../docs/responses.md) for the complete wire shape.

Request-body size errors remain HTTP-specific: 413 with `request_too_large`.
Rate-limit timing and Retry-After headers belong to the rate limiter/HTTP boundary;
a classification alone does not implement throttling. Future transports must supply
their own mapping and must not expose internal or unknown error messages.

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
