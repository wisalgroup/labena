# Deployment and recovery

Tilt, Compose and release verification share `deployments/Dockerfile`.
For release bundles, fixed build inputs and recorded operational checks, see
[release readiness](../docs/release-readiness.md). Development manifests and sample
credentials remain development-only; they are not a production deployment preset.

Run `go run ./cmd/app` for HTTP, Asynq workers and local event subscribers.
Run `go run ./cmd/scheduler` separately for recurring schedules.
Run `go run ./cmd/cli migrate up` explicitly before serving. The supplied Compose
migration service and Tilt init containers do this before starting application processes.
Both deployments run the scheduler separately with one replica.

## Choose the process shape

| Process | Runs | Use it for |
| --- | --- | --- |
| `cmd/app` | HTTP, workers and local event consumers | Application service deployment |
| `cmd/worker` | Queue workers and local subscribers; no cron scheduler | Dedicated background execution |
| `cmd/scheduler` | Central recurring schedules | One deployment per schedule set |
| `cmd/cli <command>` | One command after application construction | Migration, inspection, maintenance |
| `cmd/cli serve` | Explicit HTTP and workers selection in main | Serving through the command tree |

HTTP producers need their local subscribers in the same process. A different
worker process cannot receive another process's GoChannel events. Run exactly one
scheduler for a schedule set; ordinary workers and the notification dispatcher do
not require cron scheduling. See [lifecycle](../docs/application-lifecycle.md).

## Ingress and readiness

Set `HTTP_READINESS_DEPENDENCIES=true` in deployed applications. The supplied
Compose environment, Kubernetes profile and full `.env.example` enable it
explicitly. `/readyz` then checks configured database and Redis dependencies;
`/healthz` remains independent of downstream availability. Keep readiness timeouts
below the probe timeout and use readiness, not liveness, to remove an unhealthy
instance from service.

For direct connections, leave `HTTP_TRUSTED_PROXIES` empty. Behind a controlled
ingress, set its actual source CIDRs and restrict backend access accordingly:

```dotenv
# Documentation example only: replace with the actual ingress addresses.
HTTP_TRUSTED_PROXIES=192.0.2.10/32,2001:db8::10/128
```

The first ingress strips client-supplied `X-Forwarded-For` and writes the observed
client address. Every additional trusted proxy appends its observed peer. Verify
the complete path, including alternate maintenance access, before enabling trust.
The application uses the first untrusted address before that trusted suffix for
client rate limits. With no trust configured, clients behind a proxy share its
budget. `api.PeerIP` remains the transport peer; `api.ClientIP` exposes the accepted
client address. Neither value establishes an authenticated principal.

Both administrative dashboards require HTTPS and a preserved external Host.
Business CORS never grants access to `/admin`; cross-origin reads, preflights and
mutations are rejected even when the browser has cached valid Basic Auth.

## Retention for version 0.1

Version 0.1 retains notification delivery records and media tombstones until the
application owner performs an explicit data-retention migration. There is no
automatic age-based purge. Delivery records retain occurrence-key deduplication;
media tombstones retain repeated-delete behavior. Subscription pruning disables
stale destinations without deleting delivery history. File logs keep their
separately configured rotation policy, and the viewer's live buffer is bounded.

Monitor retained table size and include these records in backup/restore tests.
Before introducing a finite retention window, define the longest supported
business retry/replay window and privacy requirements. Preserve deduplication
keys and necessary tombstone identifiers for that window, and scrub sensitive
payloads separately only after terminal delivery and receipt processing. A purge
must exclude pending/in-flight work and document how older replays and repeated
deletes behave. Account-erasure requirements need an owner-specific migration;
they are not satisfied by the generic maintenance schedule.

## Build identity

Source runs report `dev`; OpenAPI uses the same application version as the CLI
and startup logs. `go run ./cmd/cli --version` also reports the VCS revision when
Go records it. CLI version inspection follows normal application construction.
Release images receive `VERSION` and `REVISION` build arguments from
`scripts/build-release.sh`; build from a clean, tagged commit. The API contract
header stays `X-API-Version: v1`, independently of the application release.

## Environment loading

`.env.example` is the full local feature profile. Copy it to the ignored `.env`
and fill its credential fields before running the app. `app-config.yaml` is the
corresponding Kubernetes profile, with cluster endpoints and no private credentials.
Both use Go defaults for routine tuning; see the complete
[configuration reference](configuration.md).

Tilt loads the ignored `.env.mail.yaml` as the `app-mail` Secret. This existing file
holds local application credentials, including settings added beyond SMTP:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-mail
  namespace: go-modular-dev
