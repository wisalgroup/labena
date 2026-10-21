# Responses

Application JSON endpoints return `response.JSON[T]` from
`internal/platform/api/response`. The payload stays typed under `data`, with
optional operation-specific `meta`. Request IDs and API versions are response
headers only. Huma owns
serialization and OpenAPI; handlers still register directly with `huma.Register`.

A normal handler constructs a response; it does not write JSON or headers itself.
Huma writes the returned output after the handler finishes. Custom files and streams
can retain their own body format.

## Return a result

```go
// In a handler returning (*response.JSON[UserResponse], error):
return response.OK(result), nil

// Optional metadata is a JSON object. Use serializable values.
return response.OK(result).WithMeta(map[string]any{"source": "cache"}), nil
```

```json
{
  "meta": {"source": "cache"},
  "data": {"id": "<resource-id>", "name": "Ada Example"}
}
```

| Helper | Handler output | HTTP status |
| --- | --- | --- |
| `response.OK(data)` | `*response.JSON[T]` | 200 |
| `response.Created(data)` | `*response.JSON[T]` | 201 |
| `response.Accepted(data)` | `*response.JSON[T]` | 202 |
| `response.NewJSON(data, status)` | `*response.JSON[T]` | Explicit success status permitting a body |
| `response.NoContent()` | `*response.Empty` | 204, with no body |

Use the constructors: Huma reads `JSON.Status` directly, so a literal with an
unset status is invalid. `NewJSON` panics on a non-success status or 204/205;
these are programmer errors. Return failures with `response.Error` instead.
For `NoContent`, omit `Operation.DefaultStatus` or set it to 204.

Set `Operation.DefaultStatus: 201` or `202` for those fixed outcomes. A constructor
runs at request time, so it cannot update the OpenAPI operation defined at startup.
For dynamic success statuses, document each status explicitly; Identity sign-in
returns 200 for existing accounts and 201 for new ones. Its route group shares
the generated success schema between both statuses.

Always select public response fields instead of exposing persistence entities.
`data` is always present, including `null` for a nil payload. `meta` is omitted
when empty. `WithMeta` copies the outer map; nested values remain caller-owned.
Do not modify a response while another goroutine is sending it.

Pagination stays under `data` with its typed `items`, `page`, `per_page`, `total`
and `total_pages`. Extra metadata does not replace that established page contract.
Identity sign-in uses the same token-and-user response for both 201 (new account)
and 200 (existing account). The frontend checks `data.user.profile_complete` after
either status to decide whether to request profile details.

## Declare multiple success statuses

Some operations have more than one successful outcome. Identity's OTP and social
verification return 201 for a new account and 200 for an existing one, using the
same body schema. Its registration group declares the additional status:

```go
signIns := huma.NewGroup(routes)
signIns.UseSimpleModifier(func(op *huma.Operation) {
    created := *op.Responses["200"]
    created.Description = "New account created"
    op.Responses["201"] = &created
})
// Register only operations with that same 200/201 body contract on signIns.
```

This fragment imports Huma and assumes `routes` is the module's API. The handler
chooses `response.Created(result)` or `response.OK(result)` at runtime. The modifier
copies the generated 200 response after Huma has built its typed schema; do not
apply it to bodyless or differently shaped operations.

## Return a page

Return a typed page under `data`, preserving `pagination.Pagination` as the paging
input. This handler fragment assumes `page` is `*pagination.Page[*entities.User]`
and uses Identity's existing public response conversion:

```go
items := make([]UserResponse, len(page.Items))
for i, user := range page.Items {
    items[i] = newUserResponse(user)
}
paging := pagination.NewPagination(page.Page, page.PerPage)
return response.OK(pagination.NewPage(items, paging, page.Total)), nil
```

Import `github.com/wisalgroup/labena/pkg/pagination`. An empty result has this shape:

```json
{
  "data": {
    "items": [], "page": 1, "per_page": 20, "total": 0, "total_pages": 0
  }
}
```

