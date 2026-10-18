# Module registration

Each business module exposes a package-level `Register` function. It constructs
its local collaborators and installs transports once during startup. Services
needed before registration are constructed by app and passed explicitly. Follow
[your first module](modules.html) for the smallest example.

## Read the assembly sequence

```text
App opens resources
  → constructs Mail, Notifier and Media
  → constructs the authentication service
  → builds authenticated HTTP targets
  → registers business modules and capability transports
  → validates commands and migration sources
  → installs central schedules
  → starts selected processes
```

App selects the order because dependencies are explicit. There is no discovery,
reflection, provider catalog or Module struct to implement.

## Application composition

`internal/app/modules.go` lists business registrations on the private application
and passes their explicit collaborators. This is the complete compiled file:

```go
package app

import (
	"fmt"

	"github.com/wisalgroup/labena/internal/modules/identity"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
)

// registerModules lists business registrations in dependency order.
// App translates settings and passes business collaborators explicitly.
func (a *application) registerModules(authentication service.AuthService) error {
	if err := identity.Register(a.resources, a.targets, authentication, a.cfg.Auth.GoogleEnabled); err != nil {
		return fmt.Errorf("register identity: %w", err)
	}
	return nil
}
```

`infrastructure.go` opens clients and constructs Mail, Notifier and Media. `auth.go`
constructs one Identity AuthService from `cfg.Auth`, using existing repositories,
provider integrations, AuthOptions and domain policies. `open.go` passes that
service to the API's existing Authenticator interface and to Identity's Register.

The application stores clients in `a.resources` and registries in `a.targets`.
Infrastructure setup creates the native job and event registries there, and
workers consume those same instances. Console execution uses `a.targets.RootCommand`.
Modules borrow these two bundles; they never receive the application itself.

Identity registers user media, notification and push-subscription HTTP routes
using the available Media and Notifier resources. Its push handler calls the
supplied AuthService's SubscribePush operation directly, preserving the session
transaction and lock. Disabled authentication supplies a nil authenticator, so
protected routes fail closed; a missing AuthService also rejects subscription
registration. There is no HTTP fallback to standalone notifier registration.
App still supplies notification delivery's active-session check.

Platform registration installs generic media content serving, commands, tasks and
migrations. It does not register user-specific HTTP routes or import business
definitions. The principal contract lives in `internal/platform/auth`.

Environment fields, decoding and configuration validation live in `app/config`.
There is no duplicate Identity Config or `cfg.Identity()` conversion. The domain
owns session policies, and services validate their collaborators. Business modules
never import app configuration; app passes values or constructed services.

## Business modules

Identity accepts authentication and the Google route selection explicitly:

```go
func Register(resources dependencies.Resources, targets dependencies.Targets,
    authentication service.AuthService, googleEnabled bool) error
```

This is the function signature; the implementation is in
`internal/modules/identity/register.go`. It constructs the account collaborators
as local variables:

```go
users, err := service.NewUserService(repository.NewUserRepository(resources.DB))
if err != nil {
    return err
}
```

This **function-body fragment** uses Identity's repository/service imports. The
repository borrows the app database; the service borrows the repository. Neither
owns the pool's lifetime. Registered handlers retain the same service instance;
a Module struct is unnecessary.

Attach available transports with those local variables:

```go
if targets.RootCommand != nil {
    targets.RootCommand.AddCommand(modulecli.New(users))
}
if targets.Migrations != nil {
    if err := targets.Migrations.Register("internal/modules/identity/migrations", migrations.Files); err != nil {
        return err
    }
}
```

This is an **illustrative registration excerpt**. Identity's full function also
registers welcome handlers, authentication routes and maintenance commands.

## Choose the correct target

| You are registering… | Target | Native action |
| --- | --- | --- |
| Public HTTP operation | `HTTP` | `huma.Register` |
| Session-protected operation | `AuthenticatedHTTP` | `huma.Register` |
| CLI group | `RootCommand` | `AddCommand` |
| Task handler | `TaskRouter` | Asynq `Handle` or `HandleFunc` |
| Event handler | `EventRouter` and `EventSubscriber` | Watermill `AddConsumerHandler` |
| Embedded SQL | `Migrations` | `Register(source, files)` |
| Named seed operation | `Seeders` | `Register` |
| Schedulable CLI action | `ScheduledCommands` | Register a fresh command factory |

Targets are borrowed startup registries, not dependencies looked up by services.
For clients and collaborators, use [Resources](dependency-injection.html).

## Required and optional targets

A nil target means **omit that transport**. It does not mean disable validation for
an enabled transport. For example, a registered event consumer needs both its
subscriber and the queue client used by its handler.

1. Validate required collaborators before mutating any registry.
2. Register sequentially, before runners start.
3. Return the original error with useful context.
4. Discard the partial application if registration fails.

Do not retry `Register` against partly mutated targets. Native routers can reject
duplicate handlers or conflicting paths, and registration is not transactional.

## Commands and migrations

Register commands with already constructed services. Use `cmd.Context()` during
execution; do not construct a second app from a command callback. Help intentionally
constructs the same app. See [console commands](console.html).

Register embedded SQL; execute it later through `migrate up`. Registration never
runs DDL. Module and platform SQL share one Goose history, so versions must be
globally unique. See [migrations](migrations.html).

## Keep recurring timing centralized

A module owns task handlers and command factories. App owns recurring timing,
feature conditions and enqueue options in `internal/app/schedules.go`.
Schedules are installed after registration so their destinations exist.

Adding a task does not schedule it. Adding a schedule does not start its scheduler;
that process is selected separately. See [scheduling](scheduling.html).

## Where a new feature changes code

| Change | Required assembly edits |
| --- | --- |
| Add an endpoint to an existing module | Its HTTP registration and handler |
| Add a command or job | Its delivery package and module `Register` |
| Add a business module | Its `Register` function, plus one app composition entry |
| Add recurring maintenance | Register its handler locally; add timing centrally |
| Add infrastructure | Typed config, app acquisition/cleanup, explicit consumer wiring |

Registration starts no network work, listeners or business operations. App keeps
resources alive until consumers drain. Continue with
[dependency injection](dependency-injection.html) and
[application lifecycle](application-lifecycle.html).
