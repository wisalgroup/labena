# Validation, maturity scorecards and definition of done

This document separates **executed checks**, **subjective assessments**, and **future acceptance targets**. Green tests are evidence about the configured test matrix, not proof of every deployment, provider or workload.

## Executed baseline and preservation

See [01](01-repository-assessment.md) for exact commands, scope and limitations. During this review:

- Go build, vet, race suite, formatting check, docs-builder tests and guide generation passed.
- Required PostgreSQL 17/Redis 7 integration suites passed in disposable services with all four required test endpoints set. Optional providers/backends were not thereby certified.
- API generation in an isolated copy produced byte-identical `docs/api/swagger.json`.
- The complete mail chapter example compiled in an isolated copy; all local review-document links resolved and fences were balanced.
- Pinned govulncheck found zero reachable/affected imported-package vulnerabilities and four module-only advisories.
- Four isolated probes reproduced F01, F03, F05 and F06. Their baseline assertions are not desired regression tests.
- Preexisting repository contents were hash-checked. Review changes are limited to the nine new documents in `docs/review/`; no production API, dependency, configuration or behavior change is part of this phase.

The fast suite produced 647 passing and 84 skipped test/subtest events across 84 packages, with 52 package passes and 32 packages containing no tests. This is not line coverage: nested subtests count separately and external tests cover packages without colocated tests. The required integration run subsequently enabled core DB/Redis cases. No test-coverage percentage was measured.

## Scoring rubric

Scores are **reviewer judgments from source, consumers, documentation and test evidence**, not benchmark measurements, user-study results or a numeric release certification. Do not average them into a misleading “production readiness percentage.”

| Score | Meaning within the package's actual responsibility |
| --- | --- |
| 1 | No coherent supported path or a fundamental responsibility/ownership conflict |
| 2 | A usable path exists but requires repeated workarounds or undocumented critical assumptions |
| 3 | Usable and substantially implemented; one or more evidenced consistency, learning or maintenance gaps remain |
| 4 | Coherent normal path and explicit advanced/failure semantics, backed by relevant code/tests/docs; remaining limits are deliberate |
| 5 | Exceptionally small/predictable path with demonstrated validation across the intended scope and independently verified usability; not a default target |

A no-config helper earns a 4 in configuration when that absence is intentional and its ownership is clear; it does not need a config struct to improve its score. Target 4 is the practical harmonization goal. No score 5 is claimed without broader usability/scope evidence.

Dimensions: **API** consistency; **Learn** learning curve; **Config** simplicity; **Boiler** repetitive setup; **Compose** composability; **Life** lifecycle predictability; **Encap** infrastructure encapsulation; **Extend** advanced extension; **Maintain** localized ownership; **Docs** accurate practical guidance.

## Package baseline → target scorecard

Each cell is baseline→target. Unchanged 4→4 is an explicit preserve decision. Evidence and reasons for every lower baseline/changed target are in the following table; [01](01-repository-assessment.md)'s package inventory supplies the positive API, lifecycle, configuration and consumer evidence behind retained 4s.

| Package | API | Learn | Config | Boiler | Compose | Life | Encap | Extend | Maintain | Docs |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `api` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `api/middleware` | 4→4 | 4→4 | 4→4 | 4→4 | 3→4 | 4→4 | 3→4 | 4→4 | 3→4 | 3→4 |
| `api/response` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `app` | 3→4 | 3→4 | 3→4 | 4→4 | 3→4 | 4→4 | 3→4 | 4→4 | 3→4 | 3→4 |
| `cli` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `config` | 3→4 | 3→4 | 3→4 | 4→4 | 3→4 | 4→4 | 4→4 | 4→4 | 3→4 | 4→4 |
| `dependencies` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 3→4 |
| `internal/reference` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `lifecycle` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `mail` | 3→4 | 3→4 | 3→4 | 3→4 | 4→4 | 3→4 | 3→4 | 4→4 | 3→4 | 3→4 |
| `mail/integration` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `media` | 4→4 | 3→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `media/integration` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `media/migrations` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `messaging` | 3→4 | 3→4 | 4→4 | 4→4 | 3→4 | 4→4 | 3→4 | 4→4 | 4→4 | 3→4 |
| `messaging/middleware` | 4→4 | 3→4 | 4→4 | 4→4 | 3→4 | 4→4 | 3→4 | 4→4 | 3→4 | 3→4 |
| `notification` | 4→4 | 3→4 | 4→4 | 4→4 | 3→4 | 3→4 | 3→4 | 4→4 | 3→4 | 3→4 |
| `notification/integration` | 4→4 | 3→4 | 4→4 | 4→4 | 3→4 | 4→4 | 4→4 | 4→4 | 4→4 | 3→4 |
| `notification/migrations` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `seeders` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |
| `testdb` | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 | 4→4 |

