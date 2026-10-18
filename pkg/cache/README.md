# Cache

`pkg/cache` provides raw byte operations and JSON convenience methods
through one `cache.Store` contract. Memory, database, and Redis drivers implement it. Business modules receive
`cache.Store` directly; they do not open connections or look up the app instance.
`internal/app` owns configured connections and cleanup. Connection setup
stays in `pkg/database` and `pkg/redis`.

## Use a cache

```go
if err := store.Set(ctx, "product:42", encodedProduct, 5*time.Minute); err != nil {
    return err
}
value, found, err := store.Get(ctx, "product:42")
if err != nil {
    return err // The backend failed; this is not a cache miss.
}
if found {
    // Decode value. It belongs to the caller and may be changed safely.
}
err = store.Delete(ctx, "product:42") // Missing keys are fine.
```

`Get` and `Set` use raw bytes and keep their existing encoding contract. Nil and
empty values are valid cached entries; `found` distinguishes them from a miss.
Keys must contain 1–255 UTF-8 bytes without NUL, consistently across every driver.
Keys are case-sensitive and trailing spaces are significant. Include tenant and
schema/version identifiers in business keys when needed.

`Set` replaces both value and expiry atomically. TTL must be positive: zero does
not mean forever. Remote drivers round TTL up to milliseconds; memory uses Go's
clock precision. Expired values are misses. `Get` does not extend TTL. All stores
support concurrent use and copy any data they retain. Do not mutate an input slice
while its call is running.

The common errors are `ErrInvalidKey`, `ErrInvalidTTL`, `ErrTooLarge`, memory's
`ErrFull`, memory's `ErrClosed`, and `ErrInvalidValue` for invalid JSON. Use `errors.Is`. Other backend errors retain
their causes; a timeout on a write can leave an uncertain result. Set/Delete do not
implement compare-and-swap, counters, locks, or distributed uniqueness.

## Convenience operations

Drivers provide storage operations through `deps.Cache`. Typed loading is a
package function, with the result type inferred from the loader. `Product` below
is your application's JSON-compatible type.

```go
exists, err := store.Exists(ctx, "product:42")

err = cache.SetMulti(ctx, store, map[string]any{
    "product:42": map[string]any{"name": "Notebook", "price": 1200},
    "feature:enabled": true,
}, 5*time.Minute)
values, err := cache.GetMulti[any](ctx, store, []string{"product:42", "feature:enabled"})

value, err := cache.Remember(ctx, store, "product:42", 5*time.Minute,
    func(ctx context.Context) (Product, error) {
        return loadProduct(ctx)
    })


// Administrative invalidation: clears this store/namespace, keeps it usable.
err = store.Clear(ctx)
```

Handle each error before using its result. `Exists` checks presence without
copying or decoding the value; expired entries are absent. It is only a snapshot,
not a lock or an atomic check-and-set operation.

**Encoding:** Store.GetMulti/SetMulti read and write raw bytes. The package
functions cache.GetMulti[T]/SetMulti[T] handle JSON separately. For T=any,
objects are `map[string]any`, arrays are `[]any`, and numbers are `json.Number`.
Typed `cache.Remember` decodes both the loaded and cached value into the loader's
return type, so structs and integer precision are preserved. `Remember[any]`
retains the dynamic behavior. JSON null follows the destination type's normal
decoding rules. Use pointer types when absence must remain distinct from a zero
value. Values must round-trip through JSON; use raw Get/Set for other encodings.

Raw Get can read the encoded bytes written by these helpers. Raw Set values used
with a JSON helper must contain one valid JSON value; malformed data returns
`ErrInvalidValue` rather than triggering a reload. Existing raw data needs no
migration unless you start accessing those same keys with a JSON helper.

**Bulk calls:** up to 1,000 input keys/items and 8 MiB of encoded key/value data
per call, also subject to each driver's per-entry limit. GetMulti omits misses,
retains empty-byte hits (or JSON null through the helper), collapses duplicate keys, and returns an empty non-nil map
for no hits. On error it returns no result. Results are not a cross-key snapshot.
The byte limit is a logical payload limit, not an allocation or RSS guarantee;
JSON decoding and remote-client buffers need additional memory.

