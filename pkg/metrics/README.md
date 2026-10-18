# Metrics

An isolated OpenTelemetry meter provider with a Prometheus scrape handler. No global
registration, environment reads, listeners, or backend connections. The app owns its
lifetime; services borrow native meters and instruments.

```go
provider, err := metrics.New("orders", logger)
if err != nil {
    return err
}
// Give provider.Handler() to a dedicated private HTTP server.
// Pass provider.Meter("example/orders") to instrumentation.
// Stop the server and instrument users before provider.Shutdown(ctx).
```

Each provider registers Go/runtime and OS-supported process collectors in its own
registry. The handler exposes GET /metrics. Configure listener timeouts and network
access in the caller; do not mount it on a public business router.

Collection permits two concurrent requests, coalesces gathers, and responds with a
timeout after five seconds. Timeout does not stop a blocked callback. Observable
callbacks must be fast, nonblocking and avoid external I/O. The SDK limits attribute
sets to 2,000 per instrument stream; use fixed names and bounded labels anyway.

The application uses a native no-op meter provider when disabled. This package
always constructs an enabled provider. See the [application guide](../../docs/observability.md)
for configuration, HTTP metrics, PromQL examples and remaining integration phases.
