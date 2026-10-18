# Database connections and transactions

`database` configures Bun over PostgreSQL (pgx), MySQL (go-sql-driver), or SQLite. It returns native Bun
and `database/sql` objects; it does not introduce a generic repository or run
schema changes on connection setup.

```go
logger, err := logging.New(os.Stdout, logging.Options{})
if err != nil {
    return err
}
ctx := logging.WithLogger(context.Background(), logger.With("service", "api"))

cfg := database.DefaultConfig()
cfg.Name = "primary"
cfg.DSN = os.Getenv("DATABASE_URL")
db, err := database.Open(ctx, cfg)
if err != nil {
    return err
}
defer db.Close()

transactions, err := database.NewTxManager(db, nil)
if err != nil {
    return err
}
```

## Configuration and ownership

PostgreSQL is the default driver. `DriverPostgres` uses pgx with Bun's PostgreSQL
dialect; `DriverMySQL` uses go-sql-driver/mysql and Bun's MySQL dialect.
`DriverSQLite` retains the existing SQLite implementation. `OpenSQL`
returns the same configured pool without the Bun wrapper for SQL-only callers.
`Open` returns a `*bun.DB`; its embedded `db.DB` is the underlying `*sql.DB`, not a
second pool. Close the owner once, after all consumers have drained.

| Setting | Default |
| --- | --- |
| `MaxOpenConns` | 20 |
| `MaxIdleConns` | 5 from `DefaultConfig`; 0 in a literal disables idle retention |
| `ConnMaxLifetime` | 30 minutes |
| `ConnMaxIdleTime` | 5 minutes |
| `ConnectTimeout` | 5 seconds per connection |
| `PingTimeout` | 5 seconds for startup verification |
| `SlowQueryThreshold` | 500 milliseconds |

Zero durations select defaults; negative settings and idle limits above the open
limit are rejected. Adjust pool size for database capacity across all replicas.
Private/in-memory SQLite DSNs use one retained connection without recycling so
that the schema does not disappear between requests.

Opening a pool pings it before returning and closes it on failure. PostgreSQL
DSN settings retain pgx's TLS, runtime parameters, and authentication support.
Use `sslmode=verify-full` and appropriate certificates for remote production
connections. DSNs are omitted from JSON serialization and ordinary config/error
formatting. `ConnectionError` exposes its original cause through `Unwrap` for
`errors.Is`/`errors.As`; treat that underlying error as potentially sensitive.

All SQL connection setup lives here. Repositories and database cache borrow the same
`*bun.DB`; they do not create pools or close the application's database.

For MySQL, set `cfg.Driver = database.DriverMySQL` and use its native DSN:
`user:password@tcp(host:3306)/app?tls=true`. The package forces `parseTime=true`,
UTC decoding, UTC Bun time formatting, and session `time_zone='+00:00'` so stored
application and transaction timestamps use the same clock convention. Connection timeout
comes from Config. Other native DSN options remain available, including TLS settings;
use verified TLS for remote production connections. The reusable database and migration packages target
MySQL 8.4+ with InnoDB; installed business modules require PostgreSQL. SQLite remains available for standalone database users.

## Transactions

Services can accept a consumer-owned `Execute` interface or callback. Repositories
call `database.Executor(ctx, db)` for every operation that should participate in
that scope:

```go
err := transactions.Execute(ctx, func(ctx context.Context) error {
    executor, err := database.Executor(ctx, db)
    if err != nil {
        return err
    }
    _, err = executor.NewInsert().Model(&order).Exec(ctx)
    return err
})
```

- Successful outer execution commits; errors, cancellation, and panics roll back.
- Nested `Execute` joins the existing transaction. It does not create a savepoint.
- A failed nested callback marks the outer scope rollback-only, even if its error
  or panic is swallowed. Cross-database access through `Executor` is rejected.
- Explicit nested transaction options must match the outer scope. Nil options
  inherit that scope's options. Configure isolation/read-only settings with
  `NewTxManager(db, &sql.TxOptions{...})`; the options are copied.
- `Begin`/`Commit`/`Rollback` support explicit ownership. Do not call them to settle
  a transaction owned by an executing callback. Rollback is idempotent afterward. A canceled PostgreSQL rollback may return
  `context.Canceled` while pgx discards the connection; this error is preserved,
  not silently treated as a successful round trip.
- Managers can be shared across requests. A transaction context is single-owner:
  do not use it concurrently or let it escape its callback/scope.
- Use bounded operation contexts for queries and transaction scopes. Pool
  connection timeouts do not impose query deadlines. Commit errors are not retried automatically; their outcome may be uncertain.
  Retrying a transaction callback could repeat non-database side effects.

Repositories use `Executor` in HTTP, console and worker operations to join the
caller's transaction. Asynq enqueueing, GoChannel publication and external channel
delivery cannot commit atomically with SQL; framework adapters reject transaction
contexts before those effects. Notification `Enqueue` is the specific exception:
it stores delivery intent in SQL and can join the business transaction. There is
no general-purpose database queue, pubsub or event outbox in this application.

