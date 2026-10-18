# Hasher

`pkg/hasher` provides password hashing using Go's bcrypt with bounded CPU admission.
Create one `*hasher.Hasher` with `hasher.New()` per
application, then call `Hash(ctx, plaintext)` or `Compare(ctx, plaintext, storedHash)`.
It has no imports from `internal` and owns no background work or closable resources.

Defaults: bcrypt cost 10, two concurrent operations, no queued work (`ErrBusy` on
saturation). Passwords over 72 bytes are rejected, never truncated. Strength policy
belongs to the caller. Empty stored hashes perform dummy comparison; malformed or
unsupported hashes return false. Imported costs above 14 are rejected. Cancellation
is observed before and after bcrypt; it cannot interrupt a running bcrypt operation.

Persist only the returned hash. Never log either plaintext or hashes. This package
supports bcrypt hashes only; changing algorithms requires an explicit migration.
