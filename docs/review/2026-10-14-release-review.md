**Release review — 14 October 2026**

The current architecture is suitable for a first public application-template release after targeted corrections. Keep the package layout, explicit registration, owner contracts, Resources/Targets and centralized lifecycle. The highest-value remaining work concerns authentication edge cases, push lifecycle policy, resource limits, truthful readiness and release documentation.

My recommendation is to prepare a `v0.1.0` candidate after addressing the findings below. Declare a stable `v1` only after deciding which exported APIs and operational contracts you will maintain. Go's versioning guidance distinguishes development releases from stable v1 APIs. [Go module version numbering](https://go.dev/doc/modules/version-numbers).

This is a review, not an implementation plan that supersedes `docs/conventions.md`. No production code was changed. Existing working-copy changes were preserved. Temporary probes were removed from the repository after execution.

**Review baseline and scope**

The reviewed working copy is based on commit `124aad93c0c1661b70cac95835808f06da241a12`, with substantial uncommitted application and naming changes already present. The commit alone does not identify the reviewed source. A 387-file manifest of Go/SQL source, tests, examples, verification scripts, workflow YAML, module files, Dockerfile and `.dockerignore` has SHA-256 `2f12f5e9ed758938ce181c5d8bba845743de1badfed02f59f592a8d3f0c9fcc5`; the manifest is retained as a local verification artifact. This is review evidence, not a release bundle.

The repository contains 211 production Go files under cmd/internal/pkg and 161 Go test files. Review focused on construction and ownership; authentication and revocation; HTTP authorization, errors and uploads; SQL transaction boundaries; queues, local events and scheduling; notification retries and receipts; media recovery; cache and reusable packages; configuration; documentation; and build/release automation. Tests and source inspection are evidence for the exercised paths, not an exhaustive security certification.

**Findings and priorities**

P1 means fix before publication. P2 means a concrete correction or explicit release decision is needed. Capacity risks are labeled separately from functional failures; naming preferences appear later.

| ID | Priority | Finding | Evidence |
| --- | --- | --- | --- |
| R1 | P1 | Empty password hash can compare successfully | Reproduced through exported hasher API |
| R2 | P2 | Refresh replay revokes authentication but retains push delivery | PostgreSQL/Redis probe with fake provider |
| R3 | P2 | Registration accepts a state that disables recipient push delivery | 101 registrations accepted; subsequent Send fails |
| R4 | P2 | Local event buffer does not bound active work or backlog | 256 blocked handlers with configured buffer 64 |
| R5 | P2 | Redis batch limit is checked after all values are fetched | About 64 MiB fetched before rejecting an 8 MiB result |
| R6 | P2 | Scheduler signals readiness without establishing Redis connectivity | Ready closes against a refused connection |
| R7 | P2 | Package documentation claims removed transactional async behavior | Source/documentation mismatch |

**R1 — An absent stored password must never authenticate**

