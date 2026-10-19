# Notifications

Send a message through database, email or mobile push channels using one
`notification.Message`. Business modules choose the recipient, content and eligible
channels. `notification.Notifier` stores read state and manages delivery.

Start with a database notification. Add email or push only when the operation needs
it. For ordinary email without recipient state or SQL delivery intent, use
[mail](../../../docs/mail.md). For direct provider push without SQL, use
[push](../../../pkg/push/README.md).

| Operation | Choose it when | Successful return means |
| --- | --- | --- |
| `Send` | You want one immediate attempt | Selected channels were attempted; inspect all results |
| `Enqueue` | Intent must survive process failure or join a SQL write | PostgreSQL accepted immutable channel intents, subject to commit |
| `List` / `UnreadCount` / `MarkRead` | You are building in-app notification UI | Recipient-scoped database state was read or changed |

App constructs the notifier once as `Resources.Notifier`. Modules borrow it; they
do not start dispatchers, register providers or implement another notifier contract.

## Stored state

| Table | Purpose |
| --- | --- |
| `notifications` | In-app inbox content and read state |
| `notification_deliveries` | Durable channel delivery intent, attempts and outcomes |
| `push_subscriptions` | Recipient-owned device installations, push tokens and optional session bindings |

Push subscriptions are delivery destinations, not notification preferences or topic
subscriptions. A recipient can register multiple installations. Token rotation,
logout, invalid-token handling and inactivity pruning keep those destinations current.
Queued push delivery records retain the subscription ID and version so an older
message cannot follow a reassigned installation.

