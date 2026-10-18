# Framework reassessment — 2026-10-06

> Historical design record. The current implementation uses eager application
> construction for every CLI invocation, shared `dependencies.Resources`, and
> module-owned command/SQL registration through `dependencies.Targets`. Offline
> help, lazy command callbacks and separate catalogs described below were replaced
> by the simpler design in [application lifecycle](../application-lifecycle.md).

Status: staged direction approved by the owner; implementation is preceded by the
requested [focused App review](app-design-review.md). That follow-up reproduced
event shutdown defects and puts their repair before the CLI/composition refactor.
This reassesses the current working tree against the original framework brief and
the supplied independent App review. It updates future priorities; the earlier
plan remains the implementation history.

## Verdict

The project has a credible reliability foundation, but its developer experience is
not yet coherent enough to call it a broadly usable community framework starter.
The remaining gap is primarily composition and product scope, not missing drivers
or more middleware. A capable fixed application is still doing the work of a
reusable starter.

The prior phases improved error handling, shared contracts, native Asynq usage,
configuration loading, shutdown, instrumentation, tests and documentation. Those
improvements should remain. However, finishing a bounded phase did not establish
that adding/removing a module had become simple. We need that acceptance criterion
before the next restructuring, not another sequence of isolated improvements.

The target remains an opinionated modular-monolith application template, initially
copied and owned by its users. Do not simultaneously invent a plugin framework,
a DI container and an independently versioned library ecosystem.

## Review scope and evidence

Inspected App acquisition, registration, CLI construction and runtime; configuration
loading/validation; dependency bundles; Identity/Media/Notifications assembly and
representative services, handlers, repositories and shared definitions; reusable
capability packages, development profiles, docs and CI/release gates. This is a
structural reassessment, not a complete security audit or a fresh full test run.

Current inventory (Go files, excluding tests from production counts):

| Area | Production files | Production lines | Test files |
| --- | ---: | ---: | ---: |
| App | 13 | 1,641 | 17 |
| Configuration | 17 | 1,019 | 8 |
| Identity | 37 | 3,283 | 16 |
| Media | 14 | 376 | 3 |
| Notifications | 16 | 636 | 4 |
| pkg | 66 | 5,610 | 38 |

Counts describe the snapshot, not quality targets. Media's many small files suggest
an example-cost review; they do not justify flattening every module. App's 13 files
are not themselves a defect.

Concrete evidence:

1. `app/cli.go:consoleDefinition` constructs commands with nil dependencies, replaces
   Run/RunE recursively, then constructs another application and executes another
   command tree. Business command lists are maintained alongside module registration.
2. `app/cli.go:Execute` calls the globally validating `config.Load` before help.
   Reproduced without opening infrastructure:
   `APP_ENV_FILE= QUEUE_REDIS_URL=invalid go run ./cmd/cli --help`
   fails with the queue-configuration error. Offline help is resource-free but not
   independent of unrelated configuration validity.
3. `app/bootstrap.go:openDependencies` opens every configured subsystem and creates
   HTTP and CLI targets before selecting runtime services. `App.Services` explicitly
   selects execution, not acquisition. A worker or ordinary read command therefore
   still pays for unrelated configured capabilities.
4. `app/modules.go:registerModules` constructs all three modules and branches through
   Identity challenge, email, session, password and Google configuration in one function.
5. `dependencies.Dependencies` embeds registration targets and includes runtime-only
   resources. Services do not receive this bundle; the existing boundary is real,
   but storage of the two concerns is unnecessarily conflated.
6. `app/migrations.go` deliberately builds a static migration source list without
   constructing modules. That separation makes database-only migration commands possible.
7. `config/filesystems.go` still defaults to MinIO, although the quick-start and
   development launch settings now disable it. Packaging defaults and core defaults
   are not yet one predictable story.
8. `pkg/storage.Open` statically includes local, S3, GCS and Azure implementations.
   Runtime disabling does not remove their compile-time dependency cost. Existing
   `docs/research/storage-build-cost.md` is evidence of cost, not evidence that a
   driver split has already solved it.
9. CI now has real PostgreSQL/Redis jobs, but its integration script checks nonempty
   variables, not a required set of executed test outcomes. Documentation examples
   were checked manually in recent phases, not all automatically by CI.
