# Redis connections

`Open(ctx, redis.UniversalOptions, timeout)` returns a native `redis.UniversalClient`
for standalone, Sentinel or Cluster. `OpenURL(ctx, url, timeout)` returns a native
standalone `*redis.Client`, using the library's `redis`, `rediss` and `unix` URL
support. Both check connectivity and close the client if that check fails. Zero
timeout uses five seconds. Context deadlines apply to command I/O.

The caller owns `Close`. Stop workers and other users before closing the client.
Cache behavior belong to their own packages; connection
setup owns no keys, streams, queues or environment settings. Configure TLS,
credentials and pooling with native options. Errors omit connection credentials.

## Optional tracing

Attach `NewTracingHook(tracer)` once with `client.AddHook` before concurrent use.
Pass a non-nil native tracer, including a no-op tracer when needed. The hook records
bounded command names or a pipeline span, never arguments, keys or raw errors.
Misses are not failures. Dialing and pubsub receive loops are outside hook coverage.
The client/provider remain caller-owned. App attaches this hook to its Redis clients
when tracing is enabled. See [observability](../../docs/observability.md#redis-and-cache-operations).
