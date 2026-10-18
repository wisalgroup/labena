# Application migrations

This directory owns global application migrations and published infrastructure SQL.
Module-owned SQL lives alongside its module, such as `internal/modules/orders/migrations`.
Both start without SQL tables in this database-free sample. The console combines
registered sources into one timestamp-ordered Goose history for the application
database. `DATABASE_DRIVER=postgres|mysql` selects one SQL dialect for the application; there are no dialect subdirectories here.

## Publish infrastructure tables

Jobs use Asynq and Redis through the thin queue API. Local events use Watermill GoChannel.
See the [queue guide](../../../../docs/queue-jobs.md) for configuration, registration,
startup, delivery guarantees, retries and observability.

## Publication rules

- Filenames use UTC `YYYYMMDDHHMMSS_description.sql` timestamps. Publication appends
  after existing versions in its output directory, including within the same second.
  Versions must also be unique across all module directories. The combined registry
  rejects collisions; adjust conflicting new, unapplied files before deployment.
- Already-published tables are skipped. Existing SQL is never overwritten, even
  after application edits or renaming the description portion of a filename.
- Preserve the first `-- platform: ...` comment. It identifies the template, table,
  and dialect for repeat publication and guards against mixing dialects. Ordinary
  application migrations do not need this comment.
- A conflicting table definition or dialect is an error. Changing DATABASE_DRIVER
  does not convert an existing migration history to another database engine.
- Complete files are placed atomically. If a later file fails, completed files stay
  in place and the command reports the error; rerun to publish the remaining files.
- A temporary `.publish.lock` excludes concurrent publishers. If a publisher is
  interrupted, first ensure it is no longer running, then remove that lock and retry.

Published files belong to the application. Package template changes do not update
existing migrations. After deployment, add a new migration for schema changes;
never edit, rename, or replace an applied migration. Do not use sequential naming
or enable out-of-order application to hide a conflicting history.

## Apply migrations

After publication, inspect and commit the SQL with the application. Then run:

```sh
go run ./cmd/cli migrate status
go run ./cmd/cli migrate up
go run ./cmd/cli migrate version
go run ./cmd/cli migrate up --timeout=10m

# Rolls back the latest migration; its Down SQL may destroy data.
go run ./cmd/cli migrate down
```

`DATABASE_URL` is a PostgreSQL URL or native MySQL DSN. `MIGRATION_TIMEOUT` defaults
to five minutes; `--timeout` overrides the total operation deadline. Application
startup and drivers never run migrations automatically.

SQL is embedded when the application is built. **Publish before building release
binaries or container images.** `go run` rebuilds with the new files automatically;
a previously compiled console needs rebuilding. An empty history builds successfully,
but migration commands explain that a migration must be published or added first.

Other console commands, including `migrate`, construct application dependencies.
Table-publishing commands use a connection-free path through the same Cobra console.

## Add business migrations

Put global schema changes here and business schema changes in their owning module:

```sh
TZ=UTC goose -dir internal/modules/shared/migrations create global_settings sql
TZ=UTC goose -dir internal/modules/orders/migrations create create_orders sql
```

Write SQL for the application's chosen DATABASE_DRIVER. Goose's default timestamp
uses the CLI's clock; use UTC consistently across the team. If installing its
optional CLI, match the repository version (`github.com/pressly/goose/v3/cmd/goose@v3.28.0`).
The application console does not require that CLI.

## Execution and maintenance

The app registers this directory, then each module registers its own embedded
source through Register. `Registry.FS(DATABASE_DRIVER)` validates all
sources and presents one flat history to Goose. Source paths appear in collision
errors; file names and version numbers are preserved. Direct SQL files are included;
subdirectories are not discovered recursively.

Versions are ordered globally, regardless of source registration order. A module
migration that needs a shared table must have a later timestamp. `migrate down`
rolls back the latest applied migration across all sources, not one per module.
There is one `goose_db_version` table and one lock for this database. Do not run
Goose against just one module folder using that shared version table. Keep all
applied migrations registered, even if their module runners are disabled. There
is no per-module execution filter or separate database routing in the default app.

The execution code lives in [database/migrate](../../../../pkg/database/migrate/README.md).
`NewProvider(ctx, db, history)` takes the combined filesystem; explicit tooling may
pass os.DirFS for a standalone history. `RunDB` borrows the app's Bun pool; `Run`
opens and closes its own pool. Progress uses the context logger. Go migration
global registration is disabled.

Migration processes serialize with PostgreSQL session advisory locks or MySQL
GET_LOCK scoped to the database. Use direct connections or session pooling;
transaction-mode PgBouncer is unsuitable for session locks. External migration
tools must coordinate with the same lock. Sharing a version table is insufficient.

PostgreSQL templates use transactional DDL. MySQL templates use InnoDB and Goose's
NO TRANSACTION annotation; MySQL DDL commits implicitly. Review partial outcomes
before retrying a failed migration. Down migrations drop the published tables and
their data; they are administrative operations, not application cleanup.

Progress uses `pkg/logging` from context. Normal progress does not print SQL or
credentials. Inspect migration status and database diagnostics after an error.

Integration tests publish package templates into temporary histories for each
engine. They do not depend on which infrastructure the application has opted into.
Existing applied migrations remain unchanged.
Publication does not inspect, reset, or modify any existing database.

## Seeders are separate

Use `go run ./cmd/cli seed` to list registered seeders or `seed orders.example`
to run a selected one. Seeders do not run during migration commands and have no
Goose version entries. See [seeders](../../../platform/seeders/README.md).

## Historical queue migrations

Existing async and Watermill queue migrations remain as immutable applied history.
The current Asynq queue stores jobs in Redis and requires no SQL queue schema.
The runtime does not read or transfer historical pending rows. Drain or export
them with the previous runtime before cutover; removing runtime code does not
authorize deleting stored work. Keep the existing Goose migration history.

## Rebuild a development database

```sh
go run ./cmd/cli migrate refresh
```

**This permanently deletes data and database objects**, including objects not
listed in migrations, then applies all registered shared and module migrations
from the beginning. It does not call Down migrations, run seeders, or reset
external Redis/broker data. Unlike truncating rows, it rebuilds changed columns
and clears Goose's version history.

Stop other application processes before running it. The migration session lock
covers both reset and migration replay, but does not stop HTTP requests or workers.
`MIGRATION_TIMEOUT` applies to the whole operation; override it with
`migrate refresh --timeout=2m`. The command runs immediately without an interactive
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