10. `go.mod` still uses the local `go-modular` module path. Security reporting,
    license inventory, recovery evidence and release artifacts remain incomplete.

## What to preserve

- Shared entities, enums and event contracts stay in shared. This is an accepted
  coupling decision, including combined Bun/domain entities where already chosen.
- Explicit constructors, small consumer-owned interfaces and ordinary Register functions.
- One app-owned lifecycle; borrowed module dependencies; reverse cleanup; join-before-close.
- Native Gin, Cobra, Asynq and Watermill APIs at their appropriate boundaries.
- Separate tasks and local events; GoChannel remains process-local and best-effort.
- Central queue definitions and application-owned schedule timing.
- Resource-free help/completion and database-only migration construction.
- Meaningful failure/concurrency tests. Do not remove them to make directories smaller.

## Assessment of the independent review

| Proposal | Assessment |
| --- | --- |
| CLI construction is too clever | Agree. This is the clearest simplification target. Preserve its offline behavior while removing recursive callback replacement and double execution. |
| Split infrastructure and registration | Agree. First remove embedding and clarify ownership. A new public container API is not necessary. |
| Break down module composition | Agree. Use named per-module composition functions and explicit typed results, initially in app. |
| Add map[reflect.Type]any with MustGet | Reject. It is still runtime service lookup, even if restricted to composition. It weakens compile-time dependency checks and conflicts with the engineering guide. |
| Remove migration catalog immediately | Disagree. Moving migration registration behind constructed services regresses database-only execution. Keep the small static catalog unless a proven resource-free alternative reduces total complexity. |
| Modules should own transport registrations | Already substantially true. The remaining duplication is the offline CLI tree, not absent module HTTP/task/event registration. |
| Introduce Module structs/New/Register universally | Not justified. It adds a lifecycle-shaped object where ordinary functions already suffice. Consider a typed service result only where several operations need to be passed together. |
| Rename Registration to Registry | Optional terminology, not a meaningful simplification by itself. Registration already describes borrowed targets; multiple registries already exist underneath. |
| Always open infrastructure for --help | Reject. It trades an implementation inconvenience for a worse developer experience and outage behavior. |
| Add BootNone/BootDatabase/BootApplication | Useful as a planning model, insufficient as an implementation. It does not by itself solve dependency binding, argument parsing or duplicate trees. Prove the full command flow first. |
| Group acquisition into generic stages | Use only if it clarifies real ordering. Existing direct sequencing is understandable; wrapping it in four more methods can just hide it. |
| Reduce App to five nested structs | Do not optimize the appearance of App first. Introduce a private state group only when invariants and ownership actually become simpler. |
| Remove small files | Low priority. Keep editable schedules easy to find. File count is not an acceptance criterion. |

The source review's statement that Close drains resources needs precision: Run owns
drain. Close refuses to close dependencies while work remains active. Preserve that.

## Revised priorities

### A. Establish the supported developer workflow — P0, small

**Problem:** We improved individual packages without proving the complete add/remove
module experience. Starter, framework and reusable-library promises are conflated.

**Affected:** README, release/support matrix, quick-start, module guide, plan acceptance.

**Approach:** Keep the current full starter as a reference, but define and exercise
three concrete workflows: an HTTP-only module; a persisted module with CLI; a module
with a task or local subscriber. Record all edits and infrastructure each needs.
Recommend template-first publication; do not split into multiple Go modules now.

**Benefit:** Every subsequent abstraction must reduce a demonstrated workflow cost.
**Dependencies:** None. **Risk:** Overpromising minimal-runtime support before it exists.
**Exit:** Reproducible baseline with edit locations, required settings and observed
startup behavior. This is an engineering exercise, not an invented usability score.

### B. Replace double-tree CLI execution — P0, medium/high

**Problem:** Nil-dependent command construction, callback rewriting and re-execution
make adding commands and understanding flags unnecessarily hard. Help validates
unrelated infrastructure settings.

**Affected:** app/cli.go, platform/cli, module CLI constructors, config loading, CLI tests.

