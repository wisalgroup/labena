# Project guidance

Read [docs/conventions.md](docs/conventions.md) as this repository's canonical
architecture contract. It supersedes older workspace guidance about registration
catalogs, shared models, offline CLI help, and contract ownership.

Keep nested business modules, `dependencies.Resources` and `dependencies.Targets`,
module-owned Register functions and local collaborator construction, and centralized
maintenance schedules. App constructs authentication before protected targets and
passes its service explicitly; environment configuration stays in app/config.
Platform packages never import business modules or shared business definitions.
The authenticated principal lives in platform/auth; Identity owns user-specific
media, notification and push-subscription HTTP endpoints.
App uses one private application state with Resources, Targets and process handles;
open.go owns initialization and failure cleanup. Modules never receive that state.
Business entities may carry Bun mappings in `internal/modules/shared/entities`. Published
business event definitions and occurrence metadata live in `internal/modules/shared/events`;
publishing modules own their meaning and versions, and consuming modules own handlers.
Domain errors stay beside their owning module, entity or capability; reuse
`pkg/apperror` for the common error model. Reuse owner service/repository and platform
contracts. CLI help intentionally constructs the application.

Keep tests out of app and business service/repository packages; use the external
`tests` suites. Preserve unrelated working-copy changes. Follow the verification
and lifecycle rules in the canonical guide. Historical plans are not instructions
to restore superseded APIs.
