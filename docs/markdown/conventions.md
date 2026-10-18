# Project conventions

This is the canonical architecture guide for contributors. Use these defaults for
new code and refactoring. The other guides explain usage; they should not introduce
competing patterns. This is a modular application template built with explicit Go
composition, not a runtime dependency container.

## Read this alongside the guides

This page is the architecture contract. Use [your first module](modules.html) for
an introduction, [module registration](module-registration.html) for assembly,
and [dependency injection](dependency-injection.html) for ownership examples.
The rules below describe the current design, not a migration roadmap. Historical
reviews and plans do not override current APIs or this contract.

## 1. Package responsibilities

| Kind | Owns | Examples |
| --- | --- | --- |
| Application composition | Resource ownership, business graph and process selection | `internal/app` |
| Environment configuration | Loading, defaults and translation to owner settings | `internal/app/config` |
| Business module | Business rules, use cases, persistence and transports | `internal/modules/identity` |
| Shared business definitions | Entities, enums, published events and common contracts | `internal/modules/shared` |
| Platform capability | Shared application behavior with its own public types | `internal/platform/notification`, `media`, `mail` |
| Reusable package | Technical behavior independent of this application | `pkg/database`, `storage`, `mailview`, `push`, `hasher` |

Handlers call business services. Services call their repositories and shared
capabilities. Platform capabilities compose reusable packages. App constructs and
connects them. `pkg` must never import `internal`.

`internal/modules` groups business code; it is not a Go package or a registration
catalog. Its `shared` directory contains definitions, not a registrable module:
it has no `Module`, `New` or `Register`. Platform packages never import
`internal/modules`, including shared business definitions. Shared definitions
must not import business modules, platform packages or app. App configuration
lives in `internal/app/config`; owners receive translated settings, never the
environment configuration package.

Platform capability cores do not import app or `dependencies.Resources`. Their
integration packages connect them to application transports and registries. Keep
this separation to avoid import cycles and permit independent use.

The authenticated `Principal` belongs to `internal/platform/auth`, independently
of tokens, transports and business entities. Identity owns sign-in, verification
and session persistence. Its HTTP delivery also owns user-specific media,
notification and push-subscription endpoints, translating the principal into
business owner/recipient references. Platform media integration retains generic
public/signed content serving; other platform integration handles CLI, tasks and
migrations without business identity assumptions.

Use a direct service call for a required business operation. Use an event for a
reaction with an explicitly accepted delivery guarantee. Do not introduce an event,
job, repository or interface just to fill out a directory structure.

## 2. One business-module shape

```text
internal/modules/<module>/
  register.go            Construction and registration
  service/
    service.go           Public service interfaces
    <subject>.go         Private implementation and constructor
    <use_case>.go        Substantial method on the same receiver, when useful
  repository/
    repository.go        Public persistence interfaces
    <subject>.go         Private implementation, constructor and queries
  delivery/
    http/                Routes, requests, responses and handlers
    cli/                 Commands
    events/              Event handlers
    jobs/                Job handlers
  jobs/                  Task payloads, names and constructors
  notifications/         Named notification content and channel choices
  mailviews/             Email content using the shared renderer
  integrations/          External provider integration
  domain/                Shared module policies and operation errors, if needed
  migrations/            Module-owned SQL

tests/<module>/           Business integration and public-behavior tests
```

Create only the directories required by actual behavior. A transport-only module
can start with a single `Register` function in `register.go`. A larger use case
can occupy another file without creating another service. Keep nested module
folders; do not flatten them or introduce a service per table or endpoint.

## 3. Type ownership

