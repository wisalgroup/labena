# Cache

Cache keeps replaceable data close to an operation. Inject `cache.Store` into the
service that needs it, choose a finite TTL, and keep authoritative data in its
owning repository. Start with `cache.Remember` to load a value only when absent.

## Configure a driver

For one-process development:

```dotenv
CACHE_DRIVER=memory
```

For shared cache across application replicas:

```dotenv
CACHE_DRIVER=redis
CACHE_REDIS_URL=redis://127.0.0.1:6379/0
CACHE_NAMESPACE=myapp
```

The code default is `disabled`, which leaves `Resources.Cache` nil. The example
environment selects Redis. Required cache dependencies should be checked during
service construction; optional caching needs an explicit bypass path.

| Driver | Scope and capacity | Expiration and ownership |
| --- | --- | --- |
| `memory` | One process; bounded count and total bytes; rejects full writes | Lazy expiration; app owns and closes the store |
| `redis` | Shared backend; capacity and eviction are backend policy | Native TTL; store borrows a Redis client |
| `database` | Shared PostgreSQL/MySQL table | Expired rows are unreadable; explicit cleanup reclaims them; pool is borrowed |

Changing a driver does not transfer cached values. Namespaces separate keys, not
permissions. Do not cache secrets or cross-tenant results without a deliberate
access and retention policy.

## Load on a miss and work with multiple values

The two loading forms share the same behavior:

| Need | API | Return value |
| --- | --- | --- |
| Preserve a known Go result type | `cache.Remember(ctx, store, key, ttl, load)` | Your loader's type |
| Work with dynamic JSON | `store.Remember(ctx, key, ttl, load)` | `any` containing JSON objects, arrays, or scalars |

