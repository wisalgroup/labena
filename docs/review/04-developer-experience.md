# Developer experience and canonical workflows

The comparison is against the current repository, not an imagined older design. Several workflows are already simple and should remain unchanged. The improvements remove contradictory rules and unsafe defaults rather than inventing shorter syntax. Finding IDs refer to [03](03-platform-harmonization.md).

Code marked **current** uses existing APIs. **Proposed** marks a new API/behavior. Fragments identify their surrounding context; they are not advertised as standalone programs. Source-backed complete examples already compile in the repository build.

## The learning path

A developer should first accomplish one persisted operation, then learn boundaries as needed:

1. Start disposable PostgreSQL/Redis with the quickstart profile, migrate explicitly, register/read an account and stop the app.
2. Add the existing one-file catalog module and one app installation block.
3. Add a business operation through an owner service/repository and call it from HTTP and CLI.
4. Add either an ordinary task, a best-effort local event, or SQL notification intent according to its required guarantee.
5. Add mail/media/push only when the workflow needs them; see each independent-use recipe.
6. Learn role selection, central scheduling, failure inspection and graceful shutdown before deployment.

The full feature profile remains available and enabled as approved. A minimal tutorial profile is an explicit teaching tool, not a proposal to change the application's defaults.

## 1. Create and register a module — keep the good path

**Current source:** [catalog/module.go](../../examples/module/catalog/module.go), 26 physical lines including imports/comments. Its `New(Resources)` constructs a value; `Register(Targets)` attaches a native Gin route only when HTTP is provided.

```go
// Current module-root method; imports dependencies, response and Gin.
func (m *Module) Register(targets dependencies.Targets) error {
    if targets.HTTP == nil {
        return nil
    }
    targets.HTTP.GET("/catalog/hello/:name", func(c *gin.Context) {
        response.OK(c, gin.H{"message": "Hello, " + c.Param("name")})
    })
    return nil
}
```

**Current and target installation fragment**, inside `application.registerModules`, after importing the module:

```go
if err := catalog.New(a.resources).Register(a.targets); err != nil {
    return err
}
```

One app location installs the module. Its own module root registers routes, commands, migrations, events and jobs as they are added. Recurring timing adds one deliberate central-schedule edit. Do not add empty service/repository folders for this hello route.

**Before → after:** same construction and registration APIs, same two bundles, same installation point. Fix the guide's `Resources.Notifications` typo to `Resources.Notifier`; source-backed snippets remove a broken-copy/paste hazard (F09). No generic Module registry or callback-based command invocation is introduced.

## 2. Configure and initialize capabilities

**Current application path, retained:** load the environment once, pass typed config to app.

```go
cfg, err := config.Load()
if err != nil {
    return err
}
return app.Serve(ctx, cfg, app.Roles{})
```

This function-body fragment is the core of [cmd/app/main.go](../../cmd/app/main.go). `ctx` is owned by the signal-aware entrypoint. `FromEnvironment` supplies an isolated map for tools/tests; capability constructors do not read environment variables.

**Before (F05):** a Redis namespace can pass `config.FromEnvironment` and fail when the driver is constructed. **Target:** the same public config call rejects it immediately, using owner validation. No new configuration builder or application-facing options layer is required.

**Current named-storage path:**

```go
connection, err := app.OpenFilesystemConnection(ctx, cfg, "local")
```

**Proposed named-storage path (F07):**

```go
options, err := cfg.Filesystem.Options("local")
if err != nil {
    return err
}
connection, err := storage.Open(ctx, options)
if err != nil {
    return err
}
defer connection.Close() // In a returning function, preserve a material close error.
```

These fragments assume validated `cfg config.Config` and an operation context. The target adds one explicit conversion call but removes the app import and installed-module dependency graph from the standalone example. App itself registers `connection.Close` with lifecycle rather than deferring it during construction. `connection.Bucket` retains native Go CDK operations and options.

## 3. Define and consume services/repositories

**Current and target:** reuse owner interfaces. Assembly in `identity/module.go` already has this shape:

```go
repo := repository.NewUserRepository(deps.DB)
users, err := service.NewUserService(repo, service.UserOptions{Events: deps.Events})
if err != nil {
    return nil, err
}
```

This is an assembly fragment returning `(*Module,error)`, not service code. Service code accepts the narrower repository and publisher; it does not import Resources.

| Need | Current and target location |
| --- | --- |
| `UserService` interface | `identity/service/service.go` |
| `userService`, `NewUserService` | `identity/service/user.go` |
| Another substantial user use case | A topic file in the same service package, with a method on `*userService` |
| `UserRepository` interface | `identity/repository/repository.go` |
| Private SQL implementation and constructor | `identity/repository/user.go` |
| Shared business entity | `internal/shared/entities`, with owner module controlling writes |
| External consumer needing user operations | Accept `service.UserService`; do not redefine it in shared/contracts |

**Before → after:** no new abstractions or mandatory files. Replace conflicting documentation that encourages another transaction interface with the actual `*database.TxManager` pattern. A local consumer interface is reserved for a real narrower boundary, such as a transport fake, not required everywhere.

