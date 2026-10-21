# Incremental refactoring roadmap

Status: **implemented and locally verified on 2026-10-11 after approval**. The task descriptions below preserve the approved plan; the implementation record distinguishes completed changes from deferred optional work and release-only gates. The nine review documents describe the pre-change baseline unless marked otherwise. Finding rationale lives in [03](03-platform-harmonization.md), with objective gates in [08](08-validation-and-quality-gates.md).

Scope discipline: preserve the existing dirty working copy; retain nested module folders and the four-file app package; do not add tests to app or business service/repository packages; keep existing configuration enablement and credentials; do not add chat/realtime/mobile, generic DI or new repository/service hierarchies.

Effort bands are estimates including tests/docs: S half–one day, M one–three days, L three–five days. Tasks are designed for separate reviews. A phase boundary is an evidence gate, not permission to sweep unrelated cosmetic edits into a large patch.

## Implementation record — 2026-10-11

| Work | Result |
| --- | --- |
| T0.1–T0.2 | Saved the pre-change file manifest and source copies outside the repository; added focused regression checks alongside the fixes. The earlier review's four probes remain baseline evidence. |
| T1.1–T1.2 | Canonical conventions and repository-local AGENTS guidance agree. Assembly documentation uses a source-checked excerpt; transactions teach the existing TxManager and link the compiled profile workflow. |
| T2.1 / T2.6 | HTTP errors log bounded categories and safe codes. Event attempt spans retain status; only the outer logger reports a failed retry cycle. |
| T2.2 / T3.1 | Publisher methods guard before serialization; the app installs the optional queue guard outermost. Existing Identity publication occurs after repository persistence; its subscriber/CLI enqueues do not execute inside a framework transaction. Notification dispatch already rejects transaction scopes. |
| T2.3 / T2.5 | SMTP has a configurable 20 MiB aggregate raw attachment bound, cooperative context checks, unconditional requested cleanup, joined close errors and pure configuration validation. Cache owners validate selected settings before I/O; installed app dialect checks precede acquisition. |
| T2.4 / T3.2 | Added the versioned pruning handler, shared 90-day retention policy and push-conditional central hourly schedule. Removed the dispatcher retention timer while keeping delivery/receipt polling. |
| T2.7 / T3.1 | Named disk selection now lives in FilesystemConfig.Options; app, tests and the standalone example use storage.Open. The app helper was removed. |
| T2.8 | Extracted pending-age and subscription persistence into private store methods in existing files. SQL and transaction selection remain unchanged. Further push-workflow SQL extraction is deferred: there is no demonstrated benefit sufficient to move its locking/effect sequencing in this patch. |
| T3.3 | Required PostgreSQL/Redis workflows passed with race detection, including profile SQL intent, queued mail, Identity subscription/session behavior, app lifecycle and central schedule conditions. |
| T4.1–T4.3 | Updated canonical examples, capability READMEs, configuration, scheduling, diagnostics and attachment ownership guides. Kept all 51 public slugs; generated API contract is unchanged. |
| T5.1–T5.3 | Core integration, focused regressions, static checks, examples, documentation tests, vulnerability scan and bounded-attachment benchmark passed. Fresh-copy checks exercised actual onboarding sources and independent capabilities; this is scripted evidence, not measured newcomer usability. |
| T5.4 | Release boundary recorded below and in release-readiness. Public module path, security contact, support policy and publication remain owner decisions. |

The local SMTP fixture revealed an additional inconsistency within T2.5: the old
constructor always forced authentication discovery even when both credentials
were omitted. It now leaves authentication off for credential-free relays and
uses discovery when a validated pair is provided. TLS verification behavior was
not weakened. No live provider was contacted.

### Migration inventory

- **Storage:** replace `app.OpenFilesystemConnection(ctx, cfg, name)` with
  `settings, err := cfg.Filesystem.Options(name)` followed by
  `storage.Open(ctx, settings)`. Check both errors and preserve connection cleanup.
  See the complete `examples/storage/main.go`.
