# Requests and validation

Declare HTTP inputs as Go structs. Huma binds path, query, header, and body values,
then validates them before calling the handler. The same fields and tags describe
the request in the live OpenAPI document.

Services still enforce business rules. CLI commands, jobs, and event handlers call
them without passing through HTTP validation.

## JSON inputs

This complete registration helper echoes a validated name. Add it to a transport
package and call it from module registration with the chosen `huma.API`:

```go
package http

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/internal/platform/api/response"
)

type NameRequest struct {
	Name string `json:"name" minLength:"1" maxLength:"100"`
}

func RegisterEcho(router huma.API) {
	huma.Register(router, huma.Operation{
		OperationID:  "example.echoName",
		Method:       "POST",
		Path:         "/examples/name",
		Summary:      "Echo a validated name",
		MaxBodyBytes: 4096,
	}, func(ctx context.Context, input *struct{ Body NameRequest }) (*response.JSON[NameRequest], error) {
		return response.OK(input.Body), nil
	})
}
```

With the app's `/api/v1` target:

```sh
curl --fail-with-body http://localhost:8080/api/v1/examples/name \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada"}'
```

The response is 200 with `data: {"name":"Ada"}` and request metadata. This example
echoes input; it does not save an account. Remove the example route when it has
served its learning purpose.

### Declare constraints next to fields

| Tag | Example | Meaning |
| --- | --- | --- |
| `minLength`, `maxLength` | `maxLength:"100"` | String length bounds |
| `minimum`, `maximum` | `minimum:"1"` | Numeric bounds |
| `format` | `format:"uuid"` | A supported formatted value |
| `pattern` | `pattern:"^[0-9]{6}$"` | A string pattern, useful for OTPs with leading zeroes |
| `enum` | `enum:"ios,android,web"` | Accepted values |
| `default` | `default:"20"` | A default for an omitted value |
| `required` | `required:"false"` | Explicitly allow omission |
| `nullable` | `nullable:"true"` | Explicitly allow JSON null |
| `doc`, `example` | `doc:"Display name"` | Schema explanation and example |

Keep validation tags on HTTP inputs. A maximum name length in the schema improves
client feedback; the service must still enforce the same business rule for its
other callers. Avoid separate validator frameworks for the same request.

## Path, query and headers

Combine sources in one input:

```go
type ShowRequest struct {
    ID     string `path:"id" format:"uuid"`
    Locale string `query:"locale" default:"en" enum:"en,ar"`
    Client string `header:"X-Client" required:"false" maxLength:"100"`
}
```

Register a path such as `/items/{id}`. Add a `Body` field when JSON is also needed.
The handler receives already bound values; do not look them up again in a second
query parser. A valid UUID does not establish ownership. Read the principal from
context and authorize the resource before the protected operation.

Credential handling belongs to the protected route policy. Do not add an access
token body/query field to replace the standard `Authorization: Bearer …` header.

## Optional fields and partial updates

Use pointers when the service must distinguish an omitted field from its zero
value. Identity's `PATCH /users/me` request is:

```go
type UpdateProfileRequest struct {
    Body struct {
        Name *string `json:"name" maxLength:"100" nullable:"true" required:"false"`
    }
}
```

Its current service behavior is:

| JSON body | Mapped value | Outcome |
| --- | --- | --- |
| `{}` | `Name == nil` | 422: provide at least one profile field |
| `{"name":null}` | `Name == nil` | 422: provide at least one profile field |
| `{"name":"Ada"}` | Pointer to `"Ada"` | Validate and update the name |
| `{"name":""}` | Pointer to empty string | 422: the entity requires a nonempty display name |

A pointer distinguishes missing from empty, but this request deliberately treats
omitted and null alike: neither supplies a change, so the current single-field
operation rejects both. If a future operation requires three states—omit, clear,
and set—design an explicit representation and test all three. Do not assume a
plain pointer preserves that distinction.

Huma handles the input pointer; mapping methods may also use pointer receivers,
as Identity does. That convention does not move mutation or business rules into
the request type.

## Map to a service input

Keep simple conversion beside the request. Identity's existing mapper in
`internal/modules/identity/delivery/http/requests.go` is:

```go
func (req *UpdateProfileRequest) Input() service.UpdateProfileInput {
    return service.UpdateProfileInput{Name: req.Body.Name}
}
```

The file imports `github.com/wisalgroup/labena/internal/modules/identity/service`. The handler supplies the
trusted account ID separately:

```go
user, err := h.svc.UpdateProfile(ctx, userID, req.Input())
```

For sign-in, `SignInRequest.Input(ip)` also receives the peer address extracted at
the HTTP boundary. The request mapper does structural conversion. Authentication,
authorization, external calls, and business orchestration remain in the handler
and service where their context is explicit.

Do not add HTTP schema tags to `service.UpdateProfileInput`. That type is also
usable by a command, task, or another module.

## Pagination

Embed the shared input instead of implementing page parsing again:

```go
type ListProductsRequest struct {
    api.PaginationInput
    Search string `query:"search" maxLength:"100"`
}

// Inside the handler:
page := input.Pagination() // pagination.Pagination
```

Import `github.com/wisalgroup/labena/internal/platform/api`. Pass `page` to the service. Its Huma
resolver runs automatically; no pagination middleware registration is required.

| Request | Outcome |
| --- | --- |
| No pagination parameters | Page 1, 20 items per page |
| `?page=2&per_page=10` | Validated `pagination.Pagination{Page: 2, PerPage: 10}` |
| `?per_page=101` | 422; HTTP rejects values above 100 |
| `?page=`, `?page=1&page=2`, malformed integer | 422 |
| Page too large to represent its SQL offset | 422 |

`pagination.NewPagination` normalizes programmatic values for non-HTTP callers;
the HTTP layer rejects invalid input instead of silently normalizing it.
Repositories still need stable ordering. See [page queries](database-queries.md#return-a-page).

## JSON decoding and limits

A request contains exactly one JSON value matching its declared body type. The
platform applies these rules before the handler runs:

| Input | Status |
| --- | --- |
| Malformed JSON, duplicate members including case variants, trailing JSON value, invalid UTF-8 | 400 |
| Unknown object fields or schema/type violations | 422 |
| Full body exceeds the operation's limit | 413 |
| Unsupported media type, compression, or non-UTF-8 charset | 415 |

JSON objects, arrays, and scalar bodies are supported. JSON and structured `+json`
media types are accepted; an absent Content-Type defaults to JSON. Compression is
not enabled. Content-Encoding may be absent or `identity`.

The default body limit is **1 MiB**, including trailing whitespace. Nested JSON is
bounded to **1,000 container levels**. Huma's default body-read timeout is **five
seconds**. Set `Operation.MaxBodyBytes` and `BodyReadTimeout` for each operation that
needs different bounds. A file-content limit and a whole multipart-request limit
are separate budgets.

## Multipart bodies

Use Huma's raw multipart form together with the platform's bounded parser:

```go
// Import mime/multipart.
type UploadRequest struct {
    RawBody multipart.Form
}
```

Register after authentication. This example uses a one-file operation with a 2 MiB
whole-request limit and a handler you implement:

```go
huma.Register(protected, huma.Operation{
    OperationID:   "example.upload",
    Method:        "POST",
    Path:          "/uploads",
    DefaultStatus: 201,
    MaxBodyBytes:  2 << 20,
    Middlewares:   huma.Middlewares{api.Multipart},
    RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{
        "multipart/form-data": {Schema: &huma.Schema{
            Type: "object", Required: []string{"file"}, AdditionalProperties: false,
            Properties: map[string]*huma.Schema{
                "file": {Type: "string", Format: "binary"},
            },
        }},
    }},
}, handler.upload)
```

The schema describes the upload. The handler still checks the permitted parts:

```go
form := &input.RawBody
if len(form.File) != 1 || len(form.File["file"]) != 1 || len(form.Value) != 0 {
    return nil, response.Error(ctx, apperror.Validation("Provide one file part named file."))
}
file, err := form.File["file"][0].Open()
if err != nil {
    return nil, response.Error(ctx, err)
}
defer file.Close()
// Pass file and ctx to the owning capability, then return its public result.
```

Imports include `github.com/wisalgroup/labena/pkg/apperror` and the response package. The middleware
bounds the full request, spools files above 1 MiB to disk, and removes temporary
files after the handler returns or panics. It does not decide content policy,
ownership, allowed filenames, or business authorization.

For the installed Media endpoint, use its existing integration instead of writing
another upload controller:

```sh
curl --fail-with-body http://localhost:8080/api/v1/media \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -F 'file=@./avatar.png' \
  -F 'visibility=private'
```

Set `ACCESS_TOKEN` to your current access token. See [Media](media.html) for its
20 MiB file policy, default visibility, and returned URL.

## Application validation errors

Huma handles structural validation. After it succeeds, a service can return one
or more safe business validation messages:

```go
return apperror.InvalidFields(map[string][]string{
    "name": {"Choose a display name."},
    "email": {"Use an eligible email address."},
})
```

The HTTP boundary translates these into the same `errors` list shape as native
Huma validation. Huma's own diagnostic values are redacted; application messages
are intentionally public. See [error handling](errors.md#return-several-validation-messages)
and the [wire response](responses.md#multiple-validation-messages).

## Verify the boundary

Test through `server.ServeHTTP`, not only by invoking the handler. Include a valid
request, an omitted optional field, a boundary value, unknown fields, malformed
JSON, an oversized body, and unauthorized access where applicable. For uploads,
include unexpected parts and cleanup after failure.

The installed HTTP behavior is covered by `internal/platform/api` and
`docs/api/contract_test.go`; business scenarios belong in `tests/<module>`.
Continue with [responses](responses.md) and [middleware](middleware.md).