The initial notification migration creates these tables directly, even when push
is disabled. See the [schema adoption notes](../../../docs/upgrading.md#version-02-push-subscription-schema)
when updating an application created from an earlier development snapshot.

## Create your first database notification

Apply the normal application migrations and use the existing `Resources.Notifier`.
Database-only `Send` needs PostgreSQL; it does not need a running worker, Redis,
SMTP or Expo.

Inside an existing operation, after authorizing the UUID `userID`:

```go
recipient := notification.Recipient{
    Type: string(entities.UserEntityType),
    ID: userID.String(),
}
message := notification.Message{
    Type: "identity.profile_updated",
    Database: json.RawMessage(`{"title":"Profile updated","body":"Your changes were saved."}`),
}
_, err := notifier.Send(ctx, recipient, message)
return err
```

Import `encoding/json`, `github.com/wisalgroup/labena/internal/modules/shared/entities` and
`github.com/wisalgroup/labena/internal/platform/notification`. `ctx` and `notifier` are borrowed by
the operation. With no surrounding transaction, success makes the database record
available immediately through `List` or the authenticated HTTP routes. Inside a
transaction, visibility still depends on its commit.

This example omits `Key`, so every invocation creates a new occurrence. Read the
[key rules below](#omit-the-key-for-a-new-occurrence-on-every-call) before making
an operation retryable. For a real profile write plus notification, follow the
[compiled transactional workflow](../../../docs/transactional-notifications.md).

## Define a module notification

Modules keep a plural `notifications` package; the shared capability is singular
`notification`. Both imports work without aliases. A builder is an ordinary Go
function, with no base class, service registration or per-notification handler.
For an illustrative Orders module, put this complete function in its `notifications`
package and import `encoding/json`, `internal/platform/notification` and `pkg/mailview`:

```go
func NewOrderShipped(orderID string) (notification.Message, error) {
    data, err := json.Marshal(struct {
        OrderID string `json:"order_id"`
        Title   string `json:"title"`
    }{OrderID: orderID, Title: "Your order has shipped"})
    if err != nil { return notification.Message{}, err }

    return notification.Message{
        Key: "shipment:" + orderID,
        Type: "order.shipped",
        Database: data,
        Email: &mailview.Email{
            Subject: "Your order has shipped",
            Title: "On its way",
            Paragraphs: []string{"Your order has shipped."},
        },
    }, nil
}
```

`Key` optionally identifies one business occurrence per recipient, not an attempt. Include a
shipment version when an order can ship more than once. A retry reuses its key;
a new occurrence uses a new key. `Type` is a stable module-owned name used by clients
and operators. Types require no central registration. Explicit keys allow 1–200 UTF-8 bytes;
types allow 1–100, without surrounding whitespace or control characters.

### Omit the key for a new occurrence on every call

```go
message := notification.Message{
    Type: "announcement.published",
    Database: json.RawMessage(`{"title":"A new announcement is available"}`),
}
receipt, err := notifier.Enqueue(ctx, recipient, message)
```

When `Key` is empty, each `Send` or `Enqueue` call generates a fresh UUID key.
All selected channels share that key, and queued worker retries reuse the persisted
key. The caller's Message is not modified. Repeating the original call, even with
the same Message value, creates a new occurrence and can produce another notification.
Use an explicit stable key when an event, job or request may be retried, especially
after an uncertain enqueue result. Only the empty string requests generation;
whitespace-only or otherwise invalid explicit keys are rejected.

`Enqueue` deduplicates by recipient, key and channel, including completed deliveries.
`Send` deduplicates database records, but attempts external channels immediately—even
with an explicit key. Use `Enqueue` when email retries need persisted deduplication.

## Select channels

| Constant | Message field | Behavior |
| --- | --- | --- |
| `ChannelDatabase` (`database`) | `Database json.RawMessage` | Store JSON data with recipient ownership and read state |
| `ChannelEmail` (`email`) | `Email *mailview.Email` | Render branded text/HTML using `pkg/mailview`, then send with `internal/platform/mail` |
| `ChannelPush` (`push`) | `Push *push.Message` | Send to the recipient’s registered installations |

Non-nil content selects a channel. `message.Channels()` returns the typed channels
in delivery order. Set `message.Email = nil` for a database-only notification, or
`message.Database = nil` for email-only delivery. A message needs at least one
channel; there is no separate `Via` list to keep synchronized with the content.

The `database` name follows [Laravel's database channel](https://laravel.com/docs/13.x/notifications#database-notifications):
notification type and JSON data are stored for the application's interface.
Database content must be a valid UTF-8 JSON **object**, at most `MaxDataSize` (32 KiB).
An empty object is valid; null, arrays, scalars and invalid JSON are rejected.
Titles, text, links, identifiers and nested values belong inside this object.
There is no fixed `Body` field and no double JSON encoding. Build dynamic data with
`json.Marshal`; do not interpolate untrusted values into a JSON string.

Email accepts the existing `mailview.Email` directly. Module `mailviews` builders
can compose it with the shared branded layout. Rendering and address validation
happen before either channel has effects. Queued email snapshots its rendered
subject, text, HTML and destination; a retry does not rerender newer templates.
Each encoded channel snapshot is limited to 64 KiB. Rich synchronous attachments
remain available through `internal/platform/mail`; queued content contains no open files or readers.
PostgreSQL normalizes JSONB formatting and numbers. Separate storage limits allow
64 KiB for normalized database data and 128 KiB for a normalized channel snapshot;
oversized numeric expansion is rejected by database constraints during persistence.

## Address any model

```go
recipient := notification.Recipient{
    Type: string(entities.UserEntityType),
    ID: user.ID.String(),
    Email: user.Email.String(),
}
receipt, err := notifier.Enqueue(ctx, recipient, message)
if err != nil {
    return err
}
// receipt.DeliveryIDs identifies the persisted channel intents.
```

This fragment assumes an authorized, loaded `user`, a prepared `message`, `ctx`
and the borrowed notifier. Import `internal/modules/shared/entities`; do not make up
module-local discriminators. Add stable admin or other model constants to
`internal/modules/shared/entities/entity_types.go` when introducing those models.

`Recipient.Type` is a stable logical model name, matching `[a-z][a-z0-9_.-]{0,63}`.
`Recipient.ID` is a canonical UTF-8 string of 1–200 bytes without surrounding
whitespace or control characters. Numeric IDs use decimal strings; UUIDs use their
standard form. `(type, id)` scopes persistence, duplicate suppression and read state.
Email is only a destination and is excluded from database-channel snapshots and
stored records. Platform code never imports a model to discover its address.

The caller authorizes recipients and owns existence/deletion policy. There is no
foreign key to arbitrary model tables. Stop new publication and coordinate workers
before purging a deleted recipient; process-local pub/sub is not durable cleanup.

## Send immediately or queue durably

| Operation | Guarantee and dependencies |
| --- | --- |
| `Send(ctx, recipient, message)` | One attempt per selected channel; returns `[]Result` with typed channel and error |
| `Enqueue(ctx, recipient, message)` | Atomically persists channel intents; returns `Receipt.DeliveryIDs` |
| `Dispatch(ctx)` | Submits a bounded batch of due IDs to Asynq; database and queue required |
| `Deliver(ctx, id)` | Claims and attempts one persisted channel; called by the worker |

`Send` validates all content before effects. Database-only messages join the
caller's SQL transaction, replacing a separate record/write API. Messages containing
email or push must run outside transactions. A failed channel does not prevent independent
channels from being attempted; inspect the results before deciding what to retry.
SMTP acceptance does not prove that a message reached the recipient.

`Enqueue` needs a database but no queue client in the calling process. It performs
no Redis, SMTP or pub/sub calls. Its receipt means intent is persisted, subject to
the caller's commit. Put business changes and notification intent in the same transaction:

See the [complete, compiled profile update](../../../docs/transactional-notifications.md)
for this transaction pattern with real framework services. All participating SQL
collaborators must share the same database pool. A receipt returned inside a
transaction is not proof that its outer caller eventually committed.

App exposes the same instance as `dependencies.Resources.Notifier`. Construct
with `New(db, Options{})` for database-only use or `New(nil, Options{Mailer: sender,
MailRenderer: views})` for email-only sends. Missing SQL returns `database.ErrNilDB`;
missing email/queue configuration returns `ErrUnavailable`. A configured Mailer
requires MailRenderer at construction. No instance owns or closes its borrowed clients.

> [!WALKTHROUGH] Follow a durable notification
>
> Each step has a different guarantee. A saved intent, a queued task and an
> accepted provider call are not the same outcome.
>
> 1. **Save intent.** The business operation calls Enqueue in its SQL transaction.
>    PostgreSQL stores one snapshot per selected channel and destination.
>
>    No Redis or provider call happens here. A rollback removes both the business
>    changes and the new notification intents.
>
> 2. **Commit.** The outer transaction makes both the business change and its
>    notification intent visible together.
>
>    A receipt returned inside the transaction still depends on that commit.
>    A successful commit does not mean a notification has been delivered.
>
> 3. **Dispatch.** The worker-owned dispatcher scans due ledger rows and submits
>    delivery IDs to Asynq in Redis.
>
>    The ledger remains the source of pending work if Redis is unavailable.
>    Dispatch does not require the cron scheduler.
>
> 4. **Attempt delivery.** A worker claims one channel with a lease, reads the
>    saved content and attempts the database, email or push operation.
>
>    Channels progress independently. A failure in email does not undo a stored
>    database notification.
>
> 5. **Record or retry.** Persist the result. Failed attempts wait until due;
>    expired claims can recover and exhausted deliveries need deliberate retry.
>
>    A crash after an external provider accepts the message can still cause a
>    duplicate. A lease cannot undo a message already sent.

## Handle partial immediate results

`Send` validates the whole message before effects, then attempts channels
independently. A later failure does not roll back an earlier successful channel.
In a function that returns `([]notification.Result, error)`, preserve both outputs:

```go
results, err := notifier.Send(ctx, recipient, message)
return results, err
```

| Result | Caller responsibility |
| --- | --- |
| Validation error with no results | Fix content, destination or configuration before trying again |
| Database succeeded, email failed | Do not assume the database record disappeared; decide which effect to retry |
| Push has `PushTicketID` | Persist/check its receipt if required; `Send` does not poll it |
| External result is uncertain | A repeat attempt may duplicate the external message |

A stable `Key` deduplicates database records even for immediate sends. It does not
suppress repeated immediate email/push attempts. Prefer `Enqueue` for persistent,
independent channel retries. Never retry the entire multi-channel call blindly.

## Read stored notifications

`List`, `UnreadCount` and `MarkRead` operate on `notification.Record` values. Reads
are bounded and newest-first; marking read preserves the first timestamp. HTTP
routes are `GET /notifications`, `GET /notifications/unread-count`, and
`PATCH /notifications/{id}/read`. Default routes use the authenticated Identity user;
other model types require their own authorized handlers.

Responses include `type`, `data`, `recipient_type`, `recipient_id` and timestamps.
`data` is a JSON object, including nested values. The payload schema is owned by the module, while read state is owned by the
notification capability.

### Read from Go

These function-body fragments assume an authorized `recipient`, `ctx` and notifier:

```go
page, err := notifier.List(ctx, recipient, pagination.NewPagination(1, 20))
if err != nil {
    return err
}
// page.Items contains records; page.Total is the matching record count.
```

```go
count, err := notifier.UnreadCount(ctx, recipient)
if err != nil {
    return err
}
// count is an integer, independent of pagination.
```

```go
return notifier.MarkRead(ctx, recipient, notificationID)
```

Import `github.com/wisalgroup/labena/pkg/pagination` for the first example. `notificationID` is a UUID.
Repeated `MarkRead` preserves the first read time. Passing another recipient's ID
does not grant access to their record. There is no implicit “mark all read” action.

### Read from HTTP

With an Identity access token stored in `$ACCESS_TOKEN`:

```sh
curl -H "Authorization: Bearer $ACCESS_TOKEN" \
  'http://localhost:8080/api/v1/notifications?page=1&per_page=20'
curl -H "Authorization: Bearer $ACCESS_TOKEN" \
  http://localhost:8080/api/v1/notifications/unread-count
curl -X PATCH -H "Authorization: Bearer $ACCESS_TOKEN" \
  http://localhost:8080/api/v1/notifications/NOTIFICATION_ID/read
```

The list returns the framework's `data` envelope containing `items`, `page`,
`per_page`, `total` and `total_pages`. Unread count returns `data: {"count": N}`;
marking read returns **204** with no body. Normal request metadata is still
provided by the [response layer](../../../docs/responses.md).

The caller cannot select recipient type or ID through these default routes.
They derive it from the authenticated user. An Admin module supplies its own
authorized handlers and calls the same recipient-scoped methods.

## Push and database notifications

This fragment assumes `exportID`, JSON-object `data`, expiry `expires`, an
authorized recipient and a borrowed notifier. Import `github.com/wisalgroup/labena/pkg/push`:

```go
message := notification.Message{
    Key: "export:" + exportID + ":ready",
    Type: "export.ready",
    Database: data,
    Push: &push.Message{
        Title: "Your export is ready",
        Body: "Open the application to download it.",
        Data: map[string]string{"export_id": exportID},
        ExpiresAt: &expires,
    },
}
receipt, err := notifier.Enqueue(ctx, recipient, message)
if err != nil {
    return err
}
// Persist or return receipt.DeliveryIDs when the operation needs delivery tracking.
```

Queued and synchronous notification push automatically includes reserved string data
fields `notification_key` and `notification_type`; caller values for these fields are
overwritten in a copied payload. Push works without selecting Database.
Channel delivery order is not guaranteed.

`Enqueue` freezes the push target set once under a transaction lock. Each destination
has an independent delivery row. Repeating the key cannot add newly registered
installations or replace content. Registration rotation, reassignment, unsubscribe,
expiry or logout prevents an old queued version from following the new destination.
Refreshing an unchanged registration only updates its freshness. No registration
produces a skipped delivery, rather than a job that later targets new devices.
A recipient may have at most 100 active installations per push project. Subscribe
enforces this atomically, including concurrent registrations. Existing installations
can rotate at capacity. `ErrInstallationLimit` asks the caller to unsubscribe a device
before adding another; it is also returned when legacy data already exceeds the
limit. Resolve legacy excess with recipient-scoped Unsubscribe (or DetachSession
for a deliberate logout), then retry. Delivery never silently truncates destinations.

`Subscribe`, `Unsubscribe`, `DetachSession` and `PruneSubscriptions` work for any
model; callers authorize recipients. HTTP defaults serve Identity users only. Active
installations cannot be reassigned to a different recipient without detachment.
The provider/project scopes registrations and uniqueness. Device registration is
private and never included in HTTP responses, operator responses or logs.

Identity's explicit session-revocation methods detach registrations atomically with
logout. Before each new provider send, session-bound destinations also call
`Options.SessionActive`. App supplies Identity's authoritative account/session check,
so replay revocation, single-session displacement, suspension and expiry prevent new
push attempts. A false result disables only the checked subscription version and
skips delivery; a backend error is retried without detaching the registration. This
also protects already queued intents. A concurrent registration for a fresh session
survives the stale check. The check cannot retract an in-flight provider request.

Standalone session-bound callers must supply SessionActive; missing checks fail
closed. Sessionless recipients remain supported without that callback. Callers still
own recipient authorization and may explicitly detach on deletion. Push registration
is not itself an authentication credential. App centrally schedules
`notifications.prune_subscriptions.v1` hourly when push is enabled. Each run disables
up to 1000 registrations inactive for 90 days; the cutoff is computed at execution.
Delivery/receipt polling does not perform retention cleanup and needs no cron.
Workers-only deployments need a scheduler owner or the CLI command
`notifications prune-subscriptions` for retention. See the
[scheduling guide](../../../docs/scheduling.md) for rollout and backlog handling.

Invalid registrations are disabled, permanent failures stop automatic retries, and
transient failures retry independently. Provider Retry-After is respected. Expired
messages are skipped. Logs contain delivery ID/channel/attempt only. Optional OTel
metrics expose `notification.delivery.attempts` with channel/outcome and
`notification.pending.age` in seconds; alert on growing backlog age, permanent
failures, throttling, and invalid registrations. Retention of delivery history remains
an application/operator policy because purging it also removes deduplication.

## Register and detach a push installation

The default authenticated routes are:

| Method | Path under `/api/v1` | Result |
| --- | --- | --- |
| `POST` | `/notifications/subscriptions` | Register or refresh a device installation; returns ID, installation ID and update time |
| `DELETE` | `/notifications/subscriptions/{id}` | Detach a recipient-owned installation; returns 204 |

The POST body contains only `installation_id` (a client-persisted UUID) and
`registration` (the Expo push token). Identity supplies recipient and session
identity from the access token. The provider registration is never echoed back.

Use [the Expo client example](../../../pkg/push/README.md#expo-mobile-client) for
permissions, token acquisition and the authenticated request. Refresh unchanged
registrations on application startup so inactivity pruning reflects current use.

Identity's HTTP delivery calls its AuthService's `SubscribePush` operation directly.
Identity rechecks the session and holds its account lock through registration,
serializing it with logout. Direct `Notifier.Subscribe` calls do not have that
Identity policy; custom integrations must authorize their recipients and coordinate
session revocation themselves.

For a non-Identity integration inside this repository:

```go
subscription, err := notifier.Subscribe(ctx, recipient, installationID, sessionID, registration)
if err != nil {
    return err
}
// Persist subscription.ID for this installation; do not expose its registration.
```

All four values are supplied and authorized by the caller. Nonempty session IDs
require a `SessionActive` callback in the notifier options. `sessionID` can be empty
for a model without sessions. `DetachSession(ctx, recipient, "")` disables all
installations for that recipient; a nonempty session ID targets only that session.
Coordinate account deletion and stop new publication before purging recipient data.

## Expo acceptance and receipts

Expo push registrations replace native provider tokens. New registrations must use
`ExpoPushToken[...]` or `ExponentPushToken[...]`. Configure `PUSH_EXPO_PROJECT_ID`
and optionally `PUSH_EXPO_ACCESS_TOKEN` (required when enhanced push security is enabled).

Accepted push deliveries enter `awaiting_receipt`. The existing dispatcher/worker
checks receipts after 15 minutes; missing receipts and lookup failures are polled
again after five minutes without resending or consuming send attempts. An `ok` receipt
confirms provider handoff only. Definitive receipt failures follow normal retry or
permanent-failure handling, and invalid destinations are disabled by registration version.

No confirmation within 24 hours is a failed, uncertain outcome requiring operator
review. Manual retry clears its ticket and can duplicate delivery. Synchronous Send
exposes `Result.PushTicketID` for caller-owned receipt checks. See the
[push guide](../../../pkg/push/README.md) for standalone usage and Expo client setup.
Receipt state is persisted with the delivery. A worker restart resumes polling
that ticket; it does not intentionally resend merely because a receipt is pending.

## Delivery lifecycle

| Status | Meaning |
| --- | --- |
| `DeliveryPending` (`pending`) | Waiting for dispatch or next due attempt |
| `DeliverySending` (`sending`) | A worker holds the current lease |
| `DeliveryAwaitingReceipt` (`awaiting_receipt`) | Expo accepted a ticket; provider handoff is not yet confirmed |
| `DeliverySent` (`sent`) | Database record stored, SMTP accepted, or Expo confirmed provider handoff |
| `DeliveryFailed` (`failed`) | Terminal failure or exhausted/uncertain delivery requiring operator review |
| `DeliverySkipped` (`skipped`) | No eligible destination, detached registration or expired content |

Zero and unknown status values are invalid. “Sent” never means the user read it. One row
exists per `(recipient_type, recipient_id, key, channel, destination)`.
Destination is empty except for push, which uses a subscription ID. Enqueue calls with the same key
return the original IDs and retain the first type, content, destination and state.
Channels retry independently; successful channels are not intentionally resent.

`NewDispatcher(notifier, interval)` validates its arguments without starting work;
zero selects one second. Invalid intervals or a missing notifier return an error
before readiness can be signaled. Worker processes run the dispatcher every second (configurable with
`NOTIFICATION_DISPATCH_INTERVAL`, minimum 100ms). Each scan submits at most 100 IDs
using `notifications.deliver.v1`. No cron scheduler is needed. Replicas coordinate
through database claims; `notifications dispatch` remains available for operators. Without dispatch, intent stays in
PostgreSQL. Losing Redis does not remove the persisted delivery backlog.

Claims use the database clock, a two-minute lease and an ownership token. Provider
attempts have a 30-second deadline, followed by up to five seconds to persist the
outcome. The ledger allows eight attempts with exponential delay starting at one
minute and up to 20% jitter. Asynq retries infrastructure errors three times;
recorded provider failures wait for the next dispatch. Expired claims recover;
exhausted deliveries require deliberate `RetryDelivery`.

SMTP cannot guarantee exactly-once delivery: a worker may crash after acceptance.
Lease ownership protects database updates but cannot undo an external effect.
`GetDelivery` and `ListDeliveries` expose metadata only; logs omit content, destinations
and provider error details. Keep backups and define retention before purging rows:
deleting records also removes duplicate suppression. No automatic purge is implied.

## Inspect and retry deliveries

Use the same database/Redis profile as the running application:

```sh
go run ./cmd/cli notifications list user USER_ID
go run ./cmd/cli notifications unread user USER_ID
go run ./cmd/cli notifications read user USER_ID NOTIFICATION_ID
go run ./cmd/cli notifications deliveries --status pending
go run ./cmd/cli notifications deliveries --status awaiting_receipt
go run ./cmd/cli notifications deliveries --status failed
go run ./cmd/cli notifications delivery DELIVERY_ID
```

Use `--status ''` to include all delivery states and `--page 2` for the next bounded
page. Operator responses contain metadata, not private channel snapshots or provider
registrations. The CLI constructs dependencies but does not start workers.

After checking the cause and duplication risk:

```sh
go run ./cmd/cli notifications retry DELIVERY_ID
go run ./cmd/cli notifications dispatch
```

Retry accepts only a terminal **failed** delivery and makes it eligible again.
Dispatch submits a bounded batch of due IDs; a running worker is still required.
These are different from replaying an Asynq task in the [queue inspector](../../../docs/queues.md#inspection-and-administration).
The SQL ledger owns notification provider retry state.

For stale push installations, `notifications prune-subscriptions` disables a batch
inactive for 90 days. The central hourly schedule performs the same operation when
push and a scheduler owner are enabled. It is separate from delivery polling.

## Use each capability independently

- Database only: `notification.New(db, notification.Options{})`.
- Email only: `notification.New(nil, notification.Options{Mailer: mailer, MailRenderer: views})`.
- Push only: `push.NewExpo`, then `Send(ctx, registration, message)` and `Receipt(ctx, ticket.ID)`.
- Durable notification delivery: supply PostgreSQL to `New`, and Asynq to the worker
  instance. The producer instance needs no Redis connection. Both must have selected
  channel clients configured for preparation and validation.

See [push](../../../pkg/push/README.md) for
constructors, credentials, frontend setup and standalone examples. Modules reuse
these owned capabilities; do not redefine their contracts. Internal Watermill events
remain process-local. For important notifications, persist intent in the business
transaction rather than relying on an in-memory event to eventually enqueue it.

A producer and worker may use separate notifier instances backed by the same
PostgreSQL database. Both need collaborators for the selected channels because
preparation validates and snapshots content before storing intent. Only the
worker dispatcher needs Redis.

```go
notifier, err := notification.New(db, notification.Options{
    Queue: queue,
    Mailer: mailer,
    MailRenderer: views,
    Pusher: pusher,
    PushProject: projectID,
})
if err != nil {
    return err
}
```

This is an assembly fragment using already constructed clients; omit unneeded
optional fields. The notifier never closes them. For standalone framework
integration, `integration.Register(notifier, targets)` installs selected
migrations, commands and tasks and starts no work. Identity's HTTP delivery owns
the user notification and push-subscription routes. Other business modules can
authorize their own recipients before calling the capability directly.

`notification.NewDispatcher(notifier, 0)` selects the one-second polling interval;
call its `Run(ctx)` once as part of your own worker lifecycle, and wait for it before
closing clients. Start the actual Asynq worker separately. A dispatcher alone does
not execute delivery tasks. `internal/platform` remains application-owned Go code;
external projects can use `pkg/push` independently without importing it.

## Extend when a provider is ready

Future channels add typed content and a `Channel` constant, then prepare a durable
snapshot and deliver it through the existing retry flow. Keep those two operations
in `notifier.go`; add the provider to `Options` and permit its channel in SQL.
Business notification types still need no registration. There is no generic driver
registry or map of untyped values for callers to configure.

SMS needs a phone destination and text; WhatsApp commonly needs provider-specific
template identifiers and parameters. These channels are not implemented or advertised
as valid enum values yet. Add providers and their behavior together. Preferences,
localization and recipient eligibility belong in module builders until shared policy
is actually needed.

## Development migration and Identity example

The capability owns migrations for database records, channel delivery intent and
push installations. App registers these through `notification/integration` and the
[central migration runner](../../../docs/migrations.md) applies them. Constructing
a notifier does not create tables.

Use the checked-in baseline for a new database. Existing deployments must preserve
applied migrations and receive explicit forward changes; runtime startup never
resets a schema. Keep delivery history retention deliberate because purging it also
removes duplicate suppression.

The [onboarding walkthrough](../../../docs/onboarding.md) connects the shipped
`notifications.NewWelcome` builder, mail view, local event and worker. The job
reloads current account state and skips inactive/deleted users; unverified users
receive no email. The local signup event can be lost before an intent is saved.
Use the [transactional example](../../../docs/transactional-notifications.md) when
notification intent must commit with the business operation.

## Test and troubleshoot notifications

```sh
go test ./internal/platform/notification/... ./pkg/push/...
go test ./tests/modules -run TestProfileNotificationWorkflowPostgres -count=1
```

The second test needs `DATABASE_TEST_POSTGRES` pointing to a disposable PostgreSQL
database; without it, PostgreSQL-dependent cases skip. It proves commit, rollback,
delayed visibility and repeat delivery of database notifications. The broader
[integration gate](../../../docs/testing-database.md) also exercises real Redis.
Provider tests use local fake HTTP/SMTP behavior, not real recipient delivery.

| Symptom | Inspect | Explanation |
| --- | --- | --- |
| Enqueue succeeded, List is empty | Delivery status and dispatcher/worker | Intent is not yet a database record |
| Same key returns old content | Occurrence identity | First persisted content wins; use a new key for a new business occurrence |
| Every request creates another record | Empty `Key` | Each original call generates a fresh occurrence |
| Database works but email is unavailable | Mailer, renderer and destination | All selected channel content must be prepared before persistence |
| Push is skipped | Registered target set, version and expiry | Enqueue freezes eligible installations; later devices are not added to the same occurrence |
| Push remains awaiting receipt | Ticket age and receipt lookup | Missing receipts do not justify another send |
| Delivery is failed | Attempts, permanent provider classification or uncertain receipt | Fix the cause, assess duplication, then retry deliberately |
| Delivery backlog grows | Pending age, queue health and worker capacity | Durable SQL intent survives Redis outage but still needs execution |

Alert on increasing `notification.pending.age`, repeated permanent failures and
provider throttling. Use channel/outcome metrics for aggregation and delivery IDs
for investigation. Keep payloads, destinations and provider responses out of routine
logs. See [observability](../../../docs/observability.md) for correlation.

## Package responsibilities

| Area | Owns |
| --- | --- |
| `notification.go` | Public content, recipient, channel and record types |
| `notifier.go` | Preparation, immediate sends and record reads |
| `delivery.go` | Persisted delivery lifecycle, dispatch and operator methods |
| `store.go` | SQL behavior and recipient boundaries |
| `subscriptions.go`, `push.go` | Registration lifecycle, frozen destinations and receipts |
| `integration/` | CLI, Asynq and migration registration |

Modules own content and business eligibility. The capability owns delivery state.
The underlying mail/push clients own provider calls. App owns construction and
shutdown. Keep those roles distinct when adding a channel or custom transport.