- **Effects:** Publisher and the app queue now return the existing
  `database.ErrTransactionActive` for framework transaction contexts, and preserve
  cancellation before work. Native clients bypass the policy. Post-commit
  publication still has a failure gap; no generic outbox was introduced.
- **SMTP:** oversized immediate attachments return `mail.ErrAttachmentsTooLarge`.
  Set `SMTPConfig.MaxAttachmentBytes`, or app `MailConfig.MaxAttachmentBytes`, in Go
  to override the default; no environment key was added. Requested close errors
  are returned even after acceptance, so do not blindly retry them. The guard in
  the Mailer wrapper can reject a call before SMTP owns readers; caller cleanup
  remains the simplest default.
- **Configuration:** only the selected cache driver validates its options.
  Redis/database zero settings use owner defaults; memory capacity/entry size
  remain required and zero byte budget uses 64 MiB. Disabled/unused cache settings
  no longer cause irrelevant operational validation failures. Invalid selected
  namespaces/tables and SMTP settings fail earlier. Reusable database MySQL support
  remains; configured installed-app modules require PostgreSQL before acquisition.
- **Scheduling:** roll out workers recognizing
  `notifications.prune_subscriptions.v1` before enabling the hourly schedule.
  Workers without a scheduler keep delivery/receipt processing but must invoke
  retention explicitly or assign a scheduler owner. Each task handles at most
  1000 stale subscriptions. Disable the schedule and drain accepted pruning tasks
  before removing handlers on rollback. No payload timestamps or schema changes.
- **Diagnostics:** HTTP raw `error` text is replaced by `error_category` and safe
  `error_code`; event attempt warnings no longer claim `retry_budget_exhausted`.
  Adjust log queries that relied on these fields.

### Verification and preservation

The full `scripts/verify-integration.sh` suite passed on Go 1.27.2 with race
detection, disposable PostgreSQL 17 and Redis 7, and all four required test
endpoints. New tests verify aggregate attachment boundaries, cancellation,
read/seek/close/transport failures, successful local TLS SMTP acceptance, effect
rejection before serialization, retry log ownership, pure validation, source-based
guides, pruning batches/idempotency and both enabled/disabled central schedules.
Existing admitted-request, worker and subscriber drain tests passed; the patch
adds no background goroutines or runtime owner.

`go vet ./...`, cmd/example builds, the shared Linux AMD64 Docker image, formatting,
documentation builder tests and API regeneration passed. A second documentation
build produced byte-identical output. The image was built locally, not published
or exercised under production traffic. The pinned govulncheck v1.8.0 reported no reachable or
imported-package findings; four module-only advisories remain in unused code.
This is dated evidence. Optional MySQL, Redis Cluster and remote storage suites
were not configured; Google, Expo and external SMTP were not contacted.

The bounded overflow benchmark (Darwin ARM64, Apple M5 Pro, Go 1.27.2) supplied a
1 MiB reader with a 1 KiB limit. Each iteration consumed exactly 1025 bytes and
rejected before transport: 1325–1479 ns/op, 4405 B/op, 60 allocations across three
runs. This measures the rejection path only; it is neither a before/after throughput
comparison nor a production concurrency/load claim.

A fresh copy without private environment files passed the compiled Catalog and
guide checks, standalone local storage upload/read/delete, local Identity mail
previews and CLI route inspection. The four commands took 18.211 seconds in total
with existing Go caches and required no source edits. The separate PostgreSQL/Redis
workflow run also passed in that fresh copy, covering persistence and queued effects. No human unfamiliar with the
repository participated, so subjective score targets are not promoted to measured
results and this is not a timed manual "add a module" study.

App remains four production files and zero colocated tests; platform remains
82 production files across 21 packages. No new production package, interface
hierarchy, migration or dependency was introduced. Go module/checksum files,
example profiles, deployment app-config, navigation and API schema remain unchanged.
Environment files and credentials were not edited. New tests are outside app and
business service/repository packages. The base revision is `1b78842c26254997b3aa738ab434182ded16dbbb`; the uncommitted
implementation is identified by the retained file manifest. Complete logs, pre/post manifests, benchmark
output and the isolated example copy are retained in the private verification archive.

