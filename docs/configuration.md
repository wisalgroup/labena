# Configuration

Load settings once, validate them, then pass typed values into application
construction. Business services receive the values and clients they need; they
do not read environment variables during an operation.

## Start with a profile

For your first run, use the profile and services in [installation](index.html):

```sh
export APP_ENV_FILE=.env.quickstart.example
go run ./cmd/cli migrate up
go run ./cmd/app
```

For local edits, copy that profile to an ignored `.env` file if you do not already
have one, then unset the selector:

```sh
cp -n .env.quickstart.example .env
unset APP_ENV_FILE
```

`cp -n` preserves an existing `.env`. Edit it deliberately instead of overwriting
working credentials. Never commit real credentials to an example file.

| File | Use it for |
| --- | --- |
| `.env.quickstart.example` | First success with tutorial PostgreSQL/Redis; optional features off |
| `.env.example` | Full implemented feature set: fill provider credentials before use |
| `.env.postgres.example` | Expanded local settings, including MinIO; check its endpoints against your services |
| `.env.asynq.example` | Component-oriented settings; supply a database URL before serving installed modules |
| `deployments/development/kubernetes/app-config.yaml` | Public Kubernetes feature switches and cluster endpoints |

Examples deliberately differ. The full example enables Redis cache, MinIO,
Identity/Google, Expo, queue tooling and observability. It is not a credential-free
quickstart. Mail is always present and opens SMTP only when sending.

## Set the application name

Set `APP_NAME=MyApp` for the API title, mail defaults, log viewer and telemetry
service identity. Mail may override the sender and brand with `MAIL_FROM_NAME`
and `MAIL_BRAND_NAME`; leave them blank to inherit the app name. A logo is optional.
This setting does not change Go imports, database names, JWT issuers or deployment
resources. [Create an application](creating-applications.html) handles the initial
module rename and branding together.

## Understand precedence

The loader takes one isolated snapshot:

1. Read the selected dotenv file, if any.
2. Overlay process environment variables, including explicitly empty values.
3. Decode typed settings and apply `envDefault` values.
4. Validate the result without opening connections.

| Selector | Behavior |
| --- | --- |
| `APP_ENV_FILE` unset | Read `.env` if it exists |
| `APP_ENV_FILE=.env.local` | Require this exact file |
| `APP_ENV_FILE=''` | Read no dotenv file |

Loading does not modify the process environment. An empty process variable wins
over a dotenv value, but a field with `envDefault` can still receive its default.
Empty strings are not a universal off switch.

Override one value for a run:

```sh
APP_ENV_FILE=.env.quickstart.example HTTP_ADDRESS=127.0.0.1:8081 go run ./cmd/app
```

The server now listens on port **8081**. Other settings still come from the selected
profile and Go defaults.

## Keep routine settings at their defaults

Environment files should express deployment decisions: enabled features,
connections, credentials, public origins and deliberate tuning overrides. They do
not need to repeat every timeout and pool size.

```dotenv
APP_NAME=orders
APP_ENV=production
DATABASE_DRIVER=postgres
DATABASE_URL=postgres://USER:PASSWORD@DB_HOST:5432/DB_NAME?sslmode=require
QUEUE_REDIS_URL=redis://REDIS_HOST:6379/1
EVENTS_DRIVER=gochannel
CACHE_DRIVER=disabled
FILESYSTEM_DISK=disabled
```

This is a **fragment**, not a complete production profile. Set the remaining
settings required by enabled features. `APP_NAME` supplies telemetry's standard
`service.name`; its Go field is `App.AppName`.

Find all supported keys, exact defaults and validation constraints in the
[configuration reference](configuration-reference.html).

## Enable capabilities deliberately

