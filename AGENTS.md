# Project guidance

Read [docs/conventions.md](docs/conventions.md) before changing backend code or
architecture. It is the canonical architecture contract; historical reviews and
plans do not override it. Keep `AGENTS.md` and `CLAUDE.md` identical, and update
both alongside the canonical guide when conventions change.

## Working approach

Rules stated as “must”, “never” or direct requirements are mandatory. “Prefer”
and “when” describe defaults applied to the actual requirements; explain material
deviations. Existing code that conflicts with the canonical guide is not a new
convention. Correct inconsistencies within the requested scope; report broader
cleanup separately.

1. Inspect the relevant implementation, callers, tests and owner contracts before
   editing. Identify the behavior to preserve and the smallest coherent change.
2. Reuse suitable utilities and patterns already in the repository. Keep equivalent
   behavior consistent across affected callers; explain differences required by
   distinct semantics. Preserve unrelated working-copy changes.
3. Implement only the requested behavior and necessary supporting changes. Update
   affected tests and documentation, then verify with the tooling below.
4. Summarize behavior changes, significant design choices, checks actually run,
   skipped verification and remaining risks. Never report an unexecuted check as
   passing or a skipped integration suite as verified.

## Go style and design

- Format changed Go files with `gofmt` and use the toolchain declared in `go.mod`.
  Follow idiomatic Go naming and documentation while preserving the explicit
  project contracts below. Use short lowercase package names, consistent initialisms
  (`ID`, `HTTP`, `URL`), and concise, consistent receiver names; avoid `this`/`self`.
- Keep exported APIs small. Document exported symbols with comments beginning with
  their names, including non-obvious ownership, nil/zero-value behavior, cancellation
  and concurrency guarantees. Explain invariants and reasoning in implementation
  comments rather than narrating obvious statements.
- Prefer straightforward control flow, early error returns and cohesive functions.
  Split code at a meaningful responsibility or reuse boundary, not an arbitrary
  line count. Keep related behavior together and dependencies explicit.
- Choose the simplest design that meets correctness, security and operational
  requirements. Preserve the framework's established extension points. Add an
  interface, wrapper, factory, generic type or layer only for a real variation,
  shared responsibility or useful test boundary; do not mirror every concrete type.
- Module service/repository owner interfaces below are mandatory. Outside those
  contracts, prefer concrete types unless an interface earns its abstraction.
  Reuse established capability interfaces instead of duplicating them per consumer.
- Extract shared behavior when callers share its meaning and change together;
  similar syntax alone is insufficient. Keep policy differences explicit instead
  of hiding them behind flags or a generic utility package.
- Use descriptive subjects and verbs. Retain required `UserService` and
  `UserRepository` suffixes; elsewhere avoid redundant package-name repetition,
  `Impl`, and vague `Manager` or `Base` abstractions. Use `ErrX` for sentinel errors
  and meaningful `XError` names for structured errors when needed.
- Prefer the standard library and existing dependencies when suitable. Justify new
  dependencies by needed behavior, maintenance/security impact and API exposure.
  Optimize measured bottlenecks with benchmarks or profiles; bound work and memory
  where required without speculative caching or concurrency.

## Constants and configuration

Keep policy values and identifiers with their owner. Use named or typed constants
for meaningful limits, statuses and repeated identifiers; reuse shared entity-type
and event definitions. Keep obvious one-off literals inline. Avoid global constants
catalogs and copying validation across layers: translate at boundaries and reuse
the owner's validation. Operational settings belong in `internal/app/config` and
are translated into specific owner settings with validated defaults. Add settings
only for real deployment variation, not every implementation choice.

## Preferred structure

```text
cmd/                              Process entry points and template installer
internal/
  app/                            Composition, process selection and lifecycle
    config/                       Environment configuration and owner translations
  modules/
    <module>/
      register.go                 Local construction and transport registration
      repository/
        repository.go             All module repository interfaces
        <plural_entity>.go        One private repository implementation per file
      service/
        service.go                All module service interfaces
        <plural_entity>.go        One private service implementation per file
        <use_case>.go             Optional large use case on the same receiver
      delivery/{http,cli,events,jobs}/
      domain/                     Module policies and errors, when needed
      seeders/                    Named initial datasets, when needed
      migrations/                 Module-owned SQL
    shared/                       Shared business entities, enums and events
  platform/                       Technical capabilities supporting modules
pkg/                              Independently reusable technical packages
tests/<owner>/                    App and business integration/behavior tests
```