This completes the code/documentation harmonization scope. Remaining work is
optional deeper private SQL extraction, human DX research, target-specific provider/
load/deployment validation and public release decisions. The prior review and
release bundle describe an older source snapshot; rebuild a release bundle from
the final chosen source before publication. No commit, tag or publication was made.

## Approved naming follow-up — 2026-10-11

Common event metadata moved from `internal/shared/contracts/events` to
`internal/shared/events`; business event definitions remain in their publishing
modules. Identity now constructs welcome content through
`notifications.NewWelcome` and `mailviews.NewWelcome`. Update these imports and
call sites when adopting the changes; event JSON, topic names, notification types
and occurrence keys are unchanged. Consumers, examples, existing tests and the
51-page generated documentation were updated. Relevant Identity/shared/module/
architecture tests, vet, command/example builds and documentation checks passed.
No new tests or production abstractions were needed for this naming-only change.

## Dependency map and milestones

```text
T0.1 snapshot + baseline -> T0.2 focused behavior safeguards
             -> T1.1 canonical rules -> T1.2 example correction
T1.1 + T0.2 -> T2.1 diagnostics
             -> T2.2 transaction guards
             -> T2.5 owner validation -> T2.3 attachment hardening
             -> T2.4 pruning handler -> T2.8 private SQL locality
             -> T2.6 event telemetry
             -> T2.7 pure storage selection
T2.2 + T2.7 -> T3.1 consumers and helper removal
T2.4 -> T3.2 central pruning cutover
T3.1 + T3.2 -> T3.3 complete workflows and ownership verification
T1.2 + landed APIs -> T4.1 source-backed chapters
T2.5 + T3.2 -> T4.2 reference/operations accuracy
T4.1 + T4.2 -> T4.3 navigation and generated-doc verification
All changed behavior + docs -> T5.1 integration/race
                           -> T5.2 resource/drain exercises
                           -> T5.3 onboarding and scorecard reassessment
                           -> T5.4 release boundary record
```

Independent branches can be reviewed separately. Avoid simultaneous edits to the same SMTP/config or app-resource sections. The pruning change is deliberately staged: handler first, scheduling cutover second. The storage resolver is additive before the old app helper is removed.

## Phase 0 — Baseline & Safeguards

### T0.1 — Preserve and reproduce the working-copy baseline

- **Priority / estimate / risk:** P1/S/low. Dependencies: none. Review status: baseline captured, hashes and logs available; repeat for the exact implementation starting revision if it changes.
- **Exact scope:** existing source state, `go.mod`, `go.sum`, `.github/workflows/verify.yml`, `scripts/verify-integration.sh`; external evidence directory or CI artifacts. No production edits.
- **Problem → outcome:** a dirty checkout cannot be identified by HEAD alone → known inputs and honest baseline results.
- **Instructions:** record changed/untracked files and source hashes; run fast race/vet/build/format/doc checks; run required integrations against newly isolated PostgreSQL/Redis; record optional skips. Never run destructive fixture migrations against development data. Capture current task names, notification statuses and migration versions for comparison.
- **Compatibility/migration:** none. Preserve all preexisting changes; do not reset/clean/commit them as a side effect.
- **Verification/acceptance:** baseline logs identify toolchain, commands, backends and exit codes; all four required test endpoints present; teardown removes only created fixtures. Existing file hashes stay unchanged except intended task files.

### T0.2 — Convert demonstrated gaps into focused safeguards

