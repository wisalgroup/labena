# Directory structure

Business modules own operations and data access. App assembly connects their
explicit dependencies; reusable packages own technical behavior. The
[project conventions](conventions.html) define the current defaults and ownership rules.

## Find a capability

```text
cmd/
  app/                    HTTP, workers and local subscribers
  worker/                 Workers and local subscribers
  scheduler/              Recurring enqueue only
  cli/                    Console entry point
internal/
  app/                    Composition, process selection and lifecycle ownership
    config/               Typed environment settings and owner translations
  modules/
    identity/             Accounts and authentication
    shared/               Definitions shared across business modules
      entities/           Owned business entities, including accepted Bun mappings
      enums/              Business values grouped by concept
      events/             Published business event contracts and occurrence metadata
      domain/             Common entity identity and value objects
      migrations/         Global and published infrastructure SQL
  platform/
    auth/                 Authenticated caller identity, independent of business entities
    notification/         JSON notifications, email and durable per-channel delivery
    media/                Public/private assets and storage recovery
    lifecycle/            Process readiness, ordered drain and resource cleanup
    dependencies/         Shared resources and borrowed registration targets
    api/                  HTTP decoding, responses, middleware and health routes
    mail/                 Message types, SMTP, tracing and optional queued sending
    messaging/            Watermill GoChannel integration
    cli/                  Console and scheduled-command integration
    seeders/              Explicit seed operations
pkg/
  asynqx/                 Native Asynq integration and middleware
  database/               Connections, transactions and telemetry
    migrate/              Reusable Goose registration and execution
  cache/                  Cache contracts, drivers and telemetry
  logging/                Structured logging and context correlation
tests/                    App, cross-module and queue integration suites
examples/                 Standalone capability examples
deployments/              Local and deployment configuration
docs/                     Guide sources and static site
```

This is a map of the main responsibilities, not an exhaustive package listing.
`modules` groups business code without discovery or a registration catalog.
`modules/shared` holds definitions and is not registered as a module. Platform
packages never import business modules, including their shared definitions.
Shared definitions import neither those modules, platform packages nor app.
Notifications implements database, email and push delivery; Media implements uploads, public/private
access, internal/direct URLs and deletion recovery. SMS and media conversions remain extensions.

## Package index

Use the existing capability directly when it fits. App and module assembly pass
borrowed resources; independent callers construct only the collaborators they need
and own any resources they open. `internal` packages are usable within a copied
application template, not as public libraries imported by an unrelated Go module.

| Application or platform package | Responsibility and guide |
| --- | --- |
| `internal/app`, `platform/lifecycle` | [Construction, process ownership and shutdown](application-lifecycle.md) |
| `internal/app/config` | [Typed configuration and defaults](../deployments/configuration.md) |
| `dependencies` | [Borrowed Resources and registration Targets](dependency-injection.md) |
| `api` | [Routing](routing.md), [validation](requests.md), [middleware](middleware.md) and [responses](responses.md) |
| `auth` | Transport-independent authenticated user/session principal |
| `cli` | [Cobra commands](console.md) and [scheduled commands](scheduling.md) |
| `mail`, `mail/integration` | [SMTP and optional queued mail](mail.md); integration installs the existing worker handler |
| `media`, `media/integration`, `media/migrations` | [Media assets](../internal/platform/media/README.md), transport registration and owned SQL |
| `notification`, `notification/integration`, `notification/migrations` | [Channel intent and delivery](../internal/platform/notification/README.md), transports and owned SQL |
| `messaging`, `messaging/middleware` | [Local typed events](events.md), native Watermill execution and instrumentation |
| `seeders` | [Explicit named seed operations](seeders.md) |
| `testdb` | [Disposable integration schemas](testing-database.md); test-only, never a production dependency |
| `internal/reference` | Private syntax checks shared by Media and Notifications; not authorization or a model superclass |

