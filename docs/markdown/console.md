# Console commands

Cobra commands expose operations to trusted operators. They borrow the same
services as HTTP and task handlers; they do not own infrastructure cleanup.

Commands use a native Cobra group and action: `route list`, `cache tables`,
`migrate up`, `identity show` and `queue list`. Table publication
only writes migration files; `migrate up` applies them separately.

## Use an existing command

After following [installation](index.html), inspect the registered commands and routes:

```sh
export APP_ENV_FILE=.env.quickstart.example
go run ./cmd/cli --help
go run ./cmd/cli route list
go run ./cmd/cli identity --help
```

The quickstart does not create an account. To use account commands, complete
[Identity setup](identity.html#email-otp-configuration-and-security), sign in, and
select the same custom environment file used by that running application. Replace
the placeholder below with the returned user ID:

```sh
go run ./cmd/cli identity show '<user-id>'
go run ./cmd/cli identity welcome '<user-id>'
go run ./cmd/cli notifications list user '<user-id>'
```

`go run ./cmd/cli help`, `--help`, and invocation without arguments list every
available command, including full nested paths such as `identity show`,
`notifications dispatch` and `completion bash`. Module additions appear
automatically. Hidden and deprecated branches stay hidden. Use
`go run ./cmd/cli help identity show` or `identity show --help` for arguments,
examples and flags specific to that command.

`identity show` and `notifications list` read the database immediately. `identity welcome` submits an Asynq task; a running worker must process
it and persist channel intents. The worker dispatcher submits channel delivery work every second;
`notifications dispatch` is also available for manual operation. Listing immediately may
precede either stage. These are operator commands,
not publicly authenticated endpoints. Restrict access to the executable and credentials.

## Command reference

Run each command after `go run ./cmd/cli`. All groups share eager construction;
operator access is trusted and is not scoped by an HTTP bearer token. Placeholders
such as `USER_ID` and `DELIVERY_ID` must be replaced with actual IDs.

| Command | Availability and effect |
| --- | --- |
| `serve` | Run the explicit Processes selection in cmd/cli/main.go |
| `route list` | Read the complete registered HTTP route list without serving |
| `migrate status`, `migrate version` | Read the configured database's Goose history |
| `migrate up`, `migrate down` | Apply pending SQL or roll back the latest migration; `--timeout` overrides MIGRATION_TIMEOUT |
| `migrate refresh --force` | Delete the configured schema/database contents and rerun migrations; development/test only; never a recovery shortcut |
| `seed` | List registered seeders; none are selected implicitly |
| `seed NAME...` | Execute named seeders in order; `--timeout` defaults to 5m |
| `cache tables` | Publish cache migration source; `--dir` defaults to internal/modules/shared/migrations; does not apply SQL |
| `identity show USER_ID` | Read an account through its service |
| `identity welcome USER_ID` | Enqueue the stable welcome task; requires a worker for completion |
| `identity prune-verification` | Delete a bounded batch of expired challenges; available when challenge support is enabled |
| `identity prune-sessions` | Delete bounded expired/revoked session history; available when auth is enabled |
| `notifications list TYPE ID`, `notifications unread TYPE ID` | Read a recipient's stored records or unread count |
| `notifications read TYPE ID NOTIFICATION_ID` | Mark one recipient-owned record read |
| `notifications deliveries` | Inspect ledger metadata; `--status` defaults to failed, empty means all; `--page` defaults to 1 |
| `notifications delivery DELIVERY_ID` | Inspect one delivery's metadata |
| `notifications dispatch` | Submit due persisted delivery IDs to Redis; does not synchronously deliver |
| `notifications retry DELIVERY_ID` | Reset a terminal failed delivery for retry; external effects can repeat |
| `notifications prune-subscriptions` | Disable a bounded batch of inactive push registrations; app schedules pruning when push and its scheduler are enabled |
| `media upload TYPE ID FILE` | Upload up to 20 MiB using disk visibility; optional `--collection` and `--visibility` |
| `media list TYPE ID` | List ready assets; optional `--collection` filter |
| `media delete TYPE ID ASSET_ID` | Record deletion intent and delete the owned object |
| `media recover` | Run a bounded interrupted-upload/deletion recovery batch |
| `queue list`, `queue inspect QUEUE ID` | Read queue names or task state/payload |
| `queue replay QUEUE ID` | Move eligible work to pending without resetting retries; effects can repeat |
| `queue archive QUEUE ID`, `queue delete QUEUE ID` | Archive/delete non-active work; delete is permanent |
| `queue pause QUEUE`, `queue resume QUEUE` | Stop/resume new admission; active work continues |
| `queue cancel ID` | Request cooperative cancellation of active work |
| `completion SHELL` | Generate Cobra completion for bash, zsh, fish or powershell |

Queue commands are always registered. Capability commands may
be present even when an operation's storage or provider is unavailable; execution
reports the missing dependency. Use `COMMAND --help` for current arguments and
flags. Task payload inspection may expose private content.

See [migrations](migrations.md), [failures and retries](failed-jobs.md),
[notifications](../internal/platform/notification/README.md) and
[Media](../internal/platform/media/README.md) before operations that change state.

## Register commands explicitly

Keep native constructors under `internal/modules/<module>/delivery/cli`. Each constructor
receives its service directly, for example `New(users service.UserService)`.
The owning module's Register function adds its group to the supplied Cobra root:

```go
if targets.RootCommand != nil {
    targets.RootCommand.AddCommand(modulecli.New(users))
}
```

App calls the module Register function once with borrowed resources and targets.
There is no separate command list in app. Each command validates input, calls its
service with cmd.Context(), and writes output. App supplies logging/tracing and
owns resource cleanup. Adding a command only changes its owning module.

## Arguments and flags

Cobra validates arguments and binds flags directly. This complete standalone greeting command demonstrates both:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewGreet() *cobra.Command {
	var name string
	command := &cobra.Command{
		Use:   "greet",
		Short: "Print a greeting",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Hello, %s!\n", name)
			return err
		},
	}
	command.Flags().StringVar(&name, "name", "world", "Name to greet")
	return command
}
```

Add `cli.NewGreet()` to your module’s group in its Register function to expose the greeting beneath that group. For positional inputs, select an appropriate Cobra argument validator and validate business values inside the service too.

Only `cmd/cli` parses application commands and flags. Other entry points select their services in code. See [Entry points](entry-points.html).

## Output and errors

Use `cmd.OutOrStdout()` for results, `cmd.ErrOrStderr()` for diagnostics, and
`cmd.InOrStdin()` for input. Return errors from `RunE`; do not call `os.Exit` or
`log.Fatal`. Pass cmd.Context() into operations; it includes app logging and tracing. App cleans up after success or
failure, rather than relying on Cobra post-run hooks.

Commands contain mutable flag state. Return fresh trees and do not execute one
tree concurrently. Constructors and registration describe behavior without I/O.
The app checks names, aliases and ownership before execution.

## Startup and execution

All commands use one startup path: load and validate configuration, open resources,
construct authentication, register modules and validate commands/migrations, then execute Cobra.
Help and completion also require successful application construction. Bad settings,
unavailable configured resources or incompatible SQL sources can prevent execution.
Input validation happens after construction, before business operations.

`serve` starts processes selected in `cmd/cli/main.go`; other commands start no HTTP or
metrics listeners, queue workers, local event consumers, notification dispatcher
or scheduler, even when those processes are selected in main. Migrations use a dedicated database pool
for their lock/session contract and run only when explicitly selected.

The configured resource clients still open normally and close after the command.
Connection maintenance and telemetry export are distinct from processing business
work. Watermill registration and cleanup messages are debug diagnostics; normal
help and completion are quiet at the default info level. Startup, command and
cleanup errors remain visible.

A command must join its work before returning. Use Redis tasks to hand work to
another process. GoChannel publication in a CLI process does not reach another
process's subscribers. See [entry points](entry-points.html) and
[scheduling](scheduling.html) for distinct execution boundaries.

```sh
go run ./examples/cli greet --name Ada
go run ./cmd/cli completion bash
```

The standalone example uses native Cobra through the platform CLI setup. Each app.Execute call owns an independent invocation.

Platform capabilities put native command definitions in their `integration`
packages. Notifications commands accept `RECIPIENT_TYPE RECIPIENT_ID`; media commands
accept `OWNER_TYPE OWNER_ID`. Media upload/list support `--collection`. Both use
already constructed package instances, with no application invocation callbacks.