- **Priority / estimate / risk:** P1/M/low. Depends on T0.1.
- **Exact scope:** `tests/platform/http_test.go`, `tests/platform/mail_test.go`, cache cases in `internal/platform/config/config_test.go`, messaging middleware tests, `pkg/asynqx/middleware/client_test.go`, `tests/modules/schedules_test.go`.
- **Problem → outcome:** four isolated probes establish baseline behavior, but do not yet guard desired behavior → narrowly scoped regression cases paired with their fixes.
- **Instructions:** use review-probe inputs from the external evidence copy; express desired behavior in each implementation patch. Add TX no-effect tests around real `TxManager` plus transport spies. Add pruning-role and configuration parity cases. Reuse current fixtures; do not construct a new mock framework or put failing tests on a release branch alone.
- **Compatibility/migration:** no public API change. Tests must remain outside app and business service/repository packages.
- **Verification/acceptance:** each regression fails against its old behavior and passes with its specific fix; no live SMTP/Expo call, credential output or timing-flaky sleep assertions.

## Phase 1 — Foundation

### T1.1 — Make existing conventions authoritative and explicit

- **Priority / estimate / risk:** P2/S/low. Depends on T0.1 and this understanding report.
- **Exact scope:** `docs/conventions.md`, `CONTRIBUTING.md`; applicable `/Users/barakat/workspace/AGENTS.md` conflict treated as a separately scoped workspace instruction issue.
- **Problem → outcome:** F10 competing patterns → one project authority matching the actual desired architecture.
- **Instructions:** fold [02](02-framework-design-philosophy.md)'s approved rules into existing conventions, including effects/transactions and diagnostic ownership. Preserve module nesting, owner contracts, shared business entities, platform-owned records, centralized maintenance and eager CLI help. Link contributor guidance to it. Resolve stale inherited guidance in a repository-specific way; do not overwrite unrelated sibling-project rules.
- **Compatibility/migration:** documentation only. Runtime behavior must not be changed to fit stale prose. Editing the parent workspace file is not part of this repository review's authorization.
- **Verification/acceptance:** no applicable guidance requires `platform/registration`, shared `models` or offline command metadata. Every rule has an owner/source; no new parallel architecture manual beyond this review.

### T1.2 — Repair the demonstrably invalid learning examples

- **Priority / estimate / risk:** P2/S/low; quick win. Depends on T1.1.
- **Exact scope:** `docs/module-registration.md`, `docs/database-transactions.md`, relevant assertions in `tests/modules/onboarding_test.go` and registration examples.
- **Problem → outcome:** F09 stale Resources field and unnecessary contract choice → current, source-backed first path.
- **Instructions:** replace `Notifications` with `Notifier`; teach the existing `*database.TxManager` first; connect excerpts to their compiling source. Preserve native registration APIs and chosen return/error shapes.
- **Compatibility/migration:** docs only; regenerate published outputs through the existing builder in the implementation patch.
- **Verification/acceptance:** no stale field in active docs; canonical transaction example compiles; module tests and doc builder pass. Historical text is labeled rather than globally rewritten blindly.

## Phase 2 — Platform Harmonization

### T2.1 — Align HTTP failure diagnostics

- **Priority / estimate / risk:** P1/S/low. Depends on T1.1/T0.2. Finding F01.
- **Exact scope:** `internal/platform/api/middleware/errors.go`, existing middleware/external HTTP tests, `docs/logging.md`, safe-diagnostics section of `docs/observability.md`.
- **Instructions:** replace raw error logging with existing bounded category plus safe response code and correlation. Preserve translated statuses, committed-response handling and original error identity.
- **Compatibility/migration:** log consumers must update from raw `error` text to structured categories. No HTTP API signature/payload change.
- **Acceptance:** unknown/wrapped private marker never reaches body/log/span; expected statuses, route and request/trace correlation remain; no duplicate error report introduced.

### T2.2 — Enforce transaction boundaries for event and task publication

- **Priority / estimate / risk:** P1/M/medium. Depends on T1.1/T0.2. Finding F02.
- **Exact scope:** `internal/platform/messaging/publisher.go`; `pkg/asynqx/middleware/client.go` and tests; `internal/platform/app/resources.go`; messaging tests and transaction/queue/event guide sections.
- **Instructions:** invoke the existing database guard before marshaling in both Publisher methods. Add the proposed optional queue middleware and install outermost in app. Keep task routing/telemetry order and native options untouched. Inventory every Publish/EnqueueContext caller before enabling it.
- **Compatibility/migration:** active framework TX/canceled contexts now fail before side effects. Document native bypass and commit/enqueue gap. Do not move calls automatically into generic callbacks or introduce an outbox.
- **Acceptance:** transport spy count stays zero for transaction/canceled calls; real rollback test proves no wrapped publication; nontransaction calls/options/errors retain behavior; transactional notification intent remains accepted. Consumer audit is completed in T3.1.

