# Platform harmonization assessment

This document owns the finding records and package decisions. [01](01-repository-assessment.md) owns the observed architecture/baseline; [04](04-developer-experience.md) owns developer examples; [07](07-refactoring-roadmap.md) owns execution order.

Priority: **P0** immediate critical issue; **P1** reliability/security or consequential policy gap before claiming v1 maturity; **P2** important consistency/DX improvement; **P3** localized maintainability/cosmetic improvement. No P0 issue was established. Effort is an engineering estimate: S roughly half to one day, M one to three days, L three to five days, including focused tests/docs, not elapsed delivery promises.

## Findings and concrete remedies

### F01 — Raw errors bypass the otherwise bounded diagnostic policy

**Observation/evidence.** `ErrorHandler` in [api/middleware/errors.go](../../internal/platform/api/middleware/errors.go), line 66, records `"error", err` for 5xx failures. HTTP bodies are safely translated. Queue/mail/event adapters instead use bounded categories or sanitized boundary errors; see `pkg/logging/errors.go`, `mail/queue.go` and `messaging/logger.go`. The isolated `TestReviewHTTPErrorLogsRawText` probe confirmed that an arbitrary private marker is present in logs and absent from the response.

**Problem.** Driver or provider error strings can carry sensitive values. A service author currently has to know which transport will log the same returned error. This is a confirmed disclosure path to logs, not proof that a real credential has been exposed.

**Recommendation/design.** Keep `ErrorHandler()` unchanged publicly. Replace the raw error attribute with `error_category: logging.ErrorCategory(err)` and the translated stable error code where available. Keep route and existing request/trace correlation. Do not place the raw cause in a span event as a substitute. Preserve the original error attached to Gin for internal control flow; sanitize reporting, not error identity.

**Impact/DX.** One framework rule for ordinary transport failures; services continue returning errors. Scope: HTTP middleware, `tests/platform/http_test.go`, middleware tests, logging/observability guides.

**Trade-off.** Default logs contain less provider detail. Prefer targeted, deliberately collected diagnostics when investigating a provider, rather than routine raw error capture. No global redaction engine or debug-secret flag is needed.

**Priority/effort/dependencies.** P1/S; task T2.1 after diagnostic policy T1.1.

**Acceptance.** Tests exercise unknown and wrapped application errors, canceled/deadline failures, panic recovery and committed responses. A private marker is absent from response, logs and exported spans. Status/code and correlation remain useful; `errors.Is/As` behavior is unchanged.

### F02 — External-effect transaction rules are only partly enforced

**Observation/evidence.** `mail.Mailer.Send/Enqueue`, external notification methods and media mutations call `database.RequireNoTransaction`. `messaging.Publisher.Publish[WithID]` in [publisher.go](../../internal/platform/messaging/publisher.go), lines 41/54, and the app queue chain in [resources.go](../../internal/platform/app/resources.go), line 320, do not. `NewJSONMessage` retains the supplied context. The transaction guide warns callers not to enqueue Redis tasks inside SQL scopes.

**Problem.** A module can accidentally publish/enqueue before a SQL rollback. A local message can also carry an inappropriate transaction-bearing context beyond its scope. This is a source-confirmed framework enforcement gap; the inspected Identity registration flow already publishes after its owned commit.

**Recommendation/design.** Call `database.RequireNoTransaction(ctx)` before marshaling/publishing in both framework Publisher methods. Add an optional `asynqmiddleware.RequireNoTransaction() Middleware` in the existing client middleware file, and install it outermost in app's queue chain. Its only behavior is the existing database guard followed by `next`; it must preserve native task/options/results/errors. Standalone Asynq consumers can opt into the same policy. A raw `*asynq.Client` remains native.

```go
// Proposed application assembly: transaction/cancellation policy precedes effects.
queue := asynqmiddleware.Chain(client.EnqueueContext,
    asynqmiddleware.RequireNoTransaction(),
    // existing routing and observation middleware remain in their current order
)
```

**Impact/DX.** App users learn one external-effect rule. Affected: messaging, `pkg/asynqx/middleware`, app queue construction, service call-site audit, transaction/event/queue guides. `asynqx.Client` and `messaging.Publisher` interfaces do not change.

