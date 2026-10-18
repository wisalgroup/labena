# Session token primitives

`NewAccess(key, issuer, audience)` creates an immutable HS256 signer/verifier for
UUID account and session IDs. The key must contain 32 random bytes. `Issue` embeds
the IDs and expiry; `Parse` validates algorithm, issuer, audience, timestamps,
UUIDs and token type. Errors are package-level sentinels, independent of HTTP.

JWT verification does **not** establish current authorization or session activity.
The application must check revocation and account state. Keep signing keys private;
this implementation uses one key and has no overlapping-key rotation mechanism.

`NewOpaque()` returns a random 256-bit credential and its SHA-256 digest.
`HashOpaque(raw)` accepts only its canonical base64url encoding. Store only the
digest; raw credentials must not appear in logs or URLs. Storage, rotation, replay
detection, expiry and revocation belong to the application.
