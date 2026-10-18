# Shared types

Shared definitions make modules agree on business identity and published contracts.
They do not transfer ownership: another module can refer to a `User`, but Identity
still controls account operations and writes.

## Choose a type's home

| Type | Home | Example |
| --- | --- | --- |
| Business entity and invariants | `internal/modules/shared/entities` | `User`, `Session` |
| Business enum | `internal/modules/shared/enums` | User status, authentication platform |
| Polymorphic type constant | `internal/modules/shared/entities/entity_types.go` | `UserEntityType` |
| Published event and occurrence metadata | `internal/modules/shared/events` | `UserRegistered`, `Event` |
| Service input/result | Owning module's `service` | `UpdateProfileInput`, `SignInResult` |
| Repository projection | Owning module's `repository` | A query-specific result |
| HTTP request/response | Owning `delivery/http` | Public account response |
| Capability record/input/error | Owning platform package | `media.Asset`, `notification.Record` |
| Authenticated caller | `internal/platform/auth` | `Principal` with user and session IDs |
| Operation error | Owning domain/capability | Identity authentication failure |

Do not move every reusable struct into shared. `media.Owner` and
`notification.Recipient` have different meanings even though both identify a
business model with a type and ID.

## Records and transport shapes

Business entities may carry Bun mappings when the persisted and business shapes
agree. This avoids an otherwise identical model plus a conversion layer. Add a
separate private query shape only when persistence actually differs.

HTTP responses select public fields deliberately. A Bun entity's JSON tags do not
prove that every field is appropriate for every caller. Service inputs likewise
exclude transport concerns such as HTTP headers and Huma validation tags.

Migrations change schemas; struct tags do not execute DDL. The owning module
registers its migration source and the operator applies it explicitly.

## Identity and references

Use shared entity-type constants at business call sites:

```go
owner := media.Owner{
    Type: string(entities.UserEntityType),
    ID:   user.ID.String(),
}
recipient := notification.Recipient{
    Type: string(entities.UserEntityType),
    ID:   user.ID.String(),
}
```

This **reference fragment** assumes an existing `*entities.User` and imports
`internal/modules/shared/entities`, `internal/platform/media` and
`internal/platform/notification` under the `github.com/wisalgroup/labena` module path.

The business caller checks existence and authorization. A `(type, id)` value is
not permission, an automatic model lookup, or an ordinary foreign key across
arbitrary tables. The caller also owns deletion policy for attached records.

Type names match `[a-z][a-z0-9_.-]{0,63}`. IDs are nonempty UTF-8 strings of at most
200 bytes, with no surrounding whitespace or control characters. Media and
Notifications share private syntax validation while keeping their public types
and errors separate.

## Publish a business contract

`internal/modules/shared/events/user_registered.go` defines Identity's account-created
contract. Constructing an event only describes an occurrence:

```go
event := events.NewUserRegistered(user.ID)
```

This fragment imports `github.com/wisalgroup/labena/internal/modules/shared/events`; it does not publish.
The event contains `UserID` and metadata (`ID`, `Type`, `OccurredAt`). Identity owns
the meaning and the versioned topic. Consumers own handlers in their own
`delivery/events` package.

Keep contracts limited to IDs and event-specific values. Do not embed complete
entities or import repositories, services, application config or transports.
Publication and delivery are explicit operations covered in [events](events.html).

## Keep errors with their behavior

Use `pkg/apperror` for common classification, safe public codes and validation
issues. Define the actual failure beside the module, entity or capability that
owns its meaning. There is no global `internal/modules/shared/errors` catalog.

Across a future service boundary, share serialized schemas and stable error codes.
Go interface identity, database mappings and error values are not a transport
contract. Extract the contracts needed by consumers rather than exporting the
entire shared directory.

## Common helpers

`shared/domain.Entity` supplies UUID identity and timestamps.
`shared/domain/valueobjects.Email` owns validated email representation and its
JSON/database conversion. `events.Event` is plain occurrence metadata; it neither
buffers nor publishes events.

Technical helpers belong in `pkg` and must not import shared application types.
For example, `pagination.Pagination` remains a reusable paging input; HTTP
envelopes belong to `internal/platform/api/response`.

Continue with [conventions](conventions.html), [events](events.html), and
[entities and queries](database-queries.html).
