# Shared enums

Business modules share typed enum definitions here. Each file names a concept and
its owner. Technical library enums stay with their libraries.

```go
provider := enums.ProviderGoogle
providers := enums.AuthProviderValues() // google, apple; independent slice
if !provider.Valid() {
    return entities.ErrUnsupportedAuthProvider
}
```

Identity currently defines `AuthProvider`, `VerificationPurpose`,
`VerificationChannel`, `SessionPlatform`, and `UserStatus`. Each has a matching `TypeValues()`
function returning values in declaration order and a `Valid()` method.

These are named string types. Standard JSON, Bun, and database/sql already support
string conversion, so they need no MarshalJSON, UnmarshalJSON, Scan, or Value methods.
Decoding unknown strings succeeds; callers must validate membership explicitly.
The empty value is invalid. Use pointers for nullable columns; SQL NULL is not an
additional enum value. Entity validation translates invalid values into stable
application errors. PostgreSQL stores TEXT without enum or CHECK constraints.