Location: [pkg/hasher/hasher.go](../../pkg/hasher/hasher.go#L65).

`Compare` substitutes a dummy bcrypt hash when `encoded == ""`, but returns the comparison result unchanged. The dummy plaintext is a constant in `New`, so it can match. The timing defense becomes a valid credential for callers that pass an absent credential as an empty hash.

Executed reproduction:

```go
h, err := hasher.New()
if err != nil {
    panic(err)
}
matched, err := h.Compare(context.Background(), "non-account dummy credential", "")
// Observed: matched == true, err == nil.
```

The shipped OTP/Google sign-in flow does not use this package, so this is not evidence of an OTP endpoint bypass. It is a security defect in the exported reusable package.

Correction: preserve the dummy bcrypt work, but force a non-match whenever the original encoded credential was empty. Add a regression test using the exact dummy plaintext, alongside ordinary empty-hash and valid-hash cases.

**R2 — Define one policy for session-bound push after revocation**

Locations: [refresh replay](../../internal/modules/identity/repository/session.go#L69), [delivery eligibility](../../internal/platform/notification/push.go#L34), [explicit revocation](../../internal/modules/identity/service/session.go#L118).

Explicit `RevokeSession` and `RevokeSessions` revoke and detach registrations in one transaction. Replay detection updates the session directly inside `RotateRefresh`, returns an authentication denial after committing, and never detaches its subscriptions. Delivery checks subscription version and disabled status, but not the owning session's eligibility.

The focused probe created a verified account, session and push registration, replayed a consumed refresh token, then attempted a notification through a fake provider. Observed: authentication denied, session revoked, subscription still enabled, provider Send called once.

Source inspection shows the same missing coordination in single-session sign-in displacement and user changes that revoke sessions. Natural session expiry also does not disable a subscription by itself. Those additional paths were traced, not independently exercised in this probe.

The [notification documentation](../../internal/platform/notification/README.md#L364) already states that other lifecycle flows must supply their own detachment policy. The concern is therefore a gap in the shipped Identity policy, not an undocumented promise of universal logout semantics. Push is not an authentication credential; impact depends on notification content, but old devices can continue receiving account-related notifications.

Correction: make the intended relationship explicit and apply it to all built-in transitions. For session-bound destinations, either disable them as part of the owning transition or check current delivery eligibility through an explicit owner contract. Preserve the repository-owned commit-on-denial rule for replay detection. A best-effort post-commit event is insufficient for mandatory revocation behavior. Tests should cover replay, displaced sessions, account suspension, expiry, re-registration and already queued deliveries. Existing provider requests cannot be recalled.

**R3 — Enforce the installation limit when admitting registrations**

Locations: [Subscribe](../../internal/platform/notification/subscriptions.go#L38), [pushTargets](../../internal/platform/notification/push.go#L67).

`Subscribe` accepts unlimited distinct installation IDs. `pushTargets` loads 101 registrations and rejects the entire operation when more than 100 are active. A valid authenticated session can therefore create a state in which every subsequent push attempt for that recipient fails with `ErrInvalidMessage`. This can also happen when a client regenerates installation IDs instead of persisting one.

The probe registered 101 distinct installations through Identity and observed zero additional provider sends from the next Send call. The same expansion is used by Enqueue, so selecting push can also prevent a multi-channel intent from being persisted.

Correction: enforce a per-recipient/project active-installation limit atomically during Subscribe, permitting updates to existing installations at the limit. Return an explicit capacity error and provide a deliberate recovery path for existing excess registrations. If more than 100 installations is a supported use case, implement bounded fanout batches instead. Do not silently truncate destinations. Verify concurrent registration at the boundary and ensure other active installations remain deliverable.

**R4 — Bound admitted local events, not just the channel buffer**

Location: [internal/app/infrastructure.go](../../internal/app/infrastructure.go#L396).

The GoChannel configuration sets `OutputChannelBuffer: 64`. Watermill's publisher dispatches asynchronously and the router starts handler goroutines per message; this buffer is not a cap on active handlers or outstanding events. A probe using the installed Watermill version and the same bus configuration admitted 256 simultaneously blocked handlers.

Under slow consumers or a Redis outage in the welcome-event handler, continuing publication retains increasing work. Retry middleware limits a delivery cycle, while GoChannel can redeliver a Nack again. The current event guide correctly explains this; the missing guarantee is bounded resource use, not durable delivery or a supposed global retry count. Watermill also documents background publication. [GoChannel publishing](https://watermill.io/pubsubs/gochannel/).

Correction: define limits for active handlers and total outstanding events/bytes, together with an explicit overflow policy appropriate to best-effort local events. A semaphore inside an already spawned handler bounds execution but still leaves an unbounded queue of waiting goroutines. Likewise, waiting for acknowledgment needs a finite failure policy so a permanently failing consumer cannot indefinitely retain all admission capacity. Preserve the current best-effort contract and report rejected/dropped work with bounded telemetry. Verify slow-consumer, repeated-failure, recovery and cancellation behavior.

**R5 — Make Redis read memory proportional to the configured budget**

Location: [pkg/cache/redis/operations.go](../../pkg/cache/redis/operations.go#L102).

`GetMulti` pipelines every GET before calculating `MaxBatchBytes`. All bulk replies have already been read and retained when the loop discovers an oversized result. The returned error is correct; the 8 MiB result limit does not provide an 8 MiB working-memory limit.

The probe stored 64 individually valid entries of 1 MiB minus 256 bytes. A client hook measured 67,092,480 value bytes already fetched when GetMulti returned `ErrTooLarge`. With the permitted 1,000 keys and default entry limit, a legal batch can approach 1 GiB of value data before rejection. Concurrency multiplies the pressure. No 1 GiB stress test was run.

Correction: read bounded chunks and stop when the accumulated budget is exhausted. Choose chunk sizes against the entry bound and document the maximum overshoot. For a strict bound with externally written or legacy oversized values, cap replies or use an atomic server-side size/read check as well; a separate STRLEN check races with writes. Include a regression that measures data fetched, rather than only asserting the final error.

**R6 — Align scheduler startup readiness with the worker**

Location: [pkg/asynqx/scheduler.go](../../pkg/asynqx/scheduler.go#L26).

`SchedulerRunner.Run` closes Ready immediately after native `Start`. In the installed Asynq version, Start launches cron and its heartbeat; it does not verify Redis. App queue construction is lazy. The standalone scheduler can therefore pass lifecycle readiness while its only useful operation, enqueueing schedules, cannot succeed. The worker adapter already performs a bounded startup Ping.

The probe used an unused loopback port with refused connections and observed Ready close. This verifies the adapter's behavior; it was not a complete deployed-scheduler outage rehearsal. A separately enabled authentication/rate-limit dependency can happen to fail earlier during app construction, but does not fix the scheduler contract.

Correction: supply a bounded Redis startup probe to the scheduler adapter, consistent with worker startup, and check cancellation before starting. Keep ongoing dependency health separate from the one-time Ready signal; a successful initial Ping is not a permanent health guarantee. Verify unavailable startup, recovery and cancellation without losing resource ownership rules.

**R7 — Remove obsolete transaction guarantees from active package docs**

Location: [pkg/database/README.md](../../pkg/database/README.md#L105).

The README claims that database queues and pubsub automatically join transactions, outbox writes require them, and transactional async workers use savepoints. It also refers to async schema-publishing commands. Those implementations are absent from the current project. The current queue is Asynq, the event bus is GoChannel, and the framework's external-effect guard rejects transaction contexts. Notification Enqueue supplies a specific SQL delivery ledger.

A consumer following the package README can choose the wrong persistence boundary and assume atomic behavior that is not supplied. The central conventions and transaction guide are more accurate, but package documentation remains part of the public surface.

Correction: describe the actual Executor/TxManager behavior, notification SQL intent, and external-effect boundary. Remove obsolete async command references. Keep historical migrations and historical review documents intact. The docs builder verifies links and generated output; it cannot detect a false prose guarantee. Add a focused documentation review to release changes affecting delivery semantics.

**Naming and concepts worth settling before freezing APIs**

The current `app`, `modules`, `platform` and `pkg` ownership is understandable. Keep `dependencies.Resources` and `dependencies.Targets`: runtime collaborators and startup registration destinations have different lifetimes. Keep `Pusher`, `Mailer`, `Notifier`, `MailRenderer`, `EventPublisher`, `RootCommand`, `TaskRouter` and `EventRouter`. They identify useful roles without forcing every dependency to share a suffix. `Pusher push.Sender` is a reasonable field/type pairing.

| Area | Recommendation | Reason |
| --- | --- | --- |
| Product description | Use “modular Go application template” consistently; describe pkg as reusable packages | README/conventions reflect clone-and-customize composition. “Framework” alone can suggest a drop-in externally importable app runtime, which internal packages do not provide |
| Config versus Options | Follow the distinction already in conventions: settings versus constructor controls/collaborators | This is a repository convention, not a universal Go requirement |
| Conversion methods | Prefer names matching the target: `HTTPConfig.ServerConfig()`, `DatabaseConfig.ConnectionConfig()`, `MailConfig.SMTPConfig()`, cache `MemoryConfig/RedisConfig/DatabaseConfig` | Current methods named Options frequently return Config types; leave native Redis/CORS Options terminology intact |
| Media settings | Consider `media.Config` and `media.DiskConfig` for current settings-only Options types | Base URL, signing key, visibility and URL mode are settings; no new wrapper is needed |
| Mixed constructor structs | Keep cohesive collaborator-bearing Options; do not split every existing struct mechanically | `notification.Options` and `service.AuthOptions` already convey construction dependencies. Avoid extra types just for naming symmetry |
| Diagnostics | Align lifecycle error prefixes with `lifecycle:` and refresh stale “mail views required” wording | Error ownership and renamed `MailRenderer` should agree; preserve errors.Is identities and public codes |
| Secret settings | Apply deliberate JSON/log-format redaction to credential-bearing config types, including SMTP Password and Expo AccessToken | Database config already has stronger redaction. This is preventive consistency; no default credential logging leak was established |
| Exported helpers | Inventory the public surface before v1, including currently unused `pkg/mapper` | Each exported package is a maintenance promise. Keep it if it is intended for consumers; avoid adding generalized helpers without an actual use case |

Three semantic distinctions should remain prominent in every guide: a process selection is not a module selection; an authenticated API is not operation-specific authorization; and successful publication/queue acceptance/persisted notification intent/provider acceptance/device receipt are different outcomes. Current conventions mostly handle these well. Preserve their specificity as documentation is shortened.

The long access-token default and unlimited refresh/session lifetimes are deliberate current policies, not findings of an authentication bypass. Before publication, document the production profile you support, including retained refresh-token history, session-bound push policy and proxy topology. The peer-based limiter intentionally ignores forwarded headers; deployments behind a proxy need an explicit ingress/rate-limit policy rather than blindly trusting client-supplied headers.

**Publication work that remains**

1. Choose the public module path and product/support identity. The local-only module path works in this checkout but does not establish an externally resolvable dependency path. Validate the chosen path from an independent consumer project after updating imports. Use a fresh immutable tag for each publication. [Go publishing guidance](https://go.dev/doc/modules/publishing).
2. Establish the private security reporting destination and supported-version policy. SECURITY.md explicitly lists these as unfinished. These are release-owner decisions; a review cannot invent the contact or support commitment.
3. Make module metadata tidy. `go mod tidy -diff` exited 1: it would remove unused `github.com/samber/lo` and add checksum entries for `go.opentelemetry.io/otel/sdk/log/logtest`. This does not invalidate the passed builds. Commit the intentional tidy result during implementation and add a read-only tidy check to CI.
4. Produce a fresh clean-tag release bundle after all corrections and naming decisions. The existing dated readiness records correctly describe historical evidence and should remain historical. Re-run candidate verification for each advertised architecture; record known skips, toolchain, backend versions, module inventory and notices. Include `.dockerignore` in the source-input manifest because it influences Docker inputs. Update the release-readiness artifact list to include scheduler, which the build script already exports.
5. Keep the support matrix narrow and explicit: PostgreSQL and standalone Redis are core CI; generic MySQL, Redis Cluster, cloud storage and live Google/SMTP/Expo integrations require their own evidence. A driver interface or mock does not certify a deployment. Runtime and active-load outage/drain checks must match the intended hosting target. No throughput or recovery-time claim follows from the current unit/race suite.

**Verification evidence and limits**

| Check | Result and provenance |
| --- | --- |
| Full race-enabled integration suite | Passed immediately before this review, on the same production source, using disposable PostgreSQL 17 and Redis 7 with all required test endpoints; the log is retained as a local verification artifact |
| Vet and command/example builds | Passed in that same preceding verification |
| Documentation tests and generation | Passed in the preceding verification; 53 pages generated |
| Current vulnerability scan | `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...` exited 0 during this review: zero reachable and zero imported-package findings; one module-only advisory in code not called by this project |
| Password hasher probe | Reproduced the empty-hash match |
| Identity/push probes | Race-enabled focused test passed against disposable PostgreSQL/Redis, confirming replay retained delivery and the 101-registration failure; fake provider only |
| Redis cache probe | Measured about 64 MiB of replies before oversized-result rejection |
| Event concurrency probe | 256 concurrently blocked calls with the current GoChannel output buffer of 64 |
| Scheduler probe | Ready signaled despite connection refusal |
| Module metadata | `go mod tidy -diff` failed with the explicit tidy differences above; files were not rewritten |
| Working-copy hygiene | Temporary repository test removed; no production fixes, commits, tags or publications performed |

The small probes intentionally asserted observed problematic behavior; their passing status does not mean the behavior is acceptable. Their purpose was to distinguish demonstrated findings from speculation. They should become normal regressions with the opposite expected behavior when the fixes are implemented.

This review did not rerun the full image/release-bundle process, a production load test, a deployed outage/restore exercise or live third-party delivery. The current source remains a working copy. The only retained repository addition from this review is this report.

**Suggested correction order**

Fix R1 first. Address R2 and R3 together because they share subscription lifecycle and admission policy. Then bound events/cache and align scheduler readiness. Correct the active documentation and perform the small naming pass before making the public API promise. Finish with tidy metadata and a clean candidate verification record. Keep each correction independently reviewable and add tests for the demonstrated failure boundaries, respecting the external app/business test suites.

**Approved implementation — 14 October 2026**

The findings above preserve the review baseline. The user subsequently approved
fixing them. The working copy now includes these corrections:

| Finding | Implemented correction | Regression evidence |
| --- | --- | --- |
| R1 | Empty original hashes always compare false after dummy bcrypt work | Exact dummy-plaintext case in hasher tests |
| R2 | Notifier checks an explicit SessionActive callback before each session-bound provider send; app supplies Identity's authoritative repository check | Replay, displacement, suspension, expiry, queued intent, fresh-session recovery, missing/failed checks, and registration rotation during a stale check |
| R3 | Subscribe serializes recipient/project admission and enforces 100 active installations; existing registrations can rotate at capacity | Concurrent admission accepts exactly the remaining slot, existing recipients remain deliverable, and legacy excess has an explicit error/recovery path |
| R4 | LocalBus retains GoChannel while bounding outstanding count/data and active handler cycles; exhausted consumer cycles are logged and settled | Capacity and size rejection, blocked publication cleanup, bounded concurrency, retry exhaustion and capacity recovery |
| R5 | Single-key Lua performs atomic size/read checks; GetMulti pipelines bounded chunks | Measured reply budget, oversized external values, backend errors and existing cache tests |
| R6 | Scheduler checks its borrowed Redis pool before starting and signaling readiness | Refused Redis startup, cancellation, successful startup/drain and continued pool usability |
| R7 | Database package documentation now describes current SQL participation, Asynq/GoChannel effect guards and notification intent | Active-source review and rebuilt documentation |

Public Go API changes within this unreleased working copy:

- `media.Options` / `DiskOptions` become `media.Config` / `DiskConfig`.
- App configuration translations use `ServerConfig`, `ConnectionConfig`,
  `ProviderConfig`, `SMTPConfig`, `StorageConfig`, `LibraryConfig`,
  `MemoryConfig`, `DatabaseConfig`, `RedisConfig` and `MediaConfigs` as applicable.
  Native Redis and CORS Options names remain unchanged.
- `NewSchedulerRunner` now takes scheduler, Redis pool and startup timeout,
  and returns a runner plus an error.
- Standalone notification users supplying a nonempty session ID must provide
  `Options.SessionActive`; absent checks fail closed. Sessionless destinations
  remain supported. New subscription errors identify capacity and missing checks.
- The application event bus admits at most 128 outstanding events, 64 KiB per
  event, 8 MiB retained event data and 16 active handler cycles. One cycle permits
  the initial call and up to three retries. Exhaustion settles/discards that
  best-effort delivery. Serialization, object overhead and fixed subscriber fanout
  are additional; native bypasses retain native behavior.
- Redis cache reads require EVAL, GET and STRLEN permissions. Each read pipeline
  returns at most 8 MiB of value data; previously retained results and allocation
  overhead are additional. Oversized results still return ErrTooLarge.

SMTP and Expo credential settings now redact secrets in ordinary formatting/JSON
while retaining credential decoding. Lifecycle error prefixes identify their owner;
MailRenderer diagnostics use the current name. Module metadata is tidy, CI checks
`go mod tidy -diff`, and release source manifests include `.dockerignore`. The layout,
Resources/Targets, Pusher role name, process flags, transaction ownership, applied
migrations and environment keys remain intact. The optional mapper package was
retained; this pass establishes no new public support promise for it.

Verification completed on the updated behavior:

- Full `scripts/verify-integration.sh` race suite with all required endpoints against
  disposable PostgreSQL 17 and Redis 7; the log is retained as a local verification artifact.
- Focused real-backend regressions; the log is retained as a local verification artifact.
- `go vet ./...`, command/example builds, documentation builder tests and generation.
- Shared Docker image built for Linux AMD64. This verifies compilation/image
  construction, not a deployed load or outage rehearsal.
- Pinned govulncheck: zero reachable and imported-package findings; one module-only
  advisory remains outside called code. The log is retained as a local verification artifact.
- Read-only tidy and whitespace checks passed.

Existing user changes were preserved. No commit, tag, registry publication, live
provider delivery or clean-tag release bundle was created. Public module identity,
private security reporting and support policy remain owner decisions. Regenerate
candidate evidence after settling them and committing the intended release source.

**Approved release policy — 14 October 2026**

The user approved the recommended application-template distribution and support
policy. `SUPPORT.md` now defines latest non-prerelease maintenance, best-effort
response, PostgreSQL/standalone Redis core verification, package/API scope and
release-note requirements. `docs/upgrading.md` explains recording the adopted
source, renaming the starter module and manually porting upstream fixes. README,
contributor guidance and release readiness link those policies; the documentation
site publishes the adoption, support and security pages.

At this review baseline, the template retained a local-only module path; a public
import path was not required to distribute that source template. No `pkg` package is independently
released or supported as an external dependency. The earlier public-module-path
gate applies if that distribution model changes.

GitHub private vulnerability reporting is the selected channel, with report triage
owned by the upstream repository owner or delegated maintainers. This checkout has
no Git remote, so enabling/verifying the channel remains pending the destination
repository URL. SECURITY.md explicitly records that status. No release, tag or
publication is implied by adopting these policies.

Policy documentation verification passed: documentation builder tests, builder vet,
generation of 56 pages with local link/anchor validation, and whitespace checks.
This policy pass changed documentation only; the runtime verification above belongs
to the preceding hardening pass.
