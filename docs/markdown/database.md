# Database connections

Set the database connection in your environment file. App assembly opens one Bun pool and shares it with the repositories and database drivers that need it.

```dotenv
DATABASE_DRIVER=postgres
DATABASE_URL=postgres://app:password@127.0.0.1:5432/app?sslmode=disable
```

The credentials above are local placeholders. Use verified TLS and appropriate credentials for remote connections. Configuration is loaded centrally; repositories do not read environment variables.

## Choose a driver

| Driver | Scope |
| --- | --- |
| `postgres` | Installed modules, Bun queries, transactions, migrations and database cache |
| `mysql` | Reusable database/cache packages and custom MySQL migrations; installed module SQL is PostgreSQL |
| `sqlite` | Available through standalone `pkg/database`; not supported by the app's combined migration history |

For independent MySQL components, use its native DSN:

```dotenv
DATABASE_DRIVER=mysql
DATABASE_URL=app:password@tcp(127.0.0.1:3306)/app
```

The connection package enables MySQL time parsing, UTC decoding, and a UTC session time zone. Other driver options, including TLS options, remain available.

Changing `DATABASE_DRIVER` does not translate existing SQL or migrate data. Keep one chosen engine for an application's migration history.
The shipped app rejects a mixed migration dialect even for CLI help. A MySQL
application requires porting or replacing the installed modules' queries and SQL;
it is not an alternative quickstart profile. The required integration gate verifies
PostgreSQL and Redis; optional engines need their own verification.

## Use the app's connection

App assembly opens a database when `DATABASE_URL` is nonempty. Database cache requires that connection. With no URL and database cache disabled, `a.resources.DB` and `a.resources.Transactions` remain nil; Redis tasks and local events do not require SQL independently. Serving the installed business modules still requires PostgreSQL.

Inside the owning module’s `register.go`, inject its borrowed pool into the repository:

```go
catalogRepo, err := catalogrepository.NewProductRepository(deps.DB)
if err != nil {
    return fmt.Errorf("app: catalog repository: %w", err)
}
```

This fragment assumes the repository from [Models and queries](database-queries.html) has been added and imported as `catalogrepository`. Pass the repository to the owning service, then pass that service to package registration. Do not open another pool per request or module.

The app owns the pool and closes it after its consumers stop. Repositories and the database cache driver borrow it.

## Configure the pool

These are the current application defaults:

```dotenv
DATABASE_NAME=
DATABASE_MAX_OPEN_CONNS=20
DATABASE_MAX_IDLE_CONNS=5
DATABASE_CONN_MAX_LIFETIME=30m
DATABASE_CONN_MAX_IDLE_TIME=5m
DATABASE_CONNECT_TIMEOUT=5s
DATABASE_PING_TIMEOUT=5s
DATABASE_SLOW_QUERY_THRESHOLD=500ms
```

`DATABASE_NAME` is a diagnostic name; the DSN selects the actual database. Opening a pool includes a ping and closes the pool if initialization fails.

Budget connections across every process and replica. Ten replicas with a limit of 20 can open 200 connections, before migration tools and other services are counted. More connections are not automatically more throughput.

Connection and ping timeouts do not limit query duration. Give each operation a bounded context:

```go
ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
defer cancel()
product, err := repository.Get(ctx, id)
```

This fragment belongs in a caller that owns the operation deadline. Pass the derived context onward, handle the returned error, and use the returned product.

## Open a standalone pool

Use `pkg/database` directly for tooling that does not use app assembly. This complete function borrows its caller's configuration and context, verifies access, and closes its own pool:

```go
package probe

import (
	"context"
	"errors"

	"github.com/wisalgroup/labena/pkg/database"
)

func Check(ctx context.Context, cfg database.Config) (err error) {
	db, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var value int
	return db.NewRaw("SELECT 1").Scan(ctx, &value)
}
```

Start with `database.DefaultConfig()` and set its DSN before calling. `Open` returns `*bun.DB`. `OpenSQL` returns a native `*sql.DB` for SQL-only callers. The Bun pool's `db.DB` is its underlying SQL pool, not a second connection owner; close the owner once.

A literal `database.Config` with `MaxIdleConns: 0` disables idle retention. `DefaultConfig` supplies five idle connections. Private in-memory SQLite databases use one retained connection so their schema survives between calls.

## Query logs

Bun queries resolve `logging.FromContext(ctx)` at execution time, preserving request and job fields. Successful queries log metadata at DEBUG, slow queries at WARN, and failures at ERROR. Cancellation is DEBUG; deadline expiry is WARN. Missing rows are not logged as database failures.

Set the desired channel’s `Options.Level` to `slog.LevelDebug` in `internal/app/config/observability.go` to include successful queries.

```dotenv
DATABASE_SLOW_QUERY_THRESHOLD=500ms
```

The hook records operation, duration, and database name. It omits SQL text, bound values, credentials, and server error text. Queries through the underlying `db.DB` bypass Bun hooks. Returned driver errors may still contain sensitive detail; translate them before sending responses.

To inspect SQL for a request, open its database span in Grafana and read
`db.query.text`. Tracing captures sanitized query structure, with values replaced
by `?` and comments removed; enabling DEBUG does not add SQL to logs. See
[request SQL in Grafana](observability.html#inspect-a-requests-sql-in-grafana)
for navigation, supported queries and capture limits.

See [Transactions](database-transactions.html) before adding multi-query workflows, and [Migrations](migrations.html) before creating tables.

## Verify a connection and its schema

After configuring the intended database:

```sh
go run ./cmd/cli migrate status
go run ./cmd/cli migrate up
```

The first command inspects migration state; the second changes schema. A successful
connection ping proves access to the server, not that every required table or
permission exists. Exercise a representative request after applying migrations.

| Symptom | Investigate |
| --- | --- |
| Connection refused or startup timeout | Host/port reachability and whether the dependency is running |
| Authentication/TLS failure | Credentials, certificate configuration, and server policy; do not print the DSN |
| CLI fails before printing help | CLI intentionally constructs configured resources; required connections must work |
| Pool waits during load | Total process/replica connection budget, long queries, and unclosed rows/transactions |
| Context deadline during a query | Query duration and caller deadline, separately from connect/ping timeouts |
| Table missing after a successful connection | Registered migration history, selected database/schema, and deployed binary |

For deployment sizing, reserve connections for migrations, administration, and
other applications too. Readiness dependency checks are bounded; they do not
replace query-level error handling or recovery. Continue with
[entities and queries](database-queries.md), [database tests](testing-database.md),
and [observability](observability.md).
