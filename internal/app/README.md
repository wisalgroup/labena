# Application composition

App owns one invocation: open configured resources, construct authentication,
register transports, execute, then drain and close. Business Register functions
construct local collaborators and install transports using supplied services.

## Public entry points

The caller loads configuration and chooses execution with explicit Go flags:

```go
err := app.Serve(ctx, cfg, app.Processes{HTTP: true, Workers: true})
```

For a console invocation:

```go
err := app.Execute(ctx, cfg, app.Processes{HTTP: true, Workers: true}, args)
```

These are alternative call fragments. Handle errors in the executable. A zero
selection is rejected by Serve and `cli serve`; ordinary commands allow it.
Help and completion construct the whole application without starting runners.
The application instance is private; callers do not coordinate Run and Close.

HTTP and workers automatically run their local event consumers. Workers also run
notification dispatch. Scheduling is selected separately with `Scheduler: true`;
the supplied `cmd/scheduler` does that. Run one scheduler per schedule set.

## Where to edit

| File | Responsibility |
| --- | --- |
| `app.go` | Public Serve/Execute API and the private application state |
| `open.go` | Ordered initialization, security wiring and failure cleanup |
| `infrastructure.go` | Clients, capabilities, transport registries and platform registrations |
| `auth.go` | Construct authentication and bind notification delivery's active-session check |
| `modules.go` | Explicit business registration calls |
| `processes.go` | Execution dependencies, readiness and ordered drain |
| `schedules.go` | Central recurring timing and feature conditions |
| `console.go` | Application commands and command execution telemetry |

One private `application` holds configuration, resources, targets, native runners
and the process list. App helpers use that same instance. Registries live directly
in targets, and command execution uses `targets.RootCommand`. There are no separate
platform, console or process-plan state objects. Modules never receive the app.

Resources is complete before module construction. Targets is complete before
module registration. Business-to-business dependencies are explicit assembly
arguments. There is no discovery, service locator or generic module registry.
App constructs Identity's AuthService in `auth.go` and supplies the same instance
to HTTP authentication, Identity registration and session-aware push operations.
Disabled authentication fails closed.

## Ownership rules

Record cleanup immediately after resource acquisition. New and Register start no
business work, listeners or SQL migrations. Validate commands, migration sources
and schedules before execution. The existing platform lifecycle supervises startup,
then drains admission, workers, local event consumers and metrics in that order.
Resources close in reverse acquisition order after their users finish. Startup,
run and cleanup errors remain visible; a drain timeout does not close resources
underneath a live handler.

Installed modules require PostgreSQL for serving. No-database assembly supports
inspection but cannot serve. Migrations and seeders remain explicit commands.

## Verification

Tests belong in `tests/app` and `tests/modules`. See
[lifecycle](../../docs/application-lifecycle.md),
[module registration](../../docs/module-registration.md),
[entry points](../../docs/entry-points.md),
[dependencies](../platform/dependencies/README.md) and
[conventions](../../docs/conventions.md).
