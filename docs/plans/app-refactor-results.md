# App refactor results — 2026-10-06

> Historical design record. The current implementation uses eager application
> construction for every CLI invocation, shared `dependencies.Resources`, and
> module-owned command/SQL registration through `dependencies.Targets`. Offline
> help, lazy command callbacks and separate catalogs described below were replaced
> by the simpler design in [application lifecycle](../application-lifecycle.md).

> Superseded assembly design: modules now own construction, commands and SQL
> declarations. See the current [app guide](../../internal/platform/app/README.md)
> and [module registration](../module-registration.md). The lifecycle fixes below
> remain in place, with the Watermill runner moved into platform/messaging.


The focused App review led to a lifecycle and composition refactor. The package
now has eleven production Go files, down from thirteen in the reviewed working tree.
Tests remain split by observable behavior. This work preserves the existing dirty
working tree and does not change dependencies, database migrations, HTTP endpoints,
queue payloads, or environment role names.

## Correctness repairs

- Event shutdown now stops handler admission, explicitly closes Watermill, and joins
  admitted calls. A native timeout cannot release app resources under an active handler.
- Empty subscription sets start no native delivery loop and stop successfully.
- Partial subscription failure returns its original error without publishing readiness.
  The adapter accounts for the pinned Watermill version's unstarted-handler wait group.
- Completed runner errors survive a sibling's shutdown timeout.
- Run, Serve, and console serve enforce the same installed-module database requirement.
  Construction without a database remains available for route inspection.

## Composition changes

`App.infra` privately owns runtime clients. `App.targets` borrows registration targets.
`App.modules` retains concrete business services used by transports and cross-module
operations. The neutral dependencies package now contains Registration only: no
runtime infrastructure, transaction interface, console root, or migration registry.

Business construction and typed interactive command bindings live in
[modules.go](../../internal/platform/app/modules.go). Identity feature construction
has a named helper; Media's use of the same Notifications instance remains explicit.
Migrations stay in a resource-free catalog; recurring timing stays in schedule.go.
No container, module lifecycle interface, discovery mechanism or generic async runtime
was added.

Mail and rate-limiter construction moved into infrastructure.go. Console execution
lives in console.go; migration commands live beside their catalog in migrations.go.
The extra forwarding registration phase and retained migration registry were removed.

## Command behavior

There is one native Cobra tree and one execution. Typed callbacks acquire the app
only after input decoding and reuse it for the invocation. Cleanup surrounds the
invocation, including output failure and panic; it does not rely on Cobra hooks.
Command definitions no longer contain nil business services or rewritten callbacks.

One immutable environment snapshot supports explicit decoding scopes. Help/completion
read command-availability switches only. Migrations need database/logging settings;
queue administration needs its Redis URL. Invalid unrelated typed settings cannot
block those paths. The zero snapshot uses defaults without reading process values.

Business commands still construct the configured application, since their service
contracts use shared infrastructure. Run still selects execution rather than inferring
a dependency graph or changing acquisition. Those limits are explicit in the app guide.

## Internal API migration

- `app.Services` becomes `app.Roles`; `Queues` becomes `Workers`, and `Events` becomes
  `Subscribers`. All repository callers and current guides were updated.
- Module Register functions handle runtime routes and scheduled factories. Bind
  interactive commands in `moduleCommands`; add SQL to `registerMigrations`.
- Prune command constructors accept a typed function. Notifications commands accept
  typed Actions. Queue commands accept a callback returning a borrowed Inspector.
- App builds its interactive console when Execute is requested, not during New.

APP_ROLES names and the explicit scheduler policy are unchanged. GoChannel remains
local and best-effort; this work adds no outbox or durable event delivery. Cancellation
and native cleanup remain cooperative; a timeout cannot forcibly stop a goroutine.

## Verification

- `go test -race ./...` passed with disposable PostgreSQL 17 and Redis 7.2, including
  module workflows, cache/transaction behavior, queue delivery and administration.
- `go vet ./...` passed.
- Focused messaging/console regressions passed five repeated race-enabled runs.
- Tests cover empty subscriptions, slow handlers, partial startup failure, late
  admission, offline help under malformed backend settings, invalid input before
  acquisition, one execution of hooks/aliases/flags, original streams, snapshot
  isolation and serving prerequisites.
- Rebuilt all 38 documentation pages and checked their local links and anchors;
  no broken generated-page links remain. `git diff --check` passed.
- MySQL, Redis Cluster and external S3 suites were not run. No deployed workload
  was changed. Race detection covers exercised paths, not every possible schedule.
