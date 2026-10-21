# Application configuration reference

The tables list the environment settings decoded by `internal/app/config`.
Defaults come from `config.Defaults()` without reading files, opening connections
or starting services. Tests check this reference against those defaults. The
[configuration guide](../docs/configuration.md) explains loading and overrides.

Unset `APP_ENV_FILE` loads an optional `.env`; a nonempty process value selects a
required file; an explicitly empty value disables dotenv loading. Set this selector
outside the file being selected. Existing process variables win, even when empty;
empty decoded fields with an `envDefault` still use that default. `unset` below
means an empty value, not a generated credential.

Lists use commas. Durations use Go units such as `250ms`, `15m` or `168h`, not `7d`.
Callbacks, native clients and definitions of named queues, disks and logging
channels remain Go values. Unknown environment keys are ignored: obsolete names
are not aliases. Update settings and restart processes; there is no live reload.

The quickstart overrides several defaults, including disabling storage. Full
`.env.example` is a reference, not a ready production profile. Container addresses,
secrets and enabled features must match the deployment. These tables describe the
application adapter; independent `pkg` clients may expose additional native options.


## Find the setting you need

| Task | Reference | Usage guide |
| --- | --- | --- |
| Tune process budgets | Application and logging below | [Lifecycle](../docs/application-lifecycle.md) |
| Connect SQL or tune its pool | Database | [Connections](../docs/database.md) |
| Configure listener, CORS, or docs | HTTP and CORS | [Routing](../docs/routing.md) |
| Enable cache | Optional cache | [Cache](../docs/cache.md) |
| Run workers and recurring work | Queue and event settings | [Queues](../docs/queues.md), [scheduling](../docs/scheduling.md) |
| Send mail or push | Mail and push settings | [Mail](../docs/mail.md), [push](../pkg/push/README.md) |
| Choose asset visibility and URLs | Filesystems and media settings | [Media](../internal/platform/media/README.md) |
| Investigate a request | Tracing/metrics plus log channels | [Observability](../docs/observability.md) |

The tables below are the complete environment reference. Usage guides explain when
to change a value; keeping the Go default is usually enough. Avoid copying every
row into `.env`: publish connection information, credentials, intentional feature
choices, and the overrides your deployment actually needs.

## Application and logging

| Key | Default |
| --- | --- |
| `APP_NAME` | `labena` |
| `APP_ENV` | `development` |
| `APP_STARTUP_TIMEOUT` | `30s` |
| `APP_SHUTDOWN_TIMEOUT` | `45s` |
| `APP_CLEANUP_TIMEOUT` | `10s` |
| `MIGRATION_TIMEOUT` | `5m` |
| `LOG_CHANNELS` | `stderr` |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | `http://localhost:4318/v1/logs` |
| `LOG_VIEWER_ENABLED` | `false` |
| `LOG_VIEWER_USERNAME` | unset |
| `LOG_VIEWER_PASSWORD` | unset |
| `LOG_VIEWER_BUFFER_ENTRIES` | `2000` |
| `LOG_VIEWER_BUFFER_BYTES` | `8388608` |

Process selection lives in each main.go as explicit `app.Processes` flags. HTTP
and workers automatically include their local event consumers; a separate
`cmd/scheduler` runs recurring schedules. See [entry points](../docs/entry-points.md).

Startup and cleanup have separate budgets; the shutdown groups share the drain
budget. Queue shutdown must be shorter than app shutdown. Migration timeout includes
lock acquisition; `migrate --timeout` overrides it. Set `APP_ENV=production` in
production; `migrate refresh` is then rejected, including with `--force`.

