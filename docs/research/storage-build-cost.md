# Storage build-cost baseline

Measured locally on 2026-10-05 before changing provider organization.
Toolchain: Go 1.27.1 on darwin/arm64. The production implementation was not changed.

Two temporary main packages were compiled from the repository module:

1. Current integration: call storage.Open with Driver=local and a root supplied by
   os.Args[1], then close the returned Connection.
2. Native local floor: call fileblob.OpenBucket with the same root and local options
   (CreateDir, private directory permissions, NoTempDir), then close the bucket.

Neither program was executed. The second omits our configuration validation and
Connection wrapper and is a comparison floor, not a replacement implementation.
Both use the native Go CDK blob API.

| Program | All dependency packages | Non-standard packages | Stripped binary bytes | Empty-cache build seconds |
| --- | ---: | ---: | ---: | ---: |
| Current storage.Open, local configuration | 864 | 637 | 42,522,050 | 18.02 |
| Native fileblob-only floor | 391 | 174 | 11,817,298 | 7.60 |

Method: go list -deps counted package dependencies. Each build used its own empty
temporary GOCACHE, -p 2, -trimpath, and -ldflags="-s -w". Module downloads were
already available. These are single sequential observations on this workstation,
not repeated benchmarks, startup timings, request latency or production throughput.
Temporary caches and binaries were removed after measurement.

## Implication

The storage facade's runtime switch references every cloud provider, so even a
minimal local-configured program retains their constructors and dependencies.
Provider subpackages could let a deliberately local-only binary import only the
local implementation.

The current configurable starter app still offers every provider. Merely moving
files to subpackages while importing all providers in app would not establish a
smaller application binary. Keep its current provider selection and native bucket
API for now. If local-only build composition becomes a requirement, split provider
constructors and repeat these measurements against the actual resulting app.

No bucket keys, provider settings, credentials, connection ownership or deployment
behavior changed in this review.