**Trade-off.** The optional queue policy adds a dependency on the existing foundation transaction package. It is warranted by a real cross-component guarantee; no new transaction abstraction is added. Previously accepted unsafe calls now fail. This guard recognizes framework contexts only; native Bun transactions or discarded contexts cannot be detected. It does not provide an outbox or eliminate the post-commit enqueue crash gap.

**Priority/effort/dependencies.** P1/M; T2.2, after T1.1. Consumer migration T3.1 depends on it.

**Acceptance.** Under a real `TxManager.Execute`, guard rejects before serialization/client invocation; commit/rollback cannot leak a publication through the wrapper. Canceled contexts invoke no transport. Calls outside SQL still preserve routing, option order, returned TaskInfo and wrapped errors. `Notifier.Enqueue` continues joining SQL and remains valid. Guard tests live in messaging/middleware or external tests, not app/service/repository packages.

### F03 — Immediate SMTP attachment handling is an operational hazard

**Observation/evidence.** [mail/smtp.go](../../internal/platform/mail/smtp.go), lines 201–262, calls unbounded `io.ReadAll` before dialing. Cancellation is passed to dialing after attachment processing. Closing is deferred only after successfully adding an attachment. The isolated failing-reader probe confirmed that a canceled direct SMTP call still reads and that a failing reader is not closed despite `CloseAttachments=true`. `docs/mail.md` honestly documents partial cleanup and caller-enforced bounds.

**Problem.** The behavior is not a newly discovered violation of the documented partial-close contract, but it is an unnecessarily difficult default for a mature capability. Each caller must remember memory budgets, cancellation and failure-path cleanup. Queued mail already has a 64 KiB encoded payload limit and rejects readers.

**Recommendation/design.** Refine SMTP in place. Proposed `SMTPConfig.MaxAttachmentBytes int64` is an aggregate raw attachment-byte budget; zero uses 20 MiB, negative is invalid. Store the resolved value on SMTP. Read at most remaining budget + 1, fail with a package sentinel on overflow, and check context before processing and between reads. Set up requested cleanup at call entry, before validation/read paths. Define `CloseAttachments=true` as transferring each supplied closable reader to Send for the duration of the call; call each attachment's closer on every return path and join meaningful close errors with the primary error. Document that callers must not put the same owned reader in multiple attachment entries.

Keep nil-body behavior and seek-to-start semantics unless a separately justified compatibility change is approved. Prefer caller-owned `defer Close` in the normal example. Do not accept attachment readers in queued payloads. Do not claim cancellation can interrupt an arbitrary blocking `io.Reader`: callers must supply bounded, interruptible file/network readers; do not create a goroutine per read that can leak.

**Impact/DX.** One bounded immediate-send default, predictable explicit cleanup; existing Message/Sender/Mailer APIs remain. Scope: `mail/smtp.go`, comments in `mail/mail.go`, optional owner config default/validation, external mail tests and attachment guide.

**Trade-off.** A 20 MiB default rejects formerly unbounded input and consumes memory proportional to that bound; MIME encoding adds overhead and providers may impose lower limits. Typed config can raise/lower it. No required env variable, disk spooler, attachment store or provider registry is introduced. Closing can surface a new error after SMTP acceptance; document that this does not make retry safe.

**Priority/effort/dependencies.** P1/M; T2.3, coordinated with owner validation T2.5. No live provider is required for the regression tests.

**Acceptance.** Boundary tests at limit−1/limit/limit+1 across multiple attachments; canceled-before-read causes zero reads; interrupted cooperative reader returns promptly; all owned readers close on validation/read/transport error; original causes remain inspectable. Queue payload/attachment rejection stays unchanged. Memory benchmark confirms bounded growth with oversized input without claiming constant-memory streaming.

### F04 — Subscription retention runs outside the central schedule policy

**Observation/evidence.** [notification/delivery.go](../../internal/platform/notification/delivery.go), lines 222–237, prunes stale subscriptions immediately and hourly inside every dispatcher, using a 90-day cutoff. `subscriptions.go:102` suggests daily scheduling; the CLI has the same age policy separately. [app/modules.go](../../internal/platform/app/modules.go), line 75, centralizes media/Identity recurring work, matching `docs/scheduling.md` and the user's explicit preference.

**Problem.** Operators cannot find all maintenance timing in the central list. Every worker dispatcher performs housekeeping regardless of scheduler role. This is policy fragmentation, not proof that the current bounded SQL operation corrupts data.