`LOG_CHANNELS` selects named definitions in `config/observability.go`: stderr, stdout,
file and observability (OTLP/HTTP). Formats, paths and levels belong to those definitions.
The observability channel exports directly to the logs endpoint; it does not write
a file. Its service identity comes from `APP_NAME` and `APP_ENV`.
The optional `/admin/logs/` viewer needs both credentials and the HTTP process.
It works without OTLP, tracing or metrics. Buffer entries must be 1–100000; the
record-byte budget must be 65536–67108864. Keep it behind HTTPS and restrict ingress
to operators. See the [local viewer](../docs/logging.md#local-log-viewer) for scope
and retention details.
Relative paths resolve against the process working directory. See
[lifecycle](../docs/application-lifecycle.md) and [logging](../docs/logging.md).


## Database

| Key | Default |
| --- | --- |
| `DATABASE_NAME` | unset |
| `DATABASE_DRIVER` | `postgres` |
| `DATABASE_URL` | unset |
| `DATABASE_MAX_OPEN_CONNS` | `20` |
| `DATABASE_MAX_IDLE_CONNS` | `5` |
| `DATABASE_CONN_MAX_LIFETIME` | `30m` |
| `DATABASE_CONN_MAX_IDLE_TIME` | `5m` |
| `DATABASE_CONNECT_TIMEOUT` | `5s` |
| `DATABASE_PING_TIMEOUT` | `5s` |
| `DATABASE_SLOW_QUERY_THRESHOLD` | `500ms` |

A nonempty `DATABASE_URL` opens a connection; serving the installed modules requires
PostgreSQL and their applied migrations. `DATABASE_NAME` is an optional diagnostic
name, not a replacement for the database selected by the URL. The reusable database
package supports MySQL, but the installed Identity, Media and Notification schemas
are PostgreSQL. Switching the driver does not translate their SQL.

Budget pool limits across every API, worker and operator process. Startup validates
and pings configured connections; migrations only run on explicit command. See
[database](../docs/database.md) and [migrations](../docs/migrations.md).


## HTTP and CORS

| Key | Default |
| --- | --- |
| `HTTP_READINESS_DEPENDENCIES` | `false` |
| `HTTP_READINESS_TIMEOUT` | `1s` |
| `HTTP_TRUSTED_PROXIES` | unset |
| `HTTP_DOCS_ENABLED` | `true` |
| `HTTP_ADDRESS` | `:8080` |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` |
| `HTTP_READ_TIMEOUT` | `15s` |
| `HTTP_WRITE_TIMEOUT` | `15s` |
| `HTTP_IDLE_TIMEOUT` | `60s` |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` |
| `HTTP_MAX_HEADER_BYTES` | `1048576` |
| `HTTP_CORS_ENABLED` | `true` |
| `HTTP_CORS_ALLOW_ORIGINS` | `*` |
| `HTTP_CORS_ALLOW_METHODS` | `GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS` |
| `HTTP_CORS_ALLOW_HEADERS` | `*` |
| `HTTP_CORS_EXPOSE_HEADERS` | `X-Request-ID,X-API-Version` |
| `HTTP_CORS_ALLOW_CREDENTIALS` | `false` |
| `HTTP_CORS_MAX_AGE` | `12h` |

`HTTP_DOCS_ENABLED=true` serves `/docs` and `/openapi.json` from the registered
operations. Disable it to hide both endpoints. No file generation is required.

There is one application listener. `HTTP_READINESS_DEPENDENCIES=true` probes the
configured database, queue Redis and limiter Redis under one shared deadline.
Cache, SMTP and telemetry are excluded; `/healthz` never probes dependencies.
A worker-only process has no HTTP health listener. See
[readiness](../docs/application-lifecycle.md#startup-and-readiness).

Origins, methods and header lists are independent, comma-separated policies.
`HTTP_CORS_ALLOW_ORIGINS=*` allows any origin; it does not override the method or
request-header policy. `HTTP_CORS_ALLOW_HEADERS=*` allows any requested header,
including `Authorization` and application-specific names. Successful preflights
return the requested names in `Access-Control-Allow-Headers`, so this also works
with credentialed requests. To restrict headers, set an explicit list instead,
such as `Authorization,Content-Type,X-Client-Version`; do not mix names with `*`.

Wildcard origins cannot be combined with credentialed CORS; credentials require
explicit allowed origins. `HTTP_CORS_EXPOSE_HEADERS` separately controls which
response headers browser JavaScript can read. It requires explicit names and
exposes `X-Request-ID` and `X-API-Version` by default. CORS is browser policy, not
access control; bearer authorization still applies. See [middleware](../docs/middleware.md).

Administrative paths under `/admin` are excluded from business CORS. Both built-in
dashboards require their separate Basic Auth credentials, reject cross-origin
reads and mutations, disable caching and framing, and must be served over HTTPS
outside local development. Preserve the external Host through your ingress.

`HTTP_TRUSTED_PROXIES` is a comma-separated list of explicit IPv4/IPv6 CIDRs for
controlled ingress proxies. It is empty by default; `/0`, hostnames and wildcard
trust are rejected. Only a trusted immediate peer can supply `X-Forwarded-For`.
The server walks the chain from right to left through trusted hops and selects
the first untrusted address. It ignores `Forwarded` and `X-Real-IP`, never changes
Host or scheme from forwarding headers, and rejects malformed or duplicate
`X-Forwarded-For` fields from a trusted peer (maximum 32 addresses / 4096 bytes).
A missing header falls back to the immediate peer.

Secure the proxy-to-backend network and prevent untrusted workloads from using
trusted source addresses. The public ingress must discard incoming forwarding
claims and rebuild the chain; later trusted proxies append their observed peer.
Use exact proxy ranges, not an entire shared cluster network. Client IP affects
HTTP/OTP rate limits and session client metadata, never user authorization.
See [ingress and readiness](README.md#ingress-and-readiness).


## Queues and local events

| Key | Default |
| --- | --- |
| `QUEUE_DASHBOARD_ENABLED` | `false` |
| `QUEUE_DASHBOARD_USERNAME` | unset |
| `QUEUE_DASHBOARD_PASSWORD` | unset |
| `QUEUE_ROUTES` | unset |
| `QUEUE_REDIS_URL` | `redis://127.0.0.1:6379/1` |
| `QUEUE_DEFAULT` | `default` |
| `QUEUE_CONCURRENCY` | `4` |
| `QUEUE_STRICT_PRIORITY` | `false` |
| `QUEUE_SHUTDOWN_TIMEOUT` | `15s` |
| `QUEUE_DEFAULT_PRIORITY` | `1` |
| `QUEUE_HIGH_PRIORITY` | `3` |
| `EVENTS_DRIVER` | `gochannel` |
| `EVENTS_SHUTDOWN_TIMEOUT` | `10s` |

`QUEUE_ROUTES` maps exact task names to configured queues, for example
`identity.welcome.v1=high,mail.send.v1=default`. Unlisted tasks use `QUEUE_DEFAULT`.
The queue names are defined in `config/queues.go`; `QUEUE_<NAME>_PRIORITY` overrides
each weight. Priorities must be positive. Concurrency is total active tasks per
process, not per queue. Strict priority can starve lower-priority queues.

Retries and per-attempt timeouts belong to task options, not global environment
settings. Native retry/classification callbacks can be supplied in Go.
`QUEUE_WEIGHTS`, `QUEUE_NAME` and database-queue settings are not supported.

Command scheduling enables the `app.command` handler and allowlisted factories;
the scheduler switch separately starts recurring enqueue. Enable command handling
on every consuming worker. The notification dispatcher runs with workers without
the cron scheduler. Administrative CLI commands and the authenticated Asynq
HTTP dashboard are separately opt-in. Dashboard credentials are required when enabled.

`EVENTS_DRIVER` accepts only `gochannel`. Events and poison messages are process-local
and ephemeral. NATS/RabbitMQ development containers do not create application drivers.
See [tasks](../docs/queue-jobs.md), [scheduling](../docs/scheduling.md) and
[events](../docs/events.md).


## Optional cache

| Key | Default |
| --- | --- |
| `CACHE_DRIVER` | `disabled` |
| `CACHE_NAMESPACE` | `default` |
| `CACHE_MAX_ENTRY_BYTES` | `1048576` |
| `CACHE_MEMORY_MAX_BYTES` | `67108864` |
| `CACHE_MEMORY_CAPACITY` | `10000` |
| `CACHE_REDIS_URL` | `redis://127.0.0.1:6379/0` |
| `CACHE_DATABASE_TABLE` | `cache_entries` |
| `CACHE_OPERATION_TIMEOUT` | `5s` |

Only the selected cache driver's settings are validated. Redis/database zero
limits and timeouts resolve to their owner defaults; memory requires positive
capacity and entry size, while zero memory byte budget selects 64 MiB. Disabled
cache and unselected driver settings do not require operational configuration.
Selected namespace/table mistakes fail before connections open.

Drivers are `disabled`, `memory`, `database` and `redis`. Memory budgets apply per
process. Database cache uses the configured Bun pool; publish its table with
`go run ./cmd/cli cache tables`, then apply it with `migrate up`. Changing
`CACHE_DATABASE_TABLE` does not rename an existing table. Expired SQL rows need
explicit `PruneExpired` work; no cleaner starts automatically.

Use an explicit namespace per app/environment. Queue, cache and authentication
limits must not share a Redis database: configuration rejects matching configured
address/database pairs. Different DB numbers do not isolate resource pressure or
outages. Do not flush Redis to resolve task or authentication failures. See
[cache](../docs/cache.md).


## Mail and mail views

| Key | Default |
| --- | --- |
| `MAIL_HOST` | `sandbox.smtp.mailtrap.io` |
| `MAIL_PORT` | `2525` |
| `MAIL_USERNAME` | unset |
| `MAIL_PASSWORD` | unset |
| `MAIL_FROM_ADDRESS` | `no-reply@example.com` |
| `MAIL_FROM_NAME` | unset |
| `MAIL_BRAND_NAME` | unset |
| `MAIL_ACCENT_COLOR` | `#1D4ED8` |
| `MAIL_LOGO_URL` | unset |
| `MAIL_WEBSITE_URL` | `https://example.com` |
| `MAIL_SUPPORT_EMAIL` | `support@example.com` |
| `MAIL_FOOTER` | `Account updates, thoughtfully delivered.` |

Unset or blank `MAIL_FROM_NAME` and `MAIL_BRAND_NAME` inherit `APP_NAME`.
`MAIL_LOGO_URL` is optional; without it, mail uses text branding.

Immediate SMTP attachments default to 20 MiB total raw bytes. Override
`config.MailConfig.MaxAttachmentBytes` in Go if needed; zero uses the default.
Queued mail still rejects attachment readers and limits its encoded message to
64 KiB. SMTP options use the same pure validator as standalone mail.
The sender address is required by app; standalone SMTP may receive From per message.

Mail is always wired into the app. Startup constructs a verified STARTTLS SMTP sender,
wraps it with the existing optional Asynq enqueue capability, and registers
`mail.send.v1`. Sending opens a connection; startup does not prove SMTP delivery.
Set username and password together. SMTP and brand settings always validate;
rendering and previews do not open a connection.

Send is immediate; Enqueue stores a rendered snapshot in Redis. Both reject caller
SQL transactions. OTP mail stays immediate. For transactional multi-channel intent,
use `Notifier.Enqueue`. Replace example addresses, artwork and branding before
real delivery. See [mail](../docs/mail.md) and [templates](../docs/templates.md).


## Identity authentication

| Key | Default |
| --- | --- |
| `AUTH_GOOGLE_ENABLED` | `false` |
| `AUTH_GOOGLE_CLIENT_ID` | unset |
| `AUTH_GOOGLE_TRUSTED_PRESENTER_IDS` | unset |
| `AUTH_ENABLED` | `false` |
| `AUTH_ACCESS_TOKEN_KEY` | unset |
| `AUTH_TOKEN_ISSUER` | `labena` |
| `AUTH_TOKEN_AUDIENCE` | `labena-api` |
| `AUTH_ACCESS_TTL` | `168h` |
| `AUTH_REFRESH_TTL` | `0` |
| `AUTH_SESSION_TTL` | `0` |
| `AUTH_PHONE_VERIFICATION_ENABLED` | `false` |
| `AUTH_VERIFICATION_KEY` | unset |

`AUTH_ENABLED` enables the single email OTP signup/sign-in flow, refresh
and session endpoints. It requires PostgreSQL, configured mail, both keys and the
shared Redis limiter. Sending a code returns 202; confirming it returns 201 for a
new account or 200 for an existing account, with the user and profile-completion state.
Phone verification is not implemented; enabling it fails validation.

The two keys must be independently generated, different base64-encoded 32-byte
values. Store them as secrets and share them across replicas. Access signing-key
replacement invalidates existing access tokens; overlapping keys are not supported.
Verification-key replacement invalidates outstanding challenges and changes abuse
budget keys. Never use example placeholders as credentials.

Access lifetime must be 1 minute through 7 days (default: `168h`). Zero refresh/session lifetime
means no time-based expiry for that limit; rotation, logout and revocation still
apply. Positive refresh/session bounds are 30/90 days respectively, with access
no longer than either finite bound and refresh no longer than a finite session.
Use `168h` refresh and `720h` session for seven-day inactivity and a 30-day absolute
limit. Identity's SessionPolicy owns validation. Concurrent sessions are controlled
by `domain.AllowConcurrentSessions` in Go.

Google also requires authentication enabled and the configured client ID as token
audience. Additional trusted presenter IDs authorize `azp`, not additional audiences.
Provider linking is not implemented; matching an existing email returns a conflict.
See [Identity](../internal/modules/identity/README.md) for exact routes and account behavior.


## Rate limits

| Key | Default |
| --- | --- |
| `API_RATE_LIMIT_ENABLED` | `false` |
| `RATE_LIMIT_REDIS_URL` | `redis://127.0.0.1:6379/2` |
| `RATE_LIMIT_NAMESPACE` | `labena` |
| `API_RATE_LIMIT_REQUESTS` | `120` |
| `API_RATE_LIMIT_BURST` | `120` |
| `API_RATE_LIMIT_PERIOD` | `1m` |

The broad `/api/v1` limiter is opt-in. Identity still requires its narrower service
limits whenever auth or email verification is enabled. Redis failures fail closed;
limits are not bypassed. Namespace must match across replicas. Counts describe a
burst and gradual replenishment over the period. Health routes are exempt.
See [Identity rate limits](../internal/modules/identity/README.md#shared-rate-limiting).


## Notifications and push

| Key | Default |
| --- | --- |
| `NOTIFICATION_DISPATCH_INTERVAL` | `1s` |
| `PUSH_ENABLED` | `false` |
| `PUSH_EXPO_PROJECT_ID` | unset |
| `PUSH_EXPO_ACCESS_TOKEN` | unset |

Notification dispatch runs in worker processes; zero interval selects one second,
and a positive interval must be at least 100ms. Persistent delivery requires
PostgreSQL, Redis and a worker; adding email or push requires the corresponding
sender. Dispatch is independent of the cron scheduler.

Push is opt-in and uses Expo. A project UUID is required when enabled; the access
token is required by Expo when enhanced push security is enabled. Keep provider
credentials only on the server. Receipt polling uses the existing dispatcher and
worker, not a separate container. See [notifications](../internal/platform/notification/README.md)
and [push](../pkg/push/README.md) for subscriptions, receipts and limits.


## Optional tracing and metrics

| Key | Default |
| --- | --- |
| `TRACING_TRUST_INCOMING` | `false` |
| `TRACING_ENABLED` | `false` |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | `http://localhost:4318/v1/traces` |
| `TRACING_SAMPLE_RATIO` | `1` |
| `TRACING_EXPORT_TIMEOUT` | `5s` |
| `TRACING_SHUTDOWN_TIMEOUT` | `3s` |
| `METRICS_ENABLED` | `false` |
| `METRICS_ADDRESS` | `127.0.0.1:9090` |

Tracing and metrics are independent. The trace endpoint is a complete OTLP HTTP
traces URL. In containers, localhost means that container; select a reachable
collector. Trust incoming propagation only at an intentional trusted boundary.
`APP_NAME` supplies both telemetry identities.

Metrics use a separate listener. Give host processes distinct addresses and keep
metrics private. Tracing export failures do not fail business operations; bounded
buffers may lose spans. See [observability](../docs/observability.md).


## Filesystems

| Key | Default |
| --- | --- |
| `FILESYSTEM_ADDITIONAL_DISKS` | unset |
| `FILESYSTEM_DISK` | `minio` |

`FILESYSTEM_DISK` selects a named definition from `config/filesystems.go`, not a
driver. `disabled` omits the default disk. `FILESYSTEM_ADDITIONAL_DISKS` explicitly
opens other configured names even when the default is disabled. Unselected disks
stay unopened. Definitions are local, s3, minio, spaces, gcs and azure; MinIO and
Spaces use the S3 driver. Adding a named disk in Go exposes its prefixed fields.

For each definition, substitute its uppercase name for `<NAME>` below. The selected
driver uses only relevant fields: local uses ROOT; S3 uses BUCKET, REGION, ENDPOINT,
USE_PATH_STYLE and its key fields; GCS uses BUCKET and CREDENTIALS_FILE; Azure uses
BUCKET, ENDPOINT, ACCOUNT_NAME and ACCOUNT_KEY. Other fields are ignored.


| Key | Default |
| --- | --- |
| `FILESYSTEM_<NAME>_VISIBILITY` | `public` |
| `FILESYSTEM_<NAME>_URL_MODE` | `internal` |
| `FILESYSTEM_<NAME>_PUBLIC_URL` | `unset` |
| `FILESYSTEM_<NAME>_ROOT` | `storage/app` |
| `FILESYSTEM_<NAME>_BUCKET` | unset |
| `FILESYSTEM_<NAME>_REGION` | `us-east-1` |
| `FILESYSTEM_<NAME>_ENDPOINT` | unset |
| `FILESYSTEM_<NAME>_USE_PATH_STYLE` | `false` |
| `FILESYSTEM_<NAME>_ACCESS_KEY_ID` | unset |
| `FILESYSTEM_<NAME>_SECRET_ACCESS_KEY` | unset |
| `FILESYSTEM_<NAME>_SESSION_TOKEN` | unset |
| `FILESYSTEM_<NAME>_CREDENTIALS_FILE` | unset |
| `FILESYSTEM_<NAME>_ACCOUNT_NAME` | unset |
| `FILESYSTEM_<NAME>_ACCOUNT_KEY` | unset |


MinIO overrides the generic defaults with these development fixtures:

| Key | Default |
| --- | --- |
| `FILESYSTEM_MINIO_BUCKET` | `uploads` |
| `FILESYSTEM_MINIO_ENDPOINT` | `http://127.0.0.1:9000` |
| `FILESYSTEM_MINIO_USE_PATH_STYLE` | `true` |
| `FILESYSTEM_MINIO_ACCESS_KEY_ID` | `demo` |
| `FILESYSTEM_MINIO_SECRET_ACCESS_KEY` | `local-demo-minio` |

S3, GCS and Azure support their native credential chains when explicit credentials
are absent. Mount credential files read-only. Cloud buckets/containers must already
exist; Compose/Tilt storage setup creates the development MinIO bucket. Host MinIO
uses the address above; containers use `http://minio:9000`.

Local storage needs a writable root and deliberate shared storage for multiple
replicas. Changing a disk name or location does not migrate existing assets.
See [filesystems](../docs/filesystems.md) and [Media](../internal/platform/media/README.md).

## Diagnose configuration failures

Start with the selected file, existing process overrides and the relevant section
above. Avoid dumping an entire environment or connection URL into a bug report.
CLI help also constructs the application and can fail on unavailable resources.
Use the [troubleshooting guide](../docs/troubleshooting.md) to locate the failing
stage and choose a recovery action.

## Media URLs

| Variable | Default |
| --- | --- |
| `MEDIA_BASE_URL` | `http://localhost:8080/api/v1/media` |
| `MEDIA_SIGNING_KEY` | unset |

`MEDIA_BASE_URL` is the browser-reachable media route prefix, not a bucket URL.
Set it to your HTTPS API origin and `/api/v1/media` in production. An absolute
path is also supported for same-origin clients. Never derive it from request headers.
`MEDIA_SIGNING_KEY` is an optional independent base64-encoded 32-byte secret shared
across replicas. It enables temporary links; without it, public URLs and authenticated
private downloads still work. Rotation invalidates existing temporary links.

## Check a configuration change

After editing a profile, restart the affected process and verify the capability's
public boundary: a readiness probe, representative request, queue inspection, or
provider test appropriate to the change. Environment values do not hot-reload.

Do not print a full decoded config to diagnose it. DSNs, provider tokens, signing
keys, and SMTP credentials require private handling even when some structs omit
them from JSON. CLI help constructs resources too; it is not a connection-free
validation command.

When adding or renaming a setting in code, update this reference and its focused
check:

```sh
go test ./internal/app/config -run TestConfigurationReferenceMatchesDefaults
```

Then verify the examples, Compose interpolation, and Kubernetes ConfigMap/Secret
wiring that consume it. An environment key listed in a document but no longer read
by the code is ignored; there is no automatic alias or migration layer.