**Approach:** Build and execute one native Cobra tree. Command definitions remain
resource-free. Bind a typed operation/resolver explicitly in the composition root;
resolve its dependencies only when execution requires them. Lifecycle cleanup stays
at the command execution owner and runs on every failure path. Separate environment
snapshot/selection from capability validation. Do not parse --help manually or use
string command-name switches as a bootstrap architecture.

Prove this on Identity show plus migrate and serve before migrating every command.
Cobra hooks are available, but a hook must not be the only cleanup mechanism.
Avoid a generic dependency resolver, context service container, or stringly typed
boot annotations. Typed module CLI callbacks are ordinary explicit dependencies.

**Benefit:** One command definition, one parse/execution path, usable offline help.
**Dependencies:** A; coordinate extraction with C, but no broad runtime rewrite.
**Risks:** Flag defaults, inherited hooks, aliases, completion, stdout/stderr/stdin,
required flags, cancellation, feature-disabled commands and scheduled standalone factories.
**Exit:** --help/completion work with unavailable and unrelated invalid backend
settings; invalid arguments do not open infrastructure; command action executes once;
cleanup and input/output behavior survive success, cancellation and panic.

### C. Make composition readable and ownership explicit — P1, medium

**Problem:** A large Identity wiring function and embedded registration targets mix
construction with declaration; business services need reuse but not runtime lookup.

**Affected:** app/modules.go, dependencies package, app/bootstrap.go, module Register callers.

**Approach:** Use app-local buildIdentity/buildNotifications/buildMedia functions
with narrow explicit arguments and typed return values. Keep cross-module wiring
visible. Separate infrastructure fields from registration targets without embedding.
Keep concrete services local unless another composition step needs them; use a small
private named result when CLI lazy construction requires retaining them. Keep services
out of the neutral dependencies bundle. Avoid a services registry.

**Benefit:** Readers can follow dependency order without knowing container semantics.
**Dependencies:** A; supports B. **Risks:** Accidentally constructing duplicate services,
authenticated route setup order, typed-nil interfaces and partial initialization cleanup.
**Exit:** One instance per operation family, no nil placeholders for CLI, no module
service fetched by type/key, and no new module lifecycle interface.

### D. Make capabilities truly optional and validation execution-aware — P1, high

**Problem:** Runtime roles do not determine construction needs. A small application
inherits queue, events and example-module assumptions. Core storage defaults conflict
with the minimal development story.

**Affected:** config defaults/validation, app acquisition/runtime/health, module wiring,
CLI command scopes and development profiles.

**Approach:** Distinguish installed capabilities from running roles using a small
explicit application configuration/composition decision. A publisher-only process
can need Redis without a worker; a database command need not open mail/storage.
Do not infer a whole dependency graph through reflection. Add only the execution
scopes demonstrated in A/B, with explicit acquisition order and validated requirements.
Validate active settings before acquiring anything; help need not validate unused
credentials. Make storage default disabled and migrate examples deliberately.

An HTTP-only example must be usable without Redis, Postgres or a local event router
when its installed operations do not need them. Preserve the full starter's
requirements when Identity/Notifications/Media are installed. Do not use silent
no-op clients to fake optionality, and do not allow signup event loss to be hidden
behind an unavailable publisher.

**Benefit:** Larger applications add capabilities without burdening the smallest one.
**Dependencies:** B/C. **Risks:** Misclassifying producer dependencies, startup ordering,
new default behavior, health probes checking the wrong scope and cleanup leaks.
**Exit:** Required capabilities fail early; disabled ones acquire no resources;
worker construction does not create business HTTP routes; readiness checks match
what that process actually requires; existing full-starter flows still pass.

### E. Reduce module onboarding bookkeeping — P1, medium

**Problem:** CLI metadata and feature conditions can drift from module registration;
several app touchpoints must be understood before adding a persisted capability.

**Affected:** module Register functions, app module/console/migration composition, guides.

**Approach:** Remove duplicated CLI construction through B. Let modules keep transport
registration and command definitions. Retain a resource-free migration list and
central application schedules. Prefer two short explicit composition edits over
one magical module descriptor that mixes metadata, resources and runtime behavior.
Only reconsider static descriptors if a worked example proves a net reduction and
it does not introduce the module registry forbidden by the current guide.

