# Your first module

Create a greeting endpoint in one file, register it in the app, and call it.
Then learn where services, repositories and additional transports fit as the
module grows. Start small: the greeting needs no database or worker of its own.

## Create the module

The supplied Catalog starter is compiled by the normal test suite. Try it without
configuration or infrastructure:

```sh
go test ./tests/modules -run TestFirstModule -count=1
```

The test registers the module with an in-process HTTP server and verifies
`Hello, Ada`. Copy its source to begin an application module:

```sh
mkdir -p internal/modules/catalog
cp examples/module/catalog/register.go internal/modules/catalog/register.go
```

Complete `internal/modules/catalog/register.go`:

```go
package catalog

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/dependencies"
)

type GreetingRequest struct {
	Name string `path:"name" minLength:"1"`
}
type GreetingResponse struct {
	Message string `json:"message"`
}

// Register installs the greeting route without opening resources.
func Register(_ dependencies.Resources, targets dependencies.Targets) error {
	if targets.HTTP == nil {
		return nil
	}
	huma.Register(targets.HTTP, huma.Operation{Method: "GET", Path: "/catalog/hello/{name}", Summary: "Greet a visitor"},
		func(ctx context.Context, input *GreetingRequest) (*response.JSON[GreetingResponse], error) {
			return response.OK(GreetingResponse{Message: "Hello, " + input.Name}), nil
		})
	return nil
}
```

The `github.com/wisalgroup/labena` import prefix matches `go.mod`; change imports if you rename the
Go module.

## Understand registration

| Piece | Responsibility |
| --- | --- |
| `Register(resources, targets)` | Construct collaborators and attach routes, commands, tasks, events and migrations |
| `GreetingRequest` | Describe a path input and its Huma validation |
| `GreetingResponse` | Select the public output fields |
| `response.OK(...)` | Return the shared success envelope |

The unused Resources argument leaves room for this module's collaborators. An
absent HTTP target skips HTTP registration. Register performs no I/O and starts
no background work. There is no module instance to retain or initialize.

## Register it with the app

In `internal/app/modules.go`, add the import:

```go
"github.com/wisalgroup/labena/internal/modules/catalog"
```

In the application's `registerModules` method, add one call after Identity's registration:

```go
if err := catalog.Register(a.resources, a.targets); err != nil {
    return fmt.Errorf("register catalog: %w", err)
}
```

Future routes, commands and migrations belong in Catalog's Register function;
they need no other application list. Pass business services and settings as
explicit arguments only when this module needs them. Do not import app config
or add business services to Resources.

## Run your route

Start the [tutorial database and Redis](index.html#start-local-dependencies), apply
migrations, and restart the full app:

```sh
APP_ENV_FILE=.env.quickstart.example go run ./cmd/app
```

In another terminal:

```sh
curl -i http://127.0.0.1:8080/api/v1/catalog/hello/Ada
```

Expect **200** and an envelope like this; field order is not significant:

```json
{
  "data": {"message": "Hello, Ada"}
}
```

Open [the running API docs](http://127.0.0.1:8080/docs) to see the new operation and
its schema. Registration drives both routing and OpenAPI. No separate annotation
or generation step is required.

The isolated starter test needs no infrastructure; the full installed application
still constructs its configured PostgreSQL and Redis dependencies.

## Add business behavior when needed

Move an operation into `service` when it has business rules or several callers.
Construct module-local services once inside `Register` and pass them to HTTP,
CLI and task handlers. Services needed before registration are constructed by app
and passed explicitly, as with Identity authentication:

```text
internal/modules/catalog/
  register.go
  service/
    service.go       Public business interface
    product.go       Private implementation and constructor
  repository/
    repository.go    Persistence interface
    product.go       Queries, implementation and constructor
  delivery/
    http/            Requests, responses, handlers and registration
```

This is a **growth map**, not a scaffold to create in advance. Business entities
belong in `internal/modules/shared/entities`; module-owned migrations define their schema.
See the [persisted operation](persistent-workflow.html) for a real service shared
by HTTP and CLI.

## Keep the pattern as it grows

| Need | Add | Register |
| --- | --- | --- |
| Business rules reused by several callers | One cohesive service | Its transport handlers |
| Persistent state | Shared entity, repository and module migration | SQL through `Targets.Migrations` |
| Deferred work | `jobs` definition and `delivery/jobs` handler | Handler through `Targets.TaskRouter` |
| Business reaction | Shared event contract and consuming `delivery/events` handler | Handler through event targets |
| A console operation | Native Cobra constructor in `delivery/cli` | Group through `Targets.RootCommand` |
| Notification content | A named constructor in `notifications` | Reuse the existing notifier |
| Recurring work | An existing task or command factory | **Central timing** in `app/schedules.go` |

Scheduling is the intentional centralized exception. Modules own executable
handlers; app owns when recurring work runs. Details are in
[module registration](module-registration.html) and [scheduling](scheduling.html).

## Keep large use cases on the same service

A substantial method can move to a separate file without creating a new service:

```text
service/
  service.go        ProductService interface
  product.go        productService and NewProductService
  import.go         Import method on *productService
```

Constructors stay beside implementations. Contracts stay with their owners in
`service/service.go` and `repository/repository.go`. Consumers import those
contracts rather than redefining them or using a global contracts directory.

Services receive narrow dependencies; only module assembly receives the complete
Resources bundle. See [dependency injection](dependency-injection.html).

## Learn from an existing operation

| Example | What it demonstrates |
| --- | --- |
| [Persisted account workflow](persistent-workflow.html) | HTTP input → service → repository → CLI read |
| [Account onboarding](onboarding.html) | Event → task → notification → channel delivery |
| [SQL and notification intent](transactional-notifications.html) | Atomic business change and persisted delivery intent |
| [Media](media.html) | A platform capability with owner-local types and an integration package |

Identity's authentication transactions enforce special security rules. Read their
explanation before copying them into normal CRUD operations.

## Check your changes

```sh
gofmt -w internal/modules/catalog/register.go internal/app/modules.go
go test ./internal/modules/catalog ./tests/app ./tests/architecture
```

Place business service and repository tests in `tests/catalog`, following
`tests/identity`. Keep app tests in `tests/app`. Architecture checks enforce import
direction and test placement; behavior tests still verify authorization, errors
and persistence.

Before merging application changes, run the [full verification workflow](testing.html).
External-driver tests require disposable services; a skip is not an integration
success.

Continue with [directory structure](structure.html) for a code map,
[module registration](module-registration.html) for the complete assembly flow,
or [routing](routing.html) to develop the new endpoint.
