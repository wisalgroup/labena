# Optional local observability

Start the existing Docker Desktop Tilt setup with `tilt up`; observability is
enabled by default. Use `-- --observability=false` to opt out. For an existing
session with explicit arguments, use `tilt args -- --observability=true` to enable it.

For Compose, from the repository root:

```sh
docker compose --env-file deployments/observability/compose.env -f deployments/compose.yaml up --build -d
```

Open [Grafana](http://127.0.0.1:3000/d/labena-overview). The dashboard and Prometheus,
Loki and Tempo data sources are provisioned. Do not run both setups on the same
host ports. Both consume the same backend config files in this directory.

- prometheus.yaml: app/backend scrape targets; storage flags live in the deployments.
- alloy.alloy: one JSON file source, OTLP receiver, bounded trace export.
- loki.yaml / tempo.yaml: single-process local storage with 24-hour retention.
- grafana/: data sources, log/trace links and the editable source of the overview.
- kustomization.yaml / kubernetes.yaml: optional local Kubernetes resources. Tilt
  fills Alloy's host log path and developer UID/GID; do not apply the placeholder
  hostPath manifest directly. Separate hashed ConfigMaps trigger only the affected backend rollout on changes.
- compose.env: opt-in signals/profile for the existing single Compose file.

Grafana and Alloy's OTLP HTTP endpoint (4318) are forwarded to localhost. Local anonymous Viewer access and the
bootstrap password are unsuitable for public deployment. Files are append-only;
backend retention does not rotate the application JSON file. Volumes retain local
state on ordinary container restart, not on removal or disk loss.

See [the complete guide](../../docs/observability.md#start-the-local-stack) for usage,
resource limits, retention, outage behavior, production requirements and remaining signals.

Tempo has a 2 GiB container limit and `GOMEMLIMIT=1600MiB` in both local stacks.
The previous 512 MiB limit caused repeated OOM kills while compacting retained
trace blocks. This budget leaves room above the Go memory target for other
process memory; it is a development baseline, not a capacity guarantee.

TraceQL metrics and Grafana Trace Drilldown use Tempo 2.10's `local-blocks`
metrics-generator processor. Its WAL and trace blocks live under
`/var/tempo/generator` on the existing data volume; metric blocks are flushed to
trace storage for historical queries. All span kinds are included. This does not
enable Prometheus span metrics or service graphs. Historical metric coverage starts
with traces ingested after the processor was enabled; existing ordinary trace
blocks are not backfilled into metrics blocks.

Grafana has a 1 GiB container limit with `GOMEMLIMIT=700MiB`; its previous
512 MiB budget caused OOM restarts during dashboard queries. Each backend has
its own generated ConfigMap so unrelated configuration edits do not restart Grafana.
