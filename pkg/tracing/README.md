# Tracing

`Open(ctx, Config, logger)` acquires an owned provider and its bounded batch worker. Disabled configuration
returns a no-op provider without an export worker. Borrow its native
`TracerProvider` for instrumentation; call `Shutdown(ctx)` after all producers stop.
Setup does not contact the collector. No process-global provider is installed. The package does not import application
configuration; the official OTLP HTTP exporter can read its standard TLS settings.

The batch queue is bounded and non-blocking; export is best-effort. Failed batches
are reported via the borrowed logger without raw endpoint/error/span data and
are not returned to OpenTelemetry's global error handler. Shutdown errors concern
the shutdown operation, not a guarantee of successful delivery. Logger lifetime
must exceed provider lifetime. See [observability](../../docs/observability.md) for
configuration, limits, examples, and the phased integration plan.

## Outgoing HTTP

`NewHTTPTransport(base, tracer)` borrows a transport and non-nil native tracer. Nil
base uses http.DefaultTransport. It clones requests, injects W3C trace context and
removes baggage from the outgoing copy. Choose destinations and redirect policy
explicitly. No global client is changed. Spans end at response headers; bodies and
errors remain unchanged. CloseIdleConnections forwards to the borrowed transport
when supported. See [examples and limits](../../docs/observability.md#trace-outgoing-http-requests).
