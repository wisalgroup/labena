# Project guidance

Read [docs/conventions.md](docs/conventions.md) before changing backend code or
architecture. It is the canonical architecture contract; historical reviews and
plans do not override it. Keep `AGENTS.md` and `CLAUDE.md` identical, and update
both alongside the canonical guide when conventions change.

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

## Verification and change discipline

Keep tests out of app and business service/repository packages; use the external
`tests` suites. Platform and reusable-package tests may stay beside their owners.
Preserve unrelated working-copy changes and follow the canonical guide's lifecycle,
transaction and verification rules. Check interface placement, plural entity
filenames, private implementation types and constructor return types for every
repository/service added or reorganized. Keep both agent files identical and
regenerate the documentation site after changing its source guides.

Dedicated seeders live in `internal/modules/<module>/seeders/`, with files named by
dataset or purpose. Constructors receive individual services; services retain
business rules and transaction boundaries. Register them in the module's
`register.go` through `Targets.Seeders.Register(Seeder)` or
`RegisterFunc(name, fn)`. Use stable module-qualified names and safe rerun behavior;
registration never executes seed work.
