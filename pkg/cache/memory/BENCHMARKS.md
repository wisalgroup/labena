# Memory cache measurements

Measured 2026-09-30 on Apple M5 Pro, darwin/arm64, Go 1.27.1,
GOMAXPROCS=15. Fixed 1 KiB values, precomputed keys, one-hour TTL,
100% hits for reads, replacement of existing entries for updates, and a new
key rejected at capacity for RejectFull. No encoding, telemetry, network, or
maintenance worker is included. Setup fills the cache outside the timed loop;
Go's benchmark harness calibrates iterations before measuring. Each case ran
three times with a 200 ms target per sample, using closed-loop callers (15
workers for parallel reads).

```sh
go test ./pkg/cache/memory -run '^$' -bench BenchmarkStore -benchmem -count=3 -benchtime=200ms
```

The baseline is the prior single-mutex implementation from the working tree
before this change. The same benchmark cases were run before and after; GetInto
is a new optional operation. Values below are sample medians at 10,000 entries.

| Operation | Before ns/op | After ns/op | Before / after allocations |
| --- | ---: | ---: | --- |
| Get | 136.1 | 128.5 | 1 / 1 |
| Parallel Get | 300.8 | 146.6 | 1 / 1 |
| Update | 148.2 | 162.3 | 1 / 2 |
| RejectFull, no expiration due | 51536 | 41.06 | 0 / 0 |
| GetInto, reusable buffer | — | 60.71 | — / 0 |

Parallel Get ranged from 277.4–312.8 ns/op before and 142.3–151.3 after.
RejectFull ranged from 51403–51539 before and 40.67–41.14 after. Sequential
Get had a noisy after sample (128.4–218.1); do not infer a reliable improvement
from that median. Update ranged from 146.7–149.0 before and 157.0–176.8 after.
Three samples do not establish confidence intervals or production throughput.

Reads still allocate 1024 bytes unless the caller supplies sufficient reusable
buffer capacity. Updates now allocate about 1031 bytes in two allocations rather
than 1024 in one: copying the key prevents a short substring from retaining an
arbitrarily large caller string. This is a deliberate ownership cost. Buffer
reuse reduces allocation churn; these measurements do not establish a reduction
in whole-application GC CPU, retained heap, or RSS.

The full-cache improvement removes repeated scans when no entry could have
expired. Expiration cleanup remains a map scan under the write lock. Measure
expiry-heavy workloads and service p99 separately before sizing a busy cache.
No comparison with community cache implementations was performed. The driver
remains a pointer-containing Go map with bounded logical key/value storage,
not a zero-GC arena.
