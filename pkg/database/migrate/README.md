# Migration tooling

This package owns Goose execution, migration source registration, and publishing
optional infrastructure templates. SQL ownership lives elsewhere:

- `internal/modules/shared/migrations`: application-wide and published infrastructure SQL.
- `internal/modules/<module>/migrations`: SQL owned by a business module.

Register each embedded filesystem with `Registry.Register(path, files)`, then call
`Registry.FS(driver)` to validate and combine sources. The resulting filesystem is
flat and contains only directly owned SQL files. There is no recursive discovery.
Duplicate timestamps, invalid filenames and conflicting generated table definitions
or dialects are errors. Empty histories can be assembled but cannot be executed.

`NewProvider(ctx, db, history)` creates a scoped [Goose provider](https://pressly.github.io/goose/documentation/provider/).
`RunDB(ctx, db, history, action, timeout)` borrows a Bun pool; `Run` opens and closes
its own pool. The history is always explicit. Providers keep global Go migration
registration disabled and use one version table and the existing session lock.
No constructor or registration method executes SQL.

Refresh reserves one connection for the migration lock and uses another for Goose
replay. Its pool must allow at least two connections. The console calls `Run` with
a dedicated two-connection pool; callers using `RunDB` must provide that capacity.

Migration sources are borrowed. Embedded sources are immutable; tooling using
os.DirFS must keep files unchanged while validating or running migrations. Filenames
are preserved when directories are combined, so moving an already applied file
between registered sources does not change its Goose version. Never renumber or
remove applied files. Existing binaries still require a rebuild after file changes.

See the [application migration guide](../../../internal/modules/shared/migrations/README.md) for commands,
ordering, timestamp rules, rollback and operational limits.

## Verification

```sh
go test -race ./pkg/database/migrate ./internal/platform/testdb
```

Set DATABASE_TEST_POSTGRES and DATABASE_TEST_MYSQL to disposable services to run
backend tests. They verify combined source ordering, repeat up, status, global down,
reapplication, template publication, dialect checks and concurrent session locks.
They create and remove isolated schemas/databases; do not point them at production.

## Rebuild a development database

```sh
go run ./cmd/cli migrate refresh --force
```

**This permanently deletes data and database objects**, including objects not
listed in migrations, then applies all registered shared and module migrations
from the beginning. It does not call Down migrations, run seeders, or reset
external Redis/broker data. Unlike truncating rows, it rebuilds changed columns
and clears Goose's version history.

Stop other application processes before running it. The migration session lock
covers both reset and migration replay, but does not stop HTTP requests or workers.
`MIGRATION_TIMEOUT` applies to the whole operation; override it with
`migrate refresh --force --timeout=2m`. The command runs immediately without an interactive
confirmation. Use it only when the selected data may be discarded.

- PostgreSQL: recreates `current_schema()` from the connection's search path,
  preserving its owner. `DROP SCHEMA ... CASCADE` also removes dependent objects,
  potentially including dependencies in other schemas. Schema-specific grants
  and settings must be recreated by migrations or provisioning.
- MySQL: recreates the selected database, preserving its default character set
  and collation. The account needs DROP and CREATE database privileges.

System schemas/databases are rejected. Invalid or empty migration histories are
rejected before deletion, but invalid SQL can still fail during replay. Refresh
is not atomic end-to-end: MySQL DDL commits implicitly, and migration replay can
leave an empty or partially rebuilt database after a failure. Fix the cause and
rerun refresh only when discarding the remaining state is intentional.

This resets only the configured target; it never recreates the database server.
Rebuild deployed binaries after editing embedded migration files.

Refresh requires `--force` and `APP_ENV=development` or `test`. Staging and
production refuse it even with the flag. It deletes all schema data; take a backup
and stop other processes first. This command is not an upgrade or rollback.
