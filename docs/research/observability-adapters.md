# Observability adapter review

Reviewed 2026-10-05. Keep middleware optional and providers app-owned. Replacement
must preserve redaction, bounded metric labels, context trust and resource ownership.
Upstream branch sources below are assessment snapshots, not pinned dependencies.

| Boundary | Decision and reason |
| --- | --- |
| Gin | Retain the small adapter. Upstream otelgin supports provider/propagator configuration, but also records Gin error text and request attributes beyond our allowlist. Matching the current privacy contract would require additional filtering. |
| Bun | Retain the native query hook and pool metrics. Upstream bunotel records statements and query errors; our hook exports bounded operations without SQL text or parameters. |
| Redis | Retain the native hook. Disabling statements in redisotel does not remove its raw error recording or connection metadata. Our hook intentionally excludes these. |
| Asynq | Keep native worker middleware, queue metrics collector and scheduler callback. The client function chain adds routing and propagation; it does not replace task or scheduler APIs. |
| Watermill | Keep upstream Prometheus decorators, retry, timeout and recovery. Local trace middleware supplies the project's no-baggage propagation and independent consumer traces linked to producers. |
| Mail | Keep the small mail-operation wrapper, which excludes recipients, message contents and arbitrary provider errors. No dependency replacement is justified by this review. |
| Cache and limiter | Instrument their capability contracts so outcomes are consistent across implementations without backend keys or policy values in labels. |

Source review:

- [otelgin middleware](https://github.com/open-telemetry/opentelemetry-go-contrib/blob/main/instrumentation/github.com/gin-gonic/gin/otelgin/gin.go)
- [bunotel query hook](https://github.com/uptrace/bun/blob/master/extra/bunotel/otel.go)
- [redisotel tracing](https://github.com/redis/go-redis/blob/master/extra/redisotel/tracing.go)

Revisit these choices when upstream options can enforce the same policy with less
local code. This is not a claim that all community adapters were evaluated.