**Recommendation/design.** Retain dispatcher polling for due delivery and push receipts. Remove its subscription-pruning timer. Add an additive `notifications.prune_subscriptions.v1` native task constant in existing notification delivery definitions and handler in `notification/integration/tasks.go`; register it in integration. Keep the 90-day age in one integration helper used by CLI and task, computed at execution time. App registers an hourly task only when push is enabled, with bounded timeout and enqueue uniqueness. Reuse `PruneSubscriptions(ctx, before)` and its 1,000-row bound.

**Impact/DX.** Timing has one location; standalone users call `PruneSubscriptions` or schedule it themselves. Touch dispatcher, existing integration files, app schedule list, schedules/integration tests and operations guide.

**Trade-off.** Workers without a scheduler no longer perform retention cleanup automatically. Release notes must explicitly require one scheduler owner or operator pruning. Repeated hourly batches may need time to clear a large backlog; uniqueness is only duplicate-enqueue suppression, not distributed leader election. No change to delivery polling, lease recovery or receipt polling is justified.

**Priority/effort/dependencies.** P1/M policy fix; T2.4 followed by T3.2 rollout/docs. Ship handler before enabling schedule in mixed deployments.

**Acceptance.** Central schedules test asserts push-on/off behavior, hourly cadence and correct type; worker-without-scheduler still delivers pending notifications and checks receipts but does not prune subscriptions. Handler uses execution time, preserves active/recent subscriptions and processes bounded stale rows. CLI and handler use the same age policy. No old delivery task version changes.

### F05 — Settings have inconsistent validation owners and timing

**Observation/evidence.** [config/cache.go](../../internal/platform/config/cache.go), line 36, validates generic limits; Redis namespace rules live only in `pkg/cache/redis/store.go:35`. `config.FromEnvironment` accepts `bad:namespace`; the driver rejects it without I/O. `config/mail.go` validates sender/port/credential-pair rules more comprehensively than `mail.NewSMTP`. Generic DB config supports MySQL, while installed module migrations are PostgreSQL and reject that combination later in assembly.

**Problem.** “Configuration validated” does not consistently mean selected settings are constructible. App and standalone users see different errors/default behavior. Some deterministic failures appear after other resources have already opened.

**Recommendation/design.** Put pure default resolution/validation next to the owning component config; constructors and app converters call that same logic. Concretely add owner validation for SMTP and cache-driver configs rather than reimplementing rules in env parsing. Preserve namespace defaulting and backend-specific limits. SMTP must continue permitting an omitted default From when each message supplies a valid From; validate a configured default without removing that native usage. App mail settings still supply their validated default sender. Add app-level installed-backend preflight before acquisition when a DB is configured; retain generic MySQL capability in `pkg/database`, cache and migrations tools. Do not reject the deliberate no-DB inspection assembly merely because serving later requires DB.

**Impact/DX.** Earlier, matching errors without new setup steps. Scope: config cache/mail/validation, owner cache driver files and mail SMTP, app startup preflight, focused config and external app tests; config reference.

**Trade-off.** Sharing pure validation may expose a small owner `Validate` method; that is preferable to a generic validation layer. Some previously late failures become earlier failures. Disabled provider settings should not require secrets or connections. Defaults must remain the same except the explicitly proposed attachment bound.

**Priority/effort/dependencies.** P2/M; T2.5. Run before final source-backed config documentation T4.2.

**Acceptance.** Table tests compare app conversion and standalone construction for valid/default/invalid values; invalid namespace/from/port/selected driver fails without network/file acquisition; errors name settings without supplied secret values. Full/quickstart profiles retain enablement and required credential behavior.

### F06 — Event attempt telemetry claims a terminal outcome prematurely

**Observation/evidence.** [messaging/middleware/middleware.go](../../internal/platform/messaging/middleware/middleware.go), line 126, writes `retry_budget_exhausted: ctx.Err()==nil` from `TraceHandler`. App installs it inside retry middleware at `resources.go:487–517`. The isolated one-failure-then-success probe logs `true` even though processing succeeds.

**Problem.** Operators receive misleading exhaustion warnings and duplicate failure reports. Lack of cancellation does not establish exhausted retries.

**Recommendation/design.** Keep attempt spans and their safe error status in TraceHandler; remove its terminal-looking warning or make it an explicitly named attempt event without exhaustion claims. Let the existing outer `LogFailures` own terminal logging for the configured retry cycle. Do not rename its outcome “permanently lost” because transport redelivery and poison publication are separate policies.

