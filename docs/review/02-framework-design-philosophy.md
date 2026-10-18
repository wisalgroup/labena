# Framework design philosophy

Status: proposed design contract, grounded in [01](01-repository-assessment.md). It refines the existing [project conventions](../conventions.md); it does not create a competing architecture standard. Promote approved rules into that document during Phase 1.

## One philosophy, a few patterns

**Construct explicitly. Register locally. Run centrally. Call capabilities directly. Preserve their guarantees.**

The framework should make a developer's next action predictable without pretending HTTP, SQL, tasks, events and notifications have identical semantics. Its value is safe defaults, useful composition and clear ownership—not shielding every native library type.

Five concepts should cover the normal learning path:

1. **Configuration:** typed settings selected once at the process boundary.
2. **Resources:** constructed, borrowed collaborators available to a module constructor.
3. **Registration targets:** startup-only native routers and registries.
4. **Operations:** direct methods taking context and ordinary Go values.
5. **Runtime ownership:** the application starts runners and closes owned resources after their users finish.

Transactions and asynchronous delivery are additional concepts only when an operation needs them. They must be taught honestly; an interface cannot remove their consistency boundaries.

## Canonical rules and deliberate differences

| Concern | Preferred rule | Justified exception / limit |
| --- | --- | --- |
| Package names | Short capability nouns; retain `mail`, `media`, `notification`, `messaging`, `config`, `lifecycle` | Do not rename everything to `service`, `manager` or `provider`. `api/response` and middleware are useful subpackages |
| Public types | Name the actual role: `Notifier`, `Library`, `Sender`, `Registry`, `Message`, `Recipient`, `Owner` | Similar fields do not justify a shared Actor/EntityReference abstraction; recipient routing and asset ownership have different policies |
| Interfaces | Reuse owner contracts that have multiple consumers or implementations | Keep native concrete Bun/Gin/Asynq/Watermill types where their capabilities matter. A narrow private consumer interface is appropriate for an isolated handler; duplicating `messaging.Publisher` in each module is not |
| Module services | `service.UserService` interface, private `userService`; constructor beside implementation | No `IUserService`, `UserServiceImpl`, exported Default service, or generic base class |
| Module repositories | Owner interfaces in `repository/repository.go`; private implementations and constructors in topic files | No `repository/postgres` layer until a real second implementation requires one. Do not turn one repository into a file-per-method convention |
| Large use cases | Add a cohesive topic file with methods on the existing receiver | Split into another service only for independent responsibility/dependencies, not to shorten a file mechanically |
| Constructors | `New` constructs without network I/O or goroutines; descriptive `NewSMTP`, `NewHandler` where multiple roles coexist | Return errors when construction can reject inputs. A trivial constructor need not return an always-nil error. `Registry` may have a useful zero value |
| Resource acquisition | `Open(ctx, cfg)` acquires external resources; state cleanup ownership | Native clients can remain lazily connected constructors; document that distinction rather than falsely promising startup reachability |
| Options/config | `Config` for owned settings; `Options` for collaborators/behavior; value settings and named fields | Do not change every existing signature to `New(Options)`. `media.New(db,disks,defaultDisk)` already has three clear arguments |
| Registration | A module owns `New` and `Register(Targets)`; capability `integration.Register` installs optional transports | Register before runners start. Failure discards startup state; do not promise arbitrary live rollback/re-registration |
| Schedules | App owns recurring cadence and enablement in `app/modules.go` | Dispatcher polling, lease expiry and retry timing are protocol progress, not business cron. Subscription retention pruning is maintenance and belongs in the central list |
| Context | First parameter for I/O/operations; propagate callback context and cancellation | Constructors/registry mutation with no I/O need no context. Native context-less inspector calls retain bounded connection timeouts |
| Resource lifetime | Application owns acquired clients; services/capabilities borrow them | A returned object reader transfers close responsibility to the caller. Explicit attachment close policy must state when ownership transfers |
| Errors | Preserve causes for `errors.Is/As`; translate at delivery; use typed public errors for expected failures | Do not wrap every internal error in a new framework error. Public HTTP errors and internal diagnostic categories serve different purposes |
| Telemetry | Context logger, injected tracer/meter, stable bounded labels, one terminal log owner | Keep attempt spans and final outcome logs separate. Request IDs, recipients, object keys and raw payloads are not metric labels |
| Tests | Behavior and failure contracts at appropriate boundaries | Respect external `tests/app`, `tests/identity` and module tests; do not add test files to app or business service/repository packages |

## Configuration contract

Environment parsing belongs to `internal/platform/config` only. Packages accept typed inputs, never read `.env` or mutate process environment. A setting has one documented owner and one defaulting path.

Component-owned configuration should expose pure validation/default resolution where app and standalone construction need identical rules. The app schema converts its env values into those owner settings. It additionally validates cross-component relationships such as DB/transaction pairing, Redis isolation, enabled authentication dependencies and shutdown budgets.

Do not introduce a generic Configurable interface, reflection registry or global validator. In F05, share only real duplicated rules: SMTP settings and cache-driver configuration. Keep the app's installed PostgreSQL requirement separate from reusable database drivers that support MySQL.