### T2.3 — Bound SMTP attachment memory and clarify ownership

- **Priority / estimate / risk:** P1/M/medium. Depends on T2.5 owner normalization and T0.2. Finding F03.
- **Exact scope:** `internal/platform/mail/smtp.go`, Message/Attachment comments in `mail.go`, `tests/platform/mail_test.go` or a focused external SMTP test file, `docs/mail.md`.
- **Instructions:** add optional aggregate `MaxAttachmentBytes` with proposed zero-default 20 MiB; budget all raw attachments with limit+1 reads; check context before/between reads; register requested cleanup before validation/reads. Define joined close-error semantics and prohibit duplicating an owned reader entry. Keep caller-owned cleanup as the preferred example. Preserve queued payload bounds and reader rejection.
- **Compatibility/migration:** previously oversized attachments fail; explicit close policy becomes stronger and may return close errors after an accepted send. Warn against blind retry in that case. No new required env fields or worker payload version.
- **Acceptance:** boundary/aggregate/canceled/read-error/seek-error/close-error/transport-error cases; no read for an already canceled context; no leak on failing attachment; oversized-stream allocations bounded by configured budget. Arbitrary blocking reader limitation documented honestly.

### T2.4 — Add a schedulable subscription-pruning task

- **Priority / estimate / risk:** P1/M/medium. Depends on T1.1/T0.2. Finding F04.
- **Exact scope:** `internal/platform/notification/delivery.go` task constant; `notification/integration/tasks.go`, `register.go`, `commands.go`; integration/external queue tests.
- **Instructions:** add `notifications.prune_subscriptions.v1` with no timestamp payload, validate exact type/payload, invoke bounded pruning with a cutoff computed when the handler runs. Share one 90-day policy/helper between command and handler. Install the handler before workers start. Keep the old dispatcher timer temporarily until T3.2's coordinated cutover.
- **Compatibility/migration:** additive task type only. Do not yet enable its schedule on a deployment whose consumers lack the handler. Existing task formats/schema unchanged.
- **Acceptance:** task registration present with optional targets, handler preserves active/recent subscriptions, processes at most the existing batch limit, distinguishes invalid task payload from transient DB errors, computes current cutoff per invocation.

### T2.5 — Give selected settings one validation owner

- **Priority / estimate / risk:** P2/M/medium. Depends on T1.1/T0.2. Finding F05.
- **Exact scope:** `pkg/cache/{memory,redis,database}/store.go` configuration helpers as required; `internal/platform/mail/smtp.go`; `internal/platform/config/{cache,mail,validate}.go`; app preflight in `app/app.go`; existing config tests and `tests/app`.
- **Instructions:** extract pure normalized settings/validation from constructors without duplicating defaults; expose only methods needed by app config. Reuse SMTP host/port/from/credential-pair validation, preserving disabled mail behavior and standalone message-level From overrides when no default sender is configured. Validate selected cache namespace/table and driver rules before I/O. Preflight configured installed-app database dialect separately from reusable database support. Keep no-DB inspection behavior.
- **Compatibility/migration:** earlier deterministic errors; no driver removal or enabled-feature changes. Exact error strings may change, sentinel/cancellation behavior should not. Do not require credentials for unused providers.
- **Acceptance:** app/standalone parity table for zero/default/valid/invalid configs; no env read or network call in owner validation; current profiles retain defaults; generic MySQL tests still compile/run when configured; installed app rejects unsupported dialect before acquiring resources.

### T2.6 — Separate retry attempt telemetry from final failure

