# Scheduling

A recurring schedule decides when to enqueue work. A worker decides when that
queued task executes. Recurring timing is centralized in
`internal/app/schedules.go`: one place to review cadence, feature conditions
and enqueue options across the application. Modules own task handlers and fresh
command factories. This is the scheduling exception to module-local registration.

## Enable the scheduler

```sh
APP_ENV_FILE=.env.quickstart.example go run ./cmd/scheduler
```

Its main selects `app.Processes{Scheduler: true}`. App and worker mains do not
select a scheduler. Ordinary CLI commands and help construct registrations without
running schedules. Workers can execute the enqueued tasks in other processes.

Scheduler startup checks Redis before signaling readiness. A failed startup returns
an error; later enqueue failures are logged and missed ticks are not replayed.
Run exactly one scheduler owner per schedule set. Other replicas may process tasks
but must not independently publish the same schedules. Native Asynq scheduling is
not a distributed leader-election mechanism.

## Edit the central definitions

App's `registerSchedules` declares task and command lists, then calls
`asynqx.RegisterSchedules` once after module registration. With filesystems
configured, Media recovery runs every five minutes.
When push is enabled, `notifications.prune_subscriptions.v1` runs hourly with a
one-minute attempt timeout and one-hour enqueue uniqueness. Each execution
disables at most 1000 registrations inactive for 90 days, using a fresh cutoff.
The worker dispatcher handles delivery and receipts; it performs no retention
cleanup on startup or on a hidden timer. Identity cleanup commands run every five minutes:

| Task or command | Schedule | Required configuration |
| --- | --- | --- |
| Media recovery | Every five minutes | At least one opened disk |
| Push subscription pruning | Every hour | Push enabled |
| Identity verification cleanup | Every five minutes | Authentication enabled |
| Identity session cleanup | Every five minutes | Authentication enabled |

These are the current definitions in `app/schedules.go`; there is no schedule discovery.

These conditions register definitions; the scheduler must also be running to
publish them. Workers-only deployments still deliver notifications, but need a
scheduler owner or explicit `notifications prune-subscriptions` invocations for
retention. Large retention backlogs need repeated bounded runs.

For an existing deployment, install handler-capable workers before enabling the
new schedule. On rollback, stop scheduling first and drain pending pruning tasks
before removing their handler. Enqueue uniqueness is not leader election or
exactly-once execution. Modules never start a scheduler or declare recurring timing.
Standalone consumers can supply their own task and command lists to `asynqx.RegisterSchedules`
with their own native scheduler, routing and command registry.

To add a recurring task, append it to the central `tasks` list:

```go
tasks = append(tasks, asynqx.TaskSchedule{
    Spec: "*/5 * * * *",
    Task: func() (*asynq.Task, error) {
        return asynqx.NewTask(media.RecoverTask, nil), nil
    },
    Options: []asynq.Option{
        asynq.Unique(5 * time.Minute),
        asynq.Timeout(2 * time.Minute),
    },
})
```

This is the existing Media recovery definition: edit it in place, do not append
a second copy. It imports `time`, Asynq, `pkg/asynqx` and `internal/platform/media`.
A new capability supplies its own stable task type and registered handler.

The factory has signature `func() (*asynq.Task, error)` and builds a task without
external effects. Factories run during registration, not on each tick. Compute
values such as “90 days ago” inside the handler so they stay fresh across ticks.

The registrar validates definitions, resolves routing and builds tasks before
registering entries. Failure during native registration removes entries added by
that call and preserves rollback errors. Discard failed startup state.

## Schedule commands

Command handling is always installed during app assembly. Running the scheduler
is a separate process selection. Command support registers the native task type `app.command`. Modules explicitly
allowlist standalone command factories; Identity uses this for verification and
session pruning. A factory must return a fresh Cobra command with no parent and
no shared flag state. Do not reuse a command already attached to the console tree.

`ScheduledCommands` has a usable zero value. App assembly explicitly installs it
as a native Asynq handler; constructing the allowlist does not mutate a router.
Standalone assembly uses the same two statements:

```go
commands := &cli.ScheduledCommands{}
mux.Handle(cli.CommandTask, commands)
```

This function-body fragment imports `github.com/wisalgroup/labena/internal/platform/cli`; `mux` is an
existing `*asynq.ServeMux`. Register factories before workers start and then leave
the registry unchanged. The task type remains `app.command`, so queued invocations
keep their existing format. You can register factories and create tasks without
a mux in a producer-only process.

Register the command factory in its module, then declare its timing centrally.
Identity's existing registration follows this pattern in `register.go`:

```go
if err := targets.ScheduledCommands.Register(
    PruneSessionsCommand,
    func() *cobra.Command { return modulecli.NewPruneSessions(authentication) },
); err != nil {
    return err
}
```

This fragment runs only after checking authentication and a non-nil
`targets.ScheduledCommands`. `modulecli` imports the module's `delivery/cli`
package. The factory builds a fresh command for each worker execution.

For example, inside the appropriate configuration guard in app/schedules.go:

```go
commands = append(commands, asynqx.CommandSchedule{
    Spec: "*/5 * * * *", Command: identity.PruneVerificationCommand,
})
```

Arguments are tokenized values passed to Cobra, never shell commands. Output is
bounded to 64 KiB and written through Asynq's result writer when available; configure
native result retention if operators need to inspect completed results later.

Every app worker has command handling installed. Module feature conditions still
control which factories exist. Worker replicas need no scheduler selection.
Removing a command handler while its tasks remain queued leaves them unhandled.

## Timing and routing

Expressions use UTC in this app. `@daily` is midnight UTC. A tick makes a task
eligible; it does not promise exact execution time. Runs can overlap, and missed
ticks are not replayed after downtime.

Entry-level `asynq.Queue` options override configured task routes, then the default
queue applies. All scheduled commands share task type `app.command`; use entry
options when different commands need different queues. The native scheduler
bypasses client middleware; worker instrumentation still traces execution.

## Remove a native entry

For an explicit runtime extension, the app-owned native scheduler supports IDs:

```go
id, err := scheduler.Register("@every 30m", task, asynq.Queue("high"))
if err != nil {
    return err
}
// At a later removal decision:
return scheduler.Unregister(id)
```

These are native APIs; direct calls bypass the list registrar's validation and
routing. An entry ID identifies a schedule, not a task occurrence. Two schedules
for the same task have separate IDs. Removal stops future ticks but does not cancel
already-enqueued tasks. The app has no dynamic schedule-management endpoint or
persistent schedule-definition store: its configured lists rebuild at startup.

## Verify recurring work

1. Confirm the schedule and its feature condition in `app/schedules.go`.
2. Start one scheduler owner and at least one worker with the same Redis database.
3. Inspect `go run ./cmd/cli queue list` and the [dashboard](queues.html#asynqmon-dashboard).
4. Observe a tick and the handler's business result; a registered definition alone proves neither.
5. Stop the scheduler and confirm that future ticks stop while already queued work can finish.

```sh
go test ./pkg/asynqx/... ./internal/platform/cli/...
```

Those tests cover schedule validation, rollback and fresh command execution. They
do not wait for real recurring production jobs. For a long interval, test the
underlying handler directly or use an explicit operator command before deployment.

| Symptom | Check |
| --- | --- |
| No recurring tasks | Scheduler process, enablement, configuration guard and shared Redis URL |
| Duplicate occurrences | Multiple scheduler owners, overlapping execution or an uncertain enqueue |
| A scheduled command fails as unknown | Requested factory registered on every consuming worker |
| Old cutoff date used repeatedly | Values captured in the startup factory instead of computed in the handler |
| Removed schedule still runs | Already queued tasks; removal only affects future ticks |

See [worker shutdown](queues.html#readiness-and-shutdown), [task retries](queues.html#set-retries-per-task)
and [console conventions](console.html). Publication success is separate from execution success.