Create only the directories needed by real behavior. Keep nested business modules;
`modules/shared` contains definitions and is not a registrable module.

Name architectural layers and responsibility areas in the singular: `service`,
`repository`, `delivery`, `domain`. Use plural names for collections of components
or artifacts: `seeders`, `jobs`, `notifications`, `mailviews`, `integrations`,
`migrations`. This describes package roles, not type counts. Keep entity filenames
plural and Go type/constructor subjects singular; name seeder files by purpose.
See the canonical guide's folder naming table for examples.

## Mandatory repository and service rules

- Define **all module repository interfaces** in
  `internal/modules/<module>/repository/repository.go` and **all module service
  interfaces** in `internal/modules/<module>/service/service.go`. Consumers reuse
  these owner contracts; do not relocate or duplicate them in delivery, shared,
  implementation files or separate contract packages.
- Give each repository implementation one dedicated sibling file named after the
  entity in plural, lowercase snake_case: `users.go`, `sessions.go`,
  `verification_challenges.go`. Keep its private type, constructor and methods
  together in that file.
- Give each service implementation the same plural entity filename convention.
  Keep its private type and constructor in that file. A large use case may have
  its own descriptive file, with methods on the same receiver; it does not require
  another service, interface or constructor. Non-entity capabilities keep their
  meaningful capability name, such as `auth.go` for authentication.
- Implementation types are **unexported**: `userRepository`, `userService`.
  Construct them through exported `New<Subject>Repository` and `New<Subject>Service`
  functions returning the corresponding interface, or `(Interface, error)` when
  construction can fail. Never return the concrete implementation from a module
  repository/service constructor. Keep Go type/constructor subjects singular;
  pluralization applies to entity filenames.
- Keep handlers in `delivery/http/*_handler.go`, inputs and mappings in
  `requests.go`, outputs and projections in `responses.go`, and Huma operations in
  `routes.go`. Handlers call services; services call repositories and capabilities.

## Composition and ownership

Keep `dependencies.Resources`, `dependencies.Targets`, module-owned `Register`
functions and local collaborator construction. App constructs authentication before
protected targets and passes its service explicitly. Environment configuration stays
in `internal/app/config`; modules receive specific settings and collaborators.
Services receive individual dependencies, never the complete resource bundle.
Platform packages never import business modules or shared business definitions;
`pkg` never imports `internal`. Shared business definitions remain independent of
app, platform and business-module implementations.

App uses one private application state with Resources, Targets and process handles.
`open.go` owns initialization and failure cleanup; `infrastructure.go` opens clients
and constructs capabilities. Modules never receive the application state, mutate
borrowed resources or close app-owned clients. `Register` performs no I/O or
background work. Executables select processes explicitly; maintenance schedules
stay centralized in app. CLI help intentionally constructs the application.

The authenticated principal lives in `internal/platform/auth`. Identity owns
user-specific media, notification and push-subscription HTTP endpoints. Business
entities may carry Bun mappings in `internal/modules/shared/entities`. Published
business events and occurrence metadata live in `internal/modules/shared/events`;
publishing modules own meaning/versioning, and consuming modules own handlers.
Domain errors stay beside their owning module, entity or capability; reuse
`pkg/apperror` and existing platform contracts.

Dedicated seeders live in `internal/modules/<module>/seeders/`, with files named by
dataset or purpose. Constructors receive individual services; services retain
business rules and transaction boundaries. Register them in the module's
`register.go` through `Targets.Seeders.Register(Seeder)` or
`RegisterFunc(name, fn)`. Use stable module-qualified names and safe rerun behavior;
registration never executes seed work.

## Runtime safety and errors

