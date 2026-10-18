# Routing

A route connects an HTTP method and path to a typed Go handler. Register it with
`huma.Register`: the same declaration supplies routing, input validation, and
OpenAPI. Modules receive ready-to-use APIs through `dependencies.Targets`.

Start with [your first module](modules.md) if you have not added one yet. This page
explains how its HTTP routes fit together; [handlers](handlers.md) covers the code
that runs after a request has been validated.

## Register a route

Inside the module's `Register` function:

```go
if targets.HTTP != nil {
    huma.Register(targets.HTTP, huma.Operation{
        OperationID: "catalog.greet",
        Method:      "GET",
        Path:        "/catalog/hello/{name}",
        Summary:     "Greet a visitor",
    }, func(ctx context.Context, input *GreetingRequest) (*response.JSON[GreetingResponse], error) {
        return response.OK(GreetingResponse{Message: "Hello, " + input.Name}), nil
    })
}
```

Imports: `context`, `github.com/danielgtaylor/huma/v2`, and
`github.com/wisalgroup/labena/internal/platform/api/response`. The request and response types are:

```go
type GreetingRequest struct {
    Name string `path:"name" minLength:"1"`
}

type GreetingResponse struct {
    Message string `json:"message"`
}
```

App supplies a group prefixed with `/api/v1`, so this operation is available at:

```sh
curl http://localhost:8080/api/v1/catalog/hello/Ada
```

```json
{
  "data": {"message": "Hello, Ada"}
}
```

This is an alternative registration for the existing Catalog example; do not
register both versions at the same path. The complete compiling starter lives in
`examples/module/catalog/register.go`.

A nil HTTP target means that transport is omitted. Skip its registration and
continue registering other module capabilities. Registration installs definitions;
it starts no listeners, goroutines, or business work.

## Choose a public or protected route

| Target | Use it for | What it supplies |
| --- | --- | --- |
| `targets.HTTP` | Public operations such as requesting a sign-in code | The business API prefix and common policies |
| `targets.AuthenticatedHTTP` | Operations requiring a signed-in user | The same prefix plus bearer enforcement and OpenAPI security |

Use the protected target for account-specific routes:

```go
if targets.AuthenticatedHTTP != nil {
    registerAccountRoutes(targets.AuthenticatedHTTP, users)
}
```

Here `registerAccountRoutes` is a module-owned helper accepting `huma.API` and its
existing service contract. It receives the service directly; it does not look up
application resources during a request.

