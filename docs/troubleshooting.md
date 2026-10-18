# Troubleshooting

Locate the stage that failed before retrying the whole operation. Keep the request
ID, safe error code, process role and task/delivery ID when available. Do not include
tokens, OTPs, recipient payloads, environment dumps or connection credentials in a
report. Configuration and transport errors deliberately omit sensitive values.

## Pick the failing boundary

| What you observe | Start here |
| --- | --- |
| Process cannot construct or CLI help fails | [Configuration and startup](#configuration-or-startup-fails) |
| 401, 422, missing sign-in route | [Authentication and HTTP](#authentication-fails-or-a-route-is-missing) |
| Accepted task never executes | [Queue state](#queued-work-does-not-run) |
| Notification intent exists but nothing arrives | [Delivery ledger](#notifications-are-pending-failed-or-duplicated) |
| Reaction missing after an event | [Local event guarantees](#a-local-event-disappeared) |
| Wrong asset URL or missing bytes | [Media and storage](#media-upload-download-or-cleanup-fails) |
| Missing logs, trace, or SQL text | [Observability](#request-diagnostics-are-missing) |
| Pod stays unready or termination times out | [Lifecycle](#readiness-or-shutdown-does-not-finish) |

## Configuration or startup fails

Use the same profile in each terminal. Existing shell values override dotenv;
changing `.env` does not change a running process. `APP_ENV_FILE=''` disables file
loading, while an explicit path must exist. Defaults and supported keys are in the
[configuration reference](../deployments/configuration.md).

| Symptom | Check and next action |
| --- | --- |
| A dependency opens unexpectedly | Check the selected profile and overrides for that capability. The full defaults select MinIO; quickstart disables it. |
| `DATABASE_URL is required` | Installed modules need PostgreSQL. The component-only Asynq profile cannot serve them without a database. |
| Database or Redis connection refused | Match the host/port to where the process runs. Container service names work inside their network; host processes need published ports. |
| Missing tables or columns | Run `migrate status` against the intended database. Apply pending migrations with `migrate up`. Rewritten development baselines do not upgrade an older schema. |
| Conflicting migration versions or dialects | Check all registered sources and timestamp versions. Installed modules use PostgreSQL. Discard failed startup; fix source registration before retrying. |
| CLI help fails | Help uses full application construction. Fix settings or resource access first; it is not an offline configuration checker. |
| Address already in use | Stop the other local process or change its listener. Give separate host processes different metrics ports too. |
| Invalid Redis isolation | Queue, cache and limiter cannot use the same address/database pair. Namespaces alone do not satisfy this isolation check. |
| Read-only filesystem or permission denied | Select stdout/stderr or supply a writable log/storage mount with the process UID/GID. Relative paths start at the process working directory. |

Read-only diagnostics, after the application can construct:

```sh
go run ./cmd/cli migrate status
go run ./cmd/cli route list
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

Run those commands with the same selected profile as the failing process. The
quickstart disables authentication, push, and storage; mail is always constructed
and connects on send. A quickstart check does not diagnose an enabled full profile. Do not use `migrate refresh` to repair a
database containing data you need: it deletes the configured schema's objects/data.
Choose a forward migration or an explicitly disposable database instead.

## Authentication fails or a route is missing

| Symptom | Meaning and next action |
| --- | --- |
| OTP routes return 404 | Enable `AUTH_ENABLED`, configure its required dependencies and restart. The unified OTP flow has no separate registration/email-verification endpoint. |
| Protected route returns 401 | Use an application access token and an active session. Disabled auth fails closed; an `X-User-ID`, Google token or refresh token is not an app bearer token. |
| Code is rejected | Use the challenge ID returned by the latest sign-in-code request and its matching six-digit code, including leading zeroes. Check expiry, resend, attempt limits and prior consumption. |
| OTP send returns 503 | Inspect mail and Redis availability. Identity fails closed when its shared admission limiter is unavailable. A failed send can still be subject to cooldown and budgets. |
| Request returns 429 | Respect retry metadata and cooldown. Do not flush limiter keys or rotate the verification key to bypass the budget. |
| Google returns 409 | The email already belongs to an account. Sign in through its existing method; automatic linking and a linking endpoint are not implemented. |
| New Google account returns 422 | Check the safe validation response. The current provider policy can require email OTP for addresses whose ownership Google is not authoritative for. |
| Refresh returns 401 | The token can be expired, revoked, malformed or already rotated. Concurrent refresh/retrying a used token can revoke the entire session. |
| JSON returns 400, 413 or 422 | Follow the endpoint schema: one object, known fields, valid values and bounded body. Do not retry an unchanged invalid payload. |

Serialize refresh calls per session. Persist a successful replacement before the
next refresh; never keep retrying an old refresh token after an uncertain result.
Recover with a new sign-in when the session is invalid. Access tokens expire after seven days by default,
while refresh tokens and sessions have no absolute time limit by default. Revocation
and account state still apply. See
[Identity](../internal/modules/identity/README.md#refresh-and-replay-behavior) and the
[API contract](api/README.md).

OTP send returns an accepted challenge; account creation occurs when OTP or social
proof succeeds. Verification returns 201 for a new account and 200 for an existing
account. After either status, inspect `data.user.profile_complete` and use
`PATCH /users/me` for profile completion. There is no second registration request or
verification-unavailable signup warning. Neither sign-in response confirms a
background welcome reaction completed.

## Queued work does not run

Use the always-available queue commands to inspect the actual
queue and task ID returned at enqueue:

```sh
go run ./cmd/cli queue list
go run ./cmd/cli queue inspect default TASK_ID
```

Inspection can expose saved payloads; keep output private. Check in this order:

1. Producer and worker use the same Redis address **and database number**; confirm both process profiles.
2. The worker consumes the task's queue; check `QUEUE_DEFAULT`, `QUEUE_ROUTES` and
   the named priorities. A per-task queue option can override routing.
3. A worker process is running, and the queue is not paused. HTTP-only processes
   enqueue work but do not execute it.
4. The task is due and its type is registered. Every consumer of `app.command`
   must register the requested command factory; every consumer of `mail.send.v1` needs valid SMTP settings.
5. Inspect retry/archived state and the safe error. Fix dependencies or invalid
   payloads before choosing replay. Successful tasks normally disappear unless retained.

`queue replay QUEUE ID` moves eligible work to pending and can repeat effects.
It does not reset retry history. First establish whether the previous attempt
committed or sent anything. Do not delete tasks or flush Redis as a diagnostic step.
See [failures and retries](failed-jobs.md).

For recurring work, run one `cmd/scheduler` process. `cmd/worker` never starts cron scheduling. Central
schedules also have feature conditions; command handlers are registered independently of scheduler execution. Calendar expressions use UTC, and missed ticks are not
replayed. See [scheduling](scheduling.md).

## Notifications are pending, failed or duplicated

Notification intent is stored in PostgreSQL before a dispatcher submits delivery
IDs to Redis. A returned receipt inside a transaction still depends on commit.
First inspect the ledger rather than assuming the notification is an ordinary task:

```sh
go run ./cmd/cli notifications deliveries --status pending
go run ./cmd/cli notifications deliveries --status awaiting_receipt
go run ./cmd/cli notifications deliveries --status failed
go run ./cmd/cli notifications delivery DELIVERY_ID
```

- Pending work needs a dispatcher and worker using the same database and queue.
  Workers run the dispatcher without cron. `notifications dispatch` is a manual
  enqueue operation; it does not deliver synchronously.
- Sending rows are leased. Let an expired lease recover; do not manually overwrite
  state while a worker may still own it.
- Pending rows can be waiting for their next automatic attempt; inspect the due
  time. Failed rows need operator review. Use `notifications retry DELIVERY_ID`
  after correcting the cause and accepting possible duplicate provider effects.
- An omitted key creates a new occurrence on every Enqueue call. Use the same
  business occurrence key across retries to reuse saved channel intents.
- A repeated key retains the first snapshot; editing a template does not rewrite
  existing delivery content. Database, email and push channels progress independently.

Email success means SMTP accepted the operation, not inbox placement. A network
failure after acceptance leaves uncertainty. Ordinary `Mailer.Enqueue` is tracked
in Asynq, not the notification ledger; use queue inspection for those sends.

Expo `awaiting_receipt` is normal after ticket acceptance. The dispatcher starts
receipt checks after 15 minutes and polls missing receipts without resending.
An `ok` receipt confirms provider handoff, not device display. After 24 hours an
unconfirmed result becomes a failed uncertain delivery; manual retry may duplicate.
Check the current subscription, project configuration and client permissions using
the [push guide](../pkg/push/README.md). Explicit logout detaches session-bound
subscriptions; already admitted sends cannot be retracted.

## A local event disappeared

GoChannel delivers only within the publishing process. A CLI process cannot notify
subscribers in an API/worker process. Register local handlers before producers
start, and keep subscribers with each producer role. Router retry middleware bounds
one processing cycle; a subsequent Nack can trigger GoChannel redelivery while the
process is alive. It is not a global attempt cap. There is no configured
`failed.events` dead-letter route or durable replay store. Restart and process
failure can lose in-memory events.

For the existing welcome example, an operator can enqueue the stable welcome task
with `identity welcome USER_ID`. For business work that must survive failure,
persist notification intent in the business transaction or design an explicit
outbox. Adding a broker container alone changes none of these guarantees. See
[events](events.md) and [transactional notifications](transactional-notifications.md).

## Media upload, download or cleanup fails

Check both PostgreSQL metadata and the disk selected by the asset. A configured
definition alone is not an opened disk: use the default or additional-disk list.
Keep disk names stable for existing records; changing the default does not move
previous objects. Authentication supplies user ownership; other model types need
business-authorized handlers.

The default HTTP upload accepts exactly one file and optional visibility, with a
20 MiB file limit and bounded multipart overhead. Authenticated downloads require
a ready asset belonging to the caller; the content route allows public assets or
valid temporary links. A ready metadata row with a missing object points to backing-storage or
restore consistency, not a reason to reassign ownership.

Interrupted uploads and deletions retain recoverable metadata. Run `media recover`
or the central recovery schedule, with storage available. Recovery leaves recent
uploads alone and processes bounded batches; it is not a bucket inventory cleaner.
Back up metadata and objects together. See [Media](../internal/platform/media/README.md).

### The returned asset URL does not work

| Symptom | Check |
| --- | --- |
| Direct URL contains an internal container hostname | Configure a client-reachable storage endpoint; signatures cannot be repaired by rewriting the host afterward |
| Public provider URL returns access denied | Public-read policy must cover the asset's public prefix; the app does not change bucket policy |
| Private link worked earlier | Its expiry; request a fresh authorized temporary link |
| Internal media URL points to the wrong origin | `MEDIA_BASE_URL` for this deployment |
| Changing the default disk did not move old assets | Each record keeps its disk; migration is an explicit operation |
| Old public asset still uses an internal URL | Legacy object prefixes retain the internal fallback |

See [Media](media.html) for direct versus internal URLs and visibility. A direct
provider URL bypasses application response headers and may have provider/CDN cache
behavior independent of metadata deletion.

## Request diagnostics are missing

Start with the request's response header or body ID:

```logql
{service_name="labena"} | request_id="YOUR_REQUEST_ID"
```

Use your `APP_NAME` as `service_name` if it differs. In Grafana Explore's Builder,
add a Label filter for `request_id`; it is structured metadata, not an indexed
stream label. New OTLP records need no JSON parser.

| Symptom | Next check |
| --- | --- |
| No matching log | Time range, application identity, selected `observability` channel, collector reachability |
| `request_id_extracted` or `level_extracted` | Remove an unnecessary JSON parser; old file records and new OTLP metadata use different shapes |
| Trace link exists but trace does not | Sampling, export loss, retention, and selected Tempo instance |
| INSERT span has duration but no text | SQL sanitization/size limits; use current code and a newly recorded trace |
| Query text missing from log records | Expected: sanitized SQL lives in Tempo's `db.query.text`, not Loki |
| INFO missing only from local file | Built-in file channel defaults to WARN |

Follow [request → log → trace → SQL](observability.md#inspect-a-requests-sql-in-grafana).
Do not enable raw payload or SQL-value logging to diagnose these boundaries.

## Readiness or shutdown does not finish

Readiness is a startup signal unless dependency probing is enabled. A green ready
response does not establish schema correctness, SMTP delivery or cloud health.
During shutdown it becomes unavailable before admission stops. Worker-only processes
have no business HTTP readiness endpoint.

Shutdown stops admission, drains workers then local subscribers, stops metrics and
closes owned resources. A handler that ignores cancellation can exhaust the shared
drain budget. The app then reports failure and refuses to close dependencies still
in use; it cannot forcibly stop a goroutine. Give the deployment enough termination
time for both shutdown and cleanup, and ensure the executable receives the signal.

Fix unbounded work or an unreachable dependency before increasing timeouts. Check
[application lifecycle](application-lifecycle.md) and [observability](observability.md)
for correlation and bounded diagnostics. Recovery after forced termination must
still tolerate incomplete operations and duplicate task delivery.

## Prepare a useful issue report

Include the operation and expected result, source revision, environment/role,
relevant non-secret feature switches, HTTP status and safe error type, request ID,
and task/delivery ID where applicable. State whether the problem reproduces after
restart and which backend check failed.

Attach a minimal sanitized request or task shape only when needed. Remove access
and refresh tokens, OTPs, connection URLs with credentials, provider payloads, and
full configuration dumps. Report uncertain effects explicitly—for example, “SMTP
acceptance is unknown”—so recovery does not accidentally duplicate them.