### Evidence and target justification

| Package / dimensions | Why the baseline has these scores | What supports the target |
| --- | --- | --- |
| API, response: all 4 | Existing strict JSON/body-size tests, safe envelope mapping, explicit native binder escape hatch, no listener/resource ownership; request/response guides explain limits | Preserve tests and native options; no code redesign needed |
| API middleware: Compose/Encap/Maintain/Docs 3 | F01 reporting differs from other effect boundaries; raw cause bypasses common diagnostics despite safe response mapping | T2.1 and consistent log policy remove the exception without changing handlers |
| App: API/Encap/Compose 3 | F07 resource-opening helper is an inappropriate independent-use entrypoint; F02 incomplete app-client policy | T2.2/T2.7/T3.1 reduce exported assembly responsibility and align effects |
| App: Learn/Config/Maintain/Docs 3 | F04 maintenance in two locations, F05 late selected-setting failures, F09/F10 conflicting assembly instructions | Central schedule cutover, preflight and source-backed single authority; four-file composition retained |
| CLI: all 4 | Native commands, fresh factories, explicit allowlist, bounded output and startup validation; existing CLI tests/README | Preserve; no promise to undo a command effect after output failure |
| Config: API/Learn/Config/Compose/Maintain 3 | F05 caller must learn constructor-specific validation after app validation; F07 selection in app | Shared owner validation and pure named selection. Four in Life because config already owns no resources |
| Dependencies: Docs 3, others 4 | Resources/Targets split is typed and architecture-checked; F09 guide uses wrong field | Correct compiled/source-backed assembly; no new bundle abstraction |
| Reference: all 4 | One private syntax helper, no configuration, authorization or resource lifecycle; media/notification reference tests cover consumers | Keep small/private. “Extend” means callers retain separate semantic policies, not runtime plugins |
| Lifecycle: all 4 | Explicit ready/dependency ordering, failure cleanup, staged drain and owned close; dedicated tests plus external app tests | Preserve; sustained-load/deployment scope remains separately validated, so no score 5 |
| Mail: API/Learn/Config/Boiler/Life/Encap/Maintain/Docs 3 | F03 makes callers repeat memory/cleanup/cancellation policy; F05 standalone/app validation differs; guide repeats common paths | T2.3/T2.5 owner policy plus consolidated chapter; Sender/Mailer extension already coherent (4) |
| Mail integration: all 4 | One explicit task-registration function, optional native target, breaks actual import cycle; externally exercised queue/mail path | Preserve; no requirement for separate README/test file if owner guide/external suite covers it |
| Media: Learn 3, others 4 | Operation API is compact, but developer must distinguish storage bucket from recoverable media asset and authorize polymorphic owner | Progressive capability decision/independent-use recipe in T4.1; preserve tested SQL/blob recovery complexity |
| Media integration/migrations: all 4 | Bounded upload adapter, owner-scoped routes, recovery task, explicit embedded migration registration; corresponding tests | Preserve HTTP/persistence contracts; no aesthetic relocation |
| Messaging: API/Learn/Compose/Encap/Docs 3 | F02 exception and danger of confusing GoChannel acceptance with durable work | Guard and explicit guarantee table/source-backed workflow; runner lifecycle remains 4 due startup/drain tests |
| Messaging middleware: Learn/Compose/Encap/Maintain/Docs 3 | F06 attempt handler reports final exhaustion; two reporting owners | T2.6 clear attempt-versus-final ownership, tested context/trace behavior |
| Notification: Learn/Compose/Life/Encap/Maintain/Docs 3 | F04 hidden maintenance timing, F08 SQL locality, multiple durability/provider states require careful learning | Central cadence; private persistence ownership; progressive operation/receipt/error docs. API/config/boilerplate already 4: one message, no redundant channel registry |
| Notification integration: Learn/Compose/Docs 3 | Pruning has no central task path; normal user/session adapter versus polymorphic core requires explanation | T2.4/T3.2 task integration and documented authorization callback; preserve distinct policies |
| Notification migrations: all 4 | Owned baseline schema with constraints/versions, embedded explicit registration, PostgreSQL persistence coverage | Preserve; no migration rewrite warranted |
| Seeders: all 4 | Zero-value named registry, validates requested list before executing, caller-owned side effects; small tests/README | Preserve explicit execution and partial-progress semantics |
| Testdb: all 4 | Disposable DB/Redis test helpers, clear test-only imports enforced by architecture checks, external test guide | Preserve test-only boundary; not scored as a production runtime component |

