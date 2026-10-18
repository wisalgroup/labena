# API reference

Run the application and open [the live API docs](http://localhost:8080/docs).
[OpenAPI JSON](http://localhost:8080/openapi.json) comes from the same registered
Huma operations and Go types that handle requests. Use your application's host
and port when they differ from localhost:8080.

There is no generation command or checked-in specification to maintain. After
editing a route or request/response type, rebuild/restart the application and
refresh the page. The document reflects that instance's enabled routes.
`HTTP_DOCS_ENABLED=false` disables both URLs. The JSON endpoint is self-contained;
the interactive page loads a pinned Scalar viewer from a CDN and needs network access.

## Find and try an operation

1. Start the application with the capabilities and dependencies you need.
2. Open `/docs` on that same instance and choose a tagged operation.
3. Inspect required fields, accepted values, successful statuses, and problem responses.
4. For a protected operation, supply your access token through the viewer's bearer authentication control.
5. Send the request, then copy its `X-Request-ID` response header when investigating the result.

Use a local or intended development instance when trying operations that write
data. The viewer sends real requests; it is not a simulator. A docs page does not
bypass authentication or enable a disabled capability.

To inspect the document without the interactive viewer:

```sh
curl --fail http://localhost:8080/openapi.json
```

## Enable capabilities

Business paths include the `/api/v1` prefix.

| Endpoints | Availability |
| --- | --- |
| `/auth/otp/*`, refresh, sessions, logout | `AUTH_ENABLED=true` |
| `/auth/social/{provider}` | Authentication and `AUTH_GOOGLE_ENABLED=true` |
| User reads, Media and Notifications | Protected; fail closed when authentication is disabled |

Authentication also requires valid keys, mail and rate-limiter dependencies.
Configuration validation reports missing requirements. Phone verification, Apple
sign-in and SMS delivery are not implemented. Expo push is optional.

Use `Authorization: Bearer <access_token>` on protected operations. Send refresh
tokens in the refresh body. User reads are self-service, notifications are
recipient-scoped, and Media management authorizes the owner. Content reads require public visibility or
a valid temporary link. Uploads default to public visibility.
A successful registration does not guarantee completion of best-effort local
welcome-event reactions. Email OTP sends return 202 for all addresses. OTP and
social verification return 201 for a newly created account, or 200 for an existing
account, with tokens and `data.user`. After either status, use
`data.user.profile_complete` to decide whether to prompt for the name. Update it
through `PATCH /users/me`; `GET /users/me` exposes the same completion state.

## Change an endpoint

1. Edit the Go input/output types and their Huma schema tags.
2. Register the typed handler once with `huma.Register` and `huma.Operation`.
3. Return `response.OK`, `Created` or `Accepted` with a typed result, or `response.Error(ctx, err)` for a service failure. Declare non-200 success statuses on the operation too. Rebuild/restart and refresh `/docs`.

Method, path, security, validation and schemas come from those registrations.
Use explicit stable operation IDs when clients depend on them. Tests in this
package exercise the live document, authentication, response schemas and concurrent
first requests without application configuration or infrastructure.

Application success bodies contain typed `data` and optional operation-specific
`meta`. Problems keep Huma's top-level error fields, `errors` list and optional
`meta`. Use `apperror.InvalidFields` for multiple application validation messages.
The generated body schemas contain neither `request_id` nor `api_version`;
read `X-Request-ID` and `X-API-Version` response headers instead. Both headers
also accompany 204 responses and file streams, which retain their native semantics.

For a deployment-specific snapshot, download `/openapi.json` with your preferred
HTTP client. Health, metrics and the optional queue dashboard are standard HTTP
infrastructure handlers outside the business OpenAPI document.

See [handlers](../handlers.md), [requests](../requests.md) and
[responses](../responses.md) for the common patterns.

## Keep runtime and OpenAPI aligned

The usual route change has one source of truth:

```text
Go request/response types + huma.Operation → route + validation + live OpenAPI
```

A few explicit declarations still matter:

| Change | Update |
| --- | --- |
| A new accepted field or constraint | The input type and its Huma tags |
| A new public output field | The response type and conversion |
| Fixed 201/202 result | Both the status constructor and `Operation.DefaultStatus` |
| Conditional 200/201 result | The operation's documented responses for both statuses |
| Protected route | Register on the authenticated API so enforcement and security match |
| File upload/download | The multipart or binary media-type declaration |
| Operation rename | Preserve its public `OperationID` unless intentionally changing the client contract |

A runtime response constructor cannot change the startup-time specification. Test
both sides when adding status variants, streams, or custom headers. The
[response guide](../responses.md) shows each shape.

## Export a deployment snapshot

For client generation or a reviewed release artifact:

```sh
curl --fail http://localhost:8080/openapi.json -o /tmp/go-modular-openapi.json
```

That file describes the instance you queried, including its enabled routes. It is
an export, not a new hand-maintained source. Generating a client from it is a
separate tool choice; the framework does not ship an automatic client pipeline.

## Troubleshoot the reference

| Symptom | Check |
| --- | --- |
| `/docs` and `/openapi.json` both return 404 | `HTTP_DOCS_ENABLED` on the running process |
| JSON works but interactive page does not | Network access to the pinned Scalar CDN and deployment content policies |
| Missing endpoint | Feature settings, module registration, and whether the process was rebuilt/restarted |
| Unexpected response schema | The concrete output type, response helper, and operation status declarations |
| Docs under a deployment prefix cannot load JSON | Proxy path rewriting; preserve the page's relative `openapi.json` request |

The developer documentation website and `/docs` are different surfaces: this site
explains how to use the framework; the live viewer describes HTTP operations
registered by your running application. Continue with [routing](../routing.md)
and [HTTP tests](../testing-http.md).
