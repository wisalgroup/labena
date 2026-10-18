# Framework maturity implementation plan

This records the earlier maturity pass. The subsequent approved harmonization
work and verification are tracked in [the current roadmap](../review/07-refactoring-roadmap.md).

Approved scope: consolidate the existing application template through accurate
usage guides, a small set of conventions, independently usable capabilities and
verified composition. Review every package under `internal/platform`, including
its subpackages. Optional queued mail and consolidation of `pkg/mail` into `internal/platform/mail`
are part of this scope.

This plan supersedes older phase-status claims for the next maturity pass. Earlier
plans remain historical records. Preserve nested business modules, shared business
entities, owner-defined service/repository interfaces, centralized schedules and
eager application construction for CLI help. Do not add tests inside app, service
or repository packages.

## Phases and gates

| Phase | Scope | Dependencies | Status |
| --- | --- | --- | --- |
| 1 — Accurate baseline | Canonical quickstart; correct roles, capability claims, task examples and inspection APIs; full platform inventory | None | Complete; see verification record |
| 2 — Proven learning path | Publish navigation; executable first module, persistence/notification and background-work chapters; independent mail example | 1 | Complete; published navigation and compiled workflow examples verified |
| 3 — Small API alignment | Single push-registration callback; session-policy validation authority; construction/ownership/error conventions; consistent CLI vocabulary | 2 | Complete; API decisions and verification recorded below |
| 4 — Composition verification | Verify transaction rollback, duplicate processing, authorization, queued mail, and graceful shutdown; retain existing meaningful tests | 3, except bounded mail work | Complete for the required PostgreSQL/Redis gate; reverified after phase 3; external deployment/provider evidence remains phase 6 |
| 5 — Complete guides | Identity, capabilities, operations, configuration/CLI/API reference, troubleshooting and generated-doc drift checks | 3–4 | Complete for shipped behavior; 51 published pages, checked configuration defaults and local-link validation |
| 6 — Release evidence | Public module path, support/security policy, fresh install and required integration gates, deployment termination and recovery evidence | 4–5 | Local verification and release tooling implemented; publication awaits public module path, private security contact, tagged source and target-specific gates |

For each change: inspect → understand → write the usage → simplify only the
responsible implementation → standardize → validate. Prioritize fewer concepts
and edit locations, not a file-count target. The initial release remains an
application template: internal capabilities are independently constructible inside
that application, while `pkg` is application-independent technical code.

## Platform package review

Inventory from `go list ./internal/platform/...`. This records source-level
responsibilities, dependencies and lifecycle review, not live provider or deployment
certification. Update the inventory when adding a package. A retained package does
not need cosmetic restructuring to count as reviewed.

| Package | Responsibility and integration contract | Decision / follow-up |
| --- | --- | --- |
| `api` | Native Gin setup, bounded decoding, validation and readiness routes | Retain; guide must distinguish strict JSON from optional native binders |
| `api/middleware` | Authentication, request policy, body limits and HTTP instrumentation | Retain; keep authorization and principal validation separate from token decoding |
| `api/response` | Public response envelopes and metadata; handler errors flow to middleware | Retain; streaming remains a deliberate exception to JSON envelopes |
| `app` | Acquire resources, wire modules, select processes, drain and close | Retain four-file composition; fix role documentation; integrate mail once |
| `cli` | Native Cobra trees, queue administration and allowlisted command factories | Grouped command vocabulary; zero-value ScheduledCommands registers directly as a native Asynq handler |
| `config` | One environment snapshot, typed defaults and validation before resource acquisition | IdentityConfig translates the owner's SessionPolicy and reuses its validation; reference/profile/default completeness remains in phase 5 |
| `dependencies` | Borrowed Resources and Targets used only for module composition | Retain; Mailer now exposes Send and Enqueue; services continue receiving specific collaborators |
| `internal/reference` | Shared syntax for media owners and notification recipients | Retain private reuse; syntax is not authorization; no public entity superclass |
| `lifecycle` | Readiness, ordered drain, reverse cleanup and refusal to close active dependencies | Invalid constructor settings return errors; drain and ownership guarantees retained |
| `mail` | Immediate transport plus optional Redis-backed rendered email snapshots | Consolidated message types, SMTP, tracing and queue support here; borrows queue, no background work or SQL store |
| `mail/integration` | Register the versioned mail task in the existing worker mux | Added; no app import, no lifecycle ownership, no independent worker |
| `media` | Polymorphic assets, native buckets, bounded upload and compensating recovery | Retain; document owner authorization and business-controlled replacement/deletion |
| `media/integration` | HTTP/CLI/job registration over the constructed Library | Retain; borrowed routers and clients, no duplicate library instance |
| `media/migrations` | Embedded PostgreSQL asset schema owned by Media | Retain; app aggregates histories without taking schema ownership |
| `messaging` | Typed JSON publication and lifecycle-safe local Watermill execution | NewPublisher validates its borrowed transport; RegisterRunner explicitly installs router hooks; local best effort remains documented |
| `messaging/middleware` | Native Watermill retry diagnostics and trace propagation | Retain; no second event envelope or handler abstraction |
| `notification` | Channel content, SQL intents, delivery/read state and push subscriptions | Dispatcher validates inputs at construction; regular queued mail does not duplicate this ledger or its recipient semantics |
| `notification/integration` | Native HTTP/CLI/job registration and session-aware subscription seam | One explicit callback; app supplies Identity's atomic session authorization; nil selects standalone registration |
| `notification/migrations` | Embedded PostgreSQL notification/delivery/subscription schema | Retain; explicit occurrence/channel uniqueness and stable discriminator values |
| `seeders` | Named, sequential, explicitly invoked operations | Retain small registry; rerun safety and transaction scope belong to each operation |
| `testdb` | Disposable integration schemas, package migrations and test limiter | Retain test-only purpose; production-import guard implemented |

