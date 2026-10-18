# Testing databases

Run database tests against the engine used in production. SQLite can check basic
transaction flow, but it cannot establish PostgreSQL or MySQL locking, SQL dialect,
constraint, or recovery behavior.

## Use an isolated database

`internal/platform/testdb.Open(t)` selects PostgreSQL by default, or MySQL when
`DATABASE_TEST_DRIVER=mysql`. It reads the matching `DATABASE_TEST_POSTGRES` or
`DATABASE_TEST_MYSQL` DSN and skips when that variable is absent.

The helper creates a unique PostgreSQL schema or MySQL database, applies the
cache migration templates with Goose, and registers
cleanup. It does not apply your module's application migrations. Add those through
the migration registry/provider when testing application queries, or create a
small explicit test table for a transaction contract as below.

Use only disposable database credentials. The helper creates and drops schemas or
databases, so the test account needs those permissions. Application accounts do
not need the same privileges. Isolation is within the selected database server;
it does not isolate server-wide configuration or resource limits.

## Apply the schema your test needs

`testdb.Open` installs cache fixture tables, not all business tables. For an
Identity, Media, or Notification repository test, register the matching embedded
migration sources and apply their combined history in the isolated schema.
`tests/modules/profile_test.go` and the existing Identity repository tests show
complete fixtures for those owners.

Avoid creating an approximate table in a repository test when its real constraints
are part of the behavior. Small explicit tables are useful for generic transaction
manager tests, such as the rollback example below.

## Verify a rollback

Place this complete example in `tests/identity/rollback_example_test.go`, outside
the service and repository packages. The internal fixture is for tests, not an
application dependency.

```go
package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
)

func TestRollbackExample(t *testing.T) {
	db := testdb.Open(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, "CREATE TABLE test_entries (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	manager, err := database.NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	rejected := errors.New("operation rejected")
	err = manager.Execute(ctx, func(ctx context.Context) error {
		executor, err := database.Executor(ctx, db)
		if err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, "INSERT INTO test_entries (id) VALUES (?)", 1); err != nil {
			return err
		}
		return rejected
	})
	if !errors.Is(err, rejected) {
		t.Fatalf("error=%v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_entries").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rollback left %d rows", count)
	}
}
```

The insert uses `database.Executor(ctx, db)`, so it participates in the managed
transaction. The final query uses the pool after the transaction returns. Calling
the raw pool inside the callback would bypass the transaction.

## Run PostgreSQL and MySQL suites

Set `DATABASE_TEST_POSTGRES` or `DATABASE_TEST_MYSQL` to a disposable database DSN, then run:

```sh
DATABASE_TEST_DRIVER=postgres go test -race -count=1 ./pkg/database ./pkg/database/migrate ./internal/platform/testdb
DATABASE_TEST_DRIVER=mysql go test -race -count=1 ./pkg/database ./pkg/database/migrate ./internal/platform/testdb
```

Add your module's package path to run its integration tests too. Both DSNs may be
set: engine-specific tests use their own DSN; `DATABASE_TEST_DRIVER` selects the
backend for shared transaction fixtures. Without backend settings, some database
tests run on SQLite and the external suites skip. Inspect verbose output rather
than treating that run as PostgreSQL or MySQL verification.

## Cover transaction boundaries

| Case | Assert |
| --- | --- |
| Successful operation | Changes are visible after commit |
| Callback failure | No participating writes remain |
| Nested operation fails | The outer transaction cannot commit even if the inner error is swallowed |
| Context cancellation | No later business work proceeds; rollback is attempted |
| Different pool in a transaction | The operation is rejected |
| Concurrent uniqueness conflict | The database constraint protects the invariant |
| SQL plus notification intent | Verify both commit/rollback together; tests/modules/profile_test.go exercises the existing PostgreSQL ledger |
| SQL plus arbitrary Redis/GoChannel publication | No atomic guarantee is supplied; design and verify an explicit outbox if required |

The existing database tests also cover savepoints and transaction ownership.
Do not share a managed transaction context concurrently between goroutines.
For concurrency tests, coordinate independent transactions and assert the final
stored state; sequential check-then-insert calls do not reproduce a race.

## Migrations and cleanup

Use a fresh schema to test migration application and the resulting constraints.
For upgrade tests, apply the earlier schema, insert representative data, then apply
the new migration. A fresh install alone does not test an upgrade.

Let each fixture own its resources and clean them up after users stop. Do not
truncate shared development tables. Do not wrap every test in a rollback transaction
when the behavior under test must commit, publish, or use another connection.

See [Transactions](database-transactions.html) and [Migrations](migrations.html)
for the production APIs and ownership rules.

## Diagnose a skipped or failing integration test

| Symptom | Check |
| --- | --- |
| Test says SKIP | Matching test DSN is absent; run with `-v` to see the reason |
| Permission denied creating schema/database | Use a disposable test account with fixture privileges |
| A module table is missing | Register and apply that module's actual migrations |
| Concurrent uniqueness test passes only sequentially | Coordinate separate transactions to overlap intentionally |
| Tests pass on SQLite but fail on PostgreSQL | SQL, constraints, isolation, and locking differ; the production engine is authoritative |
| State leaks between tests | Use isolated fixtures and wait for goroutines before cleanup |

Keep query deadlines bounded, including failure paths. A database error is not
always proof that no write committed; tests for retries should assert final durable
state and idempotency. See [integration setup](testing.md#run-the-required-integration-gate-locally).
