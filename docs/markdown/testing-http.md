# Testing HTTP and modules

Send requests through a fresh platform API and inspect status, headers, and body.
Most HTTP tests need neither a listening socket nor full application construction.
This exercises Huma binding, shared policies, and response serialization together.

## Test decoding and rendering together

Save this complete test in `tests/catalog/http_example_test.go`. Tests inside this
repository may import `internal/platform/api`; they do not need to live inside the
internal package itself.

```go
package catalog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/api/response"
)

func TestNameEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		status int
		called bool
	}{
		{"valid", `{"name":"Ada"}`, 200, true},
		{"missing", `{}`, 422, false},
		{"unknown field", `{"name":"Ada","role":"admin"}`, 422, false},
		{"duplicate", `{"name":"Ada","Name":"Other"}`, 400, false},
		{"malformed", `{`, 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, err := api.New(api.Options{})
			if err != nil {
				t.Fatal(err)
			}
			type nameBody struct {
				Name string `json:"name" minLength:"1" maxLength:"100"`
			}
			called := false
			huma.Register(server, huma.Operation{
				OperationID: "test.name", Method: "POST", Path: "/name", MaxBodyBytes: 1024,
			}, func(ctx context.Context, in *struct{ Body nameBody }) (*response.JSON[nameBody], error) {
				called = true
				return response.OK(in.Body), nil
			})
			req := httptest.NewRequest(http.MethodPost, "/name", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			result := httptest.NewRecorder()
			server.ServeHTTP(result, req)
			if result.Code != tc.status || called != tc.called {
				t.Fatalf("status=%d called=%v body=%s", result.Code, called, result.Body.String())
			}
			var body struct {
				Type string   `json:"type"`
				Data nameBody `json:"data"`
			}
			if err := json.Unmarshal(result.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if result.Header().Get("X-Request-ID") == "" || result.Header().Get("X-API-Version") != "v1" {
				t.Fatalf("missing response headers: %v", result.Header())
			}
			if tc.called && body.Data.Name != "Ada" {
				t.Fatalf("data=%+v", body.Data)
			}
			if tc.status == 422 && body.Type != "urn:go-modular:error:validation_failed" {
				t.Fatalf("problem type=%q", body.Type)
			}
		})
	}
}
```

Run `go test ./tests/catalog` after adding the example. It verifies that invalid
input never reaches the operation, successful results use `data`, and both paths
have correlation headers. It does not establish persistence or authorization.

## Test a real module operation

Use the existing owner service contract for a small fake, then register the actual
module transport. Identity's `internal/modules/identity/delivery/http/user_handler_test.go`
shows this pattern. Do not define a second production contract solely for a test.

| Scenario | Useful assertion |
| --- | --- |
| Valid request | Expected service input and selected public response fields |
| Invalid body/path/query | Service was never invoked |
| Application field errors | Every safe message appears at the intended location |
| Operational failure | Generic public problem; no raw provider/driver detail |
| Profile update | Omitted/null/empty name follows the current service policy |
| Pagination | Defaults, bounds, ambiguous query rejection, and stable page shape |

Test a policy, not every internal method call. A fake can confirm that the handler
passes the authenticated user ID; it cannot prove the repository enforces a
concurrent database constraint.

## Verify authentication and ownership

Protected route tests should cover:

- No Authorization header, multiple headers, malformed bearer credentials.
- Valid credentials and an active principal.
- Operational authenticator failure, which must not become anonymous access.
- Reading the current account versus another account's resource.
- A protected group with no authenticator, which must fail closed.

Authentication and authorization are different assertions. A UUID that parses or
a token that verifies does not grant permission to every resource. Use the same
`api.Authenticated` grouping that production registration uses.

## Verify multipart and streaming behavior

For uploads, test the whole request byte limit, permitted parts, authorization
before parsing, and cleanup of temporary files after success and failure. For
streaming, check opening failures before commitment separately from copy failures
after bytes have been written.

`httptest.ResponseRecorder` is useful for ordinary responses. Use a real local
`httptest.Server` when the behavior requires a network connection, disconnect,
flush, or transport abort. The platform's streaming and shutdown tests are the
reference; a recorder alone cannot establish client-visible truncation behavior.

## Check the live OpenAPI contract

```sh
go test ./docs/api ./internal/platform/api/...
```

These tests inspect the running document and response schemas. When adding a
conditional status or binary response, assert that its documented media type,
status, required fields, and security match the runtime handler. Rebuilding Go
routes updates the live document; there is no generated Swagger file to edit.

## Test full application wiring

```sh
go test -race ./internal/modules/identity/delivery/http ./internal/platform/api/...
go test ./tests/modules ./tests/app
```

`tests/modules` includes the database-free Catalog starter and compiled workflows.
Some module/app tests need disposable backends and skip without their settings;
use the [required integration gate](testing.md#run-the-required-integration-gate-locally)
for full core coverage.

Register fresh APIs for each test. Finish registration before serving. For tests
that start listeners or runners, use readiness/completion signals and bounded
contexts instead of arbitrary sleeps, then join users before closing dependencies.

Continue with [database tests](testing-database.md),
[task and event tests](testing-async.md), and [responses](responses.md).