The common foundation is explicit construction, borrowed collaborators, native
transport registration and app-owned lifetime. Framework harmony comes from those
contracts. Media, mail and notifications do not need an artificial common base
interface, identical internals or identical persistence requirements.

## Queued mail decision

Consolidate message types, the `Sender` transport contract, SMTP implementation,
tracing and optional queued delivery in `internal/platform/mail`. This replaces
the initial split between `pkg/mail` and the platform wrapper. `pkg/mailview`
remains the independent renderer; it does not import the mail capability.
`Resources.Mailer` exposes Send and Enqueue; immediate-only consumers use the
same package's narrower `Sender`. No compatibility forwarding package remains.
The concrete SMTP sender is `*mail.SMTP`, constructed by `mail.NewSMTP`.

Send is immediate. Enqueue snapshots a rendered `mail.Message` into the existing
Asynq queue; one `mail.send.v1` handler performs immediate delivery. Native routing,
retry options, tracing and metrics remain available. A nil queue supports standalone
immediate use. No mail-specific scheduler, worker pool, database schema or runtime
registry is introduced. OTP mail remains synchronous.

A queued payload is bounded to 64 KiB and accepts value fields, not attachment
readers. Modules needing attachments reopen stored files in their own jobs.
SMTP uncertainty permits duplicate sends; Redis acceptance is not SQL-atomic intent.
Use Notifier.Enqueue for that requirement. Document queue content privacy,
retry behavior and disabled-mail behavior beside the introductory example.

## API alignment decisions

Keep constructors free of network I/O and background work. Use Open for owned
resource acquisition, Register for installing definitions, and Run for execution.
Preserve native names such as tracing's Shutdown and native transport helpers'
documented programmer preconditions. Do not add error results to infallible
constructors solely for visual consistency.

This development template does not retain forwarding aliases for these changes:

| Previous API | Current API |
| --- | --- |
| `tracing.New` | `tracing.Open`; app owns the exporter's batch worker and Shutdown |
| `messaging.NewRunner` | `messaging.RegisterRunner`; returns `(*Runner, error)` and installs lifecycle hooks before handlers |
| `messaging.NewPublisher` returning only Publisher | Returns `(Publisher, error)`; rejects a missing borrowed transport |
| `lifecycle.New` panicking on invalid inputs | Returns `(*Runtime, error)` |
| `notification.NewDispatcher` validating only in Run | Returns `(*Dispatcher, error)`; validates notifier and interval during assembly |
| `cli.NewScheduledCommands(mux)` | `&cli.ScheduledCommands{}` plus `mux.Handle(cli.CommandTask, commands)` |
| Variadic notification registration callback | One explicit callback argument; pass nil for independent use without Identity's session lifecycle |
| `route:list`, `cache:tables` | `route list`, `cache tables` |

Scheduled commands can build tasks independently of a worker mux; execution uses
the same registry as a native Asynq handler. The persisted `app.command` task name
and payload remain unchanged. Schedules remain centralized in app/modules.go.

Session lifetime validation has one authority: Identity's pure SessionPolicy.
Application-specific config translates environment values through SessionPolicy()
and calls the same validation as the service. This dependency does not add an
Identity module/service import or make Identity depend on platform config.
Authentication defaults, database schemas and notification authorization remain
unchanged.

## Documentation progression

1. Start here: purpose, installation, configuration essentials, first module,
   request walkthrough, local development and troubleshooting.
2. Core concepts: package ownership, construction/registration, resources/targets,
   services/repositories/entities, errors/context/transactions, lifecycle, conventions.
3. HTTP: routing, decoding, responses/pagination, authentication/authorization,
   middleware/CORS/rate limits, API documentation.