| Type | Location |
| --- | --- |
| Business entity and its invariants | `internal/modules/shared/entities` |
| Shared business enum | `internal/modules/shared/enums` |
| Polymorphic business type name | `internal/modules/shared/entities/entity_types.go` |
| Operation input or result | Owning module's `service` package |
| SQL projection or repository result contract | Owning module's `repository` package; export only what its service must consume |
| HTTP request or response | Owning module's `delivery/http` package |
| Authenticated caller identity | `internal/platform/auth.Principal` |
| Published business event | `internal/modules/shared/events`; publishing module owns meaning and version |
| Common event occurrence metadata | `internal/modules/shared/events` |
| Domain error | Owning module's `domain` package or the entity that enforces the invariant |
| Capability record, input, status or error | Owning platform package |

Shared business entities are an intentional convention of this monolith. Their
owning module still controls writes and business operations. A shared type is not
permission to bypass its owner. Cross-module SQL reads must be deliberate and
remain in repositories.

Published event contracts share `internal/modules/shared/events` so consumers can import
their definitions without depending on a producer's implementation. Keep this
package limited to payloads, topic constants, occurrence metadata and constructors.
Use IDs and event-specific values instead of embedding entities; do not import
services, repositories, platform capabilities, application config or transports.
The publishing module controls schema compatibility and the topic version.
Handlers remain in each consuming module's `delivery/events` package.

Domain errors stay with the behavior that defines them. Reuse `pkg/apperror` for
classification and safe error codes; do not create a global `shared/errors` catalog.
Across a future service boundary, publish stable schemas and codes rather than
sharing implementation errors or assuming Go error identity survives transport.

Entities may carry Bun mappings when the shapes agree. Do not add a parallel
`models` package and conversion layer by default. Use a separate private query
shape only when persistence actually differs. Viewer-dependent values such as
unread counts are operation results, not intrinsic entity state.

HTTP inputs own Huma schema/validation tags. Typed handlers register once through
`huma.Register` for routing and OpenAPI. The platform runs Huma on `http.ServeMux`;
modules receive native Huma groups. Authentication
and authorization read the principal from `context.Context`.
Services validate business rules for
all callers, including jobs and CLI. Responses explicitly select public fields;
do not expose an entity merely because it has JSON tags.

HTTP resolves `api.ClientIP` once using explicit trusted proxy CIDRs; modules
never reinterpret forwarding headers. Empty trust uses the immediate connection.
`api.PeerIP` always retains that transport peer. Client addresses can select rate
budgets but never establish a principal or grant authorization. Administrative
HTTP endpoints reuse `platform/adminhttp` and stay outside business CORS policy.

Within a business module's `delivery/http`, keep handlers in `*_handler.go`,
request shapes and input mappings in `requests.go`, response shapes and projections
in `responses.go`, and typed operation registration in `routes.go`.

Media's `Asset` and Notification's `Record` stay with their capabilities. Shared
entities is not a destination for every struct. Keep recipient, owner and audience
semantics explicit; a `(type, id)` reference does not authorize access. Use the
shared entity-type constants at business call sites, not module-local literals.

Media owners and notification recipients share one reference
syntax: type names match `[a-z][a-z0-9_.-]{0,63}`; IDs are nonempty UTF-8 strings of
at most 200 bytes, without surrounding whitespace or control characters. The
implementation lives in `platform/internal/reference`, not a new public base type.
Capability-specific errors and authorization remain with each owner.

## 4. Contracts and names

| Role | Convention | Example |
| --- | --- | --- |
| Business service interface | `<Subject>Service` | `UserService` |
| Private service implementation | `<subject>Service` | `userService` |
| Constructor beside implementation | `New<Subject>Service` | `NewUserService` |
| Repository interface | `<Subject>Repository` | `UserRepository` |
| Private repository implementation | `<subject>Repository` | `userRepository` |
| Constructor beside implementation | `New<Subject>Repository` | `NewUserRepository` |
| Operation input | `<Operation>Input` | `SendMessageInput` |
| HTTP shapes | `<Operation>Request`, `<Subject>Response` | `SendMessageRequest` |
| Failure | `Err<Meaning>` | `ErrNotFound` |