type: Opaque
stringData:
  MAIL_HOST: sandbox.smtp.mailtrap.io
  MAIL_PORT: "2525"
  MAIL_USERNAME: ""
  MAIL_PASSWORD: ""
  AUTH_VERIFICATION_KEY: ""
  AUTH_ACCESS_TOKEN_KEY: ""
  AUTH_GOOGLE_CLIENT_ID: ""
  QUEUE_DASHBOARD_USERNAME: operator
  QUEUE_DASHBOARD_PASSWORD: ""
  PUSH_EXPO_PROJECT_ID: ""
  PUSH_EXPO_ACCESS_TOKEN: ""
```

Fill the blank values with your own credentials. Generate each Identity key
independently with `openssl rand -base64 32`, and retain the same keys across
replicas and restarts. Feature switches belong in `app-config.yaml`, so the Secret
does not silently override them. The checked-in `app-secrets` contains only the
development database/Redis connections and MinIO fixtures. Keep real credentials
out of tracked files. CLI help and migrations also validate the full profile.

Jobs use Asynq and Redis through the native Asynq integration. Local events use Watermill GoChannel.
See the [queue guide](../docs/queues.md) for configuration, registration,
startup, delivery guarantees, retries and observability.

## Optional cache

CACHE_DRIVER defaults to disabled. Select memory, database or redis independently
of the queue and pubsub drivers. Database cache shares the configured Bun connection
and uses its own central cache_entries migration. CACHE_REDIS_URL is provided through
the development app Secret; CACHE_NAMESPACE and limits use Go defaults unless overridden. Defaults
and operational details are in [configuration](configuration.md#optional-cache).

Memory cache uses `CACHE_MEMORY_MAX_BYTES=67108864` (64 MiB) by default in addition
to entry count and per-entry limits. This replaces the previous effective budget
of entry count multiplied by entry size. Raise the byte budget deliberately for
existing larger caches and leave process/container headroom for metadata and
in-flight copies. Full caches reject writes rather than evict live entries.

## Working directory for binary and IDE launches

Use the application root as the working directory, regardless of where the binary
lives. Logging fixes relative file paths against that directory at construction;
there is no go.mod discovery or executable-path fallback.

For an installed binary, run `cd /opt/myapp` followed by `./bin/app`. A systemd unit
makes the same contract explicit:

```ini
[Service]
WorkingDirectory=/opt/myapp
ExecStart=/opt/myapp/bin/app
```

In an IDE, set its launch working directory to the repository root. For VS Code's
Go launch configuration, use `"cwd": "${workspaceFolder}"`. Tilt uses the Docker
image's /app directory. Use stdout/stderr or direct OTLP for read-only container deployments;
file logging requires writable /app/storage/logs, and local file storage requires
writable /app/storage/app (or FILESYSTEM_LOCAL_ROOT).

## Persistent application storage

Use one Docker Desktop cluster with the **kubeadm** provisioner. In Docker Desktop,
open Kubernetes → Edit cluster → Kubeadm → Save changes. Switching provisioners
resets all Kubernetes workloads and volume data, including other projects. Do
this only when you intend to discard that data. Keep the checkout available in
Docker Desktop's Resources → File Sharing settings.

```sh
tilt up --context docker-desktop
tail -f "storage/logs/app-$(date -u +%F).log"
```

Tilt creates `storage/logs` and `storage/app`, resolves the absolute `storage` path,
and mounts it at `/app/storage` in both the app and migration containers. Writes go to the
same local daily file, `app-YYYY-MM-DD.log` in UTC; no copying service or polling
interval is involved. Rerun the tail command after the UTC date changes. Filesystem
buffering and editor reload behavior can affect when a reader displays new records.
Tilt uses your host UID/GID so the app and your editor can both write the files.
The container root filesystem remains read-only; the shared storage directory
contains logs and local uploaded objects.

Each project uses its own checkout folder and namespace on the same cluster.
Database and broker data still use their own PVCs. Local logs and uploaded objects
survive Pod and cluster deletion; deleting the local files removes them. The
storage mount has no quota. The application file channel rotates daily and retains
14 days by default; that limits age, not total disk usage. Runtime logs and default
local uploads are ignored by Git and Docker builds. This is a local development setup; production should use deployment-owned persistent storage and a deliberate logging
policy such as standard output or direct OTLP. Apply the app through Tilt: it replaces the
manifest's placeholder host path and default container identity.

Docker Desktop's Kind provisioner currently does not expose the required Mac
host paths. A missing host folder fails the mount instead of silently creating
an unrelated node directory. No separate cluster or NFS server is needed with
kubeadm. Re-run Tilt if you move the checkout.

## Run the application with Docker Compose

The single `deployments/compose.yaml` contains the app, migrations, infrastructure,
and persistent volumes. Run the complete stack:

```sh
docker compose -f deployments/compose.yaml up --build -d
```

Use this instead of Tilt or the host app because their default ports overlap.
The app waits for PostgreSQL migrations and Redis health. With the optional
storage profile it also waits for MinIO bucket setup. All application containers
share the named `app-storage` volume mounted at `/app/storage`, use `/app` as their
working directory, and log to stderr and file. Fresh volumes inherit the image's
UID/GID 10001 ownership. Existing volumes must already permit that user to write;
the image does not recursively change existing data ownership at startup.

The named volume is stored by Docker and survives container replacement and
ordinary `down`. It is not your local project's storage directory. Export a snapshot:

```sh
mkdir -p storage/logs
docker compose -f deployments/compose.yaml cp api:/app/storage/logs/. storage/logs/
```

To stop while retaining volumes:

```sh
docker compose -f deployments/compose.yaml down
```

Adding `--volumes` destroys these volumes, including database data. These are local
development manifests with sample credentials; production needs deployment-specific
credentials, storage backups, and log retention. Rotation belongs to the configured
logging channel; adding a volume alone does not configure it.

## Optional observability stack

Tilt starts the stack by default with `tilt up`. Disable it with
`tilt up -- --observability=false`; this also disables app tracing and metrics and
the direct OTLP observability log channel. Alloy receives logs and spans over
HTTP; it does not mount or collect application log files. Existing explicit Tilt arguments still take precedence.
Compose continues to use its observability environment profile. Both share backend
configs and provision Grafana at localhost:3000. Tilt also forwards Alloy at
localhost:4318 for a host application's trace and log exporters.
See [setup and operational limits](../docs/observability.md).

## Authentication and API rate limits

Identity uses Redis admission limits whenever `AUTH_ENABLED` is enabled. Set `RATE_LIMIT_REDIS_URL` (default `redis://127.0.0.1:6379/2`) and
`RATE_LIMIT_NAMESPACE` consistently across replicas. This is separate from cache
and queue databases. Redis unavailability returns 503 instead of bypassing limits.
`API_RATE_LIMIT_ENABLED=true` adds broad admission to `/api/v1`; common health routes
are exempt. Defaults: `API_RATE_LIMIT_REQUESTS=120`, `API_RATE_LIMIT_BURST=120`,
`API_RATE_LIMIT_PERIOD=1m`. Authentication applies stricter service-level policies.
Budgets replenish gradually and survive application restarts while Redis retains them.
Do not flush limiter keys with cache data; budget loss resets abuse protection.