Authentication proves who is calling. The handler or service must still authorize
the requested resource. See [handler authorization](handlers.md#authentication-and-policy)
and [Identity](identity.html).

## Group routes and policies

Create a group for a shared prefix, tag, or policy. This example belongs in a
registration helper after checking the target is non-nil:

```go
catalog := huma.NewGroup(targets.HTTP, "/catalog")
catalog.UseSimpleModifier(func(op *huma.Operation) {
    op.Tags = []string{"catalog"}
})

huma.Register(catalog, huma.Operation{
    OperationID: "catalog.listProducts",
    Method:      "GET",
    Path:        "/products",
    Summary:     "List products",
}, handler.list)
```

`handler.list` is the typed handler you implement for that operation. Its effective
path is `/api/v1/catalog/products`. Do not repeat `/api/v1` in the module.

Install group middleware and modifiers **before** registering operations. Prefer a
module-owned child group when changing tags or policies, so unrelated modules do
not inherit them. Use `huma.Register` and `huma.Operation` directly; there is no
second framework registration API.

## Describe an operation

The most useful operation fields are:

| Field | When to set it |
| --- | --- |
| `OperationID` | A stable, unique name for generated clients and route identification |
| `Method`, `Path` | The HTTP method and resource path |
| `Summary`, `Description`, `Tags` | Short purpose, additional contract details, and documentation grouping |
| `DefaultStatus` | Fixed successful status other than the inferred default |
| `Errors` | Failure statuses you want explicitly listed in OpenAPI |
| `MaxBodyBytes`, `BodyReadTimeout` | Body size and read deadline appropriate to this operation |
| `Middlewares` | A policy belonging only to this operation |

For example, registering a create handler:

```go
huma.Register(catalog, huma.Operation{
    OperationID:     "catalog.createProduct",
    Method:          "POST",
    Path:            "/products",
    Summary:         "Create a product",
    DefaultStatus:   201,
    Errors:          []int{401, 409, 422},
    MaxBodyBytes:    4096,
    BodyReadTimeout: 5 * time.Second,
}, handler.create)
```

Import `time` for this fragment. The handler returns `response.Created(result)`.
The status constructor controls the actual response; `DefaultStatus` describes it
in OpenAPI. For operations returning more than one successful status, see
[responses](responses.md#declare-multiple-success-statuses).

Operation IDs are derived when omitted. Use explicit IDs for a public contract and
keep them stable when only documentation wording changes. Huma registration and
ServeMux pattern conflicts are programmer errors; validate routes in tests before
serving.

## Path parameters and query values

Use `{name}` for a path parameter and declare its source on the input:

```go
type ProductRequest struct {
    ID     string `path:"id" minLength:"1" maxLength:"200"`
    Locale string `query:"locale" default:"en" enum:"en,ar"`
}
```

Register `/products/{id}`. The handler reads `input.ID` and `input.Locale` after
binding and validation; it does not parse the URL again. Add body or header fields
to the same input when needed. See [requests](requests.md).

## Trailing slashes

Register resource paths without a trailing slash. An unregistered trailing-slash
variant such as `/api/v1/auth/otp/send/` redirects to `/api/v1/auth/otp/send` with
**308**, preserving the method and body. Query strings and escaped path parameters
are preserved. Use the canonical path to avoid a round trip.

`/docs/` redirects to `/docs`, keeping its relative OpenAPI URL correct. Explicit
trailing-slash and subtree registrations retain ServeMux semantics. Unknown paths
remain 404. Repeated separators and dot segments follow Go's standard path
cleaning; do not use them as alternate resource URLs.

## Server setup and infrastructure

`api.New(api.Options{})` constructs a Huma API backed by `http.ServeMux`. App creates
the `/api/v1` group and supplies it to modules. `HTTP_ADDRESS` selects the listener.
App owns serving, readiness, and shutdown.

| Route | Purpose |
| --- | --- |
| `GET /healthz` | Liveness |
| `GET /readyz` | Startup, dependency, and shutdown readiness |
| `/api/v1/*` | Typed business operations |
| `GET /docs` | Live interactive API reference |
| `GET /openapi.json` | OpenAPI from registered operations |

Infrastructure handlers mount on `server.Mux` using standard `http.Handler` APIs.
Serve **`server`** so correlation, logging, recovery, and observability also cover
those handlers. Serving `server.Mux` directly bypasses the surrounding policies.
Health, metrics, dashboards, and standard 404/405 responses retain their own formats.

For use without full app assembly, [construct an API server directly](handlers.md#independent-use)
and supply it to a standard HTTP server or `pkg/httpserver`. Finish registration
before accepting requests.

## Inspect and verify routes

```sh
go run ./cmd/cli route list
curl --fail http://localhost:8080/openapi.json
```

The CLI constructs the application and its configured connections; it does not
start workers merely to inspect routes. The live specification reflects the
running instance, so restart after Go code changes. See [API reference](api/README.md).

| Symptom | Check |
| --- | --- |
| 404 after adding a route | The module Register function is called; the HTTP target is non-nil; the prefix is correct |
| 405 | The path exists but the HTTP method does not match |
| 401 | The operation uses the protected target and needs a valid bearer token |
| New route absent from `/docs` | Restart the running binary and confirm its feature settings |
| Duplicate registration panic | One method/path is being registered more than once or patterns conflict |

Continue with [handlers](handlers.md), [request validation](requests.md), and
[middleware](middleware.md).
