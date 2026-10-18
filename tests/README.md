# Application integration tests

Application composition, module services and repositories stay free of test files
and test-only constructors.

- `architecture` scans production imports and enforces app/service/repository test placement.
- `platform` covers capability integration, media storage/HTTP behavior and shared reference syntax.
- `app` exercises the public `app.Serve` and `app.Execute` entry points: CLI startup and
  help, input validation, routes, migrations, tracing, startup failure, resource
  cleanup, health, HTTP, metrics, module event reactions and queue delivery.
- `identity` exercises public service contracts and independently constructed repositories,
  including PostgreSQL transactions, concurrent authentication, OTPs and credential changes,
  plus user media, notification and push-subscription HTTP behavior.
- `modules` exercises module registration with explicitly supplied services, including
  authentication, cross-module transactions, duplicate event/task delivery and the
  infrastructure-free first-module example.
- `queue` exercises native queue policies, results, administration, scheduled
  commands, tracing and worker-to-local-event shutdown.

Focused tests stay with their owning capabilities: `internal/platform/lifecycle`
tests ordered drain, readiness gating, timeout ownership and cleanup failures;
`internal/platform/api` tests HTTP middleware; `internal/platform/cli` tests Cobra
and scheduled-command behavior. Tests never copy app construction or use private
application state.

Run the repository baseline:

```sh
go test -race ./...
go vet ./...
```

PostgreSQL and Redis checks require disposable services configured through
`DATABASE_TEST_POSTGRES`, `QUEUE_TEST_REDIS`, `RATE_LIMIT_TEST_REDIS` and
`CACHE_TEST_REDIS`. Use different Redis database numbers for queue, rate limiting
and cache. Tests create isolated PostgreSQL schemas and unique queue names, then
clean them up. Without the required settings, those cases explicitly skip;
console checks using disabled optional resources and lifecycle checks still run.