`TxManager.Savepoint(ctx, fn)` requires an active transaction. It isolates a child
scope: errors and panics undo child writes, and the parent may continue. A cleanup
failure makes the parent rollback-only. A successful child still depends on the
outer commit. Ordinary business workflows usually need only `Execute`; the native
Asynq worker does not supply a SQL transaction or savepoint automatically.

`database.TxFromContext(ctx, db)` returns the active `bun.Tx` when an adapter needs
it explicitly; it does not transfer commit ownership. Calling a raw `db` instead
of `Executor` bypasses the scope; the package cannot intercept such calls.

The legacy `Transactions`, `NewTransactions`, and `transaction.Transaction` names
have been removed. Use `TxManager`, `NewTxManager`, and a consumer-owned `Execute` interface or function.
When replacing `NewTransactions(db)`, use `NewTxManager(db, nil)` and handle its
validation error. Pool configuration fields now use the standard library
names: `MaxOpen` -> `MaxOpenConns`, `MaxIdle` -> `MaxIdleConns`, and `MaxLifetime` ->
`ConnMaxLifetime`.

## Context logging

Connection setup and Bun queries use `logging.FromContext(ctx)`. Attach request
or job attributes with `logging.WithLogger`; the query hook resolves that logger
at execution time, not when the pool was constructed. Without a context logger,
`pkg/logging` falls back to `slog.Default`.

Successful query metadata is DEBUG, slow queries are WARN, and query errors are
ERROR (cancellation is DEBUG; deadline expiry is WARN). Metadata includes the
operation, duration, database name, and PostgreSQL SQLSTATE when available. Raw
SQL, bound arguments, DSNs, and server error text are never included in these
logs. `sql.ErrNoRows` is not logged as a database failure. Errors are still
returned unchanged to callers. Queries made directly through `db.DB` bypass Bun
hooks; instrument those callers separately if needed.

## Central migrations

Global SQL lives in `internal/modules/shared/migrations/`; module-owned SQL lives in
`internal/modules/<module>/migrations/`. Registered sources form one Goose history with
globally unique timestamps. Cache schema templates belong to `pkg/cache/database`;
the `cache:tables` command publishes the selected driver's schema. See
[the migration guide](../../internal/modules/shared/migrations/README.md). Application
startup never migrates. The app's explicit migration command now delegates
to this central history instead of calling individual driver schema helpers.

## Verification

```sh
go test -race ./pkg/database ./pkg/logging
DATABASE_TEST_POSTGRES='postgres://user:password@localhost:5432/test?sslmode=disable' \
    go test -race ./pkg/database ./pkg/database/migrate
```

The PostgreSQL suite creates isolated schemas, then removes them. It runs the
transaction ownership tests on PostgreSQL as well as checking read-only mode,
query cancellation, native SQL transactions, and aborted commits. Without the
variable, the ownership suite uses SQLite and PostgreSQL-specific tests skip.

For MySQL integration tests, use a disposable server and an account allowed to
create/drop databases (only test fixtures need this privilege):

```sh
DATABASE_TEST_DRIVER=mysql \
DATABASE_TEST_MYSQL='root:password@tcp(localhost:3306)/test' \
    go test -race ./pkg/database ./pkg/database/migrate
```

The shared transaction/savepoint suite then runs on MySQL. Dedicated tests cover
UTC time round trips, query cancellation, migration concurrency and lock cancellation.

## Optional telemetry

`Instrument(db, name, tracerProvider, meterProvider)` adds Bun query spans/durations
and local pool metrics. Call it once before concurrent use; use no-op providers to
disable either signal. The returned function unregisters pool metrics before pool
closure. It does not remove Bun hooks. Recorded read/write spans include sanitized
SQL as `db.query.text`: values become `?` and comments are removed. Identifiers remain
visible (SQLite double-quoted tokens are also redacted); never put secrets in them.
PostgreSQL binary hex literals are redacted too, so digest-bearing INSERT/UPDATE
statements retain their query structure. Other ambiguous escapes, unsupported or
oversized SQL omit text while retaining timing/status.
Input is bounded to 64 KiB and sanitized output to 8 KiB. No SQL is added to logs or
metric labels. Values, DSNs and raw errors are omitted;
raw database/sql calls bypass query hooks. App wires its primary pool automatically
when either signal is enabled. See [observability](../../docs/observability.md#database-queries-and-pool-metrics).


## Migration tooling

`pkg/database/migrate` owns reusable Goose execution, source validation and
template publication. Application migration sources are still registered explicitly
in `internal/app/infrastructure.go` and module-owned registration; module and shared SQL remain where they
are owned. Opening a database never applies migrations.

The former `pkg/transaction.Manager` interface was removed. App composition
borrows an execution-only interface; consumers may accept the manager's
`Execute` method as a function. Manual Begin/Commit/Rollback and Savepoint remain
available on `*database.TxManager` when a caller needs that ownership explicitly.

`RequireNoTransaction(ctx)` rejects transaction-bearing contexts before an operation
performs nontransactional side effects. Notification delivery/dispatch and media
mutation use this guard; notification intent persistence deliberately joins the
caller's transaction through `Executor`.
