# Logging

Use the logger attached to the execution context. Keep messages short and put useful
context in structured fields so the same record works in the console, files, and
Grafana. Logs describe observations; they are not a transactional audit database.

## Write your first application log

Inside a service, handler, command, or task, use the supplied operation context:

```go
// Import github.com/wisalgroup/labena/pkg/logging.
logging.FromContext(ctx).InfoContext(ctx, "profile updated", "user_id", userID)
```

The app supplies the logger and request/task fields; `InfoContext` also makes the
current trace context available. Choose fields that explain the outcome without
copying the request body. Logging does not require importing application resources.

Select outputs in the environment:

```dotenv
LOG_CHANNELS=stderr,observability
```

That keeps a readable console and exports structured logs directly to the collector.
Add `file` if you also want local daily files. Formats, levels, paths, and rotation
are defined once in `internal/app/config/observability.go`:

| Channel | Output | Default level | Retention/transport |
| --- | --- | --- | --- |
| `stderr` / `stdout` | Colored readable console | INFO | Process output |
| `file` | Readable file without color | WARN | UTC daily files, 14 days |
| `observability` | OTLP/HTTP records | INFO | Bounded export to Alloy/Loki |

The application's default selection is only `stderr`; full local profiles select
more channels. An INFO request may appear in the console and Loki but not the WARN
file. That is a channel-level choice, not a missing log.

## Local log viewer

Enable the embedded viewer when you want to inspect logs without a collector or
Grafana. It shares the application HTTP listener at `/admin/logs/` and has its own
Basic Auth credentials, independent of Identity and the queue dashboard:

```dotenv
LOG_CHANNELS=stderr,file
LOG_VIEWER_ENABLED=true
LOG_VIEWER_USERNAME=operator
LOG_VIEWER_PASSWORD=replace-with-a-long-random-password
```

Serve an entry point with `app.Processes{HTTP: true}` and open
`http://localhost:8080/admin/logs/`. The browser prompts for the credentials.
Set `LOG_VIEWER_ENABLED=false` to omit the routes and live capture entirely.
Tracing, metrics and the `observability` log channel can all remain disabled.
Keep production access behind HTTPS and an operator-restricted ingress.

The viewer provides:

- Live refresh every three seconds, pause/resume, and manual refresh.
- Channel/file selection, severity filtering and case-insensitive text search,
  including structured fields and request IDs.
- Newest-first paging, expandable records, copy, and JSONL export of visible entries.
- JSON and readable Tint file previews, with light/dark appearance and keyboard controls.

Live records are copies of entries accepted by the application's named stdout,
stderr and file channels. Their existing minimum levels still apply. A record
sent to two selected channels appears once for each channel; filter by channel
when inspecting one output. Context fields and groups are preserved. Successful
viewer access logs still go to normal outputs but are excluded from live capture
to avoid polling noise; failed access remains visible.

Live capture is process-local and starts with application construction. It cannot
recover earlier stdout/stderr output, intercept unrelated `fmt.Print` or global
loggers, or read another process's stdout. Worker/scheduler history is visible
through configured files mounted into the web process; use separate paths for
each writing process. Use centralized observability for multi-instance aggregation.

File history uses only file channels defined in `config/observability.go`, including
their existing single-file or daily names. It does not enable a channel, change
retention, create directories, or accept arbitrary paths. Missing directories have
no files to list. Refresh the sources after daily rotation or removal. Text files
retain their original timestamps in the message; JSON timestamps also appear in
the time column. Only complete newline-terminated records are shown.

The default live buffer retains at most 2,000 entries and 8 MiB of record strings,
plus bounded entry/index overhead. Oldest entries expire when either limit is
reached; the view reports the number expired. `LOG_VIEWER_BUFFER_ENTRIES` and
`LOG_VIEWER_BUFFER_BYTES` adjust these bounds. Each record preview is limited to
16 KiB; exports contain those same visible previews, not complete original files.
Each file query reads at most 1 MiB and returns at most 500 entries. The page requests
200 at a time and displays up to 1,000; load older or narrow the filters to continue.
The file directory is limited to fewer than 4,096 entries. Concurrent viewer requests
are bounded; busy clients receive a retry response. File browsing is a best-effort
view of changing files, not a retained snapshot or audit archive.

