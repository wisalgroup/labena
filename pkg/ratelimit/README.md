# Rate limiting

`Limiter.Allow` atomically admits and charges an operation. `Limit` allows `Burst`
immediate operations and replenishes `Rate` admissions per `Period`. Redis uses
[redis_rate](https://github.com/go-redis/redis_rate)'s GCRA algorithm, shared across
replicas with automatic key expiry. This is gradual replenishment, not fixed windows.

```go
limiter, err := ratelimit.NewRedis(redisClient, "my-app")
// Handle err. The caller owns redisClient and closes it after its users stop.
decision, err := limiter.Allow(ctx, "operation:opaque-account-key", ratelimit.Limit{
    Rate: 10, Burst: 10, Period: time.Hour,
})
// On error, do not proceed. On denial, decision.RetryAfter gives the wait.
```

Keep credentials and raw personal information out of keys. Hash account identifiers
with a keyed digest. Use consistent namespaces and policies across replicas.
Independent checks can conservatively consume a budget before a later check denies.
A database rollback does not refund admission. Redis data loss resets budgets;
configure persistence, capacity and eviction accordingly, separately from cache cleanup.

Run real Redis tests with `RATE_LIMIT_TEST_REDIS=redis://localhost:6379/2`.
Tests use isolated namespaces and never flush a database.

## Telemetry

`WithTelemetry(limiter, tracerProvider, meterProvider)` optionally observes every
admission call. App installs it once for the shared limiter when tracing or metrics
are enabled, covering both API and Identity policies. It borrows all dependencies.

`ratelimit.decisions` counts calls; `ratelimit.duration` records seconds. Their only
label is `outcome`: `allowed`, `denied`, `error`, `canceled`, or `timeout`. `error`
means the limiter returned an unclassified failure (including a backend failure),
not a policy rejection. Trace spans use the same categories. Keys, policy values,
and underlying error messages are never recorded. Decisions, retry delays and
error identity pass through unchanged; instrumentation does not retry calls.