A constructor may supply documented defaults for zero values; a pure `Config.Validate` must describe whether zero means default or invalid. Shared normalization must not mutate caller-owned maps. `FromEnvironment` should reject deterministically invalid selected settings before resource acquisition. Disabled capabilities need not require unused secrets.

Code defaults, quickstart overrides, the full example and Kubernetes profile must be shown separately. Preserve current enablement and private credential handling. Do not turn operational tuning constants into dozens of mandatory environment entries. New SMTP attachment budgeting is one optional typed setting; no new required env key is needed.

## Construction, integration and extension

`dependencies.Resources` is a **constructor bundle**, not a service locator. Module construction extracts narrow collaborators and builds its services once. No service stores Resources or reaches back into app. Cross-module business services remain explicit arguments owned by their defining package.

`dependencies.Targets` is a **startup registration bundle**. A nil target omits a transport. Dependencies required by an enabled operation are validated; nil never silently turns delivery into success or grants access.

A capability core remains usable without `app`, Identity, HTTP, CLI or migration execution. Its sibling `integration` package is allowed to know native registration targets. This dependency separation justifies even mail's one-file integration package; merging it into the core would introduce a cycle through Resources.

A module is installed once in `app/modules.go`; its routes, commands, event handlers, job handlers and migrations are registered in its own `module.go`. The application-wide schedule is the explicit exception. Adding a module must not require edits to app resource construction, per-transport global lists and a second module manifest.

Use the smallest existing extension point:

- Replace SMTP with a `mail.Sender`; keep `mail.New(sender, queue)` and the same handler.
- Replace a cache implementation through `cache.Store` and preserve its TTL, miss and size behavior.
- Pass native buckets to `media.New`; keep media's metadata/recovery rules.
- Pass a native Watermill transport to `messaging.NewPublisher` and a matching subscriber/router; explicitly assess transport durability.
- Use native Asynq options, middleware and inspector rather than a second framework task object.
- Use a module's social-provider interface for verification; keep provider SDK types in `integrations`.

No runtime plugin registry, DI container, facade singleton, generic repository, universal driver lifecycle or reflection-based module discovery is proposed.

## Transaction and delivery contract

| Operation | Commit/acceptance boundary | Required behavior |
| --- | --- | --- |
| Repository mutation using `database.Executor` | Caller or owned SQL transaction | Same pool and transaction-bearing context; nested `Execute` joins, errors mark rollback-only |
| Database-only `Notifier.Send` | SQL operation | May join SQL; read-state record is not external delivery |
| `Notifier.Enqueue` | Durable SQL intent | May join business transaction; receipt is provisional until commit |
| `Mailer.Send`, `Mailer.Enqueue`, external Notifier delivery, media upload/delete/recover | External effects or separate storage | Reject enclosing framework SQL transaction; no silent fallback |
| Framework `messaging.Publisher`, app-injected queue | Local transport / Redis acceptance | Proposed F02: reject enclosing framework SQL transaction before serialization or transport; propagate canceled context |
| Raw native clients | Underlying library semantics | Framework cannot enforce a guarantee when callers bypass its adapter or discard the transaction context |

A post-commit event remains best-effort. A post-commit Redis enqueue still has a crash gap. A notification ledger guarantees persisted intent, not exactly-once SMTP or device display. Do not market a transaction guard as a generic outbox.

Borrowed transaction contexts must not escape into goroutines or messages. Context is for cancellation, correlation and the scoped database executor; it is not an untyped business service container.

## Security and observability contract

Return safe HTTP Problem values; retain original errors internally for control flow. Routine logs/spans record approved categories, stable route/task/topic names, status and correlation identifiers. Do not attach arbitrary errors containing SQL/provider text, request bodies, credentials or notification payloads. A diagnostic escape hatch, if ever needed, must be deliberate and access-controlled; this blueprint does not add a global “log secrets” switch.

Authorization belongs to the business/transport boundary. `Recipient.Valid` and `Owner.Valid` check reference syntax only. Default media/notification routes are user-scoped; another model type needs its own authorized adapter. Identity's push-subscription callback coordinates with logout in the same DB transaction and lock order; simplifying it to an unchecked direct Notifier call would be a regression.

Lifecycle readiness, dependency readiness and delivery completion are separate observations. Cancellation is cooperative. Retry budgets, external acceptance uncertainty and bounded queues must be documented at each effect boundary.

## Documentation and compatibility contract

Each substantial capability has one authoritative usage guide and source-level contracts for defaults, ownership, concurrency, cancellation and errors. Small packages link to their owner's guide. Examples use current exported APIs and explicitly mark fragments or future proposals.

Before v1, migrate repository callers directly when an exported helper is moved; do not accumulate compatibility aliases without supported consumers. Persisted task names, payload versions, migration versions, event names and stored reference/disk names are operational contracts even before v1. Avoid changing them merely for singular/plural symmetry.

The proposed new pruning task is additive. Existing `mail.send.v1`, `media.recover.v1`, notification dispatch/delivery tasks and `app.command` keep their formats. No migration rewrite is justified by this audit.

The no-extra-layer test is: **does this abstraction remove a repeated decision, enforce a real guarantee, or isolate a replaceable effect?** If none applies, keep the direct call. [06](06-target-architecture.md) applies these rules to the actual tree.