**Impact/DX.** One owner for final failure reports, unchanged middleware signatures/order; updates existing event middleware tests and observability guide.

**Trade-off.** Fewer warning lines; attempt detail remains in spans and native retry diagnostics. No new telemetry facade is required.

**Priority/effort/dependencies.** P2/S; T2.6 after T1.1; independent of persistence changes.

**Acceptance.** Fail-once-then-success emits no exhaustion/final-failure log; exhausted cycle logs once at its boundary; canceled cycle is categorized correctly; context restoration and producer links remain intact. No raw errors/payloads enter exported telemetry.

### F07 — Independent named storage unnecessarily depends on app assembly

**Observation/evidence.** [app/resources.go](../../internal/platform/app/resources.go), line 158, exports `OpenFilesystemConnection(ctx, config.Config, name)`. [examples/storage/main.go](../../examples/storage/main.go) imports app for it. This imports the application composition graph, including installed modules, to select and open one reusable bucket. `DiskConfig.Options` and `storage.Open` already supply the underlying pieces.

**Problem.** A developer seeking independent storage must learn an app helper instead of the foundation's acquisition pattern. Selection policy is in an unexpected package.

**Recommendation/design.** Add pure `FilesystemConfig.Options(name string) (storage.Config,error)` to the existing config file. It resolves the name and validates the selected disk via owner config without I/O. `app.openFilesystem` and the standalone example then call `storage.Open(ctx, options)`. Remove the exported app helper after migrating all repository usages and docs. `DiskConfig.Options()` remains the raw conversion for a known definition.

**Impact/DX.** Independent storage no longer imports app; caller owns the returned connection. Native bucket operations and advanced options remain. This is dependency improvement, not a claim that the two-call API has fewer lines.

**Trade-off.** Exported helper removal is source-breaking for external template forks; provide an exact migration note. Do not create another filesystem package, registry or cached manager to preserve a one-call aesthetic.

**Priority/effort/dependencies.** P2/S; T2.7 then consumer/docs migration T3.1.

**Acceptance.** Unknown/nil/invalid named definitions fail without I/O; valid named selection produces the same provider settings. Example builds without an app import. Existing bucket lifecycle and supported native provider features remain unchanged.

### F08 — Notification persistence ownership is dispersed inside one package

**Observation/evidence.** [notification/store.go](../../internal/platform/notification/store.go) owns ledger/read-state SQL, but dispatcher metrics perform a raw SQL query at `delivery.go:242`, and subscriptions/push methods also construct SQL directly.

**Problem.** A maintainer changing persistence must inspect orchestrators and private store helpers. This is a locality concern; existing PostgreSQL tests establish substantial behavior, and no failed persistence invariant was demonstrated.

**Recommendation/design.** Move raw SQL behind private `store` receiver methods, keeping them in the current topic files where appropriate. For example `store.pendingAge(ctx)` and subscription persistence helpers; orchestration owns validation, provider calls and workflow decisions. Preserve one package and six production files. Do not export a Store/Repository interface or add separate SubscriptionService/DeliveryService abstractions.

**Impact/DX.** No application-facing API change; maintainers get a consistent place/receiver for DB operations. Scope only notification implementation and existing behavior tests.

**Trade-off.** A mechanical extraction can accidentally change transaction executor choice or lock order; leave SQL and operation sequence unchanged in the first patch. This is lower priority than correctness fixes and can be omitted if review shows the helper merely forwards once without clarifying ownership.

**Priority/effort/dependencies.** P3/M; T2.8 after T2.4 establishes the pruning handler tests; timing cutover remains T3.2.

**Acceptance.** Existing PostgreSQL ledger, retry/lease, receipt, subscription/logout-race tests remain green; no schema/task/payload/public API change; orchestrators no longer build ad hoc queries; code review confirms same lock order and transaction executor. File count does not grow.

### F09 — Documentation validation misses invalid assembly fragments and conflicting patterns

**Observation/evidence.** [docs/module-registration.md](../module-registration.md), line 23, uses `a.resources.Notifications`; actual field at `dependencies/dependencies.go:49` is `Notifier`. `tests/modules/onboarding_test.go` compiles selected complete examples, while assembly fragments are outside that protection. `docs/database-transactions.md` suggests defining an execution interface even though the canonical profile example and Identity accept `*database.TxManager` and the project asks to reuse owner contracts.