| Reusable package | Responsibility and usage |
| --- | --- |
| `apperror` | [Safe application errors](responses.md) and inspectable causes |
| `asynqx`, `asynqx/middleware` | [Native tasks](queue-jobs.md), routing, worker/scheduler lifetime and instrumentation |
| `cache` and its memory/database/redis drivers | [Bounded cache operations](cache.md); backend semantics remain explicit |
| `database`, `database/migrate` | [Connections](database.md), [transactions](database-transactions.md) and [Goose migrations](migrations.md) |
| `hasher` | Standalone string hashing; see pkg/hasher/README.md; Identity does not use passwords |
| `httpserver` | [HTTP lifetime and upgraded-connection drain](application-lifecycle.md) |
| `httpclient` | [Tagged outbound requests, body encodings, middleware and provider integrations](../pkg/httpclient/README.md) |
| `logging` | [Named log outputs and context correlation](logging.md) |
| `mapper` | Typed collection transformations; inspect with `go doc ./pkg/mapper` |
| `mailview` | [Embedded HTML/text rendering](mail.md) without a mail transport |
| `metrics`, `tracing` | [Optional OpenTelemetry providers](observability.md) with explicit ownership |
| `otp`, `token` | Cryptographic code/token primitives; [Identity](../internal/modules/identity/README.md) owns account policy and session records |
| `pagination` | [Bounded page inputs and responses](responses.md) |
| `push` | [Independent Expo sending and receipt lookup](../pkg/push/README.md) |
| `ratelimit` | Shared Redis admission; [Identity's usage](../internal/modules/identity/README.md#shared-rate-limiting) |
| `redis` | Explicit Redis connection setup; see pkg/redis/README.md; queue/cache/limiter ownership remains separate |
| `storage` | [Native Go CDK buckets](filesystems.md); Media adds metadata and recovery |

For package-level APIs, run `go doc ./internal/platform/notification` or
`go doc ./pkg/storage` from the repository root. These commands inspect source
without constructing the application. Package READMEs contain additional native
usage; they are not all published as separate website pages.

## Follow startup

| Read | Responsibility |
| --- | --- |
| `cmd/app/main.go` | Process entry point and selected services |
| `internal/app/app.go` | Public entry points and private application state |
| `internal/app/infrastructure.go` | Acquire resources and register their cleanup |
| `internal/app/auth.go` | Construct authentication and bind notification delivery's active-session check |
| `internal/app/modules.go` | Call business registration functions |
| `internal/app/open.go` | Initialize the application, bind security operations and clean up failures |
| `internal/app/processes.go` | Select execution, readiness dependencies and shutdown phases |
| `internal/app/schedules.go` | Declare recurring timing and conditions |
| `internal/app/console.go` | Execute commands, migrations and table publication |
| `internal/platform/media/library.go` | Construct and use the media Library |
| `internal/platform/media/integration/register.go` | Register a constructed Library with selected transports |
| `internal/modules/identity/delivery/http/routes.go` | Register account, user media, notification and session-bound subscription endpoints |
| `internal/platform/lifecycle/lifecycle.go` | Supervise readiness, staged drain and resource cleanup |

Configuration loads before serving. Registration does not perform I/O or start
workers. Open functions acquire connections; New functions assemble borrowed
dependencies. App owns cleanup. Use the documented constructor and registration contracts for each capability.
All CLI commands, including help, construct the application before execution.
See [lifecycle](application-lifecycle.html) and [registration](module-registration.html).

## Grow a module only when needed

The [first module tutorial](modules.html) starts with one file. A module with
persistence and several transports can grow into:

```text
internal/modules/<module>/
  register.go             Internal construction and all registrations
  domain/                 Policies and operation errors
  service/
    service.go            Public business-service interfaces
    user.go, auth.go       Private implementations and constructors
    verification.go       Related methods on the same service receiver
    session.go            Substantial authentication use cases
  repository/
    repository.go         Focused persistence interfaces
    user.go, ...          Private repositories, constructors and queries
    transaction.go        Shared transaction helpers
  migrations/             Embedded module-owned SQL
  jobs/                   Native Asynq task types, payloads and constructors
  notifications/          Business notification content and channel choices
  mailviews/              Email content using the shared branded layout
  integrations/           External provider contracts and implementations
  delivery/
    http/                 Requests, responses, routes and handlers
    cli/                  Commands
    events/               Event handlers
    jobs/                 Queue handlers
```

Create only the directories your behavior needs. A matching interface for every
struct, a module registry, and a generic repository are not required. Service and
repository directories contain production code only; Identity's public-behavior
and persistence tests live under `tests/identity`.

## Preserve ownership across shared definitions

Business entities and enums belong in shared; each business module retains ownership
of its operations and writes. Identity follows this convention. Platform capabilities own
their records, inputs, statuses, errors and storage contracts inside their packages.
Mail, Media and Notifications register generic transports through integration packages;
only persisted capabilities own migrations. These cores import neither app nor Resources.
Identity owns user-specific media and notification HTTP operations. The generic
public/signed media content route remains in platform; no platform package imports
shared business definitions.

Entities may carry Bun mappings where the shapes agree. Do not duplicate an entity
merely to populate a models directory. Published business event contracts and
occurrence metadata live in `internal/modules/shared/events`. Producers own the meaning
and version; consumers own handlers in `delivery/events`. Domain errors remain
beside their owning module, entity or platform capability, using `pkg/apperror`
for common classification and safe codes.

Each definition has an owner. That module controls writes, queries, mapping and
invariants. Shared packages contain neither module services nor configuration or
infrastructure setup. Cross-module SQL reads remain deliberate repository-level
coupling, not permission to bypass another module's operations.

`pkg` must not import `internal`. Put reusable technical behavior there; put
application assembly in `internal/app` and shared technical capabilities in
`internal/platform`.

## Locate a change

| Change | Location |
| --- | --- |
| HTTP input/output | Module `delivery/http` |
| Business operation or its inputs | Module `service` |
| Query or persistence mapping | Owner's repository |
| Capability entity, input, error or status | Owning platform package |
| Business event definition | `internal/modules/shared/events` |
| Job constructor / handler | Module `jobs` / `delivery/jobs` |
| Notification content / mail view | Module `notifications` / `mailviews` |
| Event handler | Module `delivery/events` |
| Recurring timing | `internal/app/schedules.go` |
| Queue routing and priorities | `internal/app/config/queues.go` |
| Infrastructure acquisition | App assembly using `pkg` constructors |
| Environment setting | `internal/app/config` |
| Module SQL | `internal/modules/<module>/migrations` |

Register module migration sources through targets.Migrations in register.go. Module and shared SQL
use one Goose history with globally unique timestamp versions. Preserve applied migrations and add new versions for schema changes. Normal
application startup never runs DDL. See [migrations](migrations.html) for ordering
and rollout, and [shared types](shared-types.html) for definition ownership.
