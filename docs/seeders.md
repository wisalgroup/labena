# Seeders

Seeders create or update deliberate initial data through named service operations.
Run them explicitly from the CLI after applying migrations. They do not run during
startup, have no automatic history, and must be safe to rerun.

## Run a seeder

List the registered operations:

```sh
go run ./cmd/cli seed
```

The shipped modules currently register no seeders. The Catalog operation below is
an example you add before invoking it:

```sh
go run ./cmd/cli seed catalog.defaults
go run ./cmd/cli seed catalog.defaults --timeout=10m
```

The default total deadline is five minutes. Seeders receive the command context
and its logger. CLI construction opens configured dependencies; executing a seeder
does not start HTTP, queue workers, or event processing.

## Register a service operation

Extend the [Catalog products tutorial](database-queries.md) with a
`SeedDefaults(ctx context.Context) error` method on its `ProductService` interface
in `internal/modules/catalog/service/service.go`. Construct that service with its existing
repository, then register the bound method in `internal/modules/catalog/register.go`:

```go
// Inside Register; products is the constructed ProductService.
if targets.Seeders != nil {
    if err := targets.Seeders.Register("catalog.defaults", products.SeedDefaults); err != nil {
        return err
    }
}
```

`targets` is `dependencies.Targets`. It carries a registry, not a database. The
service already has the repository it needs; do not reach into app resources when
the command runs. Registration installs the function without executing it.

Use module-qualified names. Valid names match `[a-z][a-z0-9_.-]{0,127}` and must be
unique. A nil function or duplicate name is rejected during registration.

## Insert defaults safely

Keep the SQL in the repository. Add `EnsureDefaults(ctx context.Context) error` to
Catalog's `ProductRepository` and this method to
`internal/modules/catalog/repository/product.go` from the query tutorial:

```go
func (r *productRepository) EnsureDefaults(ctx context.Context) error {
    executor, err := database.Executor(ctx, r.db)
    if err != nil {
        return err
    }
    _, err = executor.NewRaw(
        "INSERT INTO products (id, name) VALUES (?, ?) ON CONFLICT (id) DO NOTHING",
        "starter", "Starter product",
    ).Exec(ctx)
    return err
}
```

The file already imports `context` and `github.com/wisalgroup/labena/pkg/database`. This PostgreSQL
statement preserves an existing product. Its primary key makes concurrent runs
converge on one row. Apply the tutorial's products migration first.

The service operation delegates to its repository:

```go
// In internal/modules/catalog/service/product.go; s.products is ProductRepository.
func (s *productService) SeedDefaults(ctx context.Context) error {
    if err := s.products.EnsureDefaults(ctx); err != nil {
        return err
    }
    logging.FromContext(ctx).InfoContext(ctx, "catalog defaults ensured")
    return nil
}
```

Import `context` and `github.com/wisalgroup/labena/pkg/logging` in that service file. This method
illustrates an addition to the proposed service, not an existing shipped type.
Do not log the seeded payload if it contains sensitive data.

For an independently ported MySQL application, use the corresponding statement:

```sql
INSERT INTO products (id, name) VALUES (?, ?)
ON DUPLICATE KEY UPDATE id = id
```

The installed modules and tutorial use PostgreSQL. Choose the SQL for your actual
engine; changing the connection driver does not port the schema.

### Choose a rerun policy

| Desired behavior | Pattern |
| --- | --- |
| Insert missing defaults only | Stable key plus conflict no-op |
| Update selected system-owned values | Explicit upsert of those columns |
| Preserve user-edited values | Never overwrite them through a broad upsert |
| Detect unexpected existing state | Read/validate and return a clear failure |

Do not assume “seed” means destructive replacement. Document the policy in the
operation and test a second invocation against existing data.

## Run several seeders

Once both example operations are registered:

```sh
go run ./cmd/cli seed catalog.defaults catalog.prices
```

The CLI validates the whole selection first. Unknown or duplicate names fail
before any selected operation runs. Valid selections execute sequentially in the
requested order and stop on the first error or cancellation.

Previously completed operations remain completed. There is no automatic all-seeders
selection, retry, rollback, or once-only tracking. If order matters, make it
explicit in the command or in one orchestrating operation.

## Own transaction boundaries

A seeder with several dependent writes should use its injected transaction manager:

```go
return s.transactions.Execute(ctx, func(ctx context.Context) error {
    if err := s.products.EnsureDefaults(ctx); err != nil {
        return err
    }
    return s.prices.EnsureDefaults(ctx)
})
```

This fragment assumes two repository operations using `database.Executor` with
the same pool and callback context. The registry does not create a transaction for
you. Keep mail, raw queue submissions, and other external effects outside that
scope. Notification intent can use its documented SQL transaction path.

Stable keys and constraints make reruns predictable. Cancellation or a failed
commit can leave an uncertain outcome; inspect durable state before retrying work
with external effects. See [transactions](database-transactions.md).

## Verify and troubleshoot

A useful seeder test applies migrations to a disposable database, runs the seeder
twice, and asserts the intended final rows and preserved user data. Put business
coverage in `tests/catalog`, not the service or repository package.

| Symptom | Check |
| --- | --- |
| Seeder does not appear in `seed` | Module registration and non-nil `Targets.Seeders` |
| Duplicate registration | The name is global; qualify it with the module |
| Missing table | Apply migrations before running seeders |
| Earlier seeder persisted after later failure | Selection is sequential, not one automatic transaction |
| Deadline expired | Batch size, query context propagation, and the total command timeout |

Continue with [console commands](console.md), [migrations](migrations.md), and
[database testing](testing-database.md).
