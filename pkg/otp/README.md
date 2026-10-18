# OTP primitives

`Generate()` returns a cryptographically random six-digit decimal code.
`NewDigester(key)` copies a secret of at least 32 bytes. `Digest(fields...)` binds
ordered proof fields with HMAC-SHA256; `Key(scope, value)` provides separate keyed
fingerprints for private rate-limit identities. Compare digests with `hmac.Equal`.

Keep the key stable across replicas and restarts. Use the same proof field order
when issuing and verifying; changing the key or field order invalidates proofs.
This package provides no delivery, storage, eligibility, expiry, one-time consumption,
rate limiting or recovery policy. The application must supply those protections.
