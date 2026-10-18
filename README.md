# Labena

[Labena](https://github.com/wisalgroup/labena) is a modular Go application template
with explicit constructors, typed Huma HTTP
operations, Cobra commands, Bun persistence and Redis-backed jobs. Business modules
own operations; app assembly owns connections, registration and process lifetime.

Adopt a release as the starting source for your own application, rename its module,
and maintain your customized copy. Updates are applied through reviewed upstream
changes. See [adoption and updates](docs/upgrading.md) and the
[support policy](SUPPORT.md).

## Start here

| I want to… | Read |
| --- | --- |
| Run the app for the first time | [Installation](docs/getting-started.md) |
| Create my application or apply an upstream fix | [Adoption and updates](docs/upgrading.md) |
| Understand the pattern without infrastructure | [Your first module](docs/modules.md) |
| Find the right file to change | [Directory structure](docs/structure.md) |
| Build a stored operation | [HTTP and CLI workflow](docs/persistent-workflow.md) |
| Learn the engineering rules | [Project conventions](docs/conventions.md) |
| Configure or troubleshoot a deployment | [Configuration](docs/configuration.md) and [deployment](deployments/README.md) |

Try the compiled Catalog starter without a database or Redis:

```sh
go test ./tests/modules -run TestFirstModule -count=1
```

To browse the searchable documentation site:

```sh
go run docs/build.go
go run docs/serve.go
```

Open [the local documentation](http://127.0.0.1:8091). It includes task-based guides,
examples, a page outline, search and downloadable Markdown. See
[documentation maintenance](docs/README.md) for editing and verification.

## Included capabilities

| Capability | Integration |
| --- | --- |
| HTTP and OpenAPI | Huma on Go's native HTTP mux; live `/docs` and `/openapi.json` |
| Business persistence | PostgreSQL with Bun and explicit Goose migrations |
| Authentication | Email OTP and Google sign-in; account creation and sign-in share the same flow |
| Tasks | Asynq with Redis, routing, retries and administration |
| Events | Watermill GoChannel; process-local and best-effort |
| Mail | SMTP, shared HTML/text views and optional queued sending |
| Notifications | Database JSON, email and Expo push with a PostgreSQL delivery ledger |
| Media | Public/private assets, storage URLs, metadata and deletion recovery |
| Cache | Optional memory, Redis or database driver |
| Observability | Structured logs, traces, metrics and request correlation |

Use the Go version declared in `go.mod`. This is an application template: reusable
packages may support additional drivers, but the installed modules require
PostgreSQL. Core CI covers PostgreSQL and standalone Redis; live providers and
additional drivers have the verification limits listed in the
[support matrix](SUPPORT.md#verified-scope). Maintenance targets the latest
published release on a best-effort basis. The module path is
`github.com/wisalgroup/labena`; adopters replace it with their application's path.
The `pkg` packages are maintained with the template and have no
independent external releases. See [security reporting](SECURITY.md),
[contributing](CONTRIBUTING.md) and the [MIT license](LICENSE).

## Run locally

Follow [installation](docs/getting-started.md) to start tutorial PostgreSQL and Redis,
then run:

```sh
export APP_ENV_FILE=.env.quickstart.example
go run ./cmd/cli migrate up
go run ./cmd/app
```

The tutorial disables optional features. Mail is constructed but does not connect
until sending. Startup does not run migrations. The full `.env.example` enables
implemented capabilities and needs credentials before use; routine defaults live
in Go and the [configuration reference](deployments/configuration.md).

Check readiness, list routes, or open the live API docs:

```sh
curl -i http://127.0.0.1:8080/readyz
APP_ENV_FILE=.env.quickstart.example go run ./cmd/cli route list
```

The running API serves [documentation](http://127.0.0.1:8080/docs) and
[OpenAPI](http://127.0.0.1:8080/openapi.json) directly from registered operations.
No schema-generation step is required after route edits.

## Structure

```text
cmd/                         App, worker, scheduler and CLI entry points
internal/
  app/                      Composition, schedules and lifecycle ownership
    config/                 Environment settings and owner translations
  modules/
    identity/               Nested account/authentication module
    shared/                 Business entities, enums, event contracts and SQL
  platform/
    dependencies/           Borrowed Resources and registration Targets
    api/, cli/, messaging/  Transport and execution integration
    mail/, notification/    Email and durable channel delivery
    media/                  Managed assets and storage recovery
pkg/                         Reusable technical capabilities
tests/                       Business, application and integration tests
examples/                    Compiled usage examples
docs/                        Guide sources and static site
```

Each module constructs local services and registers its transports and SQL in a
package-level `Register` function. App constructs authentication before protected
targets and passes the service explicitly. App has one registration call per module. Recurring schedules are
centralized in `internal/app/schedules.go`. Services receive specific
collaborators; they do not retain a resource lookup container.

Shared business entities remain owned by their business module. Platform types,
such as `notification.Record` and `media.Asset`, remain inside their capabilities.
`pkg` never imports `internal`. See [conventions](docs/conventions.md) for the
complete contract.

## Follow connected workflows

- [A persisted HTTP and CLI operation](docs/persistent-workflow.md): prove an email,
  create/read the account, and reuse its service from another transport.
- [Account onboarding](docs/onboarding.md): local event → task → notification →
  channel delivery. Local publication is best-effort.
- [SQL and notification intent](docs/transactional-notifications.md): commit business
  state and delivery intent together. Redis enqueue and SMTP are not SQL-atomic.
- [Media](internal/platform/media/README.md): select visibility and choose internal
  application URLs or direct provider URLs.

`cmd/app` and `cli serve` explicitly select HTTP and workers. `cmd/worker` runs queue workers with
local subscribers and no scheduler. Events do not cross processes; Redis tasks do.
Run one `cmd/scheduler` per schedule set. Ordinary CLI commands construct connections
but do not run application workers. See [entry points](docs/entry-points.md).

## Observe and operate

The optional local stack sends traces and logs through OTLP to Alloy, then Tempo
and Loki. Prometheus collects metrics; Grafana connects requests, logs and traces.
Readable file logging is independent of OTLP export.

```sh
tilt up
```

Tilt enables storage and observability by default. Compose offers optional profiles.
Use [local development](docs/local-development.md),
[observability](docs/observability.md) and [deployment](deployments/README.md) for
endpoints, configuration and investigation steps.

App stops admission, drains workers and subscribers, and closes owned resources
after their users finish. Cancellation is cooperative and time-bounded; review
[application lifecycle](docs/application-lifecycle.md) when adding long-running work.

## Verify

```sh
go test -race ./...
go vet ./...
govulncheck ./...
go run docs/build.go
```

Set the documented test connection variables for disposable PostgreSQL, Redis and
other external drivers. Tests skip unavailable backends; a passing unit suite alone
does not prove those integrations. See [testing](docs/testing.md) for exact setup
and [contributing](CONTRIBUTING.md) for the change workflow.
