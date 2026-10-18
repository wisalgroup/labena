# Installation

Start the application with PostgreSQL and Redis, confirm that it is ready, then
choose your next feature. You do not need a cloud account, Kubernetes, or email
credentials for this first run.

Want to understand a module first? Run `go test ./tests/modules -run TestFirstModule`
and follow [your first module](modules.html). That example runs entirely in process.

This tutorial runs the template as supplied. To make it your own application,
follow [adoption and updates](upgrading.md) to record the source release, rename
the module and establish how you will incorporate future security fixes.

## Requirements

- The Go version declared in `go.mod` (`go version` shows your installed version).
- Docker for the two disposable services below.
- Two terminals, opened at the repository root.

The installed application uses PostgreSQL, Redis-backed Asynq tasks, and
process-local Watermill events. Redis persists tasks; local events do not persist.

## Start local dependencies

These containers use dedicated host ports and disposable data. The credentials
below are tutorial values only:

```sh
docker run --rm -d --name go-modular-quickstart-postgres \
  -e POSTGRES_USER=demo -e POSTGRES_PASSWORD=local-demo \
  -e POSTGRES_DB=go_modular -p 127.0.0.1:55432:5432 postgres:17-alpine

docker run --rm -d --name go-modular-quickstart-redis \
  -p 127.0.0.1:56379:6379 redis:7-alpine
```

Check both services before starting the application:

```sh
docker exec go-modular-quickstart-postgres pg_isready -U demo -d go_modular
docker exec go-modular-quickstart-redis redis-cli ping
```

Expect `accepting connections` and `PONG`. PostgreSQL may need a few seconds;
repeat the readiness check if it is still starting. For retained data, see
[local development](local-development.html).

## Configure and migrate

Select the supplied tutorial profile in the first terminal:

```sh
export APP_ENV_FILE=.env.quickstart.example
go run ./cmd/cli migrate up
```

This profile points to the two containers above. It disables authentication,
cache, storage, push, scheduling and telemetry. Mail is always constructed, but
SMTP connects only when sending: this tutorial sends no email.

`migrate up` applies the registered Goose migrations and returns to your prompt.
Migrations are explicit; starting the server never changes the schema. Confirm
that there are no pending migrations:

```sh
go run ./cmd/cli migrate status
```

Existing shell variables override the profile. An unexpected database, credential
error or exporter usually means an inherited override; review
[configuration precedence](configuration.html#understand-precedence).

## Start the application

In the same terminal:

```sh
go run ./cmd/app
```

The app main explicitly starts HTTP, queue workers, local event subscribers and the
notification dispatcher. Run cmd/scheduler separately when recurring maintenance is needed. Leave this
terminal running.

## Check the app and enable signup

In the second terminal:

```sh
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

Both return **200** after successful startup. `/healthz` checks the HTTP listener;
`/readyz` checks application startup/shutdown state. Dependency probes are a
separate [readiness option](application-lifecycle.html#startup-and-readiness).

Open [the running API documentation](http://127.0.0.1:8080/docs), or inspect its
current schema:

```sh
curl http://127.0.0.1:8080/openapi.json
```

These routes reflect the registered application immediately. No OpenAPI generation
command is required after editing a route.

The quickstart has authentication disabled, so protected routes intentionally
reject requests. To try signup/sign-in, copy the tutorial profile into your own
local file, configure SMTP and Identity keys, and follow
[Identity setup](identity.html#email-otp-configuration-and-security). Do not use
`.env.example` without filling its enabled capabilities' credentials.

| Ready to try… | Continue with… |
| --- | --- |
| An endpoint you can change | [Your first module](modules.html) |
| Email OTP and social sign-in | [Identity](identity.html) |
| A stored account through HTTP and CLI | [Persisted operation walkthrough](persistent-workflow.html) |
| A background task | [Queues](queues.html) |
| The full container stack | [Local development](local-development.html) |

## Run a console command

Select the same profile in the second terminal:

```sh
export APP_ENV_FILE=.env.quickstart.example
go run ./cmd/cli help
go run ./cmd/cli route list
```

Help lists nested command paths. `route list` prints methods, paths and operation
names without opening another HTTP listener. Ordinary CLI commands construct and
connect the application but do not run workers, subscribers or schedules.
See [console commands](console.html) for the complete execution model.

## Follow the request through the code

When reading the supplied Identity module, follow this sequence:

| Responsibility | Location |
| --- | --- |
| Register an operation and its schema | `internal/modules/identity/delivery/http/routes.go` |
| Decode input and map service arguments | `internal/modules/identity/delivery/http/requests.go` |
| Authorize and call the operation | `internal/modules/identity/delivery/http/*_handler.go` |
| Apply business rules | `internal/modules/identity/service` |
| Execute queries | `internal/modules/identity/repository` |
| Define the user and its invariants | `internal/modules/shared/entities` |
| Construct and connect the module | `internal/modules/identity/register.go`, `internal/app/modules.go` |

The [directory map](structure.html) and [dependency guide](dependency-injection.html)
explain why these pieces are separate and where shared capabilities fit.

## Stop and verify

Press **Ctrl+C** in the application terminal. The application drains admitted work
within its shutdown budget, then closes owned resources. Remove the tutorial
containers when finished; this deletes their disposable data:

```sh
docker rm -f go-modular-quickstart-postgres go-modular-quickstart-redis
```

For a quick source check:

```sh
go test ./tests/modules -run TestFirstModule -count=1
go vet ./...
```

Use the [testing guide](testing.html) for the full suite and disposable integration
services. Tests that skip because their service settings are absent have not
verified those integrations.

## If the first run fails

| Symptom | First check |
| --- | --- |
| Docker reports a name already in use | A previous tutorial container is still running |
| Database connection refused | PostgreSQL readiness and host port `55432` |
| Redis connection refused | Redis readiness and host port `56379` |
| Credentials or unexpected services are required | `APP_ENV_FILE` and inherited process variables |
| HTTP address is already in use | Another app, Compose or Tilt process owns port `8080` |
| Protected endpoint returns an error | Authentication is intentionally off in this tutorial |

Continue with [configuration](configuration.html), then
[your first module](modules.html). More detailed symptoms are covered in
[troubleshooting](troubleshooting.html).
