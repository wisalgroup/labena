# Release readiness

This repository is distributed as a modular Go application template. The
[support policy](../SUPPORT.md) defines latest-release maintenance, compatibility
and verification scope. The [adoption guide](upgrading.md) explains module renaming
and updates for customized applications. The following gates distinguish verified
behavior from remaining release work; a passing working-copy check is not a
published release.

## Verification matrix

| Capability | Required evidence / current gate |
| --- | --- |
| Go behavior and lifecycle | Fast CI race tests and vet with the go.mod toolchain |
| Commands and examples | CI builds cmd and examples packages |
| PostgreSQL module queries and migrations | Required disposable PostgreSQL integration job |
| Asynq, Redis cache and rate limits | Required disposable Redis integration job |
| GoChannel | Local integration tests; explicitly ephemeral, not durable delivery |
| HTTP contract and docs | Live OpenAPI contract tests, compiled guide examples, and regenerated documentation site |
| Reachable dependency vulnerabilities | Pinned govulncheck tool; advisory database remains current |
| Generic MySQL, Redis Cluster, cloud storage | Not required core CI; validate separately before claiming support |
| Live Google sign-in, SMTP providers | Local/unit coverage does not establish live provider compatibility |

## Build a local release bundle

The same `deployments/Dockerfile` serves Tilt, Compose and release
verification. Its Go builder is pinned by digest, refuses automatic toolchain
downloads, verifies module checksums and builds with readonly dependencies.
The non-root runtime contains the four application commands (app, cli, worker and scheduler), CA certificates and
the project and Go licenses. Time-zone data is embedded; there is no shell or package manager.
Keep `/tmp` writable and mount writable storage when using file logs or local media.
Select configuration explicitly, including `APP_ENV_FILE=''` in managed deployments.
Use HTTP probes rather than shell probes; run `/app/cli` directly for operator commands.

From a stable checkout, with Docker, Git, Go and `shasum` available:

```sh
./scripts/build-release.sh build/release-candidate linux/amd64
# Use linux/arm64 for an ARM deployment. The output directory must be new.
```

The build command requires a clean checkout with a version tag pointing at HEAD
(for example `v0.1.0`) and does not publish anything. It injects that version and
commit into all four binaries. Local builds without those arguments report `dev`.
It saves:

- `app`, `cli`, `worker`, `scheduler` and `binaries.sha256`;
- image identity/configuration and each binary's embedded build/module information;
- source-file checksums, Git revision, `version.txt` and working-tree status;
- the full selected module graph, target-specific license report and copied notices.

The pinned license tool runs on the host while inspecting the selected Linux target.
Keep `notices` with any binary/image distribution; the application image itself only
contains project and Go notices. Review the report and its warnings, including files
the scanner cannot inspect. The module path is `github.com/wisalgroup/labena`;
while the upstream repository is private, unauthenticated tools cannot resolve its
source/license URL. Record the upstream repository/tag and retain the local project
license in the release evidence. Independently publishing importable Go packages
would require accessible source and a separate API support declaration.
License generation does not establish provider compatibility
or expand the support policy. Tools and evidence belong to this local bundle, not
the app's runtime or go.mod.

Use the immutable identity in `image.id` for subsequent tagging/export. Keep the bundle
with the final published digest, test results and release notes. A local image ID is
not a registry publication or signature. Repeat the build from the same source and
toolchain, then compare `binaries.sha256`. Matching binaries do not imply byte-identical
OCI archives; image metadata and exporter settings also affect archive identity.
Before publication, require a clean tagged checkout. A dirty checkout's Git revision
alone cannot identify what was tested; `source.sha256` records the actual build inputs.

## How to read the recorded evidence

The dated sections below preserve historical verification. They describe the
source and fixtures tested at that time, including behavior that may since have
changed. They are not certification of the current working tree, current provider
compatibility, or current vulnerability status.

For a new release candidate, create a new evidence record rather than replacing
old checksums or silently carrying forward earlier passes. Record the commit/tag,
source checksums, toolchain, architecture, backend versions, commands, results,
skips, and operational limitations. The [testing guide](testing.md) gives the
repeatable core gate; deployment-specific recovery and provider checks remain
separate.

## Candidate hardening verification on 2026-10-18

The local v0.1.0 candidate was checked with Go 1.27.2 and disposable PostgreSQL 17
and standalone Redis 7. The release bundle records its exact source revision,
version and artifact identities; this source record does not announce publication.