**Benefit:** A documented, limited set of intentional edits rather than scattered
framework internals. **Dependencies:** B/C. **Risks:** Missing migration sources,
registration conflicts, same module wired twice, loss of offline commands.
**Exit:** A persisted example needs module files plus explicit composition and
migration-source entries. Scheduling adds one timing entry only when needed.
Removing that example leaves no hidden CLI or config dependencies.

### F. Calibrate examples and package costs — P2, medium

**Problem:** Media has 14 production files for 376 lines; novice users can mistake
that expanded layout for a mandatory pattern. Cloud storage remains statically
linked even when disabled.

**Affected:** Media example, module guide, pkg/storage and config/filesystems, examples.

**Approach:** Keep Identity as the rich reference. Evaluate flattening genuinely
trivial adapters in Media using A's workflow rather than flattening by line count.
Keep separate HTTP wire types and real query/transaction boundaries. Do not move
entities/enums/contracts out of shared. Do not rename all public service types for
cosmetic consistency. For cloud drivers, compare a core local-storage import path
with explicit driver subpackages; app imports must also change for this to reduce
build cost. Keep Go CDK object APIs rather than adding a storage abstraction.

**Benefit:** Simpler starting examples and an honest optional-dependency story.
**Dependencies:** A/D/E. **Risks:** Import churn, lost discoverability, configuration
compatibility, different binaries' supported driver sets. **Exit:** Build-cost
measurement and a tested minimal example; no mandatory folder-per-concept rule.

### G. Make documentation drift detectable — P1 in parallel, medium

**Problem:** Recent manual rewrites fixed drift but do not prevent recurrence. Site
build success does not establish that paths, snippets, flags or API claims are correct.

**Affected:** docs builder/tests, tutorials, examples, CI and generated API checks.

**Approach:** Keep the current docs engine. Add automated local-link/anchor checks,
compile selected complete tutorial examples, and smoke-test documented quick-start
operations against disposable services. Distinguish fragments from runnable files.
Check generated site changes in CI or publish generated output as artifacts rather
than silently rewriting tracked files. Avoid keeping a third hand-maintained command
catalog for documentation.

**Benefit:** Fewer polished but false guides. **Dependencies:** New tests can start now;
update tutorial contracts alongside B–F. **Risks:** Brittle source-string assertions,
platform-only snippets, generated churn. **Exit:** Deliberately broken route/example
and link fixtures fail the relevant CI check. API generation remains stable.

### H. Complete release evidence, not release claims — P0 release gate, large/ongoing

**Problem:** Current runtime tests do not establish a reproducible public product or
production recovery. CI variables alone do not prove required tests executed.

**Affected:** CI, integration runner, release-readiness, SECURITY, dependency inventory,
deployment artifacts, migration upgrade tests and operations documentation.

**Approach:** Require named core integration scenarios to pass rather than merely
requiring nonempty URLs. Publish dependency/license inventory and supported matrix.
Choose the public module path and private security-reporting channel with the owner.
Prove clean-checkout builds after the current dirty work is intentionally committed;
do not pretend a working-tree run is that evidence. Rehearse Postgres/Redis restore,
termination and dependency outage behavior on disposable infrastructure. Benchmark
only stated representative workloads. Run an explicit authentication/security-policy
review before advertising Identity as production-ready; architectural cleanliness
is not evidence of authentication assurance.

**Benefit:** Reviewable release promises. **Dependencies:** A; final gating after B–G.
**Risks:** License omissions, leaked local fixtures, untested upgrades, platform-specific
results and noisy benchmark claims. **Exit:** Tagged reproducible artifacts, private
reporting, measured recovery notes and a truthful versioned support/upgrade policy.

## Proposed package shape

An ownership sketch, not a target file count:

```text
cmd/{app,worker,cli}/       signals and process exit
internal/platform/
  app/
    app.go                 public ownership contract
    bootstrap.go           explicit acquisition and construction
    modules.go             cross-module wiring and module command bindings
    console.go             one Cobra tree and framework command execution
    migrations.go          resource-free source list and migration commands
    schedule.go            editable application timing
    runtime.go             existing supervision and staged drain
    http.go                HTTP composition
    queue.go               native Asynq composition
    messaging.go           GoChannel composition
    infrastructure.go      connection construction helpers
  config/                  typed settings, active-scope validation
  dependencies/            neutral borrowed infrastructure and registration contracts
  api/                     boundary adapters and middleware
  cli/                     reusable Cobra integration
  messaging/               application event conventions
internal/<module>/
  module.go                ordinary registration functions
  service/                 operations and inputs when warranted
  repository/              real persistence boundaries
  delivery/                only transports actually used
  migrations/              owned SQL
internal/shared/           existing accepted ownership and coupling convention
pkg/                       reusable capabilities, no internal imports
```

