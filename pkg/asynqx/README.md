# Asynq integration

Use native Asynq tasks, options, clients, handlers, muxes, inspectors and schedulers.
This package supplies the small client contract, middleware, and lifecycle helpers
needed to integrate them into an application.

## Construct tasks

`NewTask(taskType, payload, options...)` returns a native `*asynq.Task` with an
initialized header map. Client middleware injects context when it is enqueued;
module code needs no tracing imports or header setup. Native Asynq constructors
remain supported, but a task with nil headers cannot propagate producer context.
Construct a fresh task per enqueue; middleware mutates trace headers.

The native scheduler bypasses client middleware. Scheduled tasks therefore do not
capture registration-time context; worker attempts still create their own spans.

## Ownership

`NewWorker(server, handler, redisPool, startupTimeout)` borrows its arguments and
does no I/O. Call `Run(ctx)` once. It checks Redis before starting the native server,
signals `Ready()`, then stops processing when the context is canceled.

Native shutdown can finish while a timed-out Go handler is still returning.
The worker tracks those calls and joins them before returning, preventing resource
cleanup underneath a handler. Handlers must cooperate with cancellation: Go cannot
forcibly terminate one. The owner must not close borrowed dependencies until Run
returns, even if its shutdown deadline has expired.

`NewSchedulerRunner(scheduler, redisPool, startupTimeout)` returns a runner and an
error, and adapts a borrowed native scheduler to the same Run/Ready lifecycle.
It checks Redis within the startup timeout before starting cron or signaling Ready.
Readiness is a one-time startup signal, not continuous dependency health.
Register every entry first; call Run once. Do not also call
native Start, Run or Shutdown on resources managed by these adapters.

These helpers do not own signals, configuration, application modules or Redis pool
cleanup. App composition retains those responsibilities.

## Register schedules

`TaskSchedule` holds a cron expression, a task factory and native options.
`CommandSchedule` holds a cron expression, an allowed command and arguments.
App keeps these editable lists in `internal/app/schedules.go`.

`RegisterSchedules` builds and validates every entry and resolves its routing
before registering anything. Factories should only build tasks, without external
side effects. A registration failure removes entries added by that call and
preserves rollback errors. Discard failed startup state; do not start the scheduler
after an incomplete rollback.

Calendar expressions use the timezone configured on the native scheduler (UTC in
this app). This is not distributed scheduler coordination; only one scheduler owner
should publish each recurring definition.

## Administration

Cobra administration adapters live in `internal/platform/cli.NewQueueCommand`.
App always injects its borrowed native inspector into the queue CLI commands.
The optional HTTP dashboard has separate configuration and authentication, owned
by app composition. See the [queue guide](../../docs/queues.md) for both entry points.

See [middleware](middleware/README.md), [scheduling](../../docs/scheduling.md), and
[task usage](../../docs/queue-jobs.md).