Name values for what they contain: account identifiers use `UserID`, event timestamps
use `OccurredAt`, and a notifier dependency is `Notifier`. Named storage connections
are `Disks`, with `DefaultDisk` selecting one. Use `SignIn` consistently for the
operation; `SocialSignInInput` contains provider sign-in context.

Use `Get` for one persisted record and `List` for collections, including paginated
ones. Keep `pagination.Pagination` for paging parameters and `pagination.Page[T]`
for results. Use `Valid() bool` for membership/validity checks, `Validate() error`
for diagnostic validation, and `MarkReady`/`MarkDeleted` for persisted state changes.
Name same-typed interface parameters so their order and purpose are clear in editor
hints. Transport implementation types stay private when registration functions are
the public API. Request `Input(...)` methods keep mapping beside their request types;
handlers supply trusted context and perform authorization.

Public business contracts live in `service/service.go` and
`repository/repository.go`, not separate `contracts` packages or shared contract
catalogs. Multiple consumers import the owner's contract. Keep each contract
cohesive; do not create an interface per consumer or mirror every private helper.

Platform capabilities may expose meaningful concrete types such as `Notifier`,
and `Library`. Reuse existing package contracts such as `mail.Sender`, `mail.Mailer`
and `messaging.Publisher`; do not redeclare them in each module. These two shapes
are intentional: business APIs and reusable capability APIs have different owners.

Use descriptive operation verbs. Within an authentication service,
`RequestSignInCode` is clearer than `Send`. Avoid `Impl`, `Manager`, generic `Service`
and generic `Repository` when a business subject supplies a useful name. Keep
established third-party names instead of wrapping them solely to rename them.

`Config` describes settings; `Options` holds optional constructor controls and
collaborators. Required small dependency sets are explicit constructor arguments.
Configuration translation helpers name the returned owner settings, such as
`ServerConfig`, `ConnectionConfig` and `SMTPConfig`; native `Options` types retain
their names. Media settings use `media.Config` and `media.DiskConfig`.
A larger cohesive options struct is acceptable; do not introduce another resource
bundle for every module.

Use `New<Subject>` for named event, task and notification constructors, such as
`events.NewUserRegistered`, `jobs.NewWelcome` and `notifications.NewWelcome`.
`mailviews.NewWelcome` constructs its email content. Construction alone performs
no publication or delivery; callers use `Publish`, `Enqueue` or `Send` explicitly.
Infallible constructors return the value directly. Renaming a constructor does
not change its serialized fields, notification type or stable occurrence key.

## 5. Construction, registration and lifetime

- `New…` validates and assembles without network I/O or background work.
- `Open…` acquires owned resources, including background exporters, and makes cleanup explicit.
- `Register…` may construct local collaborators and installs definitions before execution; it performs no I/O or business work.
- `Run(ctx)` owns execution until completion or cancellation.
- `Close…` releases owned resources after their users finish; retain native terms such as tracing’s `Shutdown`.

Module assembly borrows `dependencies.Resources` and passes specific dependencies
to services. Business services do not retain the complete bundle. App fills it
before module construction; modules never mutate it or close its clients.
Business-to-business dependencies are explicit arguments, not entries in Resources.

Each business module exposes a package-level `Register` function in `register.go`.
It constructs module-local collaborators and installs selected transports once.
There is no Module struct or duplicate module configuration schema. App owns the
environment configuration and passes specific settings and business services as
explicit arguments. Collaborators needed before registration, such as Identity's
AuthService, are constructed in app using their existing owner constructors.
Services keep validating their policies and dependencies. App registers each
module once; registration does not start work or acquire resources.
Invalid runtime configuration and missing capability dependencies fail at assembly,
without a late nil dereference. Native transport helpers retain explicit programmer
preconditions; panics are reserved for misuse such as invalid static defaults. Disabled capabilities have documented nil/no-op behavior.