This sketch intentionally does not add Container, Module, Provider or generic
Registry packages. The focused App review keeps module-specific command composition
in modules.go and consolidates cli.go into console.go; it does not add a second
console composition file.

## Representative developer experience

Existing business callers should remain this simple:

```go
task, err := tasks.NewWelcome(userID)
if err != nil {
    return err
}
_, err = queue.EnqueueContext(ctx, task)
return err
```

Proposed composition sketch (names/signatures are illustrative, not available APIs):

```go
identityOps, err := buildIdentity(cfg.Identity, infra.DB, infra.Events,
    infra.Mailer, infra.MailViews, infra.RateLimiter)
if err != nil { return err }

inbox, err := buildNotifications(infra.DB, infra.Queue)
if err != nil { return err }

mediaOps, err := buildMedia(infra.DB, inbox, infra.Transactions)
if err != nil { return err }

// Ordinary functions borrow concrete operations and registration targets.
if err := identity.Register(targets, identityOps); err != nil { return err }
if err := notifications.Register(targets, inbox, subscriber); err != nil { return err }
return media.Register(targets, mediaOps, subscriber)
```

Notice the visible Media → Notifications dependency. No MustGet, type map, module
ordering resolver or duplicate repository construction is required.

Proposed CLI shape to prove first: construct a native command with a typed operation
callback, where the outer app execution scope owns lazy construction and cleanup.
The callback is invoked only by RunE after Cobra's normal argument handling. The
exact API should be selected by the vertical slice in B, not declared mature here.
No resolver enters a business service.

## Next implementation slice

The follow-up [App review](app-design-review.md) takes precedence for the immediate
sequence: first repair event handler lifetime and empty-router shutdown, then
separate dependency/registration storage and proceed with a narrow B/C slice.
Retain A's developer workflow as an acceptance test throughout.

The B/C slice covers Identity show, root/nested help, migrate and
serve. Preserve aliases, streams, flags, cancellation and one-owner cleanup. Show
before/after traces of command execution and the edit list for adding a module.
Do not begin by merging files, adding a container, moving shared definitions or
rewriting the proven runner. Keep larger acquisition changes for D once the command
and composition boundary is clear.

## Reusable reassessment prompt

> Deeply review the current working tree as a mature Go modular-monolith framework
> boilerplate intended for community use. Produce a prioritized plan, not code.
> Evaluate simple predictable APIs, configuration, lifecycle ownership, capability
> replacement, consistent terminology, thin modules, documentation and production
> reliability with minimal necessary abstraction. Read actual callers and tests;
> identify concrete evidence and distinguish observed defects from design preferences.
> Preserve central shared entities/enums/event contracts and combined Bun entities
> where intentionally accepted. Preserve explicit Go composition, native library
> APIs, GoChannel's stated limitations, offline CLI help and database-only migrations.
> Do not introduce reflection DI, service locators or generic module runtimes.
> For each proposal state problem, affected areas, approach, benefit, prerequisites,
> compatibility risks and observable exit criteria. Include proposed structure and
> realistic developer-facing examples. Measure adding/removing a small module and
> optional infrastructure; file count is not a success criterion. Compare prior
> recommendations with current code and reject those that add more concepts than
> they remove. Report inspection/testing limits and unfinished release evidence.

## Reference boundaries

The [Go layout guide](https://go.dev/doc/modules/layout) supports internal packages
for application internals and describes cmd as a convention; it does not prescribe
a framework container or a universal module layout. These proposals are project
choices, not claims of a single Go community standard.

[Cobra's command execution](https://github.com/spf13/cobra/blob/main/command.go)
provides native parsing, help and hooks. Respect that execution model rather than
building another command interpreter. Verify implementation details against the
repository's pinned version during B.