- **Priority / estimate / risk:** P2/S/low; quick win. Depends on T1.1/T0.2. Finding F06.
- **Exact scope:** `internal/platform/messaging/middleware/middleware.go` and focused middleware tests; `docs/observability.md`/`docs/events.md`.
- **Instructions:** remove exhaustion claims from TraceHandler. Preserve attempt span status, links and context restoration. Keep outer LogFailures as final cycle reporter; do not reconfigure retry counts, poison topic or transport guarantees.
- **Compatibility/migration:** warning volume/fields change; dashboards must use actual final outcome categories.
- **Acceptance:** fail-then-success produces no final-failure/exhausted record; exhausted cycle reports once; cancellation is categorized; no payload or error text exported.

### T2.7 — Expose pure named-disk selection outside app

- **Priority / estimate / risk:** P2/S/low. Depends on T1.1. Finding F07.
- **Exact scope:** `internal/platform/config/filesystems.go` and tests; app helper can delegate temporarily until consumer migration.
- **Instructions:** add `FilesystemConfig.Options(name) (storage.Config,error)`, resolving only a known named definition and validating owner options without I/O. Preserve `DiskConfig.Options`, disabled/default/additional semantics and stable disk names. Do not cache/open buckets in config.
- **Compatibility/migration:** additive API in this task; old helper removal is T3.1.
- **Acceptance:** unknown/nil/invalid definitions fail purely; valid conversion matches old helper; tests confirm caller map/settings are not mutated and providers remain selectable.

### T2.8 — Localize notification SQL without introducing a new layer

- **Priority / estimate / risk:** P3/M/medium; defer if correctness work needs focus. Depends on T2.4 and existing integration safeguards. Finding F08.
- **Exact scope:** existing `notification/{store,delivery,subscriptions,push}.go`; existing PostgreSQL/receipt/subscription tests.
- **Instructions:** move persistence operations to private `store` receiver helpers in existing files; first move the pending-age query and direct subscription SQL. Keep workflow decisions on Notifier. Preserve SQL, executor selection, lock order, timestamps and effect ordering in the first extraction. No exported repository or new service type.
- **Compatibility/migration:** none; no migrations/public APIs/task names change. This is optional if extraction fails the “clearer ownership” review test.
- **Acceptance:** no added production files; no orchestration raw-query construction in the targeted methods; ledger/leases/receipt/logout-race tests pass unchanged. Reviewer can compare old/new SQL and lock ordering directly.

## Phase 3 — Application & Module Integration

### T3.1 — Migrate consumers and remove the obsolete app storage API

- **Priority / estimate / risk:** P2/M/medium. Depends on T2.2/T2.7.
- **Exact scope:** `examples/storage/main.go`, `internal/platform/app/resources.go`, all references found for `OpenFilesystemConnection`, publication/enqueue consumers in Identity/examples; storage/dependency docs.
- **Instructions:** use the pure selector followed by `storage.Open`, preserve close-error handling; remove the old helper after all in-repo callers move. Verify no current business consumer invokes Publisher/app queue inside a framework TX. If one is found, document its delivery guarantee and choose notification intent or explicit post-commit best effort; do not bypass the guard with `context.Background`.
- **Compatibility/migration:** publish exact old→new storage call mapping for template forks. Existing task/event data formats retained; no compatibility alias unless a real supported downstream consumer is identified.
- **Acceptance:** `rg OpenFilesystemConnection` finds no active source/guide references except migration history; storage example imports no app; all cmd/examples compile; registration transaction/onboarding tests pass.

### T3.2 — Cut subscription pruning over to the central schedule

- **Priority / estimate / risk:** P1/M/medium. Depends on T2.4; coordinate worker rollout first.
- **Exact scope:** `internal/platform/app/modules.go`, `notification/delivery.go`, pruning comments/README, `tests/modules/schedules_test.go`, notification/external queue tests; deployment and scheduling guides.
- **Instructions:** add push-conditional hourly native schedule with bounded timeout/uniqueness; remove only the dispatcher retention timer. Preserve dispatch and receipt polling and their worker role. Document one scheduler owner, manual CLI alternative and retention backlog behavior.
- **Compatibility/migration:** workers-only installations must enable a scheduler owner or call pruning explicitly. Roll out handler-capable workers, then schedule ownership; rollback disables the new schedule before removing consumers. Queued new task versions must be drained/retained appropriately before downgrading handlers.
- **Acceptance:** all maintenance cadence declared centrally; disabled push adds no schedule; no cron still allows delivery/receipts; no hidden subscription prune on dispatcher startup; bounded pruning is idempotent under duplicates.

