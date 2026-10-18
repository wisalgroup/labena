# Shared application definitions

This directory holds business definitions that multiple modules may reference.
It is not a registrable module and has no construction or registration entry point.
Platform packages never import these definitions. Business delivery adapters map
them to platform-owned inputs. Definitions never depend on business modules,
platform packages or app.
The business owner still controls operations and writes. Start with
[shared types](../../../docs/shared-types.md) for examples and the
[canonical conventions](../../../docs/conventions.md) for the ownership contract.

## Find a definition

| Location | Responsibility |
| --- | --- |
| `entities` | Business entities, invariants and accepted Bun mappings |
| `entities/entity_types.go` | Stable polymorphic type names, such as `UserEntityType` |
| `enums` | Business values grouped by concept |
| `events` | Published payloads, versioned topics, constructors and occurrence metadata |
| `domain` | Common entity identity and timestamps |
| `domain/valueobjects` | Validated email value and its conversions |
| `migrations` | Global schema and explicitly published infrastructure SQL |

Identity owns its shared account/authentication records. Notification and Media
records remain inside their platform packages; those capabilities do not depend
on a User model. Their recipient/owner references support multiple model types
without authorizing access or automatically validating model existence.
The transport-independent authenticated principal lives in `internal/platform/auth`.
Identity's HTTP delivery maps it to user media owners and notification recipients.

## Keep contracts small

`events.NewUserRegistered(userID)` constructs a payload and fresh occurrence
metadata; it publishes nothing. Identity owns that event's meaning and topic
version. Consumers own handlers under their own `delivery/events` packages.

Use IDs and event-specific values rather than entity snapshots. Shared event
contracts import neither services, repositories, config nor transports. Across a
future service boundary, extract the serialized contracts actually needed by
consumers instead of publishing all shared Go types.

## Preserve owner boundaries

Service inputs/results live beside services, HTTP shapes beside handlers, and
capability types inside their platform packages. Errors live with their behavior;
`pkg/apperror` supplies the common classification model. There is no global shared
error catalog or shared service/repository contract catalog.

Bun tags on entities describe mapping, not schema execution. Migrations are
registered by their owner and applied explicitly. Another module's access to a
shared struct does not authorize direct writes or bypass the owning service.

Reusable technical packages never import `internal/modules/shared`. Pagination belongs in
`pkg/pagination`; HTTP envelopes belong in `internal/platform/api/response`.
See [directory structure](../../../docs/structure.md) for the full package map.