The JSON SetMulti helper encodes the complete batch before calling the driver.
The raw driver validates all keys, sizes and TTL before any writes. Invalid keys,
TTLs, unsupported values and size violations therefore write nothing. After
validation, runtime errors can leave partial writes: memory admits sorted keys
in order, Redis pipelines atomic per-key SET-with-TTL commands, and SQL uses one
upsert statement through the transaction executor. There is no portable batch
transaction or rollback promise. Empty batches are no-ops after validation.

**Loading:** Remember calls the closure only on a miss, passes its context, and
starts the TTL when the result is stored. Cache errors never trigger the loader.
Loader errors, invalid JSON values, cancellation and cache-write errors propagate;
failed loads are not cached. Closures run synchronously and must honor cancellation.
Concurrent misses may invoke the closure more than once: this is not singleflight
or a distributed lock. A fill racing Delete/Clear can repopulate a key; use a
module-specific consistency policy or versioned keys where that matters.

Hits retain the existing TTL. Every write requires an explicit positive TTL;
no-expiry entries are unsupported. `cache.MaxTTL` is a finite upper bound, not
permanent storage. The misleading Forever convenience method was removed.
Clear, Delete, memory restarts and remote eviction can still remove entries.

**Clear:** memory clears one instance atomically; SQL deletes one namespace using
the caller's transaction; Redis scans the escaped namespace prefix and unlinks
keys in batches across all cluster primaries. Redis never uses FLUSHDB/FLUSHALL.
Remote operations have the configured timeout; errors can leave an uncertain or
partial result. Stop writers for a complete administrative clear. Concurrent
writes, loads or Redis topology changes can survive the traversal. A retry is
safe for cache data but may also remove newly written entries.

Adding these methods expands `cache.Store`; custom implementations and test fakes
must implement them. Driver constructors and raw Get/Set/Delete signatures remain
unchanged. Telemetry covers Exists/Clear/bulk operations and the Get/Set calls
inside loading helpers without recording keys or payloads.

## Application lifecycle

The app already registers each resource immediately after acquisition. Its
shutdown drains app work, then closes resources once in reverse order and returns
cleanup errors. Memory's original store is registered even when telemetry wraps
it. The app-owned Redis client is closed once; the SQL cache borrows the app's
Bun pool, which the app closes once after all its users stop. Initialization
failures use the same cleanup path. `Clear` never closes any resource.

Modules receive a borrowed `cache.Store` and must not close its backing client.
Standalone callers own the memory store or the remote client they opened, as in
[the example](../../examples/cache/main.go). No loading or cleanup goroutine is
started by the cache, so there is no extra lifecycle component to register.

## Choose a driver

| Driver | Constructor | Ownership and expiry |
| --- | --- | --- |
| `memory` | `memory.New(capacity, maxEntryBytes)` | One process; caller closes the store; lazy expiry |
| `database` | `cachedb.New(db, cachedb.Config{})` | Caller owns `*bun.DB`; expiry checked by server; explicit cleanup |
| `redis` | `cacheredis.New(client, cacheredis.Config{})` | Caller owns `redis.UniversalClient`; native Redis TTL |

Use aliases `cachedb` and `cacheredis` to distinguish the cache drivers from the
connection packages. Constructors start no goroutines, create no SQL schema, and
perform no connectivity check. Open a ready client through the connection package
before constructing a remote store. Remote stores have no `Close` method because
they own no connections or background tasks. Stop their users before closing clients.

The memory driver limits entry count, key-plus-value bytes per entry, and total
retained key-plus-value bytes. `New(capacity, maxEntryBytes)` keeps usage simple
and applies a **64 MiB total byte budget**. For a different budget:

```go
store, err := memory.NewWithConfig(memory.Config{
    Capacity: 10000,
    MaxEntryBytes: 1 << 20,
    MaxBytes: 128 << 20,
})
```

This introduces a tighter default than the previous entry-count-only limit.
Applications retaining more than 64 MiB must explicitly raise
`CACHE_MEMORY_MAX_BYTES` (or `Config.MaxBytes`). A single entry exceeding either
byte limit returns `ErrTooLarge`; exhausting total bytes or entry count returns
`ErrFull`. A failed replacement preserves the previous value and TTL. Updates
at entry capacity remain allowed if the byte budget permits them. Live entries
are never evicted. Treat admission failures as a caching decision, not evidence
that authoritative data was saved or lost.

The byte budget counts logical key/value lengths, not total heap or RSS. Map
metadata, allocator overhead, in-flight reads, and caller buffers require memory
headroom. Keys are copied as well as values, preventing short substring keys from
retaining larger caller allocations. Reads share a read lock and copy immutable
values outside it. Ordinary Get still allocates caller-owned bytes on nonempty
hits; an optional concrete-memory API supports buffer reuse:

```go
buf, found, err := store.GetInto(ctx, key, buf[:0])
```

GetInto appends to the supplied buffer and never retains it. Misses and errors
leave it unchanged; check `found` before using the contents. Each concurrent
caller must own its buffer. This method is not part of `cache.Store` and direct
calls do not pass through the optional telemetry wrapper.

Expiry is checked on reads. Admission reclaims expired entries when needed, but
skips the scan if the earliest possible expiry is still in the future. No
background goroutine starts. For idle caches, an explicitly owned maintenance
job can call `store.PruneExpired(ctx, 1000)`. It removes at most that many expired
entries, checks cancellation during scanning, and may return partial progress
with an error. A scan can inspect the whole map under the write lock: use modest
capacities and measure latency if expirations or admission failures are frequent.

`store.Usage()` reports retained entries, logical bytes, configured limits, and
closed state without exposing keys. Expired entries awaiting cleanup count toward
usage. Existing telemetry reports operation outcomes and durations. Each instance
is isolated. `Close(ctx)` is idempotent and releases its map and values; stop users
first if in-flight reads must finish before shutdown.

Remote Config fields:

| Field | Default | Meaning |
| --- | --- | --- |
| `Namespace` | `default` | Store isolation; same key rules, with colon additionally forbidden |
| `MaxEntryBytes` | 1048576 (1 MiB) | Maximum key + value bytes accepted by Set |
| `OperationTimeout` | 5s | Upper bound for each operation; caller deadlines can be earlier |
| `Table` | `cache_entries` | Database only; lowercase bare identifier up to 40 characters |

Namespace does not provide access control. Choose distinct namespaces per app and
environment. Remote caches have no per-store entry-count limit; configure backend
capacity and monitor usage. The byte limit bounds writes through this store, not
records written directly by other tools.

## Database: PostgreSQL and MySQL

```go
db, err := database.Open(ctx, cfg.Database.ConnectionConfig()) // github.com/wisalgroup/labena/pkg/database
if err != nil { return err }
defer db.Close()
store, err := cachedb.New(db, cachedb.Config{Namespace: "catalog"})
```

The driver uses Bun's PostgreSQL or MySQL dialect automatically. MySQL requires
8.4+ with InnoDB and the UTC connection settings supplied by `pkg/database`. SQLite
is not supported by this cache driver. SQL uses the context logging hooks already
installed by `pkg/database`; query values are not included in those logs.

Publish with `go run ./cmd/cli cache tables`, then apply the central
[Goose migrations](../../internal/modules/shared/migrations/README.md) before using the driver. SQL is
selected by DATABASE_DRIVER and published directly into `internal/modules/shared/migrations/`. The
`cache_entries` table has four columns: namespace, cache_key, value, and expires_at.
Its primary key is `(namespace, cache_key)`; its expiry index supports bounded
namespace cleanup. PostgreSQL uses BYTEA and TIMESTAMPTZ; MySQL uses binary keys,
LONGBLOB and UTC DATETIME(6). Values can contain arbitrary bytes. This table belongs
to cache. Asynq jobs use Redis and have no SQL queue schema.

All SQL reads, writes and Clear use `database.Executor(ctx, db)`. They automatically participate
in a transaction for the exact same Bun instance. Rollback undoes cache writes and
deletes. TTL starts when Set executes, not at commit; a long transaction can commit
an already-expired entry. This SQL-only feature is not a Redis/memory guarantee.

Expired rows stay in storage until cleanup; they cannot be read as hits. Run this
explicitly from a maintenance command or scheduled job, outside business transactions:

```go
removed, err := store.PruneExpired(ctx, 1000)
```

Each call removes at most the requested limit (1–10000), in the store's namespace.
It uses a bounded read-committed transaction and `FOR UPDATE SKIP LOCKED`, so it
can run alongside other cleaners and writers without deleting a refreshed value.
Locked rows can remain for the next pass; zero deletions do not prove the entire
table is empty. No cleanup worker starts automatically. `cache tables` respects
CACHE_DATABASE_TABLE and preserves existing published files. Changing the setting
after deployment does not rename or migrate an existing table.

## Redis