Batch operations are covered [below](#raw-and-typed-batches); use Remember first for
an ordinary single-resource lookup.

### Store and typed loading

This complete helper caches a transport-independent product summary. Place it in
the service that owns the query and inject its store and loader:

```go
package service

import (
	"context"
	"time"

	"github.com/wisalgroup/labena/pkg/cache"
)

type ProductSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func CachedProduct(ctx context.Context, store cache.Store, id string,
	load func(context.Context) (ProductSummary, error),
) (ProductSummary, error) {
	return cache.Remember(ctx, store, "catalog:v1:product:"+id,
		5*time.Minute, load)
}
```

This proposed Catalog helper requires a non-nil store. The caller authorizes the
operation and chooses a key containing every dimension that changes the result,
such as tenant or locale. The loader reads authoritative data using its supplied
context. The first successful call loads and caches; later calls return the cached
summary until expiry or deletion.

The dynamic form uses the same cache path:

```go
value, err := store.Remember(ctx, "settings:v1", 5*time.Minute,
    func(ctx context.Context) (any, error) {
        return map[string]any{"theme": "dark"}, nil
    })
if err != nil {
    return err
}
// value is a map[string]any on both misses and hits.
_ = value
```

Dynamic objects become `map[string]any`, arrays become `[]any`, and numbers become
`json.Number`. The typed helper restores its concrete type. Both forms JSON-encode
and decode on a miss as well as a hit, producing caller-owned values with the same
shape on both paths. Values must round-trip through JSON.

### Understand Remember's guarantees

| Event | Behavior |
| --- | --- |
| Hit | Decode and return; keep the existing TTL |
| Miss | Call the loader, encode, then store; TTL starts after loading |
| Loader failure | Return it; do not cache the failure |
| Backend read failure or corrupt cached JSON | Return an error; do not call the loader |
| Encoding or write failure | Return an error, even if loading succeeded |
| Concurrent misses | More than one loader may run |
| Cancellation during loading | Loader should stop; canceled results are not published afterward |

There is no automatic stampede lock or stale-while-revalidate behavior. A loader
racing `Delete` or `Clear` can refill a key. Use versioned keys or an explicit
consistency strategy where this matters. Do not put irreversible work in a loader.

## Store, read, and delete

The base store reads and writes raw bytes. This fragment belongs in an operation
that already has a store and serialized `encodedProduct`:

```go
const key = "catalog:v1:product:42"
if err := store.Set(ctx, key, encodedProduct, 5*time.Minute); err != nil {
    return err
}
value, found, err := store.Get(ctx, key)
if err != nil {
    return err
}
if found {
    // Decode value using the format used by the writer.
    _ = value
}
return store.Delete(ctx, key)
```

Import `time`; the injected interface is `github.com/wisalgroup/labena/pkg/cache.Store`. Nil and empty
byte slices are valid entries, so use `found`, not `len(value)`, to recognize a hit.
Deleting a missing key succeeds.

`Set` atomically replaces one value and its expiry. TTL must be positive; zero does
not mean forever. `Get` does not extend TTL. Expired entries are misses, and remote
drivers round positive TTLs up to whole milliseconds.

Keys contain 1–255 UTF-8 bytes without NUL, are case-sensitive, and preserve trailing
spaces. The size limit includes key and value. Returned data belongs to the caller;
retained inputs are copied, but do not mutate slices or maps while a call is running.

### Check presence

```go
found, err := store.Exists(ctx, "catalog:v1:product:42")
if err != nil {
    return err
}
```

Use `found` for a presence-only operation. An `Exists` followed by `Set` is not an
atomic “create if absent” operation; the interface provides no uniqueness lock.

## Raw and typed batches

Use store methods for bytes:

```go
if err := store.SetMulti(ctx, map[string][]byte{
    "binary:v1": {0, 255, 128},
    "empty:v1":  nil,
}, time.Minute); err != nil {
    return err
}
raw, err := store.GetMulti(ctx, []string{"binary:v1", "empty:v1", "missing:v1"})
if err != nil {
    return err
}
// raw contains the two hits; missing keys are omitted.
_ = raw
```

Use package helpers for JSON:

```go
if err := cache.SetMulti(ctx, store, map[string]ProductSummary{
    "catalog:v1:product:42": {ID: "42", Name: "Starter product"},
}, time.Minute); err != nil {
    return err
}
products, err := cache.GetMulti[ProductSummary](ctx, store,
    []string{"catalog:v1:product:42"})
if err != nil {
    return err
}
_ = products
```

`ProductSummary` is the type defined above. `cache.GetMulti[any]` supports dynamic
JSON. Do not read arbitrary binary values with JSON helpers; incompatible or
corrupt data is an error, not a miss.

Batches accept at most **1,000 keys/items** and **8 MiB** of key/value data, subject
to each entry's size limit. Writes validate the batch first; a later backend
failure may leave partial writes. Reads are not a cross-key snapshot. A read or
JSON-decoding error returns no partial result and does not mutate cached entries.

## Invalidate after a write

Delete the relevant key after the authoritative write has committed. This optional
cache example runs outside any caller SQL transaction. A successful repository
call inside a joined transaction is not yet a commit; the outer operation must
invalidate only after its transaction completes.

```go
if err := s.products.Rename(ctx, id, name); err != nil {
    return err
}
if err := s.cache.Delete(ctx, "catalog:v1:product:"+id); err != nil {
    logging.FromContext(ctx).WarnContext(ctx, "product cache invalidation failed",
        "error_category", logging.ErrorCategory(err))
}
return nil
```

This proposed service fragment uses its repository and cache dependencies and
imports `github.com/wisalgroup/labena/pkg/logging`. It returns the committed result even if cache
invalidation fails. The cached value can remain stale until its TTL expires; the
earlier example bounds that lifetime to five minutes. Choose this policy only
when that stale window is acceptable. Stricter consistency needs an explicit
design; reporting an error cannot roll back an already committed SQL write.

Commit-before-invalidation alone does not eliminate stale-fill races. Version keys
when a type or representation changes, and use a deliberate policy for concurrent
writes and loaders. Cache is not the source of truth.

## Clear a namespace

```go
if err := store.Clear(ctx); err != nil {
    return err
}
```

This administrative operation keeps the store usable. Memory clears the instance;
Redis scans/unlinks matching namespace keys across primaries; SQL deletes within
its namespace and current transaction. A remote error may leave a partial clear,
and concurrent writers can repopulate data. Quiesce writers when a complete clear
is required. `Clear` never closes a store or its borrowed connection.

## Capacity, timeouts, and tuning

The main defaults are:

| Setting | Default | Meaning |
| --- | --- | --- |
| `CACHE_NAMESPACE` | `default` | Remote key namespace |
| `CACHE_MAX_ENTRY_BYTES` | `1048576` | Key plus value limit |
| `CACHE_MEMORY_CAPACITY` | `10000` | Retained entry count |
| `CACHE_MEMORY_MAX_BYTES` | `67108864` | Retained key/value budget; 64 MiB |
| `CACHE_OPERATION_TIMEOUT` | `5s` | Remote operation bound |
| `CACHE_DATABASE_TABLE` | `cache_entries` | SQL table |

Only configure values you need to change. Process memory also includes map
metadata and in-flight copies. Memory returns `cache.ErrFull` instead of evicting
live entries; a rejected replacement leaves its old value intact. Expired entries
are reclaimed on reads or admission pressure, without a maintenance goroutine.

Concrete memory stores additionally expose `Usage()`, `PruneExpired(ctx, limit)`,
and `GetInto(ctx, key, buf[:0])` for explicit cleanup or buffer reuse. Portable
services should depend on `cache.Store` unless they need those controls.

Redis keys use `cache:<namespace>:<key>`. The URL-based app setup opens a standalone
client. `pkg/redis.Open` also supports explicit Sentinel/Cluster configuration for
independent integrations; changing a URL alone does not describe every cluster
topology. Backend eviction can remove entries before their TTL.

## Database cache

Set `CACHE_DRIVER=database` with the database driver and URL, then publish and apply
the cache table:

```sh
go run ./cmd/cli cache tables
go run ./cmd/cli migrate up
```

Expired rows remain stored until cleanup. With a concrete database cache store:

```go
removed, err := store.PruneExpired(ctx, 1000)
```

Handle `err` before using the deleted count. `PruneExpired` belongs to
`cache/database.Store`, not the portable interface. It removes a bounded batch in
that namespace; locked rows can remain for a later pass. Schedule maintenance
explicitly using the application's [central scheduling](scheduling.html).

Database cache operations resolve `database.Executor(ctx, pool)` and join a SQL
transaction using the same pool. Rollback undoes its writes and deletes. TTL starts
at `Set`, not commit. Memory and Redis do not share that transaction behavior.
See [transactions](database-transactions.md).

## Misses, failures, and invalidation

A backend failure is not a miss. Decide explicitly whether your service should
fail or fall back to authoritative storage. Remember itself propagates the error;
it does not silently fall back. Avoid logging cached payloads.

| Symptom | Check |
| --- | --- |
| Cache dependency is nil | `CACHE_DRIVER` and constructor validation |
| Every replica loads separately | The `memory` driver is process-local |
| `ErrInvalidTTL` | Supply a positive duration; no forever entries |
| `ErrFull` | Memory capacity/byte budget and expired-entry cleanup |
| `ErrTooLarge` | Key + value size and batch limits |
| `ErrInvalidValue` | Writer/reader encoding and key version agree |
| SQL table missing | Publish cache tables, rebuild, and apply migrations |
| Deleted value reappears | Concurrent loader or writer refilled it |

Use `errors.Is` with stable `cache.Err…` values. There are no portable counters,
locks, compare-and-swap operations, or atomic multi-key transactions in this store.

## Ownership and example

App owns resource cleanup once, after users stop. Remote stores borrow their
clients; modules must not close shared clients or pools. Constructors do not create
tables or start background work. Standalone code owns the resources it opens.

```sh
CACHE_DRIVER=memory go run ./examples/cache
```

The example loads central configuration, writes and reads an entry, and closes its
resources. Remote variants need the selected backend and, for SQL, applied
migrations. Test hit/miss behavior, expiry, loader failures, wrong encodings, and
invalidation outcomes against the driver your application actually uses.

Continue with [database queries](database-queries.md),
[transactions](database-transactions.md), and [testing](testing.md).
