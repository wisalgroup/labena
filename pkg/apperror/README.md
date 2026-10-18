# Application errors

`apperror` describes safe failures without depending on HTTP, gRPC, or a broker.
Use one typed code and a public message. Validation errors may also contain field
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

| Code | HTTP status |
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

## Migration from Kind and a separate code

Use `Conflict(message)` instead of `Conflict("identity.email_taken", message)`
or `New(KindConflict, "identity.email_taken", message)`. Use `Code()` instead of `Kind()`.
Message-only constructors are available for every code: `Internal`, `InvalidInput`,
`Validation`, `Unauthenticated`, `Forbidden`, `NotFound`, `Conflict`, `Unavailable`,
and `RateLimited`. `Validation(message)` supplies a general validation message;
`InvalidFields(fields)` includes field-level details. `New(code, message)` remains
the low-level constructor; application callers should prefer the named shortcuts.

This changes the public JSON error code: `identity.email_taken` becomes `conflict`,
`identity.user_not_found` becomes `not_found`, and malformed JSON/query errors become
`invalid_input`. Validation retains `validation_failed` and its field-message arrays.
Clients should branch on these codes and use field details for validation feedback.
Domain-specific sentinel identity remains available to Go callers.
