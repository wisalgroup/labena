# Migrations

Goose applies shared and module SQL as one timestamp-ordered history. Run migrations explicitly through the console; application startup never changes schema.

```sh
go run ./cmd/cli migrate status
go run ./cmd/cli migrate up
go run ./cmd/cli migrate version
```

Set `DATABASE_DRIVER` and `DATABASE_URL` first. An empty history needs a business migration or published infrastructure tables before Goose can run it.

## Follow a schema change from source to database

```text
SQL file → embedded module filesystem → module registration → combined Goose history → migrate up
```

Adding a table does not require another application registry. Add its SQL in the
owning module, keep its directory registered, rebuild the binary, and run the
migration command. Startup itself does not apply pending schema changes.

## Choose where SQL belongs

| Directory | Contents |
| --- | --- |
| `internal/modules/shared/migrations` | Global schema and published infrastructure SQL |
| `internal/modules/catalog/migrations` | Catalog-owned schema, once that module is added |
| `internal/modules/identity/migrations` | Account and authentication schema |
| `internal/platform/notification/migrations` | One baseline: polymorphic JSON notifications and delivery ledger |
| `internal/platform/media/migrations` | Baseline metadata for polymorphic assets; collections are an asset attribute |

Files sit directly in each directory. The registry combines explicitly registered directories; it does not select SQL by driver automatically. Write SQL for the selected engine.

Use UTC `YYYYMMDDHHMMSS_description.sql` filenames. Versions must be unique across every registered directory. The order is global timestamp order, not module registration order.

## Write a business migration

For the proposed Product model, add a new file such as `20260928090000_create_products.sql` in `internal/modules/catalog/migrations`. Choose a fresh timestamp after relevant existing migrations; this name is illustrative.

For PostgreSQL:

```sql
-- +goose Up
CREATE TABLE products (
    id VARCHAR(200) PRIMARY KEY,
    name VARCHAR(200) NOT NULL
);

-- +goose Down
DROP TABLE products;
```

The shipped modules require PostgreSQL. The generic migration package also supports
MySQL, but adopting it for the application requires porting module SQL. For a separate
MySQL application, use the same columns with InnoDB and mark the file as nontransactional:

```sql
-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE products (
    id VARCHAR(200) PRIMARY KEY,
    name VARCHAR(200) NOT NULL
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE products;
```

Choose one version, not both. MySQL DDL commits implicitly; a failure can leave partial changes. The Down operation deletes the table and its data.

The optional Goose CLI can generate timestamped files:

```sh
go install github.com/pressly/goose/v3/cmd/goose@v3.28.0
TZ=UTC goose -dir internal/modules/catalog/migrations create create_products sql
```

Create the directory first and use the CLI version matching `go.mod`. The application console does not require the CLI and has no `migrate create` subcommand. You may create timestamped files manually instead.

## Embed and register module SQL

Add `internal/modules/catalog/migrations/files.go`:

```go
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
```

This pattern requires at least one SQL file. Add real module SQL before registering
an embedded migration source; do not add empty histories merely to mirror a layout.

Register the source in your module's Register function, importing its migrations
package. The registry is supplied through dependencies.Targets:

```go
if targets.Migrations != nil {
    if err := targets.Migrations.Register("internal/modules/catalog/migrations", migrations.Files); err != nil {
        return err
    }
}
```

App calls the module's Register function once for all its handlers and SQL. There is
no additional migration catalog to edit in app. New SQL files included by the embed
pattern need no registration edit.

Registering files executes no SQL. App validates the combined history during construction. It includes direct SQL files only; nested directories are not discovered automatically. Shared migrations are already registered by app assembly.

## Apply and roll back

```sh
go run ./cmd/cli migrate up --timeout=10m
go run ./cmd/cli migrate down
```

`MIGRATION_TIMEOUT` defaults to five minutes. `--timeout` overrides the migration and lock deadline. `down` rolls back the latest applied migration across all sources; it is not a module-specific rollback and can destroy data.

Migration commands construct the application before execution, just like other
commands. Configured resources must be available and the registered SQL history
must match DATABASE_DRIVER. The shipped business migrations target PostgreSQL;
using another dialect requires replacing that history and its repositories.
Migration execution opens a dedicated database pool for its lock/session contract.
It starts no application runners and closes all acquired resources on return.

Publish or add SQL before building a release binary or image. SQL is embedded at build time. `go run` rebuilds; an existing console binary needs rebuilding to see new files.

## Maintain one history

All sources use one `goose_db_version` table and one migration lock. Do not execute a single module directory independently against that history. Keep applied migrations registered even when a module's workers are disabled.

PostgreSQL uses session advisory locks; MySQL uses database-scoped `GET_LOCK`. Use direct connections or session pooling for migration execution. Transaction-mode PgBouncer does not preserve the required session ownership. External migration tools must coordinate with the same lock.

Never edit or rename applied migrations. Add a new migration for changes and plan compatible schema transitions for rolling deployments. Inspect status and actual schema after failure, particularly for MySQL's nontransactional DDL.

Publishing files does not inspect or repair database state. If interrupted publication leaves `.publish.lock`, verify the publisher has stopped before removing it. Existing files remain intact; rerun to publish any remaining files.

Seeders are separate operations with no Goose history. See [Seeders](seeders.html).

## Existing development databases

Historical async and Watermill queue migrations remain immutable. Asynq now stores
jobs in Redis and requires no SQL queue tables. This runtime does not consume or
convert historical pending rows. Drain or export old work using the previous runtime
before cutover. Do not delete migration history or assume a mixed deployment shares jobs.

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

## Development baselines for platform capabilities

Notifications and Media now have one initial PostgreSQL migration each. The former
creates `notifications` and `notification_deliveries`; the latter creates only
`media_assets`. Earlier user-only tables and alteration chains are removed.

Use a new development database or explicitly recreate the old schema and its
migration history before applying these baselines. Merely running migrate up on
an existing database will not transform an already-applied migration. No startup
path resets a database automatically. Once a release has consumers, preserve
applied baselines and use new forward migrations.

## Inspect a failed migration

Start with read-only inspection:

```sh
go run ./cmd/cli migrate status
go run ./cmd/cli migrate version
```

Check the selected database and actual schema before deciding how to recover.
A failed DDL statement may not have the same rollback behavior on every engine.
Do not repair a shared database by deleting version rows or editing previously
applied files without a deliberate recovery procedure.

| Symptom | Check |
| --- | --- |
| New SQL is absent from status | Direct file location, embed pattern, source registration, and binary rebuild |
| Duplicate version | Timestamps must be unique across every registered source |
| Dialect validation failure | Installed PostgreSQL history cannot be run as MySQL |
| Waiting on migration lock | Another migrator and the command deadline; use session-compatible connections |
| Published tables exist as files but not in DB | Publishing and applying are separate steps |
| Refresh refused | It requires both `--force` and development/test environment |

For a release, test the complete fresh history and an upgrade from the previous
supported schema using disposable databases. Rehearse restore procedures for data
changes whose Down migration cannot recover lost information. Continue with
[seeders](seeders.md), [database testing](testing-database.md), and
[deployment](deployment.html).