| Check | Result |
| --- | --- |
| Full required integration gate | `scripts/verify-integration.sh` passed, including `go test -race -count=1 ./...` |
| Queue dashboard regression | Authenticated cross-origin mutations return 403; same-origin mutations still work. A Chrome HTML-form reproduction also returned 403 without pausing the queue |
| Subscription maintenance | Concurrent refresh remains active; cleanup skips locked rows and preserves the 1,000-row batch limit and idempotency |
| Trusted ingress | Untrusted forwarding claims cannot alter budgets; valid trusted chains separate clients; malformed, duplicate and excessive trusted chains fail closed |
| Static and build checks | `go vet ./...`, `go mod tidy -diff`, command/example builds and documentation tests passed |
| Documentation | All 56 pages regenerated with local link validation |
| Vulnerability scan | No reachable vulnerabilities or affected imported packages; one module-only advisory concerns unused OpenPGP code |
| Build metadata | A Linux ARM64 image reported injected version/revision through the CLI; source runs report `dev` |

No schema or task-payload migration is required for these hardening fixes. The
adoption changes and rollout considerations are in the [update guide](upgrading.md#version-01-hardening-changes).
`pkg/mapper`, the reserved phone-auth switch and the app's infrastructure file
remain in place. Notification records and media tombstones retain their existing
state until an explicit owner-defined retention migration.

Live provider compatibility, production ingress configuration, sustained load,
disaster recovery and the GitHub reporting channel still require deployment- or
publication-specific evidence. This local gate does not establish those outcomes.

## Local verification on 2026-10-11

This is development-template evidence, not a published release. Go 1.27.2 and
`golang.org/x/net` v0.60.0 replace versions flagged by the reachable-vulnerability
scan. See the [Go patch notes](https://go.dev/doc/devel/release#go1.27.0) and
[networking advisory](https://pkg.go.dev/vuln/GO-2026-6617).

| Exercise | Observed result |
| --- | --- |
| Required core integration | Full suite passed with race detection against disposable PostgreSQL 17 and Redis 7; all four required test endpoints configured |
| Static verification | Vet and command/example builds passed on Go 1.27.2 |
| Release bundle | Script completed for Linux ARM64; a second compilation with `go build -a` in the pinned builder produced identical app, cli and worker binaries |
| Vulnerability scan | No reachable vulnerabilities and no affected imported packages; four module-only findings remain in unused OpenPGP and Chi middleware packages |
| Dependency inventory | 490 selected modules including this application; Linux ARM64 license report has 171 entries: 69 Apache-2.0, 48 MIT, 41 BSD-3-Clause, 12 BSD-2-Clause, one MPL-2.0 (MySQL driver) |
| Fresh installation | All seven migrations applied; HTTP registration returned 201; CLI account read and local media upload/list succeeded; welcome event → job → database notification completed |
| Database outage | Pausing the isolated PostgreSQL container produced readiness 503 in about one second, while liveness remained 200; unpausing restored readiness 200 |
| Redis outage | The same readiness/liveness/recovery behavior was observed with the isolated Redis container paused |
| Kubernetes termination | Deleting the test pod invoked graceful shutdown and exited 0 in 0.414 seconds within a 70-second grace period; no active long-running request/job at deletion |
| Database and media restore | A PostgreSQL custom dump restored into a separate empty database; the account, notification and media record survived; copied local object bytes matched their SHA-256 |
| Queue restore and replay | A quiesced Redis RDB containing one pending welcome job restored into a separate Redis container; workers drained it and the existing welcome notification remained a single record |

The deployment exercise used Linux ARM64, Docker Desktop 29.6.1 and Kubernetes
1.36.1. The temporary namespace ran as UID/GID 10001 with a read-only root filesystem,
no service-account token, dropped capabilities, writable temporary/local-storage
volumes and separate startup, liveness and dependency-readiness probes. Authentication,
mail, push, scheduling and external providers were disabled for that fixture. Existing
integration checks cover authentication independently.

The recorded ARM64 binary checksums are:

| Command | SHA-256 |
| --- | --- |
| app | `b896fb1876cb84894e17611476fdd6a95b9011e5b5c32f3e91cf1a4fa2fb8cf0` |
| cli | `4fa69dbcb90d1888c6eeebaaf3a090801eb19186fecf9f63a70dbff582276fea` |
| worker | `bc36fb23e1123768aaca30a6149dd01a3f9712c824e10493ddc4f2ab6498ceda` |

These identify this working-copy verification only. Regenerate all evidence after
changing the module path or any build input. The source scan and compiled Linux
application scan both passed; that result is dated, not a permanent vulnerability
guarantee. The shared image build is now a CI gate; local runtime evidence is ARM64,
and other architectures still require their own runtime checks.

The cluster's `kubectl exec` transport still fails with an HTTP/HTTPS mismatch.
HTTP exercises used the Kubernetes API proxy; operator commands used Docker against
only the test container. No cluster settings were changed. This does not certify
`kubectl exec` or port forwarding as healthy.

Restore used stopped producers/workers and a local object snapshot with no concurrent
writers. Redis used RDB for this exercise; AOF, Redis Cluster, point-in-time database
recovery and cloud object restore need separate rehearsals. The restore target used a
separate database on the disposable PostgreSQL instance and a new Redis container.
It was a Docker process, not a Kubernetes rollout. No production RTO/RPO, sustained-load
drain, live provider delivery or historical-release upgrade guarantee was measured.

Run `scripts/verify-integration.sh` with all four test connection settings from
`.github/workflows/verify.yml`. Missing or empty settings fail before tests run.
Credentials must point to disposable services. PostgreSQL and Redis are the required
matrix; unrelated optional suites can still report skips. A successful fast job
alone is not sufficient for release.

## Platform harmonization verification — 2026-10-11

After the release-bundle exercise above, the harmonization changes passed the full
race-enabled PostgreSQL/Redis integration gate, vet, command/example builds,
focused regression checks, the shared Linux AMD64 Docker image build and the
documentation builder. A second site build produced identical output. The image
was built locally; no registry publication or Linux AMD64 runtime/load check was
performed in this pass. The 51 published URLs,
API schema, migrations, dependency versions and configuration profiles are retained.
The pinned vulnerability scan again found no reachable or imported-package findings;
four module-only advisories remain. No external provider delivery was attempted.

Regression coverage now includes safe HTTP diagnostics, transaction-effect guards,
bounded SMTP attachment reads and cleanup, selected configuration validation,
central subscription maintenance and retry-cycle diagnostics. Existing tests cover
active HTTP, queue and subscriber drain; no new Kubernetes termination, sustained
production load or disaster-recovery measurement was made for this patch.

Existing installations must deploy pruning-capable workers before enabling the
new hourly schedule; see [scheduling](scheduling.html). Mail's aggregate attachment
limit defaults to 20 MiB; see [mail](mail.html#attach-files) for ownership and retry
semantics. Standalone named storage uses configuration selection plus storage.Open.
The previous binary hashes and release bundle above identify the earlier source,
not this patch. Regenerate the bundle before publication.

## Before publication

- Record the upstream repository URL and publish the selected template adoption
  model, module-renaming instructions and [support policy](../SUPPORT.md).
- Enable GitHub private vulnerability reporting, verify the reporting entry point
  and assign report monitoring to the repository owner or a delegated maintainer;
  update the pending setup status in [SECURITY.md](../SECURITY.md) only after verification.
- Publish release notes covering fixes, compatibility changes and concrete update
  steps according to the [release-note contract](../SUPPORT.md#compatibility-and-release-notes).
- Record a tagged source revision, toolchain, dependency inventory and final license
  review, including non-Go runtime inputs such as the CA certificate bundle.
- Retain release bundles, notices and checksums; verify each published architecture.
- Run fresh installation tests. For later releases, state and test the supported
  upgrade starting points, including retained schema and queued work. This evidence
  does not promise automatic merging of customized application source.
- Rehearse database/Redis backup and restore and document measured recovery outcomes.
- Exercise process termination and dependency outages in the actual deployment target.
- Measure performance before advertising throughput, latency or resource claims.

The support policy applies to future published releases; no historical release
upgrade evidence is implied by the dated checks above. Do not rewrite applied
migrations in a deployed application; publish versioned upgrade notes for schema,
configuration, task names, event contracts and public API changes. Adopters port
fixes into their own source, following [the update workflow](upgrading.md#apply-an-upstream-update).

CI validates code and configured test backends. It does not prove disaster recovery,
cloud behavior, deployment security, exactly-once execution or lossless telemetry.
Maintain this matrix when adding a driver or capability; an interface alone is not
evidence that a replacement has the same guarantees.

## Assemble the next release evidence

| Evidence | Re-run or record for the candidate |
| --- | --- |
| Source identity | Upstream repository URL, clean tagged revision, `github.com/wisalgroup/labena` module path, exact build inputs |
| Core behavior | Race suite with all required PostgreSQL/Redis endpoints; inspect skips |
| Build and static checks | Vet, commands/examples, target image, documentation checks |
| Public contract | Live OpenAPI and schema/task/event/configuration compatibility notes |
| Operational behavior | Active-work drain, dependency outage, restart/recovery on the target |
| Retained data | Backup/restore rehearsal with measured loss/time and matched asset bytes |
| Dependencies | Current reachable-vulnerability and license review |
| Distribution | Checksums, notices, release notes, support/security contacts |

A failure or an unperformed check remains explicit in the release record. “The
interface supports a driver” and “this deployment was verified with that driver”
are different statements. Publish only the support scope backed by the candidate's
evidence. Continue with [deployment](deployment.html) and [testing](testing.md).