### T3.3 — Verify the integrated module and capability workflows

- **Priority / estimate / risk:** P1/M/medium. Depends on T3.1/T3.2 and Phase 2 behavior fixes.
- **Exact scope:** `tests/modules/{registration,onboarding,workflow,profile,schedules}_test.go`, `tests/identity`, `tests/platform`, `tests/queue`, `tests/app`, `tests/architecture/conventions_test.go`; canonical examples.
- **Instructions:** exercise HTTP → service → repository → transaction → notification and event → task → reload → channels; retain Identity logout/subscription serialization. Check resources constructed once, transport registration before run, nil target omission, startup failure cleanup and role ordering. Add only missing meaningful cases.
- **Compatibility/migration:** retain current HTTP statuses, DTOs, auth policies, nested folders and eager CLI help. No new endpoint required.
- **Acceptance:** core integration/race passes; no app/service/repository test files; no service importing Resources; one module registration location and centralized schedules; independent capability examples build without app imports except explicitly app-focused examples.

## Phase 4 — Documentation Maturity

### T4.1 — Publish source-backed canonical workflows

- **Priority / estimate / risk:** P2/M/low. Depends on T1.2 and APIs relevant to each chapter; begin representative chapter review earlier.
- **Exact scope:** `docs/{modules,module-registration,dependency-injection,database-transactions,mail,events,queue-jobs,transactional-notifications}.md`, source examples and `tests/modules/onboarding_test.go`; capability README pointers.
- **Instructions:** use [04](04-developer-experience.md)'s actual/proposed distinction during development; publish only landed APIs. Apply [05](05-documentation-strategy.md)'s mail chapter pattern, remove repeated instructions and link complete compiled sources. Keep native extension paths and explicit delivery guarantees.
- **Compatibility/migration:** preserve page slugs and meaningful anchors; add anchor redirects/links only where necessary. Do not replace the site or add a generic snippet interpreter.
- **Acceptance:** each canonical complete workflow references compiled source; fragments name prerequisites; renamed/moved API no longer appears as current; no claim of realtime implementation.

### T4.2 — Align configuration and operations references

- **Priority / estimate / risk:** P2/M/low. Depends on T2.5/T3.2.
- **Exact scope:** `deployments/configuration.md`, `docs/{configuration,scheduling,application-lifecycle,observability,troubleshooting,release-readiness}.md`, relevant capability READMEs.
- **Instructions:** reconcile defaults against typed owner settings; separate code defaults/profile overrides; state selected-driver validation and credential requirements. Explain pruning ownership change, attachment bound/close semantics, honest retry logging and readiness limits. Keep unverified provider matrices explicit.
- **Compatibility/migration:** documentation of behavior changes; no silent env/profile editing. Private credentials remain private.
- **Acceptance:** config settings map to actual fields/defaults, schedule list matches code, success meanings and error recovery are consistent across guides; no unsupported live-provider or throughput claims.

### T4.3 — Verify navigation and generated documentation

- **Priority / estimate / risk:** P2/S/low. Depends on T4.1/T4.2.
- **Exact scope:** `docs/navigation.json`, `docs/build.go` and builder tests only where required, generated guide/API artifacts, docs/source-reference validation in existing test structure.
- **Instructions:** retain the 51 slugs and source ownership table unless a deliberate editorial need is demonstrated. Run generation in a clean/disposable copy, inspect diffs, validate links and example mappings. Historical review/plans stay outside beginner navigation.
- **Compatibility/migration:** no unnecessary URL moves. Generated output belongs in the same implementation patch as changed canonical sources where repository policy requires it.
- **Acceptance:** deterministic second build yields no differences; API spec matches handler annotations; no broken links; changed snippets are covered by their source tests; docs review files are not accidentally treated as public tutorial sources.