## 4. HTTP routes, handlers and middleware

**Current and target:** native Gin registration; framework decoding/response policy. The current request guide's `EchoName` is a complete example; real Identity handlers then call their services.

```go
// Handler-body fragment; request is a typed request struct with binding tags.
if err := api.DecodeJSON(c, &request); err != nil {
    response.Error(c, err)
    return
}
result, err := users.UpdateProfile(c.Request.Context(), userID, input)
if err != nil {
    response.Error(c, err)
    return
}
response.OK(c, result)
```

For a real endpoint, derive/authorize `userID`, map validated `request` to `input`, and map the entity to the existing response DTO instead of returning internal persistence fields. See [user_handler.go](../../internal/identity/delivery/http/user_handler.go). Those are business/transport obligations, not constructor boilerplate.

Module registration uses `targets.AuthenticatedHTTP` for protected routes; the app installs Bearer middleware once. Authorization is still operation-specific. Non-JSON binders need explicit body limits; native `DecodeWith(..., binding.JSON)` intentionally bypasses strict JSON policy.

**Before → after (F01):** same handler, same response, but raw downstream failure text no longer enters ordinary HTTP logs. The developer need not special-case errors for HTTP versus queued handlers.

## 5. Transactions and durable notification intent

**Current complete example, retained:** [profile/update.go](../../examples/module/profile/update.go) updates a profile and saves its notification intent in one transaction. Its essential operation is:

```go
return transactions.Execute(ctx, func(ctx context.Context) error {
    user, err := users.UpdateProfile(ctx, userID,
        service.UpdateProfileInput{Name: &name})
    if err != nil {
        return err
    }
    _, err = notifier.Enqueue(ctx, notification.Recipient{
        Type: string(entities.UserEntityType), ID: user.ID.String(),
    }, notification.Message{
        Type: "identity.profile_updated",
        Database: json.RawMessage(`{"title":"Profile updated"}`),
    })
    return err
})
```

This function-body excerpt requires `*database.TxManager`, `service.UserService`, `*notification.Notifier`, authorized user ID and the same DB pool. Imports include `context`, `encoding/json`, Identity service, shared entities and notification. The repository resolves `database.Executor(ctx, db)` rather than using its pool directly.

An omitted notification key creates a new occurrence. For retried business requests use an occurrence identity that survives the retry; adding a stable key is a business decision, not a global rule to use user ID for every notification.

**Before:** a caller can also publish an event/enqueue a raw task inside this callback, despite documentation warning against it. **Proposed after (F02):** those wrapper calls return `database.ErrTransactionActive` before invoking the transport. The example above remains valid. Post-commit local publication is still best-effort; choose it explicitly rather than assuming the guard supplies durability.

## 6. Publish and consume events

**Current and target publication fragment**, after the business commit, using Identity's real event builder and owner publisher contract:

```go
event := events.NewUserRegistered(userID)
return publisher.PublishWithID(ctx, event.Type, event.ID.String(), event)
```

Here `publisher` is `messaging.Publisher`; `events` imports `internal/identity/events`. The actual Identity helper treats publication as a best-effort reaction after commit rather than returning an error that implies account creation failed. Preserve that result semantics.

**Current and target module registration**, adapted directly from Identity:

```go
targets.EventRoutes.AddConsumerHandler(
    "identity.welcome",
    events.UserRegisteredType,
    targets.EventSubscriber,
    eventhandlers.UserRegistered{Queue: m.queue}.Handle,
)
```

Preconditions are the module's existing target/dependency checks. The handler decodes `UserRegistered`, builds a real welcome task and enqueues it with the message context. Native Watermill handler semantics stay visible at this boundary.

**Before → after:** no event framework rewrite. F02 rejects transaction context misuse; F06 removes a misleading retry-exhaustion warning. Module developers still learn that GoChannel is in-process and loss is possible before Redis acceptance. Adding RabbitMQ/NATS containers does not change the installed transport automatically.

## 7. Define, dispatch, handle and schedule jobs

**Current and target task dispatch** uses the real [jobs/welcome.go](../../internal/identity/jobs/welcome.go):

```go
task, err := jobs.NewWelcome(userID)
if err != nil {
    return err
}
_, err = queue.EnqueueContext(ctx, task)
return err
```

The constructor returns a native task containing the UUID only, defaulting to five retries and a 30-second attempt. Current job registration stays in `identity.Module.Register`:

```go
targets.TaskRoutes.HandleFunc(jobs.WelcomeType, m.welcome.Handle)
```

`delivery/jobs.Welcome.Handle` reloads account state, skips ineligible accounts, chooses allowed channels and persists intent. Completion means intent saved, not provider delivery. This remains easier to reason about than a generic Job interface with hidden serialization.

**Current scheduling:** media recovery and Identity cleanup are in `app.registerSchedules`; subscription retention is hidden in a dispatcher timer. **Proposed scheduling (F04):** one more entry in the same central list, with an additive task installed by notification integration. A proposed fragment inside a push-enabled branch:

```go
tasks = append(tasks, asynqx.TaskSchedule{
    Spec: "@every 1h",
    Task: func() (*asynq.Task, error) {
        return asynqx.NewTask(notification.PruneSubscriptionsTask, nil), nil
    },
    Options: []asynq.Option{
        asynq.Unique(time.Hour), asynq.Timeout(time.Minute),
    },
})
```

`PruneSubscriptionsTask` is proposed, not current. The handler computes its cutoff when executed; do not serialize `time.Now()` from a task factory that only runs during registration. One scheduler owns cadence, many workers may process tasks, and native uniqueness is not a leader-election guarantee.

## 8. Notifications, push and realtime boundaries

**Current and target content composition:**

```go
// Function-body fragment: recipient is authorized, occurrenceID is retry-stable.
receipt, err := notifier.Enqueue(ctx, recipient, notification.Message{
    Key: "report:" + occurrenceID,
    Type: "reports.ready",
    Database: json.RawMessage(`{"title":"Your report is ready"}`),
    Email: &mailview.Email{
        Subject: "Your report is ready",
        Title: "Report ready",
        Paragraphs: []string{"Open your account to download it."},
    },
    Push: &push.Message{Title: "Report ready", Body: "Open your account."},
})
```

Imports are notification, encoding/json, mailview and push. The caller checks `err` before using `receipt`. Email requires an explicit destination and configured mail/views; push requires current registered installations and provider/project configuration; database delivery requires PostgreSQL. Nil content omits a channel—no second channel list or central type registration.

Use shared entity type constants when constructing the recipient, e.g. `string(entities.UserEntityType)`, not an Identity domain string constant. Other model types add their stable shared entity type and an authorized owning adapter; the core is already polymorphic.

**Before → after:** same ergonomic operation and ledger semantics. Background retention becomes explicit in central schedules; private persistence details become more local. Expo ticket acceptance and receipt success do not establish display on the device.

**Realtime is intentionally absent.** The existing notification HTTP endpoints can be polled; Expo is push delivery. Neither is a browser/mobile broadcast hub. The current WebSocket guide describes native upgrade/drain hooks only. There is no honest “before/after realtime API” to demonstrate without reintroducing a feature the user removed. A future broadcast channel requires a separate product/transport design, not this harmonization phase.

## 9. Extend or replace a capability

**Current and target mail extension:** implement the owner contract, then reuse optional queueing.

```go
// Assembly fragment. customSender implements mail.Sender; queue may be nil.
mailer, err := mail.New(customSender, queue)
if err != nil {
    return err
}
handler, err := mail.NewHandler(mailer)
if err != nil {
    return err
}
mux.Handle(mail.SendTask, handler)
```

The caller owns the sender/queue/mux and worker lifecycle. In the normal app this is installed once by mail integration; modules should simply borrow `deps.Mailer`. No new per-module publisher/sender contract or dependency container is needed.

Advanced callers retain native Asynq retry/routing options, Go CDK reader/writer options, Bun query/transaction capabilities, Watermill middleware and Cobra commands. The framework layer adds relevant guarantees rather than imitating all those APIs.

**SMTP before → proposed after:** same `Send` call, bounded attachments and consistent explicit cleanup (F03). Choose direct rendering plus sending for ordinary mail; use Notifier when recipient identity, read state or SQL intent is the actual requirement.

## 10. Bootstrap, run and stop

**Current and target complete entrypoint behavior:** signal context → config load → `app.Serve` → error to stderr/exit status. Use the existing [entrypoint source](../../cmd/app/main.go) rather than copying a new runtime DSL into every application.

`app.Roles{}` selects configured roles; explicit roles express special deployments. Local subscribers start before producers. Constructors do not start workers. The runtime owns readiness, admission stop, ordered drain and cleanup; modules do not call `Close` on borrowed DB/queue/buckets.

**Before → after:** retain proven lifecycle. Preflight selected settings earlier (F05) and document what readiness actually measures. CLI help still builds the configured application, as requested. No offline metadata registry is reintroduced.

## Measured setup and decision costs

These are counts from code, not claimed user research results.

| Workflow | Current | Target and success criterion |
| --- | --- | --- |
| Basic catalog module | 26 source lines, one module-root file, one app installation location | Same architecture and registration count; no framework manifest |
| App implementation | Four production Go files | Keep four; no test files added there |
| Module dependency concepts | Resources and Targets at assembly; narrow operation dependencies | Same two bundles; zero business-service imports of Resources |
| Recurring maintenance locations | Central app list plus dispatcher pruning timer | One central list for maintenance cadence; delivery polling explicitly excluded |
| Named storage example | Imports app to open one connection | Zero app imports; one pure selection and one native acquisition call |
| Database+email+push notification | One Message with nonnil channel fields | Same content selection; no repeated enum list/type registry |
| TX side-effect policy | Three capability families enforce it; general publisher/queue do not | Publisher and app queue enforce the same existing guard; native bypass documented |
| Published docs | 51 pages; selected snippets compiled, one confirmed stale fragment | Preserve page URLs; all canonical complete workflows source-backed; fragment checks expanded |

Time-to-first-module and beginner task-completion rates were not measured. Phase 5 specifies a reproducible onboarding exercise before making claims that the learning curve improved quantitatively.
