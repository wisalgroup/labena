# Entry points

Application executables load the same typed settings and use one application assembly
path. Each main chooses its processes explicitly in Go.

The separate `cmd/labena` installer creates new applications and is not part of
their runtime. See [creating an application](creating-applications.html).

## Select a process

| Entry point | Runs |
| --- | --- |
| `go run ./cmd/app` | HTTP, queue workers, notification dispatch and local event consumers |
| `go run ./cmd/worker` | Queue workers, notification dispatch and local event consumers |
| `go run ./cmd/scheduler` | Central recurring schedules; workers may run elsewhere |
| `go run ./cmd/cli <command>` | One command, then cleanup |
| `go run ./cmd/cli serve` | HTTP and workers, as selected in `cmd/cli/main.go` |

```sh
APP_ENV_FILE=.env.quickstart.example go run ./cmd/app
```

For recurring maintenance, run a separate terminal with the same settings:

```sh
APP_ENV_FILE=.env.quickstart.example go run ./cmd/scheduler
```

Run exactly one scheduler per schedule set. Asynq scheduling has no distributed
leader election. Ordinary app and worker replicas do not start schedules.

## Define a server entry point

This is the complete `cmd/app/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wisalgroup/labena/internal/app"
	"github.com/wisalgroup/labena/internal/app/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return app.Serve(ctx, cfg, app.Processes{HTTP: true, Workers: true})
}
```

The executable owns configuration loading, signal handling and exit status.
Serve validates, assembles, runs and cleans up before returning. To make this
executable HTTP-only, change its explicit selection to:

```go
return app.Serve(ctx, cfg, app.Processes{HTTP: true})
```

Then run `cmd/worker` separately against the same queue Redis. HTTP and workers
include local event consumers automatically. GoChannel does not share events
between processes; each producing process keeps its own consumers.

A zero Processes value is an error for serving. Environment settings cannot change
the selection. Existing deployments should remove `APP_ROLES`, `QUEUE_SCHEDULER_ENABLED` and
`QUEUE_COMMAND_SCHEDULING_ENABLED`, then choose the appropriate executable or
explicit Processes value. Scheduled-command handling is always registered.

## Keep process responsibilities distinct

| Work | HTTP selection | Workers selection | Scheduler selection | Ordinary CLI |
| --- | --- | --- | --- | --- |
| Serve HTTP | Yes | No | No | No |
| Consume local events | Yes | Yes | No | No |
| Consume queued tasks | No | Yes | No | No |
| Dispatch notification intents | No | Yes | No | Explicit command only |
| Enqueue recurring work | No | No | Yes | No |
| Construct configured dependencies and register modules | Yes | Yes | Yes | Yes |

Process selection controls execution. The module graph and command registrations
remain complete for all executables.

## Handle stop signals

Interrupt and SIGTERM cancel the invocation context. App stops admission, drains
work and releases clients. Do not call os.Exit in a service or handler. The main
reports errors and chooses the exit code after app has completed cleanup.

See [console](console.html), [scheduling](scheduling.html) and
[application lifecycle](application-lifecycle.html) for execution details.