Identity uses a pre-release PostgreSQL baseline. Older development schemas may
need a deliberate forward migration or a disposable reset; startup never rewrites
them. Do not reset a database containing required data. Clients use direct JSON
Google ID-token exchange.
See [Identity behavior and rollout](../internal/modules/identity/README.md) for details.

The full development profiles enable `HTTP_READINESS_DEPENDENCIES=true`.
Code defaults and the quickstart keep it disabled.
`HTTP_READINESS_TIMEOUT=1s` is the shared deadline for configured database,
queue Redis and rate-limit Redis checks. Set the deployment readiness probe timeout
above this budget. `/healthz` never probes dependencies. Cache, mail and telemetry
backends do not gate readiness. A shared dependency outage may remove every replica
from traffic; enable this policy only when that behavior matches your deployment.

## Experimental brokers

RabbitMQ and NATS are not used by the current app. Compose starts them only with
`--profile extra-brokers`; Tilt starts them only with `-- --extra-brokers=true`.
They are not app startup prerequisites. These switches provide infrastructure for
experiments, not supported application messaging drivers. Existing volumes remain
unmodified when the profile is disabled.

## Optional object storage

Tilt enables MinIO and its disk by default; use `-- --storage=false` to omit both.
For Compose, enable the storage profile and disk together:

```sh
FILESYSTEM_DISK=minio docker compose -f deployments/compose.yaml --profile storage up --build -d
```

Compose waits for bucket initialization before starting the API when the storage
profile is selected. Tilt includes the MinIO resource and bucket initialization
unless the storage option is explicitly false. The full application configuration,
including provider credentials, must be valid before its migration command runs.

Compose respects an explicitly configured `FILESYSTEM_DISK`, including values in
an existing `.env`. For a minimal stack regardless of that file, run:

```sh
FILESYSTEM_DISK=disabled docker compose -f deployments/compose.yaml up --build -d
```