These targets depend on actual implementation and verification. Keeping intentional limitations explicit is sufficient for 4; adding more wrappers/features is not a path to a higher score.

## Objective DX and architecture indicators

| Indicator | Measured baseline | Acceptance target / how to verify |
| --- | --- | --- |
| Go/package inventory | 84 total, 21 platform | No unmotivated package creation; every changed boundary explained |
| Platform source footprint | 82 production Go files, 45 colocated test files | No arbitrary total reduction. App stays four production files; notification extraction adds no files |
| Business-service access to Resources | Architecture tests enforce no dependency-bundle usage | Zero; verify imports and architecture tests after every module change |
| Colocated app/business service/repository tests | None in current app/Identity service/repository | Remain absent; use external behavior suites |
| Basic module | Catalog: 26 physical source lines, one module file | One app installation location; no second transport manifest or container binding |
| Maintenance cadence locations | App schedule list plus notification dispatcher housekeeping | One central maintenance list; dispatcher protocol polling explicitly exempt |
| Independent storage app import | One production example | Zero independent-storage imports of app; pure selector + owned Open/Close |
| Raw HTTP error diagnostic path | One identified raw cause log site | Zero in ordinary framework HTTP failure reporting; private-marker regression |
| Retry false-final signal | Reproduced after one failure then success | Zero exhaustion/final-error logs for successful retry cycle |
| Canonical docs stale Resources field | One confirmed active fragment | Zero; source-backed/typed example gate |
| Published documentation URLs | 51 slugs | Preserve all unless an explicit redirect/migration is reviewed |
| Required core integration endpoints | Four configured in review/CI | Script fails before running if any are missing; no inferred pass from skipped tests |
| Changed exported helpers/contracts | Proposed storage removal, selected config additions, optional queue guard, pruning task, SMTP bound | Exact migration inventory; no unintended task/event/schema renames |

Setup-line counts include comments/imports and are descriptive, not targets to golf code. No measured “50% less complexity” or onboarding-time reduction is claimed.

## Required verification matrix for implementation

| Contract | Meaningful cases | Location / gate |
| --- | --- | --- |
| HTTP diagnostics | Wrapped/unknown errors, canceled/deadline, panic, written response, correlation; no sensitive marker | Existing API middleware and `tests/platform` |
| Transaction effects | Real framework scope, canceled context, rollback, marshalling not called, client spy untouched; native task options preserved | Messaging tests, `pkg/asynqx/middleware`, external module integration |
| Notification intent | Business rollback removes intent; retry-stable key; channels independent; queue outage recovery | Existing notification PostgreSQL and profile workflow tests |
| SMTP | Aggregate boundary, zero/negative budget config, canceled-before-read, failing/seek/close readers, uncertainty after acceptance | External `tests/platform`, queue mail tests; no live provider needed |
| Config parity | Owner defaults, invalid namespaces/table/from/port, unused providers, secret-safe errors, installed DB preflight | Config/driver tests, external app tests |
| Event telemetry | Attempt failure then success, exhausted cycle, cancellation, restored context, producer link | Messaging middleware + existing workflow tests |
| Schedule cutover | Push disabled/enabled, one central entry, handler registered, worker delivery without cron, no hidden prune | `tests/modules/schedules_test.go`, notification integration/queue tests |
| Notification SQL extraction | Exact states/leases/retries, push ticket/receipt paths, logout-registration race and lock order | Existing PostgreSQL persistence/session suites |
| Storage independence | Valid/unknown named definitions, same options, no mutation, no app import, close on operation error | Config tests and built storage example |
| Lifecycle | Subscribe startup error, cancellation before ready, active HTTP/job drain, noncooperative work, close order | Existing lifecycle/httpserver/asynqx and external app suites |
| Documentation | Source-backed examples compile; exact API generation; second guide build deterministic; links resolve | Existing docs/module example tests, builder and CI |