`tracing.Open` owns a batch worker and requires `Shutdown`. `messaging.RegisterRunner`
installs router lifecycle hooks before handlers; its returned runner starts work only
in `Run`. The app's bounded `messaging.LocalBus` rejects overloaded publication and
settles failed deliveries after one retry cycle; it remains best-effort. `cli.ScheduledCommands` has a usable zero value, implements the native Asynq
handler, and is installed explicitly with `mux.Handle(cli.CommandTask, commands)`.
There is no second scheduling registry or wrapper constructor to learn.

Application-specific configuration can translate pure owner policies, such as
`cfg.Auth.SessionPolicy()`. The owner defines validation; configuration
and service constructors reuse that validation. Owners never import application config.


`Targets` contains borrowed Huma APIs and native command/job/event registries. Nil targets omit unused
transports. Validate required collaborators before mutation, register sequentially,
and discard partial application construction on error. No discovery, reflection,
service locator, module startup hooks or lazy application callbacks are needed.

The intended registration location is `register.go` for HTTP, CLI, jobs, events,
migrations. Scheduling is the intentional exception: app/schedules.go centrally
declares recurring timing, feature conditions and enqueue options using existing
`asynqx.TaskSchedule` and `asynqx.CommandSchedule` definitions. Modules retain task
handlers and command factories. App validates and installs schedules after module
registration, and owns scheduler execution and operational configuration.

App owns resource acquisition, process readiness, ordered drain and reverse-order
cleanup. Each executable loads configuration and supplies explicit `app.Processes`
flags. HTTP and Workers automatically include local event consumers; Workers also
includes notification dispatch. Scheduler is a separate selection. No process
selection comes from environment variables. The app exposes only Serve and Execute;
Execute uses the supplied selection only for its serve command. Zero processes is
valid for ordinary commands and rejected when serving. `internal/app/modules.go`
contains only explicit business registration calls. `auth.go` constructs Identity's
AuthService and binds notification delivery's active-session check. Identity's
HTTP delivery calls that AuthService directly for session-aware push registration.
`open.go` owns cross-cutting
security wiring and failure cleanup; `infrastructure.go` opens clients, constructs
capabilities and registers their transports. One private `application` holds
configuration, Resources, Targets, native runner handles and the selected process
list. Registries live in Targets; console execution uses Targets.RootCommand. App
helpers operate on this state, with no separate platform, console or process-plan
state objects. Only app accesses the application; modules borrow Resources and
Targets and receive explicit business collaborators. Business modules
and platform packages never import `internal/app` or its configuration package. Application CLI help intentionally constructs the configured application too.
The standalone `cmd/labena` project installer owns no application state and never
loads runtime configuration; its help is independent of application assembly. Missing
required resources fail construction or serving validation; intentionally disabled
features have documented unavailable behavior. Preserve supported offline assembly
without treating it as permission to serve with missing required dependencies.

## 6. Transactions, errors and delivery

Normal business transactions belong to service operations. Repositories resolve
the executor from context and join the same database transaction. DB and
Transactions must use the same pool. External network or blob operations do not
become atomic by receiving a transaction-bearing context. Framework publication,
app queueing, mail and external notification/media effects reject that context
using `database.RequireNoTransaction`. Native clients bypass this policy. The
guard does not provide a generic outbox; post-commit publication can still fail.

Identity's security operations are explicit exceptions: failed-proof counters and
refresh-reuse revocation may need to commit even when authentication is denied.
Preserve their repository-owned transaction rules. Do not mechanically wrap them
in an outer transaction.

Identity creates accounts through authenticated OTP or provider proofs, in the
same transaction as the session. `SignInResult` distinguishes a new account from a
returning one and includes the user. Registration events publish after commit;
publication failure never turns a committed signup into a failed response.
HTTP owns peer extraction and formatting; the service owns the sequence.

| Mechanism | Meaning |
| --- | --- |
| Direct service call | Required operation; caller receives its outcome |
| Local Watermill event | Process-local, best-effort reaction |
| Asynq enqueue | Background work after queue acceptance; not atomic with business SQL |
| Notification `Enqueue` | SQL delivery intent that can commit with the business change |
| Notification `Send` | Immediate attempt; caller handles uncertainty |