Disabling storage does not delete existing objects or volumes. These commands
configure local infrastructure; they do not add upload processing to Media.

## Optional push

Push uses Expo Push Service externally; no local push container is required. Supply
`PUSH_EXPO_PROJECT_ID` and `PUSH_EXPO_ACCESS_TOKEN` (when enhanced push security is
enabled). Compose forwards these settings to application processes; no Firebase
service-account credential mount is needed. Worker processes dispatch notification
intents every second without requiring the cron scheduler.

The development notification baseline schema now includes subscriptions and new
channel/destination fields. Existing databases created from the previous baseline
need an explicit schema migration or a deliberate disposable-development reset;
application startup does not rewrite or reset existing tables.

## Recovery and retained state

Deployment backup policy must cover each source of retained state:

| State | Recovery requirement |
| --- | --- |
| PostgreSQL | Preserve accounts, sessions, notification intent/read state, media metadata and Goose history |
| Queue Redis | Configure persistence and backup for the accepted task backlog; ordinary jobs and queued mail have no SQL outbox |
| Rate-limit Redis | Preserve budgets separately from cache and queue administration; resetting them resets abuse controls |
| Object storage or local asset volume | Restore compatible object versions alongside media metadata; metadata alone cannot restore bytes |
| Secrets and configuration | Restore the matching keys, stable disk names and connection settings through protected deployment configuration |
| Cache and telemetry | Treat cache as reconstructible state; manage logs/traces under their own retention policy |

Rehearse recovery in an isolated deployment before claiming a recovery target:

1. Record the source revision, migration version, queue configuration and backup
   timestamps. Quiesce producers/consumers when required by the chosen consistent
   backup procedure; a database and Redis snapshot are not one atomic snapshot.
2. Restore to isolated databases, Redis and storage. Keep workers, schedulers,
   SMTP and push stopped or directed to test destinations so replay cannot send
   real messages during validation. Every CLI invocation still opens its configured
   resources, so use a dedicated recovery profile.
3. Inspect `migrate status` and representative account, notification and asset
   records. Validate object contents as well as metadata; run ordinary schema
   upgrades only according to the source revision's migration plan.
4. Inspect pending/retry/archived tasks and the notification ledger. SQL intent
   can be redispatched if Redis was lost; an ordinary task lost from Redis cannot
   be reconstructed automatically. Restoring an older ledger can repeat a provider
   effect that happened after the backup.
5. Exercise authorized reads, a new write, rollback, an idempotent task and asset
   download in that isolated environment. Media recovery cleans known interrupted
   rows; it does not reconcile unknown bucket objects after mismatched backups.
6. Record actual data loss and recovery time, then plan the cutover and controlled
   backlog replay. Start only one scheduler per schedule set and monitor the drain.

These are recovery requirements, not a completed deployment rehearsal. Native
backup commands, retention and restoration guarantees depend on the selected
deployment. Record a new rehearsal for the source and target you will publish. For current failures,
start with [troubleshooting](../docs/troubleshooting.md).

## Plan rollout and termination

A release deployment should make the sequence explicit:

1. Build an immutable image from the intended source revision.
2. Apply compatible migrations through one controlled migration execution.
3. Start application processes with the matching configuration, keys, and task handlers.
4. Wait for startup/readiness before routing traffic.
5. On replacement, send termination to the application process and allow its drain
   and cleanup budgets before forcibly stopping the container.

`APP_SHUTDOWN_TIMEOUT` defaults to 45 seconds and cleanup has a separate 10-second
budget. Choose a platform termination grace period with room for both and its own
signal/routing overhead. Queue shutdown must fit inside the application drain
budget. Increasing a timeout does not fix work that ignores cancellation.

For rolling changes, account for old and new workers sharing queues and old/new
processes using one schema. Preserve task names and payload compatibility until
accepted work is drained or deliberately migrated. Do the same for public event
contracts and stable storage disk names. Do not use `migrate refresh` during rollout.

## Verify the deployment you operate

| Exercise | Observe |
| --- | --- |
| Fresh install | Registered migrations, an authorized read/write, and required capability outputs |
| Dependency outage | Expected readiness and safe operation failures, then recovery |
| Active-work shutdown | Admission stops, work exits within bounds, dependencies close afterward |
| Backup restore | Representative rows, pending work, and asset bytes agree in an isolated target |
| Optional provider | Actual credential and delivery behavior for your provider/account |
| Telemetry outage | Business operations remain independent; export loss is visible |

The [release readiness](../docs/release-readiness.md) page distinguishes historical
checks from evidence still needed for a release. The supplied local manifests do
not establish production availability, recovery targets, or provider support.
