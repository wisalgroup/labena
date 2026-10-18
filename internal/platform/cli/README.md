# Native Cobra console

This package builds and validates Cobra command trees. Commands receive already
constructed services; app owns configuration and resource lifetime. The
[console guide](../../../docs/console.md) covers application commands, arguments,
streams, errors and administration.

## Try an independent command

```sh
go run ./examples/cli greet --name Ada
```

Expect `Hello, Ada!`. This example builds a native tree through `cli.New` without
constructing the full app or opening PostgreSQL/Redis.

For an application command, select your configured profile:

```sh
APP_ENV_FILE=.env.quickstart.example go run ./cmd/cli help
```

App construction is intentional even for help and completion. The root shows full
nested paths; `help identity show` supplies detailed per-command help.

## Register a module group

Keep command constructors in `internal/modules/<module>/delivery/cli`, accepting the owner
contract, such as `service.UserService`. In the module's `Register` function:

```go
if targets.RootCommand != nil {
    targets.RootCommand.AddCommand(modulecli.New(users))
}
```

This **Identity registration fragment** borrows the constructed `users` service.
Adding a command requires no separate app catalog. Use native Cobra flags, aliases
and argument validators; services still validate business values.

## Own one invocation

| Rule | Reason |
| --- | --- |
| Use `cmd.Context()` | Preserve cancellation, logging and tracing |
| Use command input/output streams | Support redirection and test capture |
| Return errors from `RunE` | Let the entry point report once after cleanup |
| Join work before returning | App may close borrowed clients afterward |
| Build fresh trees | Cobra flags/state are mutable and not concurrency-safe |

`cli.New` validates its initial commands; `cli.Validate` checks the finished tree
after module additions. They reject duplicate names/aliases, reused command
pointers and invalid ownership. Automatic Cobra error/usage printing is disabled;
the executable reports returned errors once.

`app.Execute` closes resources after success, error or panic. Ordinary commands do
not start HTTP/metrics listeners, task workers, local event consumers or schedules.
Resource clients can still maintain connections and export telemetry. Only `serve`
starts service runners.

## Scheduled commands and queue administration

Scheduled commands use fresh factories registered in `ScheduledCommands`; workers
never execute the interactive tree. Central app schedules choose the timing. See
[scheduling](../../../docs/scheduling.md).

`NewQueueCommand` borrows an Asynq Inspector. Queue CLI administration is always
available in the application; the optional web dashboard is a separate setting.
Native inspector operations use client timeouts and do not take a context.

GoChannel events published by an ordinary CLI command cannot reach another
process. Enqueue a durable task for work that must survive command exit; queue
acceptance does not establish business completion.

Focused Cobra tests live here. App execution tests live in `tests/app`; business
command behavior belongs in the external module suites.
