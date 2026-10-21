# Framework refactoring plan

> Historical design record. The current implementation uses eager application
> construction for every CLI invocation, shared `dependencies.Resources`, and
> module-owned command/SQL registration through `dependencies.Targets`. Offline
> help, lazy command callbacks and separate catalogs described below were replaced
> by the simpler design in [application lifecycle](../application-lifecycle.md).

Status: proposal only. No application implementation changes are part of this review.

## Recommendation

Keep the current foundation, but make the programming model coherent before expanding the capability list. The strongest public offering is an opinionated, extensible Go application starter with small reusable packages, explicit composition, and thoroughly exercised production workflows. A framework-wide container, provider interface, generic asynchronous runtime, or universal repository would add concepts without solving the main problems found here.

Prioritize reliable behavior and predictable startup over renaming folders. Keep native Gin, Bun, Asynq, Cobra, Watermill, slog and OpenTelemetry escape hatches. Use third-party libraries behind a small integration only when that integration removes actual application work: ownership, configuration, telemetry, validation, or policy.

Initially publish a template/starter and document which packages are experimental versus supported. Do not promise that downstream projects can upgrade a copied application as though it were an imported framework. Once the reusable interfaces survive real application use, publish those as importable modules if independent versioning provides value. The then-local-only module path and `internal` packages were appropriate for a copied starter, not an already consumable public framework library. [Go project layout](https://go.dev/doc/modules/layout) and [module release guidance](https://go.dev/doc/modules/release-workflow) explain the distinction.

## Review scope and evidence

Reviewed the working tree, including uncommitted and untracked code: all 88 Go package inventories and their import graph; process entry points; app acquisition, registration, runtime and cleanup; configuration; all three business modules; shared definitions; public infrastructure interfaces and significant implementations; migrations, tests, examples, documentation source/build pipeline and deployment definitions. This is an architectural and maintainability review, not a claim of line-by-line security certification or production load testing.

Checks performed during this review:

- `go test -race ./...` passed for the locally runnable suites, including cached results where applicable.
- `go vet ./...` passed.
- `go test docs/build.go docs/highlight_test.go` passed.
- Import inventory found no `pkg` → `internal` imports.
- README/AGENTS local-link check found nine broken links, including references to Orders and former platform package locations.
- Published downloadable Markdown matched the current documentation sources. The problem is stale content, not an assumed generation failure.
- `pkg/nats`, `pkg/rabbitmq` and `pkg/template` have no non-test importers in the current Go package graph. This is a maintenance signal, not proof that they can be deleted without checking intended external use.

No database, deployment, source implementation or configuration was changed for this review. External PostgreSQL, MySQL, Redis, Redis Cluster, S3, NATS/RabbitMQ and cloud-provider behavior was not revalidated during this planning pass. Earlier integration results are useful background, but are not counted as fresh verification here. No benchmarks, browser walkthrough or new vulnerability scan was performed.

## Preserve these decisions

- Explicit constructors and sequential registration; no service locator or reflection-based dependency injection.
- One application HTTP server. A separate private metrics listener is an operational choice, not a second business HTTP server.
- App-owned infrastructure, borrowed dependencies, shutdown ordering, refusal to close resources while handlers still use them, and partial-construction cleanup.
- Native Asynq tasks and options. Tasks request work; events describe facts. Do not merge the two into another `async` abstraction.
- Transaction integrity, failed-OTP attempt persistence, refresh rotation/reuse detection, account locking and single-session enforcement.
- All modules’ entities, enums and event contracts intentionally remain in `shared/entities`, `shared/enums` and `shared/contracts/events`. This explicitly accepted coupling supports bidirectional cross-module relationships without Go import cycles. Do not move these definitions back into individual modules or redesign the architecture to eliminate this tradeoff. Keep `entities.User` as both Bun model and entity and use Google UUIDs, as already chosen.
- Application-level enum validation; retain database uniqueness, foreign keys and required columns. Do not reintroduce database enums or business CHECK constraints.
- The fresh Identity migration baseline. Published/applied migrations that must preserve data become immutable; forward ALTER migrations are normal thereafter.
- Current request IDs, safe application errors and standardized responses. Do not rename public response fields or routes merely for stylistic consistency.
- Tests that protect concurrency, security, rollback and lifecycle behavior. File count alone is not a reason to delete them.

## Shared definitions: explicit architectural constraint

Centralization is intentional and is not an architectural defect to remove. The
framework accepts compile-time coupling between shared definitions to make cross-module
relationships practical. This does not transfer ownership of operations or writes.

| Shared area | Belongs here | Remains outside |
| --- | --- | --- |
| `shared/entities` | Every module’s entities, accepted Bun mappings/relationships, entity-local invariants, explicit owner comments | Repositories, SQL queries, transactions, remote calls and application workflows |
| `shared/enums` | Every module’s business enums, possible values and membership validation | Environment configuration, transport response policies and infrastructure driver selection |
| `shared/contracts/events` | Every module’s event names, versioned payloads and intrinsic event metadata | Publishers, subscribers, handlers, retries, broker topology and delivery policy |
| Other focused shared contracts | Cross-module operation data with actual shared meaning | General utilities, service implementations, infrastructure clients and dependency containers |

Use concept-oriented files and owner prefixes where needed for navigation, such as
`identity_user.go`, `identity_user_status.go` and `identity_user_registered.go`.
Keep Go identifiers concise (`entities.User`, not `entities.IdentityUser` merely to
repeat ownership). Preserve current identifiers unless ambiguity justifies a change.
Do not split central packages by module when doing so would recreate the cycles this
tradeoff intentionally avoids.

Shared definitions must not import module implementations, app/config or infrastructure
setup. The accepted Bun mappings and pure entity behavior remain valid; persistence
execution and use-case orchestration do not move into shared. Each module owns writes
to its entities. Cross-module operations use explicit injected interfaces, and
cross-module reads/transactions retain documented ownership and consistency rules.

Add import-boundary checks for these rules and review new shared additions for real
shared meaning. Moving definitions to shared is not a workaround for cyclic service
dependencies.

## Ordered work plan

Sizes are relative: S is localized; M spans several packages; L affects lifecycle, persistence or multiple execution paths. They are not delivery-time promises.

### 1. Establish the supported product and a reproducible baseline — P0, S/M

**Problem/evidence:** The repository calls itself a general boilerplate, but the root README still describes `POST /orders` and a removed Orders module. The app can select MySQL even though the shipped business repositories and migration SQL use PostgreSQL-specific behavior. No root license, contribution/security policy or CI workflow was found. The current working tree also contains extensive ongoing changes, so a moving target is unsuitable as a release baseline.

**Affected:** README, AGENTS, go.mod, deployment/configuration guides, examples, root release files and CI.

**Approach:** Document a support matrix: PostgreSQL for the shipped app; Asynq/Redis for tasks; Watermill GoChannel for ephemeral local events; optional cache, SMTP, object storage and telemetry. Distinguish generic package capabilities from integrations actually supported by the app. Select a public module path when publishing reusable packages, license with owner approval, supported Go version policy and a first pre-release tag. Record existing intentional tradeoffs and a stable review baseline. Add fast test/vet checks immediately; build the external-backend matrix in item 13.

**Benefit:** Users can understand what works and what requires extension without learning implementation history.

**Dependencies:** None; informs every subsequent item.

**Migration risks:** Import-path changes affect every caller. Do this once before public release; avoid a premature multi-module split. Document PostgreSQL-only app support instead of deleting independently useful MySQL database/cache packages automatically.

**Completion:** A clean checkout has one truthful quick start, a supported-capability matrix, and passing automated baseline checks.

### 2. Close the concrete operational and safety gaps — P0, M

**Problem/evidence:** `QueueConfig.SchedulerEnabled` defaults to true, and every selected queue worker starts a scheduler. Adding worker replicas can duplicate periodic enqueueing. `migrate refresh` is available without an explicit destructive-action gate. `pkg/mail/factory.go` treats empty encryption as `InsecureSkipVerify=true` and advertises an unimplemented Mailtrap driver; the app's SMTP path is safer. Redis isolation validation checks queue versus cache but does not include the authentication limiter.

**Affected:** app/runtime.go, app/schedule.go, config/queues.go, config/validate.go, app/cli.go, migrations/refresh.go, pkg/mail/factory.go and deployments.

**Approach:** Make scheduling an explicit process responsibility, disabled on ordinary worker replicas. Start with one designated scheduler per schedule set; do not invent leader election. Add an explicit destructive-reset flag and target display, disable/reset-gate it outside development, and document backup/restore and grant restoration limits. Remove the unused legacy mail factory or replace it with the existing explicit SMTP constructor; certificate verification must remain the default. Validate rate-limit/cache/queue endpoint and logical-database ownership together, with documented namespace alternatives only where supported.

**Benefit:** Safer copy-and-run defaults and predictable scaling.

**Dependencies:** Item 1 for supported deployment profiles; role cleanup is completed in item 3.

**Migration risks:** Scheduling stops if deployment changes omit its new explicit owner. Capture existing cron definitions and demonstrate exactly one scheduler before rollout. Reset safeguards change scripts. Database-number changes reset rate budgets or move queue data unless migrated deliberately.

**Completion:** Two worker replicas and one scheduler enqueue one periodic occurrence under the documented deployment; reset commands reject accidental use; SMTP constructor tests preserve verified TLS. Asynq explicitly warns about multiple schedulers for the same schedule. [Upstream guidance](https://github.com/hibiken/asynq/wiki/Periodic-Tasks).

### 3. Separate registration from resource acquisition and execution — P1, L

**Problem/evidence:** `app.openDependencies` creates mail, storage, queue, messaging, HTTP and console machinery before it knows which command needs them. `Execute` says help/completion construct dependencies, with a special exception for table publication. `APP_ROLES` affects `cli serve`, while other entry points hard-code different selections. A missing DB is accepted for offline assembly and turns real repository operations into runtime failures.

**Affected:** cmd/*, platform/app/{app,bootstrap,runtime,cli,infrastructure,modules}.go, dependencies, cli registration, health routes.

**Approach:** Retain the existing tested runner/cleanup implementation initially. Define explicit phases: load settings → validate selected execution → register definitions → open required resources → wire operations → register executable handlers → validate → run → drain → close. Build Cobra command descriptions without opening the whole app; instantiate dependencies only for the selected command's execution. Use straightforward separate assembly paths for migrations and serving, not a generic dependency resolver. Provide one canonical selection model for HTTP, workers and scheduler; automatically start local event consumers wherever their publishers run. Optional unused capabilities must open no resources. Serving a module that requires PostgreSQL must fail at startup when it is absent; offline inspection should not need fake usable repositories.

Keep DB pools as resources with Close, HTTP/workers as runners with Run, and ordinary services as borrowed objects with neither. Do not force every type through Init/Start/Stop/Close.

**Benefit:** Help works offline, migrations work during unrelated infrastructure outages, and operators can tell exactly what a process runs.

**Dependencies:** 1–2. Item 5 must clarify durable versus local event behavior before allowing remote subscriber roles.

**Migration risks:** Startup order, cleanup, readiness and CLI defaults are compatibility surfaces. Preserve the existing lifecycle tests before moving code; explicitly test disabled capabilities and partial failure. Context deadlines cannot forcibly terminate arbitrary third-party Close functions or noncooperative handlers—do not advertise a hard guarantee the runtime cannot provide.

**Completion:** `--help` needs no infrastructure; migration commands need only DB and migration sources; HTTP-only/worker/scheduler modes acquire only their documented dependencies; shutdown fault-injection tests pass.

### 4. Make configuration a coherent, immutable interface — P1, M

**Problem/evidence:** There is a useful typed config foundation, but central validation mixes capability and Identity rules, `godotenv.Load` mutates process environment, names mix `APP_CACHE_DRIVER`, `QUEUE_*`, `EVENTS_*` and `FILESYSTEM_*`, and disabled features sometimes validate unrelated settings. Queue configuration imports middleware types. A broad API rate-limit policy is validated even when only Identity's limiter needs Redis.

**Affected:** platform/config, app conversion code, .env examples, Compose/Kubernetes and documentation.

**Approach:** Keep one file per coherent capability. Load a snapshot without mutating process environment; expose an explicit pure decoder for tests/embedding. Each capability validates its own enabled settings; the root validates cross-capability requirements. Separate data configuration from optional native callbacks such as Asynq retry functions. Define canonical vocabulary and a deliberate one-time migration for old keys. Use typed queue/disk definitions at the top of their files and apply environment overrides to operational values. Keep `AllowMultipleSessions` as the deliberate application policy constant unless requirements change.

**Benefit:** Defaults, tests and deployments describe the same system, and optional capabilities remain optional.

**Dependencies:** 1 and 3.

**Migration risks:** Empty-value precedence, renamed env keys, disabled-feature validation and namespace changes can silently alter behavior. Publish an old→new key table, reject conflicting settings, and verify a config dump that excludes secrets. Avoid indefinite compatibility aliases.

**Completion:** Table-driven precedence/validation tests, examples and deployment settings agree; one app's config loading cannot mutate another app's environment.

### 5. Make consistency and delivery guarantees explicit — P1, L

**Problem/evidence:** Identity commits account/session changes and then publishes `UserRegistered`. Media provisioning and welcome notification enqueueing depend on the local event. GoChannel cannot survive process failure or cross replicas. `failed.events` is configured as a poison destination, but no consumer/storage for that topic was found. Publishing there is not durable failure retention. Non-nil results returned alongside post-commit errors complicate callers.

**Affected:** Identity registration/sign-in repositories and services, app/messaging.go, module event handlers, notifications/tasks, scheduler and operational docs.

**Approach:** Classify each reaction: required transactionally, required eventually, or best-effort. Use direct operations in one PostgreSQL transaction for required same-database state. Keep GoChannel for explicitly best-effort local reactions. For required asynchronous work, introduce an optional durable outbox path with stable operation IDs and idempotent consumers. Evaluate Watermill SQL + Forwarder before writing custom event outbox machinery. Verify Bun transaction participation and the final durable transport. Forwarding an outbox into nonpersistent GoChannel alone does not establish durable processing. For Asynq enqueueing, a small specific relay may still be needed; keep this separate from event delivery.

Replace the ephemeral poison destination with explicit terminal failure logging/counting for local events, or actual retained failures for the durable profile. Remove transport exceptions from ordinary successful registration results once required follow-up intent is committed durably.

**Benefit:** “Registration succeeded” has a clear meaning and failure recovery is possible.

**Dependencies:** Consistency decision before implementation; 3–4 for lifecycle/configuration. Complete before presenting required provisioning as production-safe.

**Migration risks:** New tables and relays, duplicate deliveries, payload/version compatibility, retention and replay procedures. Never promise exactly-once external effects. Keep existing partial-success handling until the replacement is exercised through crashes.

**Completion:** Tests kill the producer after commit and the relay after enqueue; work resumes without losing required intent or duplicating business effects. Best-effort examples state their loss window. [GoChannel limitations](https://watermill.io/pubsubs/gochannel/), [Watermill Forwarder](https://watermill.io/advanced/forwarder/).

### 6. Simplify Identity around named operations — P1, M/L

**Problem/evidence:** Recent sharing removed duplicate construction, but `Social` still takes `EmailVerification` to obtain limits and a digester. Passwords use its private challenge mechanics. `SignInInput` combines name, OTP proof, network peer and device information and is reused for unrelated flows. Authentication is globally tied to email-verification configuration. Public account creation and verified signup also need an explicit product distinction.

**Affected:** identity/service, repository contracts, domain policies, delivery/http, platform/app/modules.go, config/identity.go.

**Approach:** Keep cohesive account, challenge, session, password and social operations; do not merge them into one giant AuthService. Extract the actually shared OTP lifecycle into a narrowly scoped `Challenges` implementation and independent account-throttling policy; email verification uses it rather than lending unrelated internals to social login. Session issuance remains one shared operation. Use small operation-specific inputs and a reusable device value where it reduces invalid combinations. Constructors receive direct dependencies or a named dependency struct when long argument lists are genuinely hard to read; do not add a general container.

Place broad and authentication-route IP limits at ingress, and account/proof limits in the operation. Define trusted-proxy policy once. Preserve source-rate protection for every exposed transport before removing service IP arguments. OWASP recommends per-account throttling, but does not mandate a Go package location. [Authentication guidance](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html).

Keep locking, token rotation, proof consumption and committed failed attempts in repository operations that own their transactions. Keep existing `/users` and `/auth` URLs during this internal work. Decide whether public unverified `/users` creation is a supported flow or an example before changing it.

**Benefit:** Dependencies reflect meaning, each input describes one operation, and security rules are easier to locate.

**Dependencies:** 4; coordinate publication-result changes with 5.

**Migration risks:** Highest-risk refactor area: proof purpose binding, account enumeration, failed-attempt persistence, expiry, concurrent signup, password reset invalidation, social linking and refresh reuse. Preserve these tests and serialized contracts. Do not reintroduce social challenge tables or automatic email linking.

**Completion:** A maintainer can trace each sign-in mode through a short operation; transport changes cannot bypass account limits; all real-PostgreSQL/Redis authentication tests pass.

### 7. Standardize module construction and ownership — P2, M

**Problem/evidence:** Services sometimes expose a broad interface backed by a private struct, while Identity authentication uses concrete exported structs. Modules receive a broad registration bundle with different nil/required-target policies. Notifications duplicates principal extraction through its own Recipient callback/context key. Media currently represents one row per user, not an upload/media-management capability.

**Affected:** all module.go files, module service/repository interfaces, delivery packages, shared entities/contracts/inputs and dependencies.

**Approach:** Keep ordinary `Register` functions and one registration bundle for startup targets. Use a named module-specific Services struct only for modules such as Identity with several independent operations. Prefer concrete implementations returned by constructors; define small interfaces where handlers or consuming modules actually need substitution. Keep the existing layered layout where it improves navigation; do not impose empty directories on every new module. Standardize transport folders as `delivery/http`, `delivery/cli`, `delivery/tasks`, `delivery/events`; keep `tasks/` for task names, payloads and constructors. Standardize authenticated principal access through the platform adapter.

Keep all entities, enums and event contracts centralized with owner comments, including definitions that reference other modules’ entities. Organize their files by owning module and concept while retaining shared package boundaries needed for bidirectional relationships; do not introduce module-specific subpackages that recreate import cycles. Do not duplicate entities into `models` just to fill a layer. Move unused `shared/inputs/RegisterUserData` to its actual owner or delete it; service inputs belong beside operations and wire validation tags belong beside HTTP requests. Retain Entity embedding. Keep AggregateRoot optional and do not pretend its currently unused event collection drives publication. Review unused specifications/value objects individually instead of forcing them into module code.

**Benefit:** Developers learn one registration and transport pattern while small modules remain small.

**Dependencies:** 3 and the Identity reference pattern from 6.

**Migration risks:** Package moves affect imports; cross-module SQL and transactions must preserve ownership. Do not force transaction-bearing auth repositories into generic CRUD interfaces.

**Completion:** A documented small module and a richer Identity module use the same registration principles; adding a task/HTTP handler does not require editing infrastructure internals.

### 8. Stabilize the HTTP developer interface — P2, M

**Problem/evidence:** Strict `DecodeJSON` uses an isolated validator and bounded custom parsing, while `DecodeWith` delegates to Gin and has different strictness. The distinction is documented but easy to miss. Requests repeat body limits, principal access and IP extraction. Error rendering and response helpers are useful, but generic pagination lives in `shared/types`, and transport envelope types live in `shared/contracts`.

**Affected:** platform/api, response, middleware, module requests/responses/routes, shared/types, shared/contracts/api.go.

**Approach:** Keep short `response.OK/Created/Error` usage and transport-neutral `apperror`. Put API envelopes beside response rendering; move reusable pagination to `pkg/pagination`. Choose and document one default JSON policy, retaining explicit native/custom binding escape hatches. Do not replace strict parsing with ShouldBindJSON without preserving body limits, unknown-field handling, trailing-data rejection and validation semantics. Install a safe default body limit with explicit route overrides. Bind API versions at route groups and keep request-ID generation, log correlation and headers centralized. Add OpenAPI for the implemented endpoints with contract checks; avoid a parallel hand-maintained validator schema.

**Benefit:** Handlers consistently decode → authorize → call → respond, with fewer per-handler decisions.

**Dependencies:** 4 and 7; error/response wire compatibility is independent of folder moves.

**Migration risks:** JSON strictness and validation-field names are observable behavior. Preserve `request_id`, `api_version`, field-error arrays and status mappings unless introducing a versioned change. Keep wildcard CORS as the explicitly chosen default with credentials disabled.

**Completion:** Representative handlers are short; tests cover malformed input, oversized bodies, URI/query binding, authorization and stable response envelopes.

### 9. Reduce infrastructure surface area and place it by ownership — P2, M

**Problem/evidence:** Some reusable code remains in platform while app files contain reusable queue administration/scheduling machinery. Conversely, unused NATS/RabbitMQ connectors and a generic template system enlarge the advertised surface. `pkg/transaction` exposes four methods but is primarily an indirection over Bun transaction handling. `messaging.Publisher` exposes Close even though application publishers are borrowed.

**Affected:** platform/asynqx, app/queue.go and schedule.go, platform/migrations, mailview, messaging, pkg/transaction/template/nats/rabbitmq and dependencies.

**Approach:** Move the native Asynq client contract and reusable middleware to `pkg/asynqx`; keep application task routing and schedule definitions in app/config. Move reusable queue runner/admin/schedule validation alongside that integration when they no longer import app config. Keep Cobra command adapters in platform/cli. Move generic Goose integration to `pkg/database/migrate`, keeping application migration-source registration in app. Move the semantic email renderer to `pkg/mailview`, with brand config/module message content staying in internal. Remove Close from the borrowed application publisher contract; owner cleanup uses its concrete resource.

Keep app runtime private for now; it is not yet an independently consumed framework runtime. Merge `pkg/transaction` into the database capability or replace broad injection with a consumer-owned Execute-only interface/function. Retain its transaction semantics. Park unused connectors and template wrappers outside the supported starter surface, or remove after an explicit API inventory. Do not rename messaging merely to match a vendor: platform/messaging still describes application integration accurately.

**Benefit:** App files become editable composition; technical contracts have one owner; the advertised feature set matches exercised integrations.

**Dependencies:** 3–5 and package API inventory from 1.

**Migration risks:** Moving public pkg code is a source-breaking change. Do it before stable release, migrate examples/tests together, and do not bury cycles with aliases. Third-party dependencies are acceptable in pkg; importing application internals is not.

**Completion:** No business-specific constants/config in reusable packages; no runtime machinery embedded among editable schedules; native task/router APIs remain available.

### 10. Make cache and storage APIs precise, without wrapper growth — P2, M

**Problem/evidence:** Cache Store mixes raw-byte operations with JSON `any` convenience, loaders and Forever; several convenience methods are repeated by every driver and telemetry wrapper. Storage already exposes native Go CDK buckets appropriately, but all cloud SDK implementations are compiled into one package even for a local-only app. No measurements yet establish the practical cost.

**Affected:** pkg/cache drivers/telemetry, pkg/storage, config/filesystems.go, examples and dependency graph.

**Approach:** Separate storage behavior from serialization convenience. Keep a coherent byte-oriented cache contract and add typed free functions such as `cache.Remember[T]`; retain batch operations where supported without inventing an interface per method. Define explicit no-expiry behavior rather than implying an arbitrary maximum duration is eternal. Preserve bounded values, namespace-scoped clearing, cancellation and partial-batch semantics. Keep native Go CDK operations; measure build graph, binary size and cold-build time before splitting provider constructors into subpackages. App selects providers explicitly, without side-effect registration magic.

**Benefit:** Fewer repeated driver methods and type assertions, with honest backend semantics and measurable optionality.

**Dependencies:** 9; package-level interfaces should stabilize before public extraction.

**Migration risks:** Cache serialization/number types, TTL rounding, stored entries and eviction behavior may change. Use versioned cache namespaces when encoding changes. Provider package extraction changes imports, not object keys or bucket ownership.

**Completion:** Same contract suite runs against memory, PostgreSQL and Redis; typed cache examples compile; storage optimization claims include measurements.

### 11. Consolidate observability policy and preserve useful diagnostics — P2, M

**Progress:** Service identity now belongs to `App.ServiceName`; metrics and tracing consume it independently. `OTEL_SERVICE_NAME`, its default, validation behavior and exported resource attributes remain unchanged. Task factories now use `asynqx.NewTask` to initialize propagation headers while retaining native tasks and options. Request → enqueue → worker correlation is covered, including Redis persistence of headers and constructor options. Native scheduler attempts remain independent of registration context. The shared rate limiter now optionally records bounded decision/duration metrics and categorized spans without keys or underlying error messages. Opt-in HTTP dependency readiness now probes configured database, queue Redis and limiter Redis with a shared deadline and bounded concurrency; liveness remains independent. Reviewed Gin/Bun/Redis upstream adapters and recorded privacy-based retention decisions in `docs/research/observability-adapters.md`. Task and event logs now carry bounded error categories; drain timeout has an explicit operational log. Existing native queue backlog metrics and scheduler callbacks remain. Completed the signal inventory in `docs/observability.md`. A production-provider regression test verifies that HTTP operations complete while an OTLP export is blocked and its span buffer overflows; existing tests cover safe export-failure reporting and shutdown deadlines. Scheduler failures now include safe categories. Phase 11 is complete with documented limits: GoChannel remains ephemeral, telemetry can be dropped, readiness is opt-in connectivity checking, and no outbox is present.

**Problem/evidence:** Custom adapters exist across HTTP, DB, Redis, queues, events and mail. Most already bound labels and redact sensitive values. However task logs/metrics still use job terminology, trace service metadata is coupled to tracing config, ordinary native Asynq tasks without header maps do not propagate producer context, and broad error sanitization sometimes leaves only an error type for operators. Readiness currently means startup completed, not continuous dependency health.

**Affected:** pkg/logging/tracing/metrics, platform middleware, asynqx, app infrastructure, health routes, dashboards and observability deployment.

**Approach:** Keep provider construction centralized and middleware optional. Compare each custom adapter to maintained upstream instrumentation, including otelgin, and replace only when privacy, propagation/trust, route-cardinality and shutdown behavior remain correct. Establish one vocabulary: request_id, trace_id/span_id in logs, task_id/task_type, queue, handler, event type and delivery attempt. Treat metric renaming as a dashboard migration. Design an explicit supported task-construction path for trace headers while still accepting native tasks; do not claim transparent propagation for tasks that cannot carry it.

Use structured internal error categories and safe causes at the reporting owner, with public messages separate. Add signals for rate-limit rejection/backend failure, local event exhaustion, queue backlog, scheduler/outbox status and shutdown timeout. Keep liveness independent of remote services; introduce bounded readiness checks only for dependencies required by that process profile.

**Benefit:** Operators can follow failures without leaking credentials, and business code stays free of tracing/header mechanics.

**Dependencies:** 3–5 and 9. [Official otelgin interface](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin).

**Migration risks:** Changed trace parent/link semantics, metric names, log fields, alert queries and readiness-triggered traffic removal. Test redaction and cardinality before adopting upstream defaults.

**Completion:** HTTP→task and HTTP→event examples have documented correlation; no IDs/payloads enter metric labels; collector failure does not block business work.

### 12. Build one truthful learning path and fast development workflow — P2, M

**Progress:** Replaced the stale Orders first-run guide with a minimal `.env.quickstart.example`, disposable PostgreSQL/Redis setup, explicit migrations, real Identity registration and account lookup. Verified readiness 200, registration 201, duplicate email 409 and CLI lookup against fresh infrastructure. Corrected shared health-route references. Updated the first-module tutorial to use the standard response envelope and current Media/Notifications/Identity examples; extracted tutorial code compiles. Rewrote the structure guide around actual package locations and the accepted shared-definition boundary. Local links in both guides resolve. RabbitMQ and NATS are now opt-in through Compose/Tilt `extra-brokers` settings and removed from app startup prerequisites. The local-development guide uses current Identity commands and describes profile limits. MinIO is now opt-in through Compose `storage` and Tilt `--storage=true`; the default stack omits object storage and bucket initialization. Both rendered configurations were validated without applying workloads. Rewrote request/response guides around current API adapters, pagination, response envelopes, exact error codes and Identity behavior; the complete HTTP example compiles and local links resolve. Updated console and queue guides to current Identity reads and Notifications welcome-task behavior, corrected CLI initialization claims and removed stale entry-point commands. The standalone Cobra example compiles and guide links resolve. Updated testing guides to actual module/package paths and ran the standalone HTTP validation test. Seeder documentation now labels unimplemented examples and migration ownership points to current module directories. Documentation tests/build and local links pass. Phase 12 complete: final audit corrected stale scheduling, worker registration, routing and error-handling guidance. All local file links in top-level Markdown guides resolve, documentation tests pass and all 38 pages build. Remaining Orders task snippets are explicitly hypothetical extensions. Profile evaluation and tutorial smoke checks are recorded above; this is not a live deployment or an external-link availability audit.

**Problem/evidence:** Good long-form documentation coexists with removed Orders examples, broken package links, historical phase notes and competing descriptions. The environment example exposes many advanced settings before a first successful request. Local infrastructure includes brokers unused by current messaging. The custom docs pipeline is functional and its generated Markdown is synchronized; replacing it is not itself a priority.

**Affected:** README, AGENTS, docs source/navigation, package READMEs, examples, Compose/Tilt and .env examples.

**Approach:** Replace historical descriptions with current capabilities. Offer a minimal getting-started configuration and an advanced reference, not multiple competing architectural tutorials. Make unused brokers, cloud storage and observability opt-in development profiles. Teach in order: first request → module → validation/errors → PostgreSQL/transactions → tasks → local events → durable delivery → operations. Use Identity as the rich reference and one genuinely small module as the introductory example. Clearly label Media and Notifications' current scope rather than advertising upload processing or omnichannel delivery that does not exist.

Keep Markdown as the source of truth. Add link checks and compilable extracted examples before replacing the site generator. Later add a small module/task scaffold only after the conventions survive items 6–8; it should generate ordinary editable Go, with no runtime discovery or implicit dependency injection. Standardize CLI verbs such as `routes list` and `migrate up` in one documented pre-release change rather than retaining mixed colon/space syntax forever.

**Benefit:** Faster first success and fewer concepts required before a developer can make a change.

**Dependencies:** Correct the false quick-start claims immediately under 1; rewrite deep tutorials alongside their owning phases, then verify after 3–11.

**Migration risks:** CLI renames break scripts; preserve document URLs or redirects; a generator can fossilize bad conventions if added too early.

**Completion:** A clean-checkout walkthrough works without tribal knowledge; every advertised example is executable and every advanced feature states its failure guarantees.

### 13. Establish publication and production evidence — P0 baseline, P3 completion; M/L

**Progress:** CI now separates fast checks from required PostgreSQL/Redis integration. The reusable integration script rejects missing backend settings; CI also builds commands/examples, builds docs and runs govulncheck v1.8.0. `docs/release-readiness.md` records the support matrix and remaining gates. Local verification passed: uncached full race suite with disposable PostgreSQL/Redis, vet, command/example builds, documentation checks and govulncheck (no reachable findings; four findings only in required modules outside imported vulnerable packages). Fixed a stale password test to match the existing six-character policy without changing policy. Publication identity, private security reporting, dependency/license inventory, reproducible release artifacts, measured recovery and performance evidence remain open.

**Problem/evidence:** Tests cover significant hard cases, but opt-in backend suites can silently skip, there is no discovered CI workflow, and security/performance/release claims are broader than measured evidence. Cloud adapters are not equivalent to end-to-end supported app backends.

**Affected:** CI, tests/test helpers, deployment profiles, release docs, package examples, security policy and changelog.

**Approach:** Add separate fast and required integration jobs using disposable PostgreSQL/Redis; missing backend configuration must fail the integration job, not skip it. Exercise supported MySQL/cache/cloud capabilities in explicitly separate jobs, or label them experimental. Run race/vet and govulncheck using the chosen supported toolchains, test fresh install and upgrade migrations, and build docs/examples. Test process termination, Redis outages, database outages, scheduler duplication, credential lifecycle and duplicate deliveries. Publish a dependency/license inventory, security-reporting process and versioned upgrade notes. Pin release inputs appropriately. Measure startup, shutdown, representative handler/queue throughput, DB query counts and resource bounds before making performance claims.

**Benefit:** “Production-grade” becomes demonstrated behavior rather than an architectural label.

**Dependencies:** Start the baseline with 1. Full exit gate depends on every advertised capability's phase.

**Migration risks:** Flaky shared test infrastructure and excessive cloud cost. Isolate fixtures, use deterministic failure coordination, and keep unsupported integrations out of the required core matrix.

**Completion:** A release can be reproduced from a clean checkout, its promises match a test/support matrix, and operational recovery procedures have been rehearsed.

## Proposed structure

This is an ownership map, not a requirement to create every shown directory immediately. Tests remain beside their packages. Avoid placeholder packages.

```text
cmd/
  app/                         combined development/production entry point
  worker/                      worker preset, no implicit scheduler
  cli/                         Cobra entry point, also exposes explicit serve roles

internal/
  platform/
    app/
      app.go                   app lifetime and public entry points
      bootstrap.go             explicit execution-specific assembly
      modules.go               construct and register chosen modules
      schedule.go              editable schedule definitions only
      runtime.go               tested startup/drain ownership, kept private
    config/
      config.go  load.go        composition and immutable loading
      app.go  http.go  database.go
      queues.go  messaging.go  identity.go
      cache.go  filesystems.go  mail.go  observability.go  ratelimit.go
    dependencies/              startup registration targets; app-owned resources
    api/
      decode.go  binding.go
      response/                HTTP envelope and rendering
      middleware/              ingress policy and HTTP instrumentation
    cli/                       Cobra integration and scheduled command adapter
    messaging/                 Watermill composition and application policies
    seeders/                   explicit named seed operations

  identity/
    module.go                  ordinary explicit registration
    domain/                    policies and domain failures
    service/                   accounts, challenges, sessions, passwords, social
    repository/                narrow persistence contracts
      postgres/                atomic SQL operations
    delivery/
      http/                    routes, requests, responses, few cohesive handlers
      cli/                     commands
    integrations/              provider-specific sign-in verification
    mail/                      Identity email content
    migrations/                owned immutable SQL history

  notifications/               same conventions; only needed directories
    tasks/                     task names, payloads, constructors
    delivery/tasks/            native Asynq handlers/routes
    delivery/events/           event handlers/routes
    ...
  media/                       keep current scope explicit; expand only deliberately
    ...
  shared/
    entities/                  all modules’ entities/relationships, intentionally central
    enums/                     all modules’ business enums, owner documented
    contracts/
      auth/  events/  notifications/   events includes every module’s contracts
    domain/                    used Entity/value types; optional AggregateRoot
    migrations/                genuinely application-wide schema

pkg/
  apperror/
  pagination/                  page/per-page types and bounds
  asynqx/                      native client integration, middleware, execution support
  database/
    migrate/                   generic Goose integration
  cache/
    memory/  redis/  database/
  httpserver/
  logging/  metrics/  tracing/
  mail/  mailview/
  otp/  password/  token/
  ratelimit/  redis/
  storage/                     native Go CDK; provider split only after measurement

docs/                          canonical Markdown and generated site
examples/                      executable examples and compilable usage
deployments/                   minimal defaults plus optional profiles
```

Do not move everything to pkg. App config, module composition, route policy and business contracts belong in internal. Move a capability because its interface is reusable and independently testable, not because it is stateless. `pkg` is a repository convention, not a Go keyword or a requirement from other frameworks.

## Representative developer experience

The following are proposed interface sketches, not functions claimed to exist today. Constructor errors are handled by assembly; snippet fragments show the developer-facing shape.

### Application lifetime

```go
func run(ctx context.Context) (err error) {
    cfg, err := config.Load()
    if err != nil { return err }

    app, err := app.New(ctx, cfg, app.Process{
        HTTP: true,
        Workers: true,
        Scheduler: false,
    })
    if err != nil { return err }
    defer func() {
        err = errors.Join(err, app.Close(context.WithoutCancel(ctx)))
    }()
    return app.Run(ctx)
}
```

Selection is supplied before construction so disabled resources are not opened. A `Serve` convenience performs the same sequence. Signals and process exit remain in main. CLI help/inspection uses the definition path; `migrate up` opens its own bounded DB session. There is one selection policy regardless of entry point.

### Explicit module composition

```go
repo := postgres.New(db)
users, err := service.NewUsers(repo, publisher)
if err != nil { return err }

// A named struct is useful here because Identity has several capabilities.
return identity.Register(targets, identity.Services{
    Users: users,
    Verification: verification,
    Sessions: sessions,
    Passwords: passwords,
    Social: social,
})
```

A two-dependency module continues to use `media.Register(targets, mediaService)` where that is clearer. No requirement exists to copy Identity's internal size into every module.

### Thin HTTP handler

```go
type GetUserRequest struct {
    ID string `uri:"id" binding:"required,uuid"`
}

func (h *UserHandler) Get(c *gin.Context) {
    var request GetUserRequest
    if err := api.DecodeURI(c, &request); err != nil {
        response.Error(c, err)
        return
    }
    id := uuid.MustParse(request.ID) // DecodeURI has validated the UUID.
    if err := middleware.OwnUser(c, id); err != nil {
        response.Error(c, err)
        return
    }
    user, err := h.users.Get(c.Request.Context(), id)
    if err != nil {
        response.Error(c, err)
        return
    }
    response.OK(c, newUserResponse(user))
}
```

Routes select body limits and throttling. Binding validates the URI request before UUID conversion. Request IDs, tracing, error rendering and version metadata remain global policies. Authorization is explicit; there is no automatic generic CRUD controller.

### Native tasks, centralized routing

```go
// In a module operation: business payload only.
task, err := tasks.NewWelcome(userID)
if err != nil { return err }
_, err = s.queue.EnqueueContext(ctx, task)
return err

// In module task routes:
mux.HandleFunc(tasks.Welcome, handler.Welcome)

// In central queue configuration:
Routes: map[string]string{
    tasks.Welcome: "high",
},
```

One construction convention initializes trace headers; app installs middleware once. Native Asynq options, ResultWriter and Inspector remain accessible. App config must not import a service implementation; a task-contract package is suitable for the routing constant. Existing queued task names and payloads remain stable through package moves.

### Local event subscription

```go
router.AddConsumerHandler(
    "notifications.user_registered",
    events.UserRegisteredType,
    subscriber,
    eventhandlers.UserRegistered(notifications),
)
```

The handler name identifies this subscription; the event type identifies the fact; the subscriber owns transport delivery. No CQRS or universal message interface is required. Use this local transport for best-effort work only. Required follow-up intent goes through a transaction or the explicit durable profile.

### Scheduling

```go
// Editable definitions at the top of app/schedule.go.
var commandSchedules = []CommandSchedule{
    {Every: "*/5 * * * *", Command: identity.PruneSessionsCommand},
    {Every: "*/5 * * * *", Command: identity.PruneVerificationCommand},
}
```

Assembly validates all definitions with one error check and registers them before the designated scheduler starts. The scheduler owns when; modules own what. Scheduled Cobra commands remain an allowlist of fresh factories, never shell strings.

### Typed caching

```go
user, err := cache.Remember[UserSummary](ctx, store, key, time.Minute,
    func(ctx context.Context) (UserSummary, error) {
        return repository.Summary(ctx, userID)
    },
)
```

The loader's semantics, stampede behavior, error policy and serialization are explicit; no generic repository or implicit cache invalidation is introduced.

## Terminology decisions

| Concept | Recommended usage |
| --- | --- |
| Application-owned startup | app, bootstrap, registration, run, drain, close |
| Infrastructure acquisition | Open when acquiring I/O resources; New for ordinary construction |
| Background work | task, task type, task handler, queue, worker, scheduler |
| Published fact | event; event type; message for its Watermill transport representation |
| Delivery registration | handler name/subscription name, distinct from event type |
| HTTP | route, handler, middleware, request, response |
| Business operation | service method with a domain verb; interfaces only at useful substitution seams |
| Persistence | repository with concrete operations; PostgreSQL adapter |
| SQL evolution | migration; baseline only before a supported released history |
| Telemetry | task terminology in new logs/metrics, with explicit migration for current job fields |

Avoid repeated rename cycles. Preserve names already clear (`api`, `messaging`, `asynqx`, `apperror`); change names where they cause a real distinction to be lost.

## Execution sequence and review gates

1. Freeze the supported product and truthful quick start; add baseline CI (1).
2. Address scheduler ownership, reset safety, mail legacy path and Redis isolation (2).
3. Refactor execution-specific boot and immutable config together (3–4).
4. Decide required registration side effects and implement their consistency path (5).
5. Refine Identity using the established lifecycle/consistency model (6).
6. Apply one module/HTTP pattern to Media and Notifications (7–8).
7. Extract proven technical integrations and simplify capability APIs (9–10).
8. Align telemetry and developer documentation with actual behavior (11–12).
9. Complete the release evidence and support matrix (13).

Every phase should end with a diff review for dependency direction and API size, focused failure-path tests, the repository baseline, and examples matching actual usage. Do not combine file moves, schema changes, security-flow changes and public renames in one change. Keep one meaningful outcome per reviewable increment.

The first substantial implementation should be **execution-specific boot plus scheduler ownership**, after the support contract and immediate safety fixes. Another broad folder reshuffle would have less impact on developer experience.

## Implementation checkpoint — 2026-10-05

The approved plan remains in progress; this checkpoint does not mark every phase
complete. Existing working-tree changes were preserved.

Implemented foundations:

- Truthful root capability matrix, MIT license (owner selected), contribution and
  security guidance, pinned GitHub Actions verification with PostgreSQL and Redis.
- Removed the unused unsafe mail factory. Added Redis limiter/cache/queue isolation,
  environment-gated destructive refresh confirmation, scheduler opt-in defaults and
  explicit runner selection. Worker entry points cannot implicitly schedule.
- Offline CLI help/completion and database-only migrations. Central migration source
  registration is independent of module service construction.
- Isolated dotenv snapshots and pure `config.FromEnvironment`; Identity-specific
  validation now lives beside Identity configuration.
- Extracted app-independent Asynq and mail rendering to `pkg/asynqx` and `pkg/mailview`.
  Renamed notification task handlers to `delivery/tasks`; borrowed publishers no
  longer expose Close. Migrated task telemetry names and the bundled dashboard.

Remaining before completion includes role-specific acquisition for ordinary
application commands, the complete configuration cleanup, durable signup follow-up
processing, Identity challenge/limit separation, HTTP/shared-type refinements,
cache API review, readiness/diagnostic improvements, documentation reconciliation,
and release/security validation.

Owner decision: both initial Media setup and the welcome notification must be
guaranteed after signup. Do not claim that current GoChannel delivery meets this
requirement. Commit durable pending intent with account creation; demonstrate
recovery and idempotency before removing partial-success handling.

Verification for this checkpoint: `go test -race ./...` passed with disposable
PostgreSQL and Redis (queue, cache and limiter suites enabled); `go vet ./...` and
the documentation tests/build passed. A fresh migration run succeeded while the
configured Redis endpoint was unreachable. `govulncheck` reported no reachable
vulnerabilities; four findings exist in required modules outside imported
vulnerable packages. MySQL, Redis Cluster and external cloud suites were not run.
The GitHub workflow was added and inspected locally, not executed on GitHub.

### Durable registration implementation

The required signup reactions now use Watermill SQL v4.1.5, with a native event
insert inside Identity's transaction and independent PostgreSQL consumer groups.
The welcome consumer writes its inbox item directly, so no database-to-Redis relay
is needed for this workflow. Native Asynq remains unchanged for explicit tasks.
The service-level post-commit publication dependency and its partial-success
responses were removed. Ephemeral poison forwarding was removed.

A new additive migration owns the message/offset tables; application startup never
applies DDL. Handler timeouts, redelivery, safe diagnostics, pool-size checks and
`events:prune` provide bounded processing and acknowledgement-aware retention.
Historical accounts require explicit repair if their former local reactions were
lost. Mixed-version producers must finish rollout before the guarantee applies.

This completes the signup consistency implementation, not the remaining Identity,
HTTP, config, cache and release review phases.

Verification: the full `go test -race ./...` suite passed with disposable
PostgreSQL 17 and Redis 7, including rollback, delayed consumer startup,
redelivery, independent consumer acknowledgements, safe retention and native
Asynq delivery. `go vet ./...`, documentation tests and the 38-page documentation
build passed. Fresh migrations and `events:prune` ran successfully against the
disposable database; command help required no database connection. `govulncheck`
reported no reachable vulnerabilities (four findings remain in required modules
outside imported vulnerable packages). MySQL, Redis Cluster and external cloud
suites were not run. Existing accounts are not automatically backfilled.

### Revised event direction: GoChannel

The owner explicitly selected GoChannel for pub/sub and signup reactions, replacing
the earlier required-delivery decision. PostgreSQL event publication, subscribers,
pruning and the Watermill SQL dependency are removed. Account operations publish
locally after persistence; publication failure does not undo the committed account.
Media setup and welcome notifications can be lost during downtime or restart.
The transport-removal migration preserves applied history and discards its retired
pending-event tables. Notifications is the intended owner of delivery channels;
database delivery is implemented, while its email/SMS/push consolidation remains
separate work. Do not claim those providers are implemented by this change.

Verification of this revision: the full race suite passed with disposable
PostgreSQL and Redis. Focused reruns passed after final cleanup, including social
login publishing only on first registration and migration removal of both retired
event tables. Vet, documentation tests/build and whitespace checks passed.
The vulnerability scan found no reachable vulnerabilities; four findings remain
in required modules outside imported vulnerable packages. External cloud,
MySQL and Redis Cluster suites were not run. The cleanup migration was tested
on disposable databases, not manually applied to the user's development database.

### Identity operation boundaries — phase 6

Implemented shared `Challenges` and independent `AuthLimits`; EmailVerification
no longer lends private mechanics to password or social login. Verification policy
now owns OTP lifetime/cooldown/attempts, while AuthLimitPolicy owns rate budgets.
Existing digest bindings, Redis key scopes, defaults and public errors are preserved.
Operation inputs distinguish proofs, devices, OTP/password signup and login context.
HTTP paths and request/response shapes remain unchanged.

App assembly constructs each operation once and passes named `identity.Services`
to registration. Authentication no longer requires enabling standalone email
verification; required challenge resources, CLI pruning and scheduled pruning stay
active whenever either capability uses challenges. Unverified public `/users`
creation remains supported with its existing behavior; no product-flow removal,
schema rewrite, notification-provider work or durable event transport was added.

Next: apply the module-construction conventions consistently (phase 7), then refine
the HTTP developer interface (phase 8). Earlier lifecycle/config cleanup and the
remaining infrastructure, cache, observability and release work are still pending.

Verification: focused PostgreSQL/Redis authentication tests and the full
`go test -race ./...` suite passed. The HTTP integration workflow passed both with
and without standalone email verification, including OTP/password flows and
challenge-cleanup registration. Rate-key/budget compatibility and auth-only
resource validation are covered. `go vet ./...`, documentation tests/build and
whitespace checks passed. `govulncheck` found no reachable vulnerabilities; four
findings remain in required modules outside imported vulnerable packages.
MySQL, Redis Cluster, external email and live Google-provider suites were not run.

### Module construction and ownership — phase 7

Service constructors now return concrete implementations: Identity's NewUsers
returns *Users, while Media and Notifications return *Service. HTTP and CLI define
only the interfaces they consume; one-operation event/CLI adapters receive functions.
Task handlers depend only on their task operation. The module-wide service interfaces
and nil embedded test interfaces are removed. Identity keeps its named Services
registration group; smaller modules pass their single concrete service directly.

Notifications now uses the platform authenticated principal like Identity/Media,
without its Recipient callback, second context key or MustGet assertion. Registration
requirements are documented and regression-tested before route/command/task mutation.
Entities, enums and event contracts retain their shared locations and module ownership.

Removed the unused shared registration input and unused specification/date-range,
money, phone and token helpers. These had no consumers; the token helper incorrectly
coupled permissions to S3 types. Retained Entity and optional AggregateRoot. Health
routes moved from shared to platform/api without changing liveness/readiness behavior.
The shared ownership, dependency injection and module-registration guides now describe
the real modules, combined Bun entities and explicit cross-module transaction.

Verification: full race tests passed with PostgreSQL/Redis, including authentication,
GoChannel fanout, task delivery and Media/inbox rollback. Targeted registration and
principal-isolation checks passed. Vet, documentation tests/build and whitespace
checks passed. Govulncheck reported no reachable vulnerabilities, with four findings
in required modules outside imported vulnerable packages. MySQL, Redis Cluster and
external cloud/provider suites were not run. No schema or public route changes.

Next is phase 8, the HTTP developer interface. The wider lifecycle/config,
infrastructure/cache, observability and documentation/release phases remain open.

### Phase 8 checkpoint — HTTP contracts and strict decoding

Implemented the first HTTP interface increment:

- Moved the unchanged JSON envelope to `response.Envelope` and public error
  details to `response.Problem`, beside rendering. Updated all Go callers.
- Moved pagination and its bounds/serialization tests to `pkg/pagination`.
  Shared remains focused on application contracts, entities and enums.
- `DecodeJSON(c, dst)` now has a bounded 1 MiB default.
  `DecodeJSONLimit(c, dst, limit)` retains endpoint-specific limits.
  Existing Identity limits and response fields are preserved.
- Automatic JSON selection now uses the same strict parser. Explicit
  `DecodeWith(..., binding.JSON)` remains the native Gin escape hatch.
- Replaced obsolete Orders handler documentation with current decoding,
  authorization, response and pagination usage.

Compatibility: Go imports/type names and the explicit-limit decoder name change;
all repository callers were migrated. HTTP routes, envelope field names,
pagination representation and existing Identity limits remain unchanged.
Automatic JSON decoding now rejects unknown fields and trailing data, which
was the intended strict default but was previously bypassed by that adapter.

Phase 8 remains open: OpenAPI generation and endpoint contract checks are the next
increment. No API specification or complete endpoint coverage is claimed here.

Validation: `go test -race ./...` passed with disposable PostgreSQL 17 and
Redis 7 configured for database, queue, cache and limiter suites; `go vet ./...`
passed. Documentation tests and the 38-page build passed, as did diff whitespace
checks. External provider/cloud suites were not exercised.

### Phase 8 completed — generated API reference and contract checks

Added build-time Swaggo v1.16.6 generation of OpenAPI 2.0 (Swagger) JSON for
all 25 business operations across Identity, Media and Notifications. Annotations
live above handlers; request/response schemas come from the existing transport
types. The only response extraction is a named UnreadCountResponse; SessionPage
is an alias of the existing generic page. Neither changes serialization.

UUID overrides and transport-local timestamp/nullable metadata preserve actual
wire types. Generation emits no Go registry or startup work and adds no runtime
dependency or dashboard. docs/api/README.md explains feature flags, authentication,
strict JSON limits, optional routes, best-effort GoChannel reactions and schema
limitations. Infrastructure endpoints are intentionally outside the business spec.

Contract tests register real module routes without infrastructure and compare
methods, paths, operation IDs and public/protected groups with the document.
They check required path parameters, references, UUIDs, timestamps, nullable
fields, pagination item types, sensitive-field exclusion and selected validation
constraints. Protected requests must stop at authentication. Representative
registration success, malformed input, field errors and oversized bodies exercise
the real handler, decoder and renderer. These are focused contract checks, not a
new JSON Schema validator or exhaustive business-outcome tests.

CI regenerates the document and rejects drift before running the normal suite.
Generation was warning-free and byte-for-byte reproducible locally. Full race
tests passed with disposable PostgreSQL/Redis, vet passed, documentation tests and
the 38-page build passed. Govulncheck found no reachable vulnerabilities; four
findings remain in required modules outside imported vulnerable packages.
External provider/cloud suites and the GitHub-hosted workflow were not run.

Next: phase 9, reduce infrastructure surface area by actual usage and ownership.

### Phase 9 checkpoint — database capability ownership

Moved reusable Goose execution, registry validation, refresh and template
publication from internal/platform/migrations to pkg/database/migrate.
Application SQL and source registration remain in their existing owners; no
applied migration file or timestamp changed. Updated Go callers and current guides.
Backend/application fixture tests moved to internal/platform/testdb; the reusable
package and its remaining tests do not import internal application packages.
Its local dependency graph contains only pkg/database and pkg/logging.

Removed pkg/transaction's broad Manager contract. Composition now declares an
Execute-only dependencies.Transactions interface. Media still accepts a callback.
The concrete database.TxManager retains Begin, Commit, Rollback, Execute and
Savepoint unchanged, including nesting, rollback-only and cancellation semantics.

This is a Go source-breaking package/contract cleanup before stable release:
external users migrate imports to pkg/database/migrate and request only their
needed transaction operation (or use the concrete manager). There are no SQL,
configuration or runtime behavior changes.

Validation: go test -race ./... passed with disposable PostgreSQL and Redis;
go vet ./..., documentation tests/build and diff whitespace checks passed.
MySQL and external provider/cloud suites were not exercised.

Phase 9 remains open: reusable queue runner/admin/schedule machinery and the
unused NATS/RabbitMQ/template package inventory are the next increment.
Asynq middleware/mailview extraction and borrowed publisher cleanup were already
complete when this increment began.

### Phase 9 completed — queue machinery and supported infrastructure

Extracted worker startup/shutdown and handler-call joining to pkg/asynqx.Worker.
It borrows the native server, handler and Redis pool; owner cleanup still waits
for Run to return. SchedulerRunner uses the same Run/Ready lifecycle, leaving
native schedule registration available. App retains service ordering, signal and
resource ownership, Redis/configuration choices and dashboard authentication.

Moved schedule preparation, routing validation and registration rollback to
asynqx.RegisterSchedules with TaskSchedule/CommandSchedule definitions.
App/schedule.go now contains only editable timing and enablement choices.
Moved Cobra inspector commands to platform/cli.NewQueueCommand; commands borrow
the inspector and still support offline help without infrastructure. The tiny
application metrics registration adapter remains in composition, where creating
another exported wrapper would add no useful boundary.

Moved the handler-join and schedule rollback tests alongside their implementation.
Existing Redis integration tests continue to cover publication, shutdown,
results, policies, central routing and administration. No task names, payloads,
queues, schedule timing, CLI commands or dashboard routes changed.

Removal inventory:
- pkg/nats: Open; no application/example consumers.
- pkg/rabbitmq: Config/Open; only package-local tests consumed them.
- pkg/template: Config, Renderer, HTML/Markdown renderers and constructors, and
  ErrTemplateNotFound; no application/example consumers.
These unused source APIs were removed before stable release. A future broker
integration should select the native library for its actual requirements rather
than revive wrappers speculatively. Email uses pkg/mailview; other template
consumers can use the standard library. Historical research remains historical.

Removed direct NATS/AMQP dependencies and their unused nkeys/nuid dependencies.
No remaining dependency version changed. Documentation generation keeps Goldmark
through a tools build tag, so go mod tidy retains its real build-time dependency
without an unused runtime template wrapper. Updated template/scheduling guides,
package ownership guidance and native Asynq integration documentation.

Validation: full go test -race ./... passed with disposable PostgreSQL/Redis;
go vet ./..., documentation tests/38-page build, deterministic API generation,
go mod tidy -diff and whitespace checks passed. The Asynq dependency graph has
no internal imports. Govulncheck found zero reachable vulnerabilities, with four
findings in required modules outside imported vulnerable packages. External
cloud/provider, MySQL and Redis Cluster suites were not exercised.

Next: phase 10, clarify cache APIs and measure storage provider dependency costs.

### Phase 10 checkpoint — typed cache loading and storage measurements

Replaced duplicated driver/telemetry Remember and Forever methods with one generic
cache.Remember[T] function. The loader determines T; both cache hits and loads
round-trip through the same JSON decoder. Dynamic any callers keep json.Number
precision. All repository callers and the runnable cache example were migrated.

Removed the misleading Forever helper. Cache writes continue requiring positive
explicit TTLs; true no-expiry entries are unsupported. MaxTTL remains a documented
finite upper bound. This changes Go source APIs but not existing JSON bytes or
driver expiration semantics. Type/representation changes require versioned keys.

Added shared backend checks for typed structs, int64 precision above 2^53,
caller-owned results and cached type mismatches that must not invoke the loader.
Existing cancellation, expiry, null, capacity, admission failure and telemetry
checks remain in place.

Measured storage dependency counts, stripped binary size and isolated-cache build
time using two minimal programs; full details and limitations are in
docs/research/storage-build-cost.md. Current storage.Open with local configuration:
864 packages, 42,522,050 bytes, 18.02 seconds. Native local-only Go CDK floor:
391 packages, 11,817,298 bytes, 7.60 seconds. These are single local observations,
not a demonstrated improvement to the application. Merely splitting providers
while app imports all of them would retain those dependencies. Preserve current
runtime-configurable provider selection; revisit extraction for an actual
local-only build requirement.

Validation: go test -race ./... passed with disposable PostgreSQL/Redis, including
shared cache contracts; go vet ./..., documentation tests/38-page build and
whitespace checks passed. MySQL, Redis Cluster and external cloud suites were not
exercised. No storage implementation or provider configuration changed.

Phase 10 remains open: separate bulk byte storage from JSON serialization while
retaining native batching, bounds, cancellation and partial-failure behavior.

### Phase 10 completed — byte-oriented batches and typed JSON helpers

Store.GetMulti/SetMulti now operate on map[string][]byte. Removed JSON decoding
from memory, SQL and Redis drivers; telemetry wraps those raw operations.
Added cache.GetMulti[T] and cache.SetMulti[T] as JSON convenience functions above
the store. The entire JSON batch is encoded before any storage call, and each
driver validates all raw keys, entry sizes, total size and TTL before writing.

Native SQL batch queries and Redis single-key pipelines remain intact. Bounds
remain 1,000 keys/items and 8 MiB of key/value data; per-entry limits, expiry,
namespace-scoped clearing, cancellation and partial-backend-failure semantics
remain unchanged. Nil raw bytes are stored as an empty value, including SQL's
non-null byte column. Empty values remain distinct from absent keys.

Migrated all Go callers, examples, app tests and documentation. This is a
source-breaking batch method signature change: former JSON callers use the
package functions with their existing values. JSON bytes do not change, so the
migration does not rewrite stored data. Raw operations intentionally accept
non-JSON payloads. Telemetry records storage outcomes; encoding/decoding errors
are returned at the JSON helper boundary.

Shared memory/PostgreSQL/Redis tests cover binary payloads, empty values,
single/bulk interoperability, input/output ownership, duplicate/missing keys,
raw preflight bounds, cancellation, typed integer precision and invalid JSON.
Existing tests retain batch admission/partial-failure and namespace coverage.

Validation: full go test -race ./... passed with disposable PostgreSQL/Redis,
including app lifecycle and shared cache contracts. go vet ./..., documentation
tests/38-page build and whitespace checks passed. MySQL, Redis Cluster and
external cloud-provider suites were not run. Storage implementation remains
unchanged, following the measured decision in the preceding checkpoint.

Next: phase 11, observability policy, diagnostic usefulness and readiness semantics.