**Problem.** The docs builder and API generation can pass while an important learning example does not compile. Readers also face a needless contract ownership choice already decided by the project.

**Recommendation/design.** Fix the field; teach direct `*database.TxManager` first and describe consumer narrowing only as a justified advanced choice. Source the assembly excerpt from current code or add a typed fixture exercising `Resources.Notifier` and canonical construction. Extend the existing example test mechanism, not a general Markdown Go interpreter. Every substantial complete example should reference its compiled source; fragments declare prerequisites and are linked to a real caller. Keep generated docs downstream of one source.

**Impact/DX.** No runtime API change; source-backed learning path, reliable copy/paste. Scope module/transaction/dependency guides, docs builder tests where needed, external module examples/tests and navigation metadata.

**Trade-off.** Maintaining a few representative fixtures is cheaper than compiling every illustrative fragment. Do not advertise “all snippets compile” unless the full set is actually covered.

**Priority/effort/dependencies.** P2/S for corrections, M for expanded gate; T1.2 then T4.1–T4.3. Can begin immediately after factual report.

**Acceptance.** The stale field and recommended duplicate transaction contract disappear from canonical docs; example compilation fails if a referenced Resources field changes. Build remains deterministic, API spec and guide artifacts match sources, links resolve.

### F10 — Architectural instructions have drifted from the implemented conventions

**Observation/evidence.** The inherited `/Users/barakat/workspace/AGENTS.md` still describes a registration package, shared models and offline command metadata/help. The current [conventions](../conventions.md), actual package graph, direct module registration and explicit eager-help decision differ. Historical `docs/plans` and `docs/REWRITE_PLAN.md` describe earlier phases as well.

**Problem.** Another engineer/agent can faithfully follow obsolete instructions and undo the simplification. This is a governance and learning problem, not a reason to change runtime behavior back to match old prose.

**Recommendation/design.** Keep `docs/conventions.md` as the canonical project design contract. In a separately scoped implementation task, reconcile applicable workspace guidance with it or make that shared guidance defer to repository-specific conventions; do not overwrite rules for sibling projects. Retain the existing historical banners in reassessment/refactoring plans; add a banner only where missing and link the current standard. Include the no-app/service/repository-tests preference, nested module structure, shared business entities, capability-owned records, direct contracts, central schedules and eager CLI construction.

**Impact/DX.** One authoritative answer to “where does this go?” Scope docs governance and, only with the appropriate workspace scope, the inherited guidance file. No new runtime layer or duplicate architecture manual.

**Trade-off.** The parent file is outside this repository and may govern other projects. This review did not edit it. A repository-specific exception can resolve the conflict without changing all workspace policy.

**Priority/effort/dependencies.** P2/S; T1.1. Runtime fixes do not need to wait on editing the parent file.

**Acceptance.** A contributor can trace each convention to one current source; no applicable instruction requires nonexistent packages or an unapproved help lifecycle. Historical records remain accessible but cannot be mistaken for current implementation instructions.

## Package-by-package harmonization matrix

The complete purpose/API/config/lifecycle inventory is in [01](01-repository-assessment.md). “Keep” means retain implementation/API; documentation improvements still apply. Priorities refer to the specific changes, not intrinsic package quality.

