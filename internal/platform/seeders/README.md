# Seeders

A seeder is an explicitly invoked operation that inserts or updates initial data.
It is separate from a migration and has no automatic version history. Modules
register named functions inside the module's Register function; registration never runs them.

```go
return deps.Seeders.Register("catalog.defaults", svc.SeedDefaults)
```

```sh
go run ./cmd/cli seed                         # List names; run nothing
go run ./cmd/cli seed catalog.defaults        # After implementing/registering it
go run ./cmd/cli seed catalog.defaults catalog.prices --timeout=10m
```

These are illustrative operations, not shipped seeders. Implement and register
them before use. Without a name registered,
selection fails before any seeder runs. The default total deadline is five minutes.
Selection is validated completely, then runs sequentially in the requested order.
Duplicate or unknown selections fail; execution stops on cancellation or first error.
Previously completed seeders stay completed. There is no automatic retry, rollback,
all-seeders default, or execution during migrations or application startup.

Use stable keys, uniqueness constraints and appropriate upserts to make real seeders
safe to rerun. Each seeder owns its required transaction scope. Keep external effects
outside transactions and do not assume a retry is safe after an uncertain commit.
Honor context cancellation, use logging.FromContext, and join work before returning.
The registry owns no connections or goroutines and does not close borrowed clients.