| Capability | Settings to consider | What else must exist |
| --- | --- | --- |
| Authentication | `AUTH_ENABLED`, verification/access keys | PostgreSQL, SMTP, rate-limit Redis |
| Redis cache | `CACHE_DRIVER=redis`, `CACHE_REDIS_URL` | A separate Redis database from queues and rate limits |
| Object storage | `FILESYSTEM_DISK`, selected disk credentials | Bucket and its intended access policy |
| Push | `PUSH_ENABLED`, Expo project/access settings | Valid Expo registrations and worker delivery |
| Traces/log export | Tracing switch, `LOG_CHANNELS`, OTLP endpoints | An OTLP collector |
| Metrics | `METRICS_ENABLED`, `METRICS_ADDRESS` | A free listener address and a scraper |
| Queue dashboard | `QUEUE_DASHBOARD_ENABLED`, username/password | HTTP role; queue CLI needs no enable flag |
| Recurring work | `app.Processes{Scheduler: true}` | One designated scheduler and workers for its tasks |

Queue Redis defaults to database **1**, cache to **0**, and rate limits to **2**.
Validation rejects active stores configured with the same endpoint and database.
These are logical namespaces within Redis, not separate failure domains.

Generate each Identity key independently with `openssl rand -base64 32`; keep keys
stable across restarts and consistent across replicas. Provider secrets belong in
local ignored files or deployment secret storage, not ConfigMaps. See
[Identity](identity.html) and [deployment](deployment.html).

## Find the setting to change

The typed schema lives under `internal/app/config`:

| File | Responsibility |
| --- | --- |
| `config.go`, `load.go`, `validate.go` | Schema composition, snapshot loading and cross-capability validation |
| `app.go`, `http.go` | Application identity, lifetime budgets and HTTP listener |
| `database.go`, `cache.go` | Connections and cache policy |
| `queues.go`, `events.go` | Tasks, queue routing and local events |
| `filesystems.go`, `media.go` | Named disks, visibility, URL policy and Media settings |
| `mail.go`, `notifications.go` | SMTP, branding, Expo and delivery dispatch |
| `auth.go`, `ratelimit.go`, `cors.go` | Authentication policy and request admission |
| `observability.go` | Logging channels, tracing and metrics |

Channel formats, file retention and levels are defined in Go beside observability
settings. See [single or daily files](logging.html#single-or-daily-files).

## Select process roles

Each executable supplies explicit `app.Processes` flags in main.go. `cmd/app` and
`cli serve` select HTTP and workers; `cmd/worker` selects workers; `cmd/scheduler`
selects the scheduler. HTTP and workers include their local event consumers.
Environment variables configure connections and features, not execution choices.

Run one scheduler per schedule set. See
[entry points](entry-points.html#select-a-process) before splitting processes.

## Supply settings from Go

`config.Load()` reads the environment. Use `config.FromEnvironment` for an isolated
validated snapshot, or `config.Defaults()` as an editable baseline:

```go
cfg := config.Defaults()
cfg.App.ShutdownTimeout = time.Minute
cfg.Filesystem.Default = "local"
cfg.Filesystem.Disks["local"].Root = "storage/app"
```

This fragment imports `time` and `github.com/wisalgroup/labena/internal/app/config`. Set the
required connection details before calling `app.Serve(ctx, cfg, app.Processes{Workers: true})`.
`Defaults` performs no environment reads and does not validate the resulting
baseline; `Serve` validates programmatic settings before acquisition.

Maps and slices are immutable after assembly. Configuration has no live reload:
restart the application after changes.

## Add a setting

Add a typed field beside its owning capability's settings:

```go
ExportTimeout time.Duration `env:"EXPORT_TIMEOUT" envDefault:"30s"`
```

Then validate the range, translate it to owner options if needed, and pass it
explicitly to the consumer. Update the configuration reference and any profile
that needs a deployment-specific value. HTTP, CORS and events use complete
environment names on their fields rather than inherited prefixes.

Keep behavioral callbacks, such as queue retry classification, in Go. Do not add
runtime environment reads to a service as a shortcut around construction.

## Diagnose a configuration failure

| Failure | Resolution |
| --- | --- |
| `cannot load environment file` | Check the explicit path and working directory |
| A default replaces an empty value | Use the documented off switch or a nonempty override |
| CLI help fails before printing | It validates and constructs the full configured app |
| Redis isolation validation fails | Give queue, cache and active rate limits different databases or servers |
| Changing a file has no effect | Remove the process override and restart |

Continue with [local development](local-development.html),
[dependency injection](dependency-injection.html), or the complete
[configuration reference](configuration-reference.html).
