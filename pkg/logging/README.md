# Logging

`New(writer, Options)` creates a standard slog logger over a borrowed writer.
`WithLogger` and `FromContext` carry execution-scoped fields without changing the
global logger.

Tint output uses full `DEBUG`, `INFO`, `WARN` and `ERROR` severity names, matching
JSON output. When adopting this fix, port the Tint formatter in `logger.go` and
the log viewer parser in `internal/platform/logviewer/store.go` together. The
viewer also recognizes older Tint files containing `DBG`, `INF`, `WRN` and `ERR`;
existing files need no conversion.

For named outputs, `Open(Config)` creates a manager. `Default()` uses the configured
default channel list; `Logger(names...)` selects exact outputs. `Select(logger,
names...)` and `Channels(ctx, names...)` preserve fields and groups when switching
outputs. Unknown or repeated names return errors. Channels require the manager's
handler; wrapping it in an unrelated handler prevents selection.

Built-in drivers: stdout, stderr, append-only file and asynchronous otlp. Each channel has its own
format and minimum level. Custom drivers are explicit functions in `Config.Drivers`,
not global registrations. They return a concurrency-safe slog handler and an
optional owned closer. Standard streams are borrowed and never closed.

`Config.WrapHandler` optionally decorates each channel's handler once when it opens,
including channels selected later. The decorator must return a non-nil, concurrency-safe
handler that preserves `Enabled`, `WithAttrs`, and `WithGroup`; it borrows the output
and does not close it. This is the app's hook for bounded local log capture without
changing output selection or replacing the manager's public handler.

Open validates the complete configuration and opens only default outputs, closing
acquired resources if construction fails. Other outputs open on first explicit
selection. Concurrent selections share one output per channel. Failed opens may
be retried; outputs already acquired remain owned until Close. Field/group
derivation never opens unselected channels. Selection after Close returns an error.
Stop logger users before closing the manager. Close is idempotent and preserves
closer errors. Derived loggers share those resources and must not outlive them.

Fanout invokes each enabled output in selection order. Console/file writes are
synchronous; OTLP queues records for asynchronous export. Returned
handler errors do not stop other outputs. `Config.OnError` reports them; the default
reports directly to stderr. Callbacks must be concurrency-safe and avoid recursive
logging. Logging is best-effort, not durable transactional audit storage.

Open fixes relative file paths against the working directory with filepath.Abs,
including channels selected later. It never searches for a project root or uses
the executable location. File output creates missing directories (0750) and files (0600), subject to umask.
Existing permissions are unchanged. Single-file output does not rotate or reopen
after external rotation; daily mode rotates by UTC date. Neither mode fsyncs each record.
Prefer container standard streams and a collector where possible.

Application definitions live in `internal/app/config/observability.go`; the reusable
package never reads environment variables or imports app configuration. See the
[application guide](../../docs/logging.md) for runnable usage and extension examples.

## Trace correlation

Context-aware calls add `trace_id`, `span_id`, and `trace_sampled` from the current
OpenTelemetry span. This applies to New, built-in channels and custom channel
drivers. No valid span means no added fields. Deriving or switching loggers does
not capture a span; child operations use their own context. Correlation follows
the active WithGroup scope. Reserve these field names and use InfoContext and
similar methods. Plain slog loggers outside this package are unchanged.

## Daily file output

Set file options on a named channel:

```go
logging.Channel{
    Driver:   "file",
    Path:     "storage/logs/app.log",
    FileMode: logging.FileDaily,
    KeepDays: 7,
}
```

The default `FileSingle` (or empty mode) appends to `app.log` and requires
`KeepDays: 0`. Daily mode appends to `app-YYYY-MM-DD.log` in UTC; seven retention
days means today plus the previous six calendar dates. Zero keeps all daily files.
Cleanup runs on opening and daily rotation, not in a background worker. It only
removes matching regular dated files. Give each channel/process its own path.
Rotation failures reach `Config.OnError` and retry on the next write. Neither mode
provides size limits or per-record fsync. If a collector tails the daily channel,
configure its file target to match the dated filenames.

## OTLP log delivery

```go
logging.Channel{
    Driver: "otlp",
    Options: logging.Options{Level: slog.LevelInfo},
    OTLP: logging.OTLPOptions{
        Endpoint: "http://localhost:4318/v1/logs",
        ServiceName: "orders",
    },
}
```

The native OpenTelemetry slog bridge preserves attributes/groups, severity and
trace context, without global providers or filesystem output. OTLP format is
structured protocol data; tint is rejected. Empty service names use `unknown_service`
for standalone callers; app configuration requires a name when exporting logs.

A bounded nonblocking queue holds 2,048 records, with batches of up to 512 every
second. Oldest records drop on overflow; failed batches are discarded. Export
failures use rate-limited, payload-free stderr diagnostics; queue drops use SDK
stderr diagnostics. `OnError` only covers synchronous handler failures.

`Manager.Shutdown(ctx)` flushes within the smaller of the caller deadline and
`OTLPOptions.ShutdownTimeout` (default 3s). `Close()` uses the configured budget.
ExportTimeout defaults to 5s; zero selects these defaults. Application work never
waits for the collector on the logging call, and collector outages do not block
construction. Stop producers before shutdown. Queues are not durable, and shutdown
success is not proof that every prior batch was delivered.