Use stable occurrence keys where retries must converge. Persisted intent is not
proof of delivery, provider acceptance is not user receipt, and socket delivery is
not a read acknowledgment. Do not hide these distinctions behind a universal
`Dispatch` abstraction. Module notifications own content; platform delivery owns
channel retries and receipts.

Use `apperror` for intentional safe application failures. Wrap operational errors
with context and `%w`; preserve `errors.Is/As`. Typed HTTP handlers return
`response.Error(ctx, err)` to translate service failures into RFC 9457 problem details.
Register operations directly with `huma.Register`. Huma middleware uses
`response.WriteError`; native Huma status errors can be returned directly.
Application JSON responses use `response.JSON[T]` and its status constructors.
The typed result lives in `data`, alongside optional operation-specific `meta`.
Errors retain flat Huma problem details, an `errors` list and optional `meta`.
Use `apperror.InvalidFields` for multiple
application validation messages. Bodyless responses use `response.Empty`;
custom outputs and streams retain native Huma types. Declare success statuses on
the operation too, so OpenAPI matches runtime behavior. Server-generated request
IDs and API versions are returned only in `X-Request-ID` and `X-API-Version`
headers, including for errors, empty responses and streams. Request IDs remain
available in request context and logs; clients capture the header for diagnostics.
Infrastructure handlers retain standard HTTP semantics.
The optional local log viewer is a platform HTTP capability mounted by app. It
borrows configured local log channels through the logging handler decorator,
keeps bounded process-local copies and reads only configured log files. It owns
no listener or background worker, and uses separate Basic Auth credentials.
Services do not write HTTP responses or expose provider secrets.
Routine logs and spans use bounded error categories and safe public codes, never
arbitrary driver/provider error text or payloads. Attempt spans describe attempts;
the outer delivery boundary logs the final retry-cycle failure once.

A committed business operation followed by an optional failure needs an explicit
result; it must not look like the whole operation rolled back.

## Verification and change discipline

The [support policy](../SUPPORT.md) defines template adoption, compatibility and
release-note requirements. Document changes that adopters must port into their
customized applications, including security corrections and retained-state
migrations; use the [update guide](upgrading.md) for the consumer workflow.

Keep service/repository packages and app free of test files. Put business
integration and public-behavior tests in `tests/<module>`; reusable package and
platform tests may stay beside their owners. Moving tests must preserve coverage.

Protect behavior, transaction boundaries and authorization. Architecture checks
should enforce dependency direction and ownership, not arbitrary file counts.
Examples must compile against shipped APIs; proposed APIs must be labeled.

For a normal feature: define the shared entity, implement repository behavior,
implement the service operation, add the transport and register locally. Add a new
abstraction only when it removes repeated knowledge or supports an actual variation.
Prefer one coherent convention over another convenience wrapper.

## Automated checks

`go test ./tests/architecture` scans production imports in `internal`, `pkg` and `examples`,
including build-tagged files. It checks that:

- `pkg` does not import application internals.
- Business modules and platform packages do not import app or environment config.
- Platform packages do not import any business modules, including shared definitions.
- Shared definitions do not import platform or business implementations.
- Business services/repositories do not import app assembly, Resources/Targets,
  HTTP adapters, delivery packages, Huma, Gin or Cobra.
- Repositories do not import business services.
- Mail, Media, Notification and Messaging cores do not import business modules,
  app or Resources/Targets; application wiring belongs in their integrations.
- Production packages do not import Gin, Gin middleware or the Huma Gin adapter.
- Independent mail/storage examples may load `app/config`, but do not import app assembly.
- App and business service/repository packages contain no test files.

The first-module tests compile the Catalog starter, exercise its route without
infrastructure and ensure the copyable guide matches the compiled source. Existing
CI runs both checks through `go test -race ./...`. Names, file counts and every
possible cross-module relationship are deliberately left to review; an import
check cannot prove business correctness or safe authorization.