The UI and every data/asset route require Basic Auth. Responses are non-cacheable,
cross-origin data reads are rejected, and log text is never rendered as HTML.
Searches use POST bodies and stay out of page URLs and browser storage. Logs may
still contain sensitive application data: the viewer does not redact arbitrary
fields, so keep secrets out of logs and restrict access appropriately.

## Preserve error identity

Return errors with `%w` and inspect them with `errors.Is`/`errors.As`. The canonical
[error handling guide](errors.md#preserve-error-identity) explains classification,
owned sentinels, validation messages, and cancellation. Avoid logging the same
returned failure at every layer.

## Translate at the boundary

HTTP handlers use `response.Error(ctx, err)` from
`internal/platform/api/response`. The boundary hides raw operational details and
records safe failure categories. See [boundary translation](errors.md#translate-at-the-boundary)
and [response bodies](responses.md#return-an-error).

## HTTP request logs

The app automatically records each completed HTTP request using the context logger.
Records include the server name, method, route template, status, and duration. Health probes and unmatched routes are included. Successful and
redirect responses use INFO, client errors WARN, and server errors ERROR.

The same JSON or tint settings apply to these records. There is one built-in access logger. See [Middleware](middleware.html#request-logging-is-built-in)
for fields and customization.

## Retrieve the context logger

Application runners, commands, and HTTP requests receive a logger through their contexts:

```go
logger := logging.FromContext(ctx)
logger.InfoContext(ctx, "operation completed",
    "user_id", userID,
    "event_id", eventID,
)
```

The returned value is a standard `*slog.Logger`. Use structured key-value fields. `FromContext` falls back to `slog.Default()` if no logger was attached, which also makes ordinary standalone contexts usable.

Choose fields appropriate to your data policy. Do not log passwords, connection URLs containing credentials, authorization headers, full configuration structs, or complete request payloads.

## Add fields for one operation

Derive a logger and pass it through the operation's context:

```go
logger := logging.FromContext(ctx).With("component", "identity", "user_id", userID)
ctx = logging.WithLogger(ctx, logger)
logger.InfoContext(ctx, "starting operation")
```

Pass this derived `ctx` to downstream calls so they can retrieve the same fields. `WithLogger` does not mutate the parent context. It requires a non-nil logger.

For HTTP middleware, call `next(huma.WithContext(request, ctx))`, where `request` is the Huma context. See the [middleware example](middleware.html#use-the-context-logger).

## Choose the output format

Edit each channel's `Options` in `internal/app/config/observability.go`. `stderr` and `stdout` use tint with colors; `file` uses tint without colors. `observability` uses structured OTLP/HTTP delivery. Console and OTLP channels default to INFO; the readable file channel defaults to WARN. These settings are hardcoded per channel, not loaded from environment variables.

The Watermill adapter maps routine library info messages to DEBUG, including
handler registration, routing and pub/sub cleanup. This keeps ordinary CLI help
and completion free of infrastructure chatter at INFO. Watermill failures still
use ERROR; application logs retain their chosen severity. Set a channel's level
to `slog.LevelDebug` in Go when those library diagnostics are needed.

Select a console channel with `LOG_CHANNELS=stderr` or `LOG_CHANNELS=stdout`. To use JSON, change that channel's format to `logging.FormatJSON`. Rebuild after changing Go channel definitions and restart after changing the selected defaults. Tint affects application output, not Tilt's own build or Kubernetes event messages.

## Configure named channels

Channel definitions live in `internal/app/config/observability.go`. A channel is a named output with its own driver, format and minimum level. The built-in drivers are `stdout`, `stderr`, `file` and `otlp`.

Select one or more default channels with a comma-separated list:

```dotenv
LOG_CHANNELS=stdout,file
```

Every normal context log now goes to both outputs. The current file channel uses
`storage/logs/app-YYYY-MM-DD.log` with 14-day retention, as defined in Go. Unknown or repeated channel names fail configuration validation.

Channels are defined in Go; `LOG_CHANNELS` selects them and
`OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` configures direct log delivery. Add another channel inside `LoggingConfig.Channels`:

```go
channels["audit"] = logging.Channel{
    Driver: "file",
    Path:   "storage/logs/audit.log",
    Options: logging.Options{
        Format:  logging.FormatJSON,
        Level:   slog.LevelInfo,
        NoColor: true,
    },
}
```

The configuration file already imports `logging` and `log/slog`. You can set different options on each channel, including a readable console and JSON files. Keep paths, formats, levels and color settings in these channel definitions.

## Single or daily files

The `file` driver supports two modes, configured per channel in
`internal/app/config/observability.go`:

```go
channels["file"] = logging.Channel{
    Driver:   "file",
    Path:     "storage/logs/app.log",
    FileMode: logging.FileDaily,
    KeepDays: 7,
    Options:  logging.Options{Format: logging.FormatJSON, Level: slog.LevelInfo},
}
```

- `FileSingle` (also the zero-value default) appends to `app.log`. `KeepDays` must
  be zero; records inside a single file cannot be expired individually.
- `FileDaily` appends to `app-YYYY-MM-DD.log`, using the UTC date at write time.
  With no extension, `app` becomes `app-YYYY-MM-DD`. It reuses the day's file after
  restart and rotates on the first write of a new day.
- `KeepDays: 7` keeps today and the previous six UTC dates. `0` keeps all dated
  files; negative values are invalid. Retention is based on filename dates, not
  modification times or the number of files.

Cleanup runs when the channel opens and on daily rotation. No background goroutine
or scheduler is needed; idle or stopped applications do not perform cleanup.
Only regular files with that channel's exact dated naming pattern are eligible;
undated files, unrelated names, directories and symlinks are left alone.
Use a distinct path per channel and per application instance, and do not combine
this writer with external rotation of its files. Writes and rotation are serialized
within a channel; there is no cross-process rotation coordination.

A failure to open today's file fails channel opening. During rotation, it is
reported through `OnError` and the next write retries; the failed record is not
redirected into yesterday's file. Retention failures are also reported; after a
successful rotation the current record is still written. Cleanup is retried on the
next rotation or reopen. Daily retention limits age, not total disk usage.

## Log to selected channels

Select outputs from the context logger:

```go
logger, err := logging.Channels(ctx, "audit")
if err != nil {
    return err
}
logger.InfoContext(ctx, "order approved", "user_id", userID)
```

This record goes only to `audit`. To select several outputs:

```go
logger, err := logging.Channels(ctx, "audit", "stdout")
if err != nil {
    return err
}
logger.InfoContext(ctx, "manual adjustment", "user_id", userID)
```

Selection preserves fields and groups added through `With` and `WithGroup`, including request and job fields. It does not change the default logger. To use the selection throughout an operation, pass `logging.WithLogger(ctx, logger)` to that operation.

During dependency assembly, use `logging.Select(deps.Logger, "audit")` and inject the resulting `*slog.Logger` into the service that needs it. No application dependency container is added to context. Selection requires a logger created by the channel manager; a standalone `logging.New` logger or an unrelated handler has no named channels and returns an error.

## Add a logging driver

`pkg/logging` accepts explicit driver functions that return a `slog.Handler` and an optional owned closer. Add a driver to the configuration returned by `LoggingConfig.Channels`:

```go
return logging.Config{
    Default: names,
    Channels: channels,
    Drivers: map[string]logging.Driver{
        "discard": func(ch logging.Channel) (slog.Handler, io.Closer, error) {
            logger, err := logging.New(io.Discard, ch.Options)
            if err != nil {
                return nil, nil, err
            }
            return logger.Handler(), nil, nil
        },
    },
}
```

Import `io` for this example and declare a channel with `Driver: "discard"` to use it. A real integration can return its own concurrency-safe handler and closer. A driver must clean up partial construction before returning an error; return a nil closer for borrowed resources. Built-in driver names cannot be overridden.

## Ownership and output failures

### Open channels when needed

| Action | Resource behavior |
| --- | --- |
| App construction | Opens selected default channels before other dependencies |
| First `Channels`, `Select`, or `Manager.Logger` for another channel | Opens that channel once; concurrent selections share it |
| Derive fields with `With` / `WithGroup` | Does not open unused channels |
| Default output cannot open | Startup fails |
| Non-default output cannot open | Selection returns an error and may be retried |
| Select after manager Close | Rejected |

Successfully opened outputs remain manager-owned, including when a later channel
in the same selection fails. App closes them after other dependencies finish.
Table-publication commands follow the same rules: stderr/stdout need no writable
log directory, but a selected file channel does.

### Resolve paths and permissions

File paths are converted to absolute paths when the manager opens, using the
process working directory. Later channel selection keeps that location even if
the working directory changes. There is no source-tree search or executable-path
fallback.

| File operation | Behavior |
| --- | --- |
| Open an existing file | Append; preserve existing permissions |
| Create missing directories | Mode `0750`, subject to process umask |
| Create a new file | Mode `0600`, subject to process umask |
| Single-file mode | No rotation or reopen after external rotation |
| Daily mode | Open the new UTC date's file on its first write |

Neither mode syncs every record. Use stdout/stderr or direct OTLP in containers,
or provide a writable volume for files. See [daily retention](#single-or-daily-files)
for cleanup timing and failure behavior.

### Understand slow or failed outputs

Outputs run in selection order and apply their own minimum level:

| Output | Logging call behavior |
| --- | --- |
| Console or file | Synchronous write; a slow writer slows the call |
| OTLP | Enqueue into a bounded asynchronous batch processor |
| Synchronous write failure | Report to stderr and continue to remaining outputs |

`slog.Logger` does not return write errors to business code. These logs are not a
transactional audit store. OTLP export failures and queue drops have separate
asynchronous reporting described [below](#send-logs-to-loki).

For custom synchronous failure reporting, set `logging.Config.OnError`. It must be
concurrency-safe and must not log back through these channels, which would recurse.

### Stop producers before cleanup

Standalone callers of `logging.Open` own the returned manager. Stop logging before
calling `Close` or `Shutdown(ctx)`; the latter propagates a cleanup deadline to
OTLP flushing. The package neither changes `slog.Default()` nor closes stdout/stderr.

## Set the application working directory

Launch from the application root. The configured base path `storage/logs/app.log` then has a predictable location.
With the current daily mode, its output includes the UTC date:

| Environment | Working directory | Log file |
| --- | --- | --- |
| Local `go run ./cmd/app` | Repository root | `<repository>/storage/logs/app-YYYY-MM-DD.log` |
| Installed binary | `/opt/myapp` | `/opt/myapp/storage/logs/app-YYYY-MM-DD.log` |
| Docker / Tilt | `/app` from the Dockerfile | `/app/storage/logs/app-YYYY-MM-DD.log` |
| Kubernetes | `/app` on both application and migration containers | `/app/storage/logs/app-YYYY-MM-DD.log` |

For local development, run commands from the repository root. Set your IDE's working directory to that same directory; for VS Code's Go launch configuration use `"cwd": "${workspaceFolder}"`. Running from a nested directory deliberately uses that directory, not the repository root.

For an installed binary:

```sh
cd /opt/myapp
./bin/app
```

For systemd, set both paths explicitly:

```ini
[Service]
WorkingDirectory=/opt/myapp
ExecStart=/opt/myapp/bin/app
```

Docker keeps `WORKDIR /app`. Kubernetes sets `workingDir: /app` on both the `api` and `migrate` containers. File output requires writable storage at `/app/storage/logs`; stdout/stderr need no writable log directory. The development containers use a read-only root filesystem and mount storage at `/app/storage`. Tilt on Docker Desktop kubeadm mounts the local `storage/logs` directory directly at `/app/storage/logs`, using your host UID/GID. Writes appear in the same local file without copying. Compose uses a named volume, which is not the local checkout. The full development profile selects `stderr,file,observability`; the application default remains stderr. See [Local development](local-development.html#keep-file-logs-with-tilt-or-compose) for storage details.

## Create a standalone logger

Outside the application lifecycle, construct one explicitly. This complete example writes a readable message without changing the global logger:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/wisalgroup/labena/pkg/logging"
)

func main() {
	logger, err := logging.New(os.Stderr, logging.Options{
		Format: logging.FormatTint,
		Level:  slog.LevelInfo,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := logging.WithLogger(context.Background(), logger)
	logging.FromContext(ctx).InfoContext(ctx, "example started", "component", "docs")
}
```

The logger does not own its writer. If you supply an opened file, its creator must close it after logging has finished.

## Panics and cancellation

The API boundary records safe panic diagnostics and preserves transport aborts.
Cancellation is not proof that an effect never happened. Follow the
[error-handling guarantees](errors.md#panics-and-streaming-failures), and pass the
same context through dependent work so logs retain correlation.


## Connect logs to traces

When an operation has a valid OpenTelemetry span, context-aware logging calls add
`trace_id`, `span_id`, and `trace_sampled` automatically. This works with named
channels, including channels selected later. Use `InfoContext(ctx, ...)` rather
than `Info(...)`; the context determines the current span. See
[observability](observability.html#correlate-logs-with-the-current-span) for examples,
sampling and grouped loggers.

## Send logs to Loki

The `observability` channel uses the `otlp` driver, not a file. Select it alongside
readable console and daily file output:

```dotenv
LOG_CHANNELS=stderr,file,observability
OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://localhost:4318/v1/logs
```

For standalone use, define an OTLP channel with the same public API:

```go
channels["telemetry"] = logging.Channel{
    Driver: "otlp",
    Options: logging.Options{Level: slog.LevelInfo},
    OTLP: logging.OTLPOptions{
        Endpoint:    "http://localhost:4318/v1/logs",
        ServiceName: "orders",
        Environment: "development",
    },
}
```

OTLP preserves structured attributes, groups, severity and active trace context.
It does not use tint or JSON text formatting; its `Options.Level` still filters
records. It opens only when selected, does not replace global OTel providers, and
needs no log directory. The app flushes it after other dependencies close.

The supplied Alloy configuration forwards OTLP logs to Loki. Request IDs and trace
IDs are queryable metadata; the message remains the log body. Grafana uses the
metadata for **View trace** and trace-to-log navigation. No file collector or
`observability.log` is needed. Local readable files remain independent.

Exporter failures are reported through an independent stderr logger, rate-limited
to once per minute with a cumulative failed-record count. The SDK also reports
queue drops to stderr. These asynchronous failures do not go through `OnError`,
which handles synchronous handler failures. Export has bounded memory and time
budgets and is best-effort, not a durable delivery guarantee. See
[observability](observability.html#configure-log-export) for limits and deployment settings.

## Troubleshoot a missing or duplicated record

| Symptom | Check |
| --- | --- |
| INFO appears in Loki but not the file | The built-in file channel defaults to WARN |
| Selected channel fails at startup | Its configuration, path permissions, or driver validation |
| Custom channel missing | Define it in Go and select it explicitly; rebuild after definition changes |
| Log lacks a trace ID | Use the context-aware method and the active operation context |
| Duplicate records in Loki | Ensure no second console/file collector forwards the same records |
| `request_id_extracted` appears beside `request_id` | An extra JSON parser extracted a field already present as metadata; remove the unnecessary parser for new OTLP logs |
| Yesterday's file stops growing | Daily mode writes the current UTC date's file |
| Old daily files remain | Retention runs on open/rotation and only targets that channel's filename pattern |

File logging is independent of OTLP export. An exporter outage does not cause local
files to be replayed later. See [observability](observability.md#filter-logs-by-request-metadata)
for request-ID filtering and [troubleshooting](troubleshooting.md) for operation
failures beyond logging.