Use the project's actual verification commands with `APP_ENV_FILE=''`:

```sh
go vet ./...
go test -race -count=1 ./...
go build ./cmd/... ./examples/...
go test -race -count=1 docs/build.go docs/highlight_test.go
./scripts/verify-integration.sh
```

The integration script requires isolated PostgreSQL and separate Redis DBs for queue, cache and limiter. Do not copy local private credentials into test logs. API generation and `go run docs/build.go` write generated output: run them in an isolated checkout to check drift, then deliberately include generated changes with approved source changes. The pinned vulnerability command is `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...`; record its date and call-path findings separately from module-only advisories.

Keep MySQL, Redis Cluster, S3/MinIO and other cloud/provider checks marked optional/not run until configured and executed. A fake Expo server validates protocol behavior, not device delivery. Live Google/SMTP/Expo tests need dedicated credentials and explicit test recipients/devices; no such sends were performed here.

## Architecture checks to preserve or narrowly extend

The existing `tests/architecture/conventions_test.go` already checks important import/structure boundaries. Keep those rules and add only demonstrated regressions:

- `pkg` must not import application `internal` packages.
- Business service/repository code must not import app or Resources/Targets.
- Capability cores must not import their integration package, app or business module assembly.
- Native transport registration stays in module/integration roots, before runtime.
- No production import of testdb; no tests placed in app/business service/repository packages.
- The independent storage example does not import app after migration.

Do not make a brittle text checker that dictates every file name or rejects native library types. Check architecture where it expresses an actual ownership boundary; test runtime guarantees behaviorally.

## Performance and operational verification

Only introduce a performance claim after measurements on a named toolchain, architecture and workload. Existing memory-cache benchmarks and prior storage-build-cost research are useful baselines, not measurements of this proposed change.

For SMTP, measure allocations/time for small, at-budget and oversized aggregate attachments with a cooperative reader. Expect memory bounded by the configured raw-byte budget plus encoding/library overhead; do not promise constant-memory streaming or instant cancellation of an arbitrary blocking Reader.

For notification changes, compare SQL query count and maximum rows per prune/dispatch batch before/after, keeping batch limits and indexes stable. Measure idle worker behavior with/without push and cron. Do not weaken leases, idempotency or recovery grace periods for a microbenchmark win.

For lifecycle, include admitted work during shutdown, not only an idle termination. Verify listeners stop admission, workers/subscribers join, and dependencies do not close early. Document cooperative timeout limits. Repeat deployment-level probes only on isolated fixtures; no need to alter the user's Kubernetes services to validate these code changes.

No target latency/throughput/RTO/RPO is invented in this review. They require an actual deployment/workload and support promise.

## Definition of done

Harmonization is complete when:

1. Each selected finding is fixed or explicitly deferred with rationale; no speculative structural work has slipped in.
2. Every platform package follows applicable conventions or documents a meaningful semantic exception. “One pattern” does not erase SQL-versus-Redis-versus-provider guarantees.
3. The existing module shape, owner service/repository contracts, resource borrowing, eager CLI construction and centralized scheduling are consistently taught and enforced.
4. All changed call sites and complete examples compile. Named storage works without app; mail/notifier/media/CLI independent use stays documented and tested at the relevant boundary.
5. Core build/vet/race/integration and documentation gates pass, with explicit optional skips; no relevant regression is hidden by missing endpoints.
6. Runtime resource ownership, transaction rejection, attachment bounds/cleanup and retry reporting have behavioral evidence.
7. HTTP/task/event/schema/config compatibility changes have a concrete migration and rollout order; existing persisted contracts remain stable unless deliberately changed.
8. Documentation reflects shipped APIs, preserves useful URLs, names success/failure semantics and links to compiled sources. Historical guidance cannot override current conventions.
9. Objective DX indicators are met; subjective scores are reassessed from evidence rather than assumed achieved.
10. Source changes are reviewable and preserve unrelated work; no secrets, unintended generated outputs or experimental provider changes are introduced.

**Publication is a separate gate.** Public module path, security contact, support policy, clean tagged source, release notices/checksums and verified target environments remain necessary before promising v1 support. Completing this blueprint does not itself create a release.
