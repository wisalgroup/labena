# Resources and registration targets

This neutral package defines the two structs shared by app and module assembly.
It imports neither app nor business module assembly and performs no resource
construction or runtime lookup. The full guide is
[dependency injection](../../../docs/dependency-injection.md).

## Resources: clients already constructed

`Resources` contains app-owned database/transaction clients, cache, rate limits,
mail, disks, queue/event publishers, telemetry and platform capabilities. A module's
Register function selects the fields its repositories and services need:

```go
repo := repository.NewUserRepository(resources.DB)
users, err := service.NewUserService(repo)
if err != nil {
    return err
}
```

This **Identity registration fragment** assumes its repository/service imports and a
`dependencies.Resources` argument. Services keep narrow arguments, not the bundle.

Copies share the same clients. Modules must not close them, mutate disk maps or
replace entries. DB and Transactions belong to the same pool. Disabled optional
capabilities may be nil; constructors validate what their enabled operations need.
Business services from other modules remain explicit arguments, outside Resources.

## Targets: places to register behavior

| Field | Registers |
| --- | --- |
| `HTTP`, `AuthenticatedHTTP` | Typed operations directly through `huma.Register` |
| `RootCommand` | Fresh native Cobra groups |
| `TaskRouter` | Native Asynq handlers |
| `EventRouter`, `EventSubscriber` | Watermill consumers |
| `Migrations` | Embedded SQL sources in one Goose history |
| `Seeders` | Explicit named seed operations |
| `ScheduledCommands` | Fresh command factories approved for worker execution |

Nil targets omit unused transports. An event target needs its subscriber. Validate
required collaborators before mutation, register sequentially, and discard partial
construction on error. Do not retry registration against mutated targets.

App creates `AuthenticatedHTTP` using Identity's authenticator before registration;
disabled authentication fails closed. App validates commands and SQL history before
execution. Registration never runs handlers, seeders or migrations.

## Keep the distinction visible

App stores both bundles on its private application. It creates native registries
directly in Targets and gives them to runners and module registration; there is
no second app-owned registry bundle. Modules never receive the application or its
lifecycle owner.

| Task | Use |
| --- | --- |
| Query a database or send mail | A specific borrowed Resource |
| Add a command or route | A Target during startup |
| Decide when recurring maintenance runs | Central schedules in `app/schedules.go` |
| Reuse business behavior | The owning module's explicit service contract |

`Resources.Mailer` provides immediate `Send` and optional `Enqueue`, while also
satisfying the narrow `mail.Sender` contract. `Disks` contains only opened buckets;
`DefaultDisk` selects the default. Owners close their own independently opened
clients; app closes the clients in Resources after consumers finish.

Continue with [module registration](../../../docs/module-registration.md) and
[application lifecycle](../../../docs/application-lifecycle.md).