## Phase 5 — Validation & Refinement

### T5.1 — Run the complete changed-contract gate

- **Priority / estimate / risk:** P1/M/low. Depends on all implementation and documentation tasks in scope.
- **Exact scope:** current CI workflow, required integration script and all tests; no speculative new test platform.
- **Instructions:** run gates in [08](08-validation-and-quality-gates.md), with required PostgreSQL/Redis and explicit optional-provider status. Repeat only after relevant changes/failures. Run the pinned vulnerability scan and compare API/config/task/migration inventories.
- **Acceptance:** build/vet/race/docs/core integrations pass; all focused regressions pass; no accidental persisted-contract drift, stale docs or disallowed architecture imports. Record precise outputs and skips.

### T5.2 — Exercise resource bounds and shutdown with active work

- **Priority / estimate / risk:** P1/M/medium. Depends on T5.1.
- **Exact scope:** existing lifecycle/httpserver/asynqx tests and isolated deployment fixture; SMTP benchmark/test fixtures; no live external recipients.
- **Instructions:** benchmark attachment overflow/bounded input; exercise cancellation during cooperative read, worker drain with admitted work, subscriber startup failure and interrupted provider attempt. Use disposable services/local fakes. Compare allocations and durations on the same toolchain/hardware; do not infer production throughput from unit tests.
- **Compatibility/migration:** no runtime tuning changes unless measurements expose a defect; document any justified follow-up separately.
- **Acceptance:** configured byte bounds respected, no owned-reader leaks, no close-before-drain, shutdown respects documented cooperative limits; captured outcome uncertainty/retry behavior. Optional production load/RTO targets remain owner-defined.

### T5.3 — Reassess developer workflows and subjective scores

- **Priority / estimate / risk:** P2/S–M/low. Depends on T3.3/T4.3/T5.1.
- **Exact scope:** reproducible onboarding exercise, [08](08-validation-and-quality-gates.md)'s measurements, source examples and docs only where real friction is found.
- **Instructions:** start from an isolated clean checkout/profile; add catalog, run a persisted workflow, enqueue mail with a fake/local sender, use SQL intent and explain pending/failed states. Record actual files edited, commands, errors, completion time and participant familiarity. A scripted run is not human usability research.
- **Acceptance:** meet objective setup/import/registration metrics; explain any missed target score with evidence rather than inflating it. No additional abstraction is added merely to gain a point.

### T5.4 — Record the release boundary and unresolved product choices

- **Priority / estimate / risk:** P1/S/low. Depends on T5.1–T5.3; product-owner inputs only for publication.
- **Exact scope:** `docs/release-readiness.md`, `SECURITY.md`, public-module/import migration only as a separate explicitly scoped release change.
- **Instructions:** record tested revision, supported backends/architectures, unresolved optional providers and dated vulnerability evidence. Keep private security contact, public module path and support policy as explicit owner choices. Use existing release tooling after those inputs are known; this review does not tag, commit or publish.
- **Acceptance:** no “v1 ready” claim based only on a green unit suite; release scope matches actual evidence; deferred compatibility/load/provider exercises are visible.

## Review and rollback rules

Each implementation task should report the changed behavior, why it is necessary, meaningful tests, affected docs and any migration. Avoid formatting-only churn. Review stateful changes separately from helper extraction. If a phase reveals a new concern, substantiate it before expanding scope.

Rollback source changes using the project's normal reviewed Git workflow, preserving unrelated work. For the additive pruning task, stop new scheduling before removing handlers; a source rollback does not remove tasks already accepted by Redis. Never “rollback” this harmonization with rewritten applied migrations, queue flushing or deletion of stored notification/media data.

The fastest useful wins are T1.2, T2.1 and T2.6, but they should not displace transaction guards, attachment bounds or the scheduling cutover. T2.8 is the explicit optional low-priority extraction; no mandatory structural rewrite depends on it.
