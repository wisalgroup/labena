# Labena

**A batteries-included Go framework for modular applications.**

Build HTTP APIs, background workers and business workflows with authentication,
persistence, queues, mail, notifications, storage and observability already connected.
Labena combines established Go libraries with explicit constructors, module-owned
registration and a shared application lifecycle.

[Documentation](https://wisalgroup.github.io/labena/overview.html) · [Create an application](docs/creating-applications.md) ·
[First module](docs/modules.md) · [Deployment](deployments/README.md) · [Contributing](CONTRIBUTING.md)

Labena is delivered as an **application template**. Create a project, choose its Go
module and application name, and own the resulting source. You can change its
modules, dependencies and deployment model as your application grows. Upstream
updates are reviewed and applied to your copy; the [support policy](SUPPORT.md)
defines compatibility and maintenance for the current `0.x` release series.

## What's included

| Capability | Included behavior |
| --- | --- |
| **HTTP APIs** | Typed Huma operations on Go's HTTP mux, request validation, consistent responses, middleware and live OpenAPI documentation |
| **Authentication** | Email OTP, Google sign-in, access and refresh tokens, persisted sessions, revocation and rate limits |
| **Persistence** | PostgreSQL with Bun, explicit transaction boundaries, Goose migrations and seeders |
| **Background work** | Redis-backed Asynq jobs, retries, queue administration and a dedicated scheduler |
| **Events** | Watermill subscriptions for process-local, best-effort reactions |
| **Mail** | SMTP delivery, shared HTML/plain-text views, application branding and optional queued sending |
| **Notifications** | Database, email and Expo push channels, with durable notification intent and delivery tracking |
| **Storage and media** | Named storage disks, local and cloud adapters, public/private assets, signed access and deletion recovery |
| **Cache** | Memory, Redis and database stores with explicit configuration |
| **Logging** | Structured standard-output and file logs, plus an optional Basic Auth log viewer independent of OpenTelemetry |
| **Observability** | Request correlation, OpenTelemetry traces/log export, metrics and local Grafana tooling |
| **Operations** | CLI commands, health/readiness endpoints, graceful shutdown, Docker, Compose and Tilt development workflows |

Application modules require PostgreSQL; background jobs use Redis. Optional
capabilities are enabled through configuration. See the [verified support matrix](SUPPORT.md#verified-scope)
for backend and provider coverage. Phone/SMS authentication is reserved for future
work and is not part of the current sign-in flow.

## Create an application

You need the Go version declared in [go.mod](go.mod) and access to the template
repository. Git is required for local-checkout generation; Docker is used by the
local development walkthrough.

### Install from a release

Install the released CLI:

```sh
go install github.com/wisalgroup/labena/cmd/labena@latest
```

Use `@v0.3.2` instead of `@latest` for reproducible team setup.
Add Go's binary directory to your current shell if `labena` is not found:

```sh
go_bin="$(go env GOBIN)"
export PATH="${go_bin:-$(go env GOPATH)/bin}:$PATH"
labena --version
```

Add the resolved directory to your shell configuration to keep it on `PATH` in
future terminals. The public Labena repository needs no GitHub authentication.
See the [installation details](docs/creating-applications.md#install-the-command).

From the directory that should contain your new application:

```sh
labena new myapp \
  --module github.com/your-org/myapp \
  --name MyApp
```

Replace `github.com/your-org/myapp` with your application's Go module path.

The installer rewrites the Go module and imports, sets application branding, creates
a local quickstart `.env`, records template provenance in `.labena.json`, regenerates
documentation and verifies the build. It refuses to overwrite an existing directory.
It does not create a GitHub repository or copy the template's Git history.

### Use the current checkout

To try local changes, run this **from the Labena checkout**:

```sh
go install ./cmd/labena
```

Then create the app from your preferred parent directory, replacing the source path
below with the absolute path of that checkout:

```sh
labena new myapp \
  --module github.com/your-org/myapp \
  --name MyApp \
  --source /absolute/path/to/labena
```

`--source` identifies **the Labena template**, while `myapp` is the new destination.
`--source .` only works when your current directory is the Labena checkout. See
[creating applications](docs/creating-applications.md) for version selection,
private repository access and skipping verification with `--skip-checks`.

## Run locally

Open the generated application:

```sh
cd myapp
```

You can also run the following steps from the Labena checkout to evaluate it.

Start the disposable PostgreSQL and Redis services described in
[installation](docs/getting-started.md#start-local-dependencies). The tutorial uses
host ports `55432` and `56379`. Then select the matching profile, apply migrations
and start the app:

```sh
export APP_ENV_FILE=.env.quickstart.example
go run ./cmd/cli migrate up
go run ./cmd/app
```

In another terminal:

```sh
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

Open the running [API documentation](http://127.0.0.1:8080/docs) or its
[OpenAPI schema](http://127.0.0.1:8080/openapi.json). Both are generated from the
registered HTTP operations.

The quickstart disables authentication and optional integrations so you can run
without provider credentials. Protected endpoints become available after configuring
[Identity](internal/modules/identity/README.md). Mail connects only when sending;
application startup never runs migrations. The full `.env.example` enables more
capabilities and requires their credentials.

To explore a module without starting PostgreSQL or Redis:

```sh
go test ./tests/modules -run TestFirstModule -count=1
```

## Make it yours

Set `APP_NAME` to control the API title, mail defaults, log viewer and telemetry
service identity. Mail sender and brand names can be overridden independently.
The installer sets these defaults when creating your application.

Database names, token issuers/audiences, Redis namespaces, queues and deployment
resources remain explicit operational settings. A display-name change does not
rename persisted identifiers. Use the [configuration guide](docs/configuration.md)
and [complete reference](deployments/configuration.md) when enabling capabilities.

## Application design

```text
cmd/
  app/                 HTTP, workers and local event consumers
  worker/              Dedicated background execution
  scheduler/           Recurring enqueue operations
  cli/                 Application commands
  labena/              Project installer; omitted from generated applications
internal/
  app/                 Composition, process selection and resource lifecycle
    config/            Environment settings and owner configuration
  modules/
    identity/          Account and authentication operations
    shared/            Shared business entities, enums and event contracts
  platform/            Shared application capabilities and transport integration
pkg/                   Reusable technical packages
examples/              Compiled usage examples and starter modules
tests/                 Application, business and integration suites
docs/                  Guides and the static documentation site
deployments/           Container and development environment configuration
```

Modules construct their local collaborators and expose a package-level `Register`
function. App owns connections, creates authentication before protected targets,
registers modules and manages shutdown. Services receive their actual collaborators;
module registration borrows `dependencies.Resources` and `dependencies.Targets`.
Recurring schedules live in one application schedule definition.

Business modules own their writes and published event meanings. Platform packages
remain independent of business modules, and `pkg` stays independent of `internal`.
Read [your first module](docs/modules.md), [module registration](docs/module-registration.md)
and the [project conventions](docs/conventions.md) before extending the framework.

## Choose how processes run

| Command | Responsibility |
| --- | --- |
| `go run ./cmd/app` | HTTP, queue workers, notification dispatch and local event consumers |
| `go run ./cmd/worker` | Queue workers, notification dispatch and local event consumers |
| `go run ./cmd/scheduler` | Recurring enqueue operations; run one instance per schedule set |
| `go run ./cmd/cli <command>` | Migration, inspection or maintenance, then cleanup |

Entrypoints select processes explicitly in Go. Local events stay inside their
publishing process; Redis jobs can be processed by another worker process.
Notification intent can commit with a business SQL transaction. Redis enqueue and
external delivery have separate failure boundaries. The [events](docs/events.md),
[queues](docs/queues.md) and [transactional notifications](docs/transactional-notifications.md)
guides explain these guarantees.

For deployment, follow [deployment and recovery](deployments/README.md),
[application lifecycle](docs/application-lifecycle.md) and
[release readiness](docs/release-readiness.md). Supplied development manifests and
sample credentials are for local use; configure ingress, secrets, dependency
readiness, backups and retention for your environment.

## Documentation

Start with the [introduction](docs/overview.md), then follow a complete workflow:

- [Your first module](docs/modules.md): register and test a small HTTP capability.
- [A persisted HTTP and CLI operation](docs/persistent-workflow.md): reuse a business operation across transports.
- [Account onboarding](docs/onboarding.md): connect events, jobs, notifications and mail.
- [Storage and media](internal/platform/media/README.md): manage visibility and content access.
- [Logging and the log viewer](docs/logging.md): inspect activity with or without an observability stack.

Build and browse the searchable documentation locally:

```sh
go run docs/build.go
go run docs/serve.go
```

Open [the local documentation](http://127.0.0.1:8091/overview.html). The site includes
keyboard search, topic navigation, light/dark themes, diagrams and Markdown downloads.
It can be hosted as static files; see [publishing with GitHub Pages](docs/README.md#publish-with-github-pages).
The live application API at `/docs` is served by the Go application separately.

## Testing and maintenance

```sh
go test -race ./...
go vet ./...
go build ./cmd/... ./examples/...
```

CI also verifies documentation, generated applications, the container image and
vulnerabilities. Its integration job runs against disposable PostgreSQL and Redis.
Use the [testing guide](docs/testing.md) and [integration script](scripts/verify-integration.sh)
for backend setup; tests that skip an unavailable backend have not verified it.

Keep `.labena.json`, the original license and a record of the upstream changes you
incorporate. Follow [adoption and updates](docs/upgrading.md) for porting fixes into
your customized application. Updating Go dependencies alone does not update copied
framework source.

## Contributing and security

Contributions should follow [CONTRIBUTING.md](CONTRIBUTING.md) and the
[architecture contract](docs/conventions.md). Report reproducible, non-sensitive bugs
through [GitHub issues](https://github.com/wisalgroup/labena/issues).
Use the private reporting route in [SECURITY.md](SECURITY.md) for vulnerabilities.
Maintenance scope and compatibility commitments are documented in [SUPPORT.md](SUPPORT.md).
See [release notes](CHANGELOG.md) for the current release and adoption changes.

## License

Labena is available under the [MIT license](LICENSE).