```go
client, err := redis.OpenURL(ctx, cfg.Cache.RedisURL, 5*time.Second) // github.com/wisalgroup/labena/pkg/redis
if err != nil { return err }
defer client.Close()
store, err := cacheredis.New(client, cacheredis.Config{Namespace: "catalog"})
```

Redis keys are `cache:<namespace>:<key>`. Set uses one `SET ... PX` command to store
value and TTL together; no separate expiry write can fail and leave an immortal key.
Only `redis.Nil` becomes a miss. Authentication, timeouts, wrong-type errors, and
connection failures stay errors. Ordinary reads and writes do not scan or use Lua scripts. Explicit Clear uses
SCAN and UNLINK; there are no global flushes or background goroutines.

`pkg/redis.Open` accepts native standalone, Sentinel, or Cluster options;
`OpenURL` is the standalone URL helper used by app configuration. Its clients enable
context-aware I/O. A directly supplied client must enable equivalent deadline
handling. Bulk operations pipeline single-key commands and work without cross-slot transactions.
Clear traverses each cluster primary; verify ACLs allow SCAN and UNLINK.
Configure Redis maxmemory and eviction policy for cache workloads; avoid evicting
queue/event data when sharing a Redis deployment. Redis may evict an entry before
its TTL, depending on that policy.

## Application configuration

The central env-tag schema owns every setting; no driver reads environment variables.

```dotenv
CACHE_DRIVER=disabled
CACHE_NAMESPACE=default
CACHE_MAX_ENTRY_BYTES=1048576
CACHE_MEMORY_CAPACITY=10000
CACHE_MEMORY_MAX_BYTES=67108864
CACHE_REDIS_URL=redis://127.0.0.1:6379/0
CACHE_DATABASE_TABLE=cache_entries
CACHE_OPERATION_TIMEOUT=5s
```

Select `memory`, `database`, or `redis` to enable it. Disabled leaves `a.cache` nil
and opens no cache connection. Database cache borrows the application's database;
choose PostgreSQL/MySQL with DATABASE_DRIVER and DATABASE_URL. Redis cache uses its
own caller-owned client. All entry points, including console, get the same configured
dependency. Pass `resources.Cache` to module assembly and specific cache collaborators to services only where needed;
modules requiring a cache should reject nil explicitly. No example business module
is forced to cache data it does not need.

For portable business workflows, commit authoritative data before invalidating
Redis/memory entries. Transactional database cache updates can share the commit.
This package does not solve stale fill/invalidation races or cache stampedes. Pick
a consistency policy in the module; do not use a cache as the only source of truth.
Log failures once at the responsible application boundary with `pkg/logging` and
the request context; avoid logging cached payloads or sensitive keys.

## Example and verification

[One-file example](../../examples/cache/main.go):

```sh
CACHE_DRIVER=memory go run ./examples/cache
CACHE_DRIVER=redis go run ./examples/cache
# Set DATABASE_URL and DATABASE_DRIVER first for the database example.
CACHE_DRIVER=database go run ./cmd/cli cache tables
CACHE_DRIVER=database go run ./cmd/cli migrate up
CACHE_DRIVER=database go run ./examples/cache
```

Use disposable PostgreSQL, MySQL and Redis services, then set the test URLs:

```sh
export DATABASE_TEST_POSTGRES='postgres://queues:queues@127.0.0.1:15432/queues?sslmode=disable'
export DATABASE_TEST_MYSQL='root:queues@tcp(127.0.0.1:13306)/queues'
# Optional comma-separated Redis Cluster addresses:
# export CACHE_TEST_REDIS_CLUSTER=127.0.0.1:26380,127.0.0.1:26381,127.0.0.1:26382
export CACHE_TEST_REDIS='redis://127.0.0.1:16379/0'
go test -race ./pkg/cache/... ./internal/app ./pkg/database/migrate ./internal/platform/testdb
```

Backend tests skip when the corresponding variable is absent. SQL fixtures create
and remove isolated schemas/databases; use disposable services and test accounts.
The shared contract covers binary/empty values, expiry, refresh, key identity,
copy ownership, invalid input, cancellation, and atomic concurrent writes. Driver
checks cover native TTL, error-vs-miss behavior, namespaces, transaction rollback,
locked-row cleanup and operation deadlines.