`pagination.NewPage` makes `items` a non-nil slice and calculates `total_pages`.
It does not query the database. See [page requests](requests.md#pagination) and
[page queries](database-queries.md#return-a-page) for the other two steps.

## Response headers

The API server generates a fresh request ID and returns it in `X-Request-ID`.
Incoming request IDs are not trusted. The same ID is attached to request logs;
use `response.RequestID(ctx)` when an HTTP handler needs the correlation ID.
It is not inserted into JSON bodies.

`X-API-Version` defaults to `v1`. Set `api.Options.APIVersion`
when constructing a server for another API version. This setting does not change
route prefixes; keep the Huma group's path consistent with it. `Options.Version`
is the application release shown in OpenAPI and does not change `X-API-Version`.
The API version is scoped to the server, not inferred from arbitrary request paths.

Successes, errors, empty responses and streams receive both headers. This requires
serving the platform server itself, not bypassing it through `server.Mux`.
The default CORS configuration exposes both headers to browser clients.
Preserve that exposure when customizing CORS. Clients should capture `X-Request-ID`
alongside an error; a copied JSON body alone has no correlation ID.

## Return an error

```go
if err != nil {
    return nil, response.Error(ctx, err)
}
```

Services use transport-independent `apperror` errors; see [error ownership and wrapping](errors.md). At the HTTP boundary,
`response.Error(ctx, err)` recognizes wrapped errors, preserves their causes and hides
private operational diagnostics. Huma handles the returned status error.
Use `response.WriteError` to reject an application failure inside Huma middleware
and return without calling `next`. Native Huma status errors and their headers
remain supported when returned from typed handlers.
Standard `net/http` handlers can use `response.WriteHTTPError(ctx, w, err)` before
committing their response. These writers share the same service-error mapping,
safe logging. The API server supplies the response headers and installs Huma's
error-sanitization hook automatically; ordinary handlers need no extra setup.

Both application and Huma validation errors use `application/problem+json` with
flat RFC 9457 fields and optional operation-specific `meta`. There is no outer
`error` or `data` field on a problem. OpenAPI describes this body without
`request_id` or `api_version`, and Huma's global error hooks remain unchanged.

## Multiple validation messages

Collect application validation messages in the existing `apperror.InvalidFields`:

```go
// In a service; no Huma or response imports are needed.
return apperror.InvalidFields(map[string][]string{
    "email": {"Use your work email.", "This address is not eligible."},
    "profile.name": {"Choose a display name."},
})
```

The handler returns `nil, response.Error(ctx, err)`. Its response is:

```json
{
  "type": "urn:labena:error:validation_failed",
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "The given data was invalid.",
  "errors": [
    {"message": "Use your work email.", "location": "body.email"},
    {"message": "This address is not eligible.", "location": "body.email"},
    {"message": "Choose a display name.", "location": "body.profile.name"}
  ]
}
```

Every message becomes a separate Huma error detail. Field keys are sorted for
stable ordering; message order within each field is preserved. Bare names refer
to the body, including nested paths such as `items[0].name`. Explicit HTTP paths
such as `query.page`, `path.id`, `header.X-Client` or `body.email` are preserved.
Keep transport-specific paths at the HTTP boundary when possible. An empty key
refers to the whole body. Use one `InvalidFields` to aggregate field messages;
`errors.Join` is not a validation collection API.

Clients branch on `type` and HTTP status, not human-readable messages. Both native
and application validation expose an `errors` array with `message` and `location`.
Submitted values and parser internals are omitted. Huma schema diagnostics are
redacted; application messages are deliberately public and preserved. Never put
provider text, credentials or submitted secrets in those messages. Huma may report
several native validation errors before the handler runs; business validation runs
after that stage succeeds.

| Error category | HTTP status |
| --- | --- |
| Invalid JSON/input | 400 |
| Unauthenticated / forbidden | 401 / 403 |
| Not found / conflict | 404 / 409 |
| Body too large / unsupported media | 413 / 415 |
| Schema or application validation | 422 |
| Rate limited | 429 |
| Internal or unknown failure | 500 |
| Unavailable | 503 |

Huma documents a default problem response. Use `Operation.Errors` for explicit
failure statuses when clients need them in the schema.

## Custom outputs and streams

Use native Huma outputs for custom headers. Declare special fields explicitly;
do not embed `response.JSON[T]` into an output struct.

```go
type CreatedUserOutput struct {
    Location string `header:"Location"`
    Body     response.Envelope[UserResponse]
}

// Register with Operation.DefaultStatus: 201.
return &CreatedUserOutput{
    Location: "/api/v1/users/" + result.ID.String(),
    Body:     response.Envelope[UserResponse]{Data: result},
}, nil
```

The envelope receives automatic metadata. Native bare bodies are also supported
when an endpoint deliberately needs a different wire contract; they are not
silently wrapped. File streams use `Body func(huma.Context)` and keep their bytes.
Health probes, dashboards, metrics and standard router 404/405 responses retain
their native formats, with metadata headers. The request logger covers them too.

Once a response is committed, a later failure must abort the transport rather
than append a problem body. See [handlers](handlers.md) for streaming cleanup.
Cancellation does not undo a committed transaction or accepted task; retries must
account for duplicate or uncertain outcomes.

## Verify the response contract

For an endpoint, check the runtime status and body **and** its `/openapi.json`
entry. Common mistakes have straightforward fixes:

| Symptom | Fix |
| --- | --- |
| Runtime 201 but docs show 200 | Set `DefaultStatus: 201` on the operation |
| Unset or invalid status | Construct `response.JSON[T]` with its helper instead of a zero-valued literal |
| Missing response headers in an integration test | Serve the platform `server`, not only `server.Mux` |
| `items: null` | Construct the page with `pagination.NewPage` |
| Private entity fields appear | Map to a transport-owned response type |
| Client cannot find validation messages | Read the top-level problem `errors` array; errors have no `data` envelope |

Success clients read `data`; problem clients read top-level fields. Use
the `X-Request-ID` response header to investigate failures through
[observability](observability.md). Continue with [error handling](errors.md) and
[live API documentation](api/README.md).
