# Local development

Choose one environment, use one configuration profile consistently, and run the
same module contracts in HTTP, CLI and workers. This guide covers the day-to-day
loop after [installation](index.html).

## Choose your setup

| Setup | Best fit | Infrastructure |
| --- | --- | --- |
| Host Go process | Fast edit/restart and debugger use | Tutorial PostgreSQL/Redis containers |
| Docker Compose | A containerized app and retained local data | Services in `deployments/compose.yaml` |
| Tilt | Kubernetes development and resource inspection | Local cluster, Docker and Tilt |

Compose and Tilt publish overlapping host ports. Stop one before starting the
other. Redis is required for tasks; the application has no memory queue driver.
Events remain process-local in every setup.

## Run on the host

Start the [tutorial dependencies](index.html#start-local-dependencies), then select
the same profile for migrations, HTTP and commands:

```sh
export APP_ENV_FILE=.env.quickstart.example
go run ./cmd/cli migrate up
go run ./cmd/app
```

After editing Go code, stop the app with Ctrl+C and rerun it. These entry points do
not watch source files. In a second terminal:

```sh
APP_ENV_FILE=.env.quickstart.example go run ./cmd/cli route list
```

Expect a route table, with no new HTTP listener or workers. After configuring
Identity and creating an account, inspect it with:

```sh
APP_ENV_FILE=.env go run ./cmd/cli identity show '<user-id>'
```

Use the profile that created the account; `.env` here represents your configured
local profile, not a requirement to rename it.

## Run with Docker Compose

```sh
docker compose -f deployments/compose.yaml up --build -d
docker compose -f deployments/compose.yaml logs -f api
```

Compose runs migrations before the app and retains service data in named volumes.
It rewrites infrastructure addresses for communication between containers. Check
the [deployment guide](deployment.html) for credentials and all exposed ports.

Host and container addresses differ:

| Caller | PostgreSQL example | Redis example |
| --- | --- | --- |
| Host quickstart | `127.0.0.1:55432` | `127.0.0.1:56379` |
| App inside Compose | `postgres:5432` | `redis:6379` |

Do not copy a container-only hostname into a host app's connection URL.

## Run with Tilt

```sh
tilt up
```

Tilt builds the app for the local cluster and starts MinIO and observability by
default. Configure the local application Secret before running enabled providers.
The public ConfigMap and Secret have separate responsibilities; see
[deployment](deployment.html).

To omit optional stacks:

```sh
tilt up -- --storage=false --observability=false
```

Use the Tilt dashboard to inspect build failures, migration completion and app
logs. This is the Kubernetes workflow; a successful image build alone does not
prove that migrations or dependency connections succeeded.

## Optional object storage

For Compose, enable the MinIO profile and select its disk together:

```sh
FILESYSTEM_DISK=minio docker compose -f deployments/compose.yaml --profile storage up --build -d
```

The bucket initialization service runs before the app in this profile. Tilt does
the equivalent by default. `FILESYSTEM_DISK` from an existing `.env` can affect
Compose's selected disk. To explicitly run without storage:

```sh
FILESYSTEM_DISK=disabled docker compose -f deployments/compose.yaml up --build -d
```

Disabling storage preserves existing objects and volumes. See
[file storage](filesystems.html) for connections and [Media](media.html) for upload
metadata, visibility and URLs. A public asset's direct URL also requires an
appropriate bucket policy and a client-reachable address.

## Read local logs

```dotenv
LOG_CHANNELS=stderr
```

This uses readable terminal output. Select `stderr,file` to also write files, or
include `observability` to export structured logs directly over OTLP. File
collection is not needed for Loki.

The current file channel writes warnings and errors to
`storage/logs/app-YYYY-MM-DD.log`, using the UTC date and retaining 14 days.
Formats, levels, single/daily mode and retention are configured in
`internal/app/config/observability.go`; see
[single or daily files](logging.html#single-or-daily-files).

## Keep file logs with Tilt or Compose

Tilt's Docker Desktop kubeadm setup mounts the checkout's `storage/logs` at
`/app/storage/logs`. Apply through Tilt so the path and UID/GID are set correctly.
The Kind provisioner does not expose Mac folders through the same `hostPath`
arrangement. Compose instead shares the named `app-storage` volume; copy logs out
when needed:

```sh
mkdir -p storage/logs
docker compose -f deployments/compose.yaml cp api:/app/storage/logs/. storage/logs/
```

For request-to-trace correlation and Grafana, follow
[observability](observability.html).

## Run separate processes

First change the selection in `cmd/app/main.go` to `app.Processes{HTTP: true}`.
Then use the same database and queue settings in each terminal:

```sh
APP_ENV_FILE=.env.quickstart.example go run ./cmd/app
```

```sh
APP_ENV_FILE=.env.quickstart.example go run ./cmd/worker
```

The HTTP process must keep its own subscribers: GoChannel cannot move an event to
the worker process. Redis tasks can cross that boundary. Enable recurring
schedules with one `cmd/scheduler` process; the dedicated worker
never starts a scheduler. When metrics are enabled on several host processes,
assign each listener a different port.

See [entry points](entry-points.html) for explicit process selections and
[queues](queues.html) for task routing.

## Optional brokers

RabbitMQ and NATS are opt-in infrastructure experiments. Starting them does not
install an application driver or change local event guarantees:

```sh
docker compose -f deployments/compose.yaml --profile extra-brokers up --build -d
tilt up -- --extra-brokers=true
```

Use one of these commands for your selected environment, not both. Default
launches omit these brokers.

## Stop development services

| Action | Effect |
| --- | --- |
| Ctrl+C in a host app | Drain that process, then close its clients |
| `docker compose -f deployments/compose.yaml stop` | Stop containers and retain volumes |
| Compose `down` | Remove the stack's containers/networks; retain named volumes |
| Compose `down --volumes` | Also delete volume data |
| Stop the Tilt process | Leave Kubernetes resources running |
| `tilt down` | Remove managed resources; namespace/volume cleanup can delete development data |

Host-mounted logs survive Pod removal. Database, broker and object-store data have
their own volumes and backup needs. Do not use destructive cleanup to fix a
configuration error in data you want to retain.

## Diagnose startup problems

| Symptom | Check |
| --- | --- |
| Redis connection refused | Selected profile, host/container address and port |
| Unexpected dependency opens | Shell overrides and enabled features |
| Address already in use | Another host process or development stack |
| Enqueue succeeds but nothing runs | Worker queue names and Redis database |
| Local event never arrives | Handler registered and subscribers running in the producer process |
| No file entries after an INFO request | The default file channel records WARN and ERROR |
| Grafana has no data | Collector/export settings as well as the dashboard |

Continue with [troubleshooting](troubleshooting.html) for detailed diagnosis,
[testing](testing.html) for disposable test services, or
[your first module](modules.html) to add application behavior.