4. Database/state: connections, entities/queries, transactions/concurrency,
   migrations/seeders, cache/invalidation.
5. Identity: passwordless sign-in, verification/social providers, token/session
   lifetimes, account/profile lifecycle, security transaction exceptions.
6. Capabilities: mail/mailviews, notification channels and intent, Expo push,
   filesystems and media recovery.
7. Background work: mechanism selection, jobs, local events, central schedules,
   inspection/retries/replay.
8. Workflows: persistent operation, HTTP plus CLI, SQL plus notification intent,
   optional background reactions, owned asset replacement.
9. Operations: roles/deployment, health/shutdown, observability, supported backends,
   backup/restore, outages and troubleshooting.
10. Testing/contribution and reference: test placement, infrastructure gates,
    executable examples, configuration/CLI/API/package index, compatibility/security.
11. Advanced extensions: providers, transport lifecycle hooks and build composition.

Reuse the current static documentation builder and preserve page URLs. Move
existing material into discoverable navigation before writing parallel descriptions.
Each capability page teaches: purpose → smallest useful example/result → settings
and registration → ownership → failure semantics → verification → next steps.

Benchmark lessons: [Laravel](https://laravel.com/docs) for predictable feature
chapters; [Goravel](https://www.goravel.dev/getting-started/installation.html) for
an obvious installation path; [Spring Boot](https://docs.spring.io/spring-boot/documentation.html)
for guide/reference/operations separation; [Rails](https://guides.rubyonrails.org/getting_started.html)
for a growing worked example. Retain idiomatic Go composition.

## Acceptance criteria

- Every advertised startup profile either serves its stated application or is
  explicitly component-only. Clean first request within 15 minutes on a prepared
  machine is a target to measure with new users, not an already verified claim.
- A simple HTTP module needs one module file and one app registration block.
- Every platform package has a recorded purpose, dependencies, ownership decision
  and independent-use story where relevant.
- Services never retain Resources; platform cores never import app, business
  modules or registration targets. Test helpers cannot enter production imports.
- Introductory complete Go examples match compiled source; main compositions run
  through behavior/integration verification. Generated docs and links stay current.
- Session lifetime rules have one source. Constructor failures and process-role
  selection have clear contracts.
- Required PostgreSQL/Redis suites cannot silently skip. Existing security,
  transaction, recovery and drain guarantees remain intact.
- Mail can send without a queue, enqueue without immediate delivery, render before
  persistence, preserve retry content, reject unsupported input before effects,
  and use the existing worker instrumentation and lifecycle.
- Publication needs its own support, backend, deployment and recovery evidence.

## Verification record

Phase 6 local release verification completed on 2026-10-11:

- Upgraded Go to 1.27.2 and x/net to v0.60.0 after the advisory scan identified
  reachable security fixes. Source and compiled Linux application scans now report
  no reachable findings; unused module-level advisories are recorded separately.
- Consolidated the shared application image at deployments/Dockerfile with a
  digest-pinned builder and non-root shell-free runtime. Tilt and Compose use the
  same file; CI builds it. No application package or abstraction was introduced.
- Added scripts/build-release.sh for local binaries, checksums, source identity,
  module inventory and license notices. Forced recompilation produced identical
  Linux ARM64 binaries. Nothing was published, committed or tagged.
- Required PostgreSQL/Redis integration suites passed with race detection; vet,
  command/example builds, API generation and documentation checks passed.
- An isolated Docker Desktop Kubernetes pod passed fresh migration, HTTP signup,
  CLI account/media and welcome-delivery checks. Database and Redis outages returned
  readiness 503 while liveness stayed 200, then recovered. Kubernetes termination
  exited 0 within the configured grace period.
- Quiesced database, local media and Redis snapshots restored to separate test
  targets; pending welcome replay retained one notification. This is not a measured
  production RTO/RPO or live cloud-provider qualification.
- Details and limits live in [release readiness](../release-readiness.md). Public
  module path, private security-reporting contact, clean tagged source and actual
  release-target/provider gates remain pending. Phase 6 is not publication approval.

Completed on 2026-10-10:

- `scripts/verify-integration.sh`: full `go test -race -count=1 ./...` passed with
  disposable PostgreSQL 17 and Redis 7 configured for all four required endpoints.
  This includes the new rendered-mail → Redis → registered-handler workflow,
  existing Identity/notification/media behavior and application lifecycle checks.
- `go vet ./...` and `go build ./cmd/... ./examples/...` passed.
- Mail behavior checks cover immediate-only use, immutable retry snapshots, native
  options, malformed/oversized payloads, attachment rejection, cancellation,
  SQL transaction rejection, Redis errors and redacted transport failures.
- Complete guide snippets match compiled module, entry-point, task and mail sources.
- Documentation builder tests passed; 42 pages rebuilt with zero broken local
  links or HTML anchors. CI now rejects stale or untracked generated guides.
- Quickstart profile smoke passed with isolated container ports: migrations,
  readiness 200, registration 201, CLI account read, local event → job → database
  notification, and clean SIGTERM shutdown.

The required PostgreSQL/Redis suites ran. Optional MySQL, Redis Cluster, cloud
storage and live SMTP/Google/Expo behavior were not certified by this pass. Captured
mail transports were used; no external email was sent. User development services
were not used as disposable test backends.

Phase 2 and mail consolidation completed on 2026-10-11:

- Removed `pkg/mail`; all callers now import `internal/platform/mail`. The core
  contains four production files plus its integration registration. Transport
  behavior, `mail.send.v1` and its serialized message fields remain unchanged.
  Immediate consumers use `Sender`; app resources use `Mailer` with optional Enqueue.
- Published the existing Identity, deployment, configuration and API references
  in navigation, and added persistent HTTP/CLI, transactional notification and
  onboarding walkthroughs. Existing page URLs remain stable.
- Added the compiled profile-update example and a PostgreSQL composition test:
  successful commit, outer rollback, intent-insert failure rolling back the profile,
  deferred visibility and repeated delivery without duplicate database records.
- Full `scripts/verify-integration.sh` passed after the merge with disposable
  PostgreSQL 17 and Redis 7. Race detection includes the existing end-to-end
  onboarding and queued-mail workflows. No external mail was sent.
- `go vet ./...`, command/example builds, guide source checks and documentation
  builder tests passed. Rebuilt 50 pages with zero broken local links or anchors;
  nested API references resolve their downloadable specification correctly.

Phase 3 and the phase 4 composition recheck completed on 2026-10-11:

- Applied the API alignment decisions above without adding production files or
  runtime layers. Existing consumers and introductory examples use the new APIs.
- Notification HTTP checks cover the explicit session callback, rejection of
  unauthorized/invalid input before invocation, and independent nil-callback use.
  Existing PostgreSQL checks retain atomic session revocation and push registration.
- Session configuration tests cover all lifetime modes and access-token boundaries,
  preserve the policy error cause, and verify that config maps the same policy
  consumed by authentication. Real PostgreSQL tests exercise rotation, replay,
  revocation and unlimited sessions.
- Scheduled-command checks retain concurrent flag isolation, malformed-task
  rejection, bounded output and native Redis execution. CLI checks retain route
  inspection, migration publication, eager construction and cleanup.
- Constructor failures are returned before execution; existing lifecycle checks
  still verify partial startup, cancellation, drain and client ownership.
- `scripts/verify-integration.sh` passed with disposable PostgreSQL 17 and Redis 7,
  running every package with race detection and all four required test endpoints.
  Transactional profile updates, notification duplicate processing and queued-mail
  composition were reverified. No external mail was sent.
- `go vet ./...`, command/example builds, documentation builder tests and compiled
  guide checks passed. Rebuilt 50 published pages with zero broken local links or
  anchors. Tests remain outside app, service and repository packages.

Phase 5 completed on 2026-10-11:

- Replaced the incomplete configuration reference with 120 checked entries,
  including prefixed HTTP/CORS fields, queue priorities, filesystem templates and
  named defaults. Each capability explains prerequisites, ownership and failure
  behavior. Removed claims about database queues, NATS settings and unwired mail.
- Added a published troubleshooting chapter covering configuration, auth, queue
  tasks, notification ledger/receipts, events, media and lifecycle. Inspection and
  recovery steps distinguish accepted work, committed intent and provider effects.
- Added a command reference and package index to existing guides. Clarified the
  PostgreSQL application requirement versus reusable database-driver support.
  Corrected middleware installation, welcome-event ownership, Google signup status
  and unsupported account-linking claims against current source.
- Added deployment backup/restore requirements and a rehearsal procedure. Actual
  deployment/provider recovery evidence remains phase 6; the procedure is not a
  claim that a recovery target has been measured.
- The config test compares documented keys/defaults to decoded settings, including
  named definitions. The existing site builder now checks rendered links, assets
  and anchors after generating pages. CI already runs both; no second schema,
  documentation generator or runtime registration mechanism was added.
- `go test -race ./...` and `go vet ./...` passed. Documentation builder race tests
  and vet passed; compiled workflow/API checks passed in the normal suite. This
  documentation pass did not configure external integration endpoints; PostgreSQL
  and Redis evidence remains the completed phase 4 run, not a new backend claim.
- Rebuilt 51 pages with local links and anchors checked by the builder. External
  websites and live SMTP/Google/Expo delivery were not tested in this pass.

Remaining phases are explicit above. This pass does not establish a release,
upgrade promise, provider delivery guarantee or measured newcomer learning time.
