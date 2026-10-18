# Support and compatibility

[Labena](https://github.com/wisalgroup/labena) is distributed as a modular Go
application template. Start from a
published release, rename the module, and maintain the resulting application in
your own repository. The [adoption and update guide](docs/upgrading.md) explains
how to record your starting point and apply later fixes.

## Maintenance policy

Only the latest published non-prerelease version receives bug and security fixes.
Older versions receive no backports. Before the first such release, development
checkouts and prereleases are evaluation snapshots with no stable compatibility
promise.

Maintenance is best effort. There is no guaranteed response time, resolution time,
or long-term support window. Wisal Group, the upstream repository owner, is responsible for
release maintenance and private security-report triage, and may delegate those
duties to repository maintainers. Application owners are responsible for their
customizations, deployments and applying upstream fixes.

Use upstream GitHub issues for reproducible, non-sensitive bugs and feature
requests. Include the template tag or commit, Go version, relevant configuration
with secrets removed, and reproduction steps. Report vulnerabilities through the
private route in the [security policy](SECURITY.md).

## Verified scope

| Area | Scope |
| --- | --- |
| Go toolchain | The version declared in the release's `go.mod` |
| Core application backends | PostgreSQL and standalone Redis; core CI currently tests PostgreSQL 17 and Redis 7 |
| Local events | Process-local, best-effort delivery with the documented capacity and retry limits |
| MySQL and Redis Cluster | Optional driver paths; outside the required core integration matrix |
| Cloud storage, Google sign-in, SMTP and Expo | Local or fake-provider coverage where present; live compatibility requires provider-specific verification |
| Operating systems and architectures | Only the build/runtime evidence recorded for the particular release; an image build alone does not verify runtime behavior |

See [release readiness](docs/release-readiness.md) for candidate evidence and its
limitations. Support does not imply lossless events, exactly-once external effects,
automatic disaster recovery, or a throughput guarantee.

## Reusable packages

All `pkg` packages are maintained with the template and remain independent of
`internal`. None is separately released or supported as an external Go dependency
at present. The template's module path is `github.com/wisalgroup/labena`; adopting
an application includes replacing it with that application's own module path.

Reusable implementation and examples are available for application owners to
retain or extract. Extraction requires maintaining the copied code and its
dependencies. Any future independently published package must declare its public
import path, API scope, versioning and verification separately before being
advertised for `go get` consumption.

## Compatibility and release notes

Version tags identify template snapshots. During `v0`, documented breaking changes
may accompany minor releases. From `v1`, breaking changes to documented contracts
require a new major version. Patch releases preserve those contracts except where
a security correction requires rejecting previously accepted unsafe behavior;
release notes must explain that change and its migration impact.

The documented contracts include exported `pkg` APIs, HTTP operations and error
codes, configuration keys, CLI commands, persisted schemas, task names and payloads,
and event topics and payloads. Internal source layout and assembly helpers are
customization points; changes to them must be called out when adopters need to
adjust their applications. This policy does not promise automatic merges of
modified template code.

Each release must state:

- Its source tag and commit, required toolchain, and verified backends/targets.
- Bug and security fixes, with affected and fixed versions when known.
- Breaking changes, including defaults and operational behavior, and concrete
  migration steps for every affected contract.
- Required schema migrations, worker/scheduler rollout order, and handling of
  already queued tasks or stored event/notification payloads.
- Verification performed, skipped checks and known limitations.

Existing deployed migrations are immutable. Changes use new migrations and
explicit deployment instructions. A release's compatibility policy does not
authorize rewriting an adopter's schema or replacing their application files.

## Updating a customized application

Fixes to this repository do not automatically reach applications created from it.
Record the upstream tag and commit when adopting the template, follow upstream
releases and security advisories, then review and port applicable changes into
your application. Track the last incorporated release and any changes deferred.

When skipping releases, read every intervening release note and apply required
migrations in order. Validate the resulting application against its own database,
queue, storage and provider contracts before deploying. Older applications can
receive a manually ported fix, but upstream does not maintain their release
branches. See the [update workflow](docs/upgrading.md#apply-an-upstream-update).
