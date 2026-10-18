# Queue middleware

App composes `Chain(client.EnqueueContext, RequireNoTransaction(), ObserveClient(provider), Route(routing))`
once during construction. The result exposes the native EnqueueContext signature;
there is no custom task model, publisher object or publication factory.

Asynq v0.26 provides worker middleware, but no client hook. EnqueueFunc adapts its
native method to a function chain. Services consume `asynqx.Client`; app retains
ownership of Redis and the worker. Configure routing before startup.

RequireNoTransaction rejects framework SQL scopes and already canceled contexts
before tracing, routing or Redis enqueue. Standalone callers opt into it; native
clients bypass the policy. It does not close the gap between a SQL commit and
Redis acceptance. Use transactional notification intent for that notification workflow.

Observe preserves the native task and options. It injects W3C headers when tasks
are created with `asynqx.NewTask` (or native `NewTaskWithHeaders` with a non-nil map).
Tasks created with native `asynq.NewTask`
still work but cannot propagate context. Construct a fresh task for each enqueue;
concurrent use of the same task is unsupported. No baggage is injected.

Route snapshots config. Explicit enqueue Queue options override task routes,
which override the default queue. Constructor Queue options are overridden by
application routing; all other native constructor options are retained.

Worker instrumentation remains native ServeMux middleware in pkg/asynqx/middleware.
Schedule lists and registration belong to app. The native scheduler bypasses the
client chain; registration applies routing and worker execution emits telemetry.

## Naming compatibility

Code, logs and metrics use queue/task terminology. The attempt histogram is
`queue.task.duration` (Prometheus `queue_task_duration_seconds`), with bounded
`task_name`, `queue` and `outcome` labels. Logs use `task_id` and `task_name`.
This replaces the previous job-prefixed fields; migrate custom dashboards and
alerts at rollout. Historical samples retain their old names. Persisted task
names are unchanged.
OpenTelemetry `messaging.*` attributes describe queue operations as well as pubsub;
they are telemetry vocabulary, not application package names. Watermill local events
remain in platform/messaging.

QueueConfig.Routing is the single config conversion shared by validation, client
middleware and schedule registration. App owns acquisition and lifecycle; this
package owns the behavior installed on the native clients and router.