| Package | Classification | Current issue/evidence | Proposed change | Standard pattern | Breaking change | Priority |
| --- | --- | --- | --- | --- | --- | --- |
| `api` | Keep | Strict JSON, native binders and health already distinct | Clarify body-limit scope in request guide; preserve binder escape hatches | Native delivery + bounded defaults | None | P3 |
| `api/middleware` | Refine | F01 raw failure logging | Bounded diagnostic fields | Safe boundary translation/reporting | Log schema changes | P1 |
| `api/response` | Keep | Existing envelopes/options fit native Gin | Preserve; source-backed examples | Explicit response helpers | None | P3 |
| `app` | Refine | F02/F04/F05/F07 | Install guard; central pruning; early backend preflight; move storage selector | Compose/run/own, four files | Unsafe TX calls rejected; pruning ownership; helper removal | P1–P2 |
| `cli` | Keep | Native factory/allowlist/output contracts already bounded | Preserve; explain side effects before output errors/retry | Native Cobra, explicit scheduling | None | P3 |
| `config` | Refine | F05/F07 validation/selection placement | Reuse owner validation; pure named-disk resolver | One env boundary | Earlier validation errors, additive resolver | P2 |
| `dependencies` | Keep | Typed Resources/Targets intentional; F09 docs field wrong | Correct docs, retain field/API | Borrow resources, register targets | None | P2 docs |
| `internal/reference` | Keep | Shared syntax only; no business policy | Keep private, no public Actor API | Private implementation reuse | None | P3 |
| `lifecycle` | Keep | Ready/drain/cleanup behavior has real failure tests | Preserve sequencing; document one-use runtime | Own/join/close | None | P3 |
| `mail` | Refine | F03/F05 attachment/validation policy | Bounded readers, explicit cleanup, owner settings validation | New without I/O; Send/Enqueue distinct | New bound and stronger requested cleanup | P1 |
| `mail/integration` | Keep | Tiny package breaks an actual import cycle | Keep one registrar, no new mail module | Optional installation | None | P3 |
| `media` | Keep | Recovery complexity encodes real crash behavior | Preserve SQL/object protocol and native reader | Direct capability + borrowed clients | None | P3 |
| `media/integration` | Keep | User ownership intentionally adapter-specific | Preserve bounded multipart, commands/task registration | Authorized transports | None | P3 |
| `media/migrations` | Keep | One owned baseline; no demonstrated defect | Preserve versions/schema | Explicit migration registration | None | P3 |
| `messaging` | Refine | F02 absent effect guard | Guard Publisher; retain robust runner startup/drain bridge | Framework publication, native transport | Unsafe TX calls rejected | P1 |
| `messaging/middleware` | Refine | F06 attempt/final confusion | Attempt spans, single terminal log owner | Native middleware, honest outcomes | Log schema/volume changes | P2 |
| `notification` | Refine | F04 housekeeping timer; F08 SQL locality | Retain dispatcher, extract retention; private SQL helpers | Direct capability with explicit guarantees | Pruning no longer implicit in dispatcher | P1/P3 |
| `notification/integration` | Refine | F04 no centrally schedulable pruning handler | Add native pruning handler, reuse CLI cutoff logic | Integration installs; app schedules | Additive task; rollout order matters | P1 |
| `notification/migrations` | Keep | Existing constraints support ledger correctness | Preserve; no “cleaner names” table rewrite | Capability-owned schema | None | P3 |
| `seeders` | Keep | Explicit zero-value registry is simple | Preserve validation-before-run; clarify caller transaction ownership | Register/execute explicitly | None | P3 |
| `testdb` | Keep | Test fixture only, architecture guard enforces it | Keep scope clear; no cosmetic relocation | Disposable services, external integration tests | None | P3 |

## Cross-component integration decisions

- Keep native library power at transport/persistence boundaries; see the dependency table in [01](01-repository-assessment.md) and escape hatches in [06](06-target-architecture.md). JSON event publication, bounded HTTP decoding, mail policy and durable notification intent are worthwhile framework concepts. A duplicate generic router/ORM/job interface is not.
- Keep mailview rendering independent of transport; current module content builders already supply the Laravel-like composition the project needs. A SendView convenience API is not justified by the small number of explicit render-and-send sites.
- Keep Notifier and Media Library as capabilities, not business modules. Their records/contracts belong locally; shared entities hold business identities and polymorphic type constants.
- Keep notification SQL intent separate from ordinary Redis mail/tasks and local events. The common method name `Enqueue` must be documented with its different acceptance boundary, rather than hidden behind an Async interface.
- Preserve Identity's callback for atomic session-bound push subscription. That glue is justified by a cross-package invariant; no generic lifecycle hook system would make it safer.
- Keep observability optional through explicit collaborators. Do not add a common Component wrapper just to place telemetry on everything.

## Recommendations deliberately rejected

No universal service/provider/component interface; no service locator; no generic repository; no auto-discovered modules; no mandatory package-per-interface; no renaming all constructors to one signature; no flattening module transport folders; no resurrected `pkg/mail`; no common recipient/owner class; no generalized durable event outbox without a use case; no chat/mobile/realtime restoration; no cache/storage driver removal merely to shorten go.mod; no broad dependency upgrade as an architecture exercise; no rewriting applied schemas or persisted task names for cosmetic consistency.

The payoff is a more predictable framework with nearly the same architecture and number of concepts. It is not another rewrite.
