# Testing

Test decisions through direct calls, transports through their public boundary,
and storage or delivery guarantees against the real backend. A fake tests the
caller’s decisions; it cannot establish database or Redis correctness.

## Choose the smallest useful check

| You changed | Start here |
| --- | --- |
| Request schema, response, or middleware | `go test ./internal/platform/api/... ./docs/api` and the owning HTTP package |
| Business decision or repository query | `go test ./tests/<module>`; provide the backend for integration cases |
| Module construction or registration | `go test ./tests/modules ./tests/architecture` |
| Task/event behavior | The delivery package and `tests/queue` with Redis |
| Configuration defaults or guides | The configuration-reference and documentation checks below |

These are starting points. Broaden verification when the change crosses a boundary
or changes delivery, authorization, persistence, or lifecycle behavior.

## Start with focused checks

```sh
go test ./tests/identity ./internal/modules/identity/delivery/http
go test ./tests/identity -run 'TestNotification|TestPushRegistration'
go test -race ./...
go vet ./...
```

External suites skip without their connection settings. Inspect verbose output;
a passing default run does not verify PostgreSQL, Redis or MySQL integrations.

For the required PostgreSQL/Redis gate, set all four disposable endpoints and run
`./scripts/verify-integration.sh`: `DATABASE_TEST_POSTGRES`, `QUEUE_TEST_REDIS`,
`RATE_LIMIT_TEST_REDIS` and `CACHE_TEST_REDIS`. The script refuses missing values
and runs the full suite with race detection and no cached results. The CI service
setup in `.github/workflows/verify.yml` is a reproducible reference. Optional MySQL,
Redis Cluster and live provider behavior need separate evidence.

## Choose the boundary

| Behavior | Approach | Current reference |
| --- | --- | --- |
| Business decisions | Direct calls and small fakes of owner contracts | `tests/identity/service_test.go` |
| HTTP validation and authorization | Fresh Huma server and `httptest` | `internal/modules/identity/delivery/http/user_handler_test.go` |
| Task validation and retry classification | Native task and mux calls | `internal/modules/identity/delivery/jobs/welcome_test.go` |
| Event fanout and cancellation | Fresh GoChannel/router with owned shutdown | `internal/platform/messaging` tests |
| Queue persistence and native options | Disposable Redis | `tests/queue/queue_test.go` |
| Queries and concurrent constraints | Real PostgreSQL | `tests/identity` repository integration tests |
| Application wiring, HTTP and console execution | Public entry points with disposable backends | `tests/app` |
| Cross-module transactions and delivery | Explicit module registration with PostgreSQL/Redis | `tests/modules` |
| Startup, partial failure and drain | Explicit signals and bounded waits | `internal/platform/lifecycle/lifecycle_test.go` |

Service and repository packages contain production code only. Their tests import
public constructors and contracts from `tests/<module>`; avoid private test hooks.

## Keep tests useful

Assert outcomes and invariants, including failure paths. A service test should
show a decision, not duplicate every internal call. Use deterministic coordination
for concurrent work: channels, cancellation and completion signals rather than
sleeping and hoping work finished. Every test-created resource needs cleanup.

Check malformed input, unauthorized access, duplicate delivery, rollback and
cancellation where relevant. Run race tests, but remember that they check only
executed schedules. Do not chase coverage percentages or add interfaces solely
to make every constructor mockable.

## Find the next guide

- [HTTP and module tests](testing-http.html): decode, authorize and render.
- [Task and event tests](testing-async.html): native handlers and delivery boundaries.
- [Database tests](testing-database.html): real engine fixtures and transactions.

## Verify documentation and public examples

```sh
go test docs/build.go docs/highlight_test.go
go test ./tests/modules ./tests/architecture ./docs/api
go test ./internal/app/config -run TestConfigurationReferenceMatchesDefaults
go run docs/build.go
```

The builder checks local published links, anchors, and assets; the Go suite protects
selected copyable examples and configuration defaults. It does not compile every
arbitrary Markdown fragment. When adding a substantial complete example, compile
it against current APIs or back it with an existing checked source file. Regenerate
the site after changing its Markdown sources; do not hand-edit generated HTML.

CI rebuilds documentation and expects the checked-in generated artifacts to match.
The live HTTP specification has no checked-in generation step: test it with
`go test ./docs/api`.

## Run the required integration gate locally

Use disposable PostgreSQL and Redis. The following creates dedicated local test
containers; use different names or ports if those are already occupied:

```sh
docker run --rm -d --name go-modular-test-postgres \
  -e POSTGRES_PASSWORD=test-only -p 15432:5432 postgres:17-alpine
docker run --rm -d --name go-modular-test-redis \
  -p 16379:6379 redis:7-alpine
```

Wait until both readiness commands succeed:

```sh
docker exec go-modular-test-postgres pg_isready -U postgres
docker exec go-modular-test-redis redis-cli ping
```

Then configure all four test endpoints in that terminal:

```sh
export DATABASE_TEST_POSTGRES='postgres://postgres:test-only@127.0.0.1:15432/postgres?sslmode=disable'
export QUEUE_TEST_REDIS='redis://127.0.0.1:16379/5'
export RATE_LIMIT_TEST_REDIS='redis://127.0.0.1:16379/2'
export CACHE_TEST_REDIS='redis://127.0.0.1:16379/4'
./scripts/verify-integration.sh
```

The script disables dotenv loading and runs `go test -race -count=1 ./...`. Missing
settings fail before the suite starts. Optional MySQL, Redis Cluster, and live
provider tests still need their own setup; a core pass does not certify them.
When finished, remove only these disposable containers:

```sh
docker stop go-modular-test-postgres go-modular-test-redis
```

Their `--rm` option removes the containers. This procedure intentionally supplies
no persistent data volume. Do not reuse an application database or Redis instance
for convenience.

## Read the result honestly

| Result | What it establishes |
| --- | --- |
| Unit test passed | The exercised behavior under that fixture |
| Integration test skipped | No evidence for that backend from this run |
| Race suite passed | No race detected in executed schedules |
| Build passed | Types and linking are valid; runtime dependencies were not necessarily exercised |
| Provider mock passed | Caller behavior, not live delivery or provider compatibility |

Record the command, toolchain, backend versions, enabled test endpoints, and skipped
suites for release evidence. Rerun relevant checks after changing the tested source.


Use disposable infrastructure and isolated schemas/namespaces. Never point test
settings at application or production data. Dependency/security changes also need
`govulncheck ./...` with the project toolchain; review reachable findings.