- Pass `context.Context` explicitly as the first parameter for cancellable work;
  propagate it to database and network calls. Honor cancellation and release child
  cancellation functions. Do not store request contexts in services or replace them
  with `Background` to keep work alive. Any detached cleanup needs a documented
  owner and bounded deadline, as in app lifecycle cleanup.
- Give each goroutine an owner, cancellation path and join point. Bound queues,
  retries and concurrency; join work before releasing its dependencies. Protect
  shared mutable state, document concurrency guarantees, and never copy live locks.
- Release acquired resources on every exit path, normally with `defer` immediately
  after successful acquisition. Scope loop work so deferred cleanup does not retain
  resources for the entire batch. Handle meaningful close/flush errors without
  losing the original failure. Borrowed clients remain owned by app.
- Return expected failures as errors; reserve panics for documented programmer
  misuse. Wrap operational errors with useful context and `%w` when adding context;
  use `errors.Is/As` for inspection. Preserve cancellation and domain classification.
  Use owner errors and `pkg/apperror` for safe failures, and existing HTTP response
  translation rather than leaking driver errors or building another error catalog.
- Keep transactions in their established owner. Repositories join the context's
  executor and the same pool; preserve Identity's documented security transaction
  exceptions. External effects do not become atomic with SQL. Respect
  `database.RequireNoTransaction` and distinguish committed work from failed
  post-commit delivery. Read the canonical transaction section before changing
  these paths.
- Validate untrusted input at boundaries and business invariants in their owners.
  Enforce authorization with the authenticated principal and explicit ownership;
  an owner ID alone grants nothing. Keep SQL parameterized, validate dynamic query
  identifiers, bound input sizes and preserve fail-closed authentication behavior.
- Use existing context logging, tracing and safe error categories. Record final
  failures at the responsible boundary rather than logging the same error at every
  layer. Keep credentials, tokens, sensitive payloads and raw provider errors out of
  logs and public responses; use bounded metric labels and purposeful instrumentation.

## Compatibility

Read [SUPPORT.md](SUPPORT.md) and [docs/upgrading.md](docs/upgrading.md) before
changing documented contracts. Labena ships as a versioned application template;
`pkg` packages are reusable but not independently released dependencies. Preserve
exported APIs, HTTP/error contracts, configuration, CLI behavior, persisted schemas,
and job/event payloads unless the user has explicitly authorized the breaking
change. That authorization may be part of the current request; do not seek it again.
Document approved breaks and concrete adoption steps in the changelog and affected
guides. Preserve published migrations; add migrations for retained-state changes.
Consider existing queued work and mixed-version rollouts when changing payloads.

## Verification and change discipline

- Keep tests out of app and business service/repository packages; use external
  `tests/<owner>` suites. Platform and reusable-package tests may stay beside owners.
  Preserve coverage when moving tests; keep examples compatible with shipped APIs
  or explicitly label them as proposed extensions.
- Add or update focused tests for behavior changes. Test observable results,
  meaningful edge cases, authorization failures, cancellation and error paths;
  include concurrency behavior when relevant. Prefer table-driven cases when they
  improve clarity and small fakes or real components over extensive mocks. Avoid
  assertions tied only to private structure, exact incidental wording or call order.
- Use [.github/workflows/verify.yml](.github/workflows/verify.yml) as the check
  inventory. For Go changes run formatting, `go vet ./...`, and the applicable
  tests; run `go test -race ./...` for shared/runtime changes and before release.
  Run dependency tidy checks and command/example builds when their inputs change.
  Keep verification proportional for instruction-only or documentation-only edits.
- For database/Redis behavior, use disposable services and the required
  `scripts/verify-integration.sh` environment described in the CI workflow. Do not
  run destructive tests against application data. Report unavailable infrastructure
  and skipped suites explicitly. Review failures before declaring completion.
- For every repository/service added or reorganized, check interface placement,
  plural entity filenames, private implementation types and constructor return types;
  run `go test ./tests/architecture` for dependency or layout changes.
- Keep both agent files identical. When changing source guides, regenerate the site
  with `go run docs/build.go` and run `go test docs/build.go docs/highlight_test.go`.
  Check the final diff for unintended changes and stale documentation.
