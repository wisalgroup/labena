# Entities and queries

Keep Bun table mappings in `internal/modules/shared/entities` and queries in the owning module's repository. The owning module constructs the repository once and injects it into its service.

Extend the database-free Catalog starter when it needs persisted products. The
following entity and repository are tutorial additions, not shipped Product types. Add its [migration](migrations.html#write-a-business-migration) before executing queries. It does not change the database-free Catalog greeting example.

## Define an entity

Create `internal/modules/shared/entities/product.go`:

```go
package entities

import "github.com/uptrace/bun"

// Product is owned by the catalog module.
type Product struct {
	bun.BaseModel `bun:"table:products,alias:p"`
	ID            string `bun:"id,pk"`
	Name          string `bun:"name,notnull"`
}
```

Model tags map fields; they do not create schema. Keep important invariants in database constraints and service validation. This example uses application-assigned string IDs and a non-null name.

The entity carries its Bun mapping and business state together. Keep HTTP
request/response shapes in transports. Introduce a private persistence projection
only when the shapes differ; a parallel model layer is not the default. See
[project conventions](conventions.html) and [shared types](shared-types.html).

## Create a repository

Create the public contract in `internal/modules/catalog/repository/repository.go`:

```go
package repository

import (
	"context"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/pkg/pagination"
)

type ProductRepository interface {
	Create(ctx context.Context, product *entities.Product) error
	Get(ctx context.Context, id string) (*entities.Product, error)
	List(ctx context.Context, page pagination.Pagination) ([]entities.Product, int, error)
}
```

Put the private implementation and its constructor in
`internal/modules/catalog/repository/product.go`:

```go
package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/pagination"
)

var ErrProductNotFound = apperror.NotFound("The product was not found.")

type productRepository struct {
	db *bun.DB
}

func NewProductRepository(db *bun.DB) (ProductRepository, error) {
	if db == nil {
		return nil, errors.New("catalog: database is required")
	}
	return &productRepository{db: db}, nil
}

func (r *productRepository) Create(ctx context.Context, product *entities.Product) error {
	if product == nil {
		return errors.New("catalog: product is required")
	}
	executor, err := database.Executor(ctx, r.db)
	if err != nil {
		return err
	}
	_, err = executor.NewInsert().Model(product).Exec(ctx)
	return err
}

func (r *productRepository) Get(ctx context.Context, id string) (*entities.Product, error) {
	executor, err := database.Executor(ctx, r.db)
	if err != nil {
		return nil, err
	}
	product := new(entities.Product)
	err = executor.NewSelect().Model(product).Where("p.id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProductNotFound
	}
	if err != nil {
		return nil, err
	}
	return product, nil
}

func (r *productRepository) List(ctx context.Context, page pagination.Pagination) ([]entities.Product, int, error) {
	executor, err := database.Executor(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	page = pagination.NewPagination(page.Page, page.PerPage)
	products := make([]entities.Product, 0)
	total, err := executor.NewSelect().Model(&products).
		OrderExpr("p.id ASC").Limit(page.PerPage).Offset(page.Offset()).
		ScanAndCount(ctx)
	if err != nil {
		return nil, 0, err
	}
	return products, total, nil
}
```

All three operations resolve the executor from context. They work outside a transaction and join the caller's transaction when one exists. Calling `r.db.NewInsert()` directly would bypass that scope.

`Get` translates a missing row into a module error. Other errors remain available to the caller. Service operations should validate IDs and names before writing; repositories do not replace business validation.

`ErrProductNotFound` is a safe public application error owned by this repository.
If several Catalog behaviors share it, place the sentinel in the module's domain
package and reuse it. Do not replace raw database errors with public validation
messages. See [error ownership](errors.md#define-errors-beside-their-owner).

## Return a page

`List` receives `pagination.Pagination` and returns rows plus the total. The service
constructs the standard page; the handler converts entities into public responses:

```go
// Inside the proposed product service; s.products is ProductRepository.
items, total, err := s.products.List(ctx, page)
if err != nil {
    return nil, err
}
return pagination.NewPage(items, page, total), nil
```

Normalize programmatic input with `pagination.NewPagination` and use deterministic
ordering. This example orders by unique product ID, following database collation,
not creation time. For chronological results, use a timestamp plus unique ID and
an index matching that order.

`ScanAndCount` reads rows and count separately. Concurrent writes can change the
total between them; a page is not automatically a consistent snapshot. Stronger
snapshot requirements need a deliberately chosen transaction/isolation policy.
Deep offsets can also become expensive; measure actual query plans.

For cursor-based traversal, define a separate operation with a stable ordering and
an explicit cursor contract. The equivalent query fragment for ID ordering is:

```go
query := executor.NewSelect().Model(&products).OrderExpr("p.id ASC").Limit(limit)
if after != "" {
    query.Where("p.id > ?", after)
}
if err := query.Scan(ctx); err != nil {
    return nil, err
}
```

Here `limit` is validated to 1–100, `after` is the previous final ID, and the method
returns the bounded slice. It does not provide a total count or replace the
framework's `pagination.Pagination` convention for normal list endpoints.


## Update selected fields

Add this method to `product.go` and add its signature to `ProductRepository`
when the service needs this operation:

```go
func (r *productRepository) Rename(ctx context.Context, id, name string) error {
    executor, err := database.Executor(ctx, r.db)
    if err != nil {
        return err
    }
    _, err = executor.NewUpdate().Model((*entities.Product)(nil)).
        Set("name = ?", name).Where("id = ?", id).Exec(ctx)
    return err
}
```

This method updates only the name. Its contract does not distinguish a missing row from a successful update with no changes. If that distinction matters, design it explicitly: affected-row behavior for unchanged values differs by driver settings. Do not assume every zero count means “not found.”

For concurrent state transitions, include the expected state or version in the update condition and define what a failed condition means. A read followed by an unconditional update is not a concurrency guarantee.

## Delete by identity

Add `Delete(context.Context, string) error` to the interface if this operation is
needed, and put the implementation in `product.go`.

```go
func (r *productRepository) Delete(ctx context.Context, id string) error {
    executor, err := database.Executor(ctx, r.db)
    if err != nil {
        return err
    }
    _, err = executor.NewDelete().Model((*entities.Product)(nil)).
        Where("id = ?", id).Exec(ctx)
    return err
}
```

This is a hard delete; a missing row is a successful no-op. The service must decide whether deletion is allowed. Foreign-key constraints remain authoritative. Add soft deletion only when the product needs its retention and query rules.

## Load relationships deliberately

After defining a Bun relationship field named `Items` on a model, a query can load it with:

```go
err := executor.NewSelect().Model(&order).
    Relation("Items").Where("o.id = ?", id).Scan(ctx)
```

This fragment requires an Order mapping with alias `o`, the relation tags, and corresponding schema; the sample project does not contain those persisted models. Load only the relationships an operation needs. Inspect query count and size; avoid issuing a relation query for every result in a loop.

Cross-module joins stay in deliberate repository queries. Central model definitions do not grant permission to write another module's data.

## Raw SQL and dynamic input

Use the same executor for raw SQL inside a transaction:

```go
var count int
err := executor.NewRaw("SELECT count(*) FROM products WHERE name = ?", name).
    Scan(ctx, &count)
```

Bind values through placeholders. Choose table names, sort columns, and directions from an allowlist; do not concatenate request input into SQL. Keep result sets bounded and add indexes for actual filters and ordering.

## Construct once and reuse

In Catalog's `New`, borrow `Resources.DB`, construct `NewProductRepository`, and
pass its interface to `NewProductService`. The service holds that repository, not
the entire resource bundle. Neither package opens or closes the shared pool.
See [dependency injection](dependency-injection.md) for the construction pattern.

## Verify queries and diagnose failures

Test repository behavior against the actual database engine in `tests/catalog`:
create/get, missing records, deterministic pages, update/delete semantics, and
joining a transaction that rolls back. Keep tests out of the business repository
package itself.

| Symptom | Check |
| --- | --- |
| Relation or column missing | Migration applied to the same database used by the application |
| Writes survive an expected rollback | Every query uses `database.Executor` and the callback context |
| A changed row appears on two pages | Stable ordering and concurrent-write expectations |
| A query slows as data grows | Bound results, indexes, query plan, and relation query count |
| Driver details leak to clients | Return wrapped operational errors; translate only at the boundary |

Use [request SQL traces](observability.html#inspect-a-requests-sql-in-grafana) to
inspect sanitized query structure. Continue with [transactions](database-transactions.md),
[migrations](migrations.md), and [database tests](testing-database.md).