Native references: [Redis SCAN semantics](https://redis.io/docs/latest/commands/scan/),
[Redis pipelining](https://redis.io/docs/latest/develop/using-commands/pipelining/),
[Redis SET](https://redis.io/docs/latest/commands/set/) and
[Bun drivers](https://bun.uptrace.dev/guide/drivers.html).

## Optional telemetry

`WithTelemetry(store, tracerProvider, meterProvider)` borrows the store and providers.
App applies it when tracing or metrics are enabled; no-op providers disable either
signal independently. It records fixed operations and hit/miss/error outcomes without
keys or values. Close the original store/client after users stop. See
[observability](../../docs/observability.md#redis-and-cache-operations).

## Memory performance verification

Run the fixed 1 KiB payload benchmarks on the deployment toolchain and hardware:

```sh
go test ./pkg/cache/memory -run '^$' -bench BenchmarkStore -benchmem -count=5
```

[Recorded measurements and tradeoffs](memory/BENCHMARKS.md) include the baseline.
The benchmarks cover reads, parallel reads, buffer reuse, replacement, and rejected writes
at 100 and 10,000 entries. These are closed-loop microbenchmarks, not service p99
or a comparison against community caches. For deployment sizing, measure realistic
payloads, concurrency, expiration rate, hit ratio, GC CPU, live/scannable heap and
request latency. This driver remains a Go map, not a pointer-free arena cache.
No global GC tuning or pooled payload retention is introduced.

Backend contract and database integration tests live in `internal/platform/testdb`
because they use application migration fixtures. The cache packages and their
package-local tests have no dependency on `internal`.


### Store and typed loading

Use `store.Remember(ctx, key, ttl, load)` for dynamic JSON values, or
`cache.Remember(ctx, store, key, ttl, load)` with a typed loader for a typed result.
Go methods cannot introduce type parameters, so the store method accepts
`func(context.Context) (any, error)` and returns `(any, error)`. Both forms share
one implementation and the same TTL, validation and error behavior.

```go
value, err := store.Remember(ctx, "settings:v1", 5*time.Minute,
    func(ctx context.Context) (any, error) {
        return map[string]any{"theme": "dark"}, nil
    })
```

Objects return as `map[string]any`, arrays as `[]any`, and numbers as `json.Number`
on both hits and misses. Use the typed helper to retain a struct type.
Remove calls to `store.Forever` and choose an explicit retention duration.

The JSON encoding and existing cached bytes are unchanged. Version keys when
changing a value's type or representation. Cache corruption and type mismatches
return errors without invoking the loader. Loading through a telemetry-wrapped
store still records its Get/Set operations; driver methods delegate to the shared helper.


## Raw and typed batches

The store contract uses `map[string][]byte` for both batch reads and writes:

```go
err := store.SetMulti(ctx, map[string][]byte{
    "binary:v1": {0, 255, 128},
    "empty:v1": nil,
}, time.Minute)
raw, err := store.GetMulti(ctx, []string{"binary:v1", "empty:v1"})
```

An empty or nil byte value is a present empty value, never deletion. Missing keys
are omitted. Returned bytes belong to the caller; keep input maps and slices
unchanged until writes return.

For JSON, use package functions rather than store methods:

```go
err := cache.SetMulti(ctx, store, map[string]Product{"product:v1:42": product}, time.Minute)
products, err := cache.GetMulti[Product](ctx, store, []string{"product:v1:42"})
```

Here Product and product are your application type/value. Both helpers use the
driver's native batch operation. Errors return no partial read result, and JSON
decoding errors do not mutate stored data. Changing to these helpers preserves
the previous JSON encoding; no existing data rewrite is needed. Raw methods do
not JSON-encode byte slices (unlike passing bytes to a JSON helper).

This is a source-breaking method-signature change: callers wanting the former
dynamic JSON behavior use `cache.GetMulti[any]` and `cache.SetMulti` with their
existing map[string]any. Driver interfaces contain no JSON loading or encoding
methods. Telemetry describes raw storage operations; serialization failures are
returned to the caller, outside storage-operation spans.

## Redis read budgets

Redis Get checks the entry size atomically with the read using a single-key Lua
script. GetMulti reads bounded pipelines and stops when the aggregate result would
exceed 8 MiB. A pipeline returns at most another 8 MiB of value data while previously
accepted results remain retained; protocol overhead, byte copies and Go allocations
are additional. Oversized legacy/external values return ErrTooLarge without fetching
their bodies. Redis credentials must permit EVAL, GET and STRLEN as well as the other
cache commands. Scripts use one key each and retain cluster-slot compatibility.
