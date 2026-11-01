# Changelog

## Unreleased

## v0.4.0 — 2026-11-01

- Require Go 1.27.3 and update the container build toolchain to address
  [GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809), affecting certificate
  verification in the prior Go 1.27.2 baseline. Rebuild deployed binaries/images.

- Add native TLS and opt-in HTTP/2 alongside HTTP/1.1, with startup credential
  validation and TLS-aware upgraded-connection shutdown.
- Add bounded WebSocket sessions, exact origin checks, heartbeat/close handling
  and a local echo/protocol example. Add native `middleware.BearerHTTP` sharing
  the existing bearer policy. Browser authentication remains explicit.
  See the upgrade guide for TLS configuration and endpoint mounting. No schema
  or queued-payload changes; HTTP/3 and HTTP/2 extended CONNECT are not included.

### Adoption and verification

Source is the commit identified by the annotated `v0.4.0` tag. The required
toolchain is Go 1.27.3; rebuild deployed binaries and images for the certificate
verification fix. Existing plaintext HTTP/1.1 defaults are preserved. TLS,
HTTP/2 and WebSocket routes are opt-in; follow the
[native protocol adoption guide](docs/upgrading.md#native-http2-and-websocket-sessions).
No schema migration, worker rollout ordering or queued-payload conversion is required.

Verification on macOS ARM64 with Go 1.27.3 passed formatting, vet, race-enabled
Go tests (including architecture checks), command/example builds, dependency tidy,
and documentation generation/tests. Real clients verified TLS negotiation,
HTTP/2 drain, ws/wss messages and session shutdown. Targeted govulncheck scans of
the HTTP server and WebSocket integration reported no reachable vulnerabilities.
PostgreSQL/Redis integration, container builds and live provider checks were not
run; no new backend or deployment-target verification is claimed.

## v0.3.2 — 2026-11-01

- Move reusable bearer, rate-limit and multipart HTTP policies into
  `internal/platform/api/middleware`; share `NoStore` for sensitive routes.
  Move the authenticator contract and principal context/ownership helpers into
  `platform/auth`. Update imports and use `auth.FromContext` in place of
  `api.Principal`; see the upgrade guide. Existing HTTP and security behavior
  is preserved, with no schema or queued-payload changes.

### Adoption and verification

Source is the commit identified by the annotated `v0.3.2` tag; the required
toolchain is Go 1.27.2. This patch preserves HTTP behavior but changes internal
template import paths and helper names. Existing applications must follow the
[middleware and auth migration table](docs/upgrading.md#http-middleware-and-authentication-helpers).
No schema migrations, worker rollout ordering or queued-payload conversions
are required.

Formatting, vet, race-enabled Go tests (including architecture checks),
command/example builds, and documentation generation/tests passed.
PostgreSQL/Redis integration, container builds and live provider checks were not
run; no new backend or deployment-target verification is claimed.

## v0.3.1 — 2026-11-01

- Separate application error `Kind` from public `Code`; migrate classification
  constants/comparisons to `KindX` and `Kind()`. Add complete `Definition`/`Define`
  and immutable `WithCode`, `WithTitle`, `WithDetail`, `WithFields` methods.
  Problem responses gain request-correlated UUID-URN instances and omit empty
  details. See the upgrade guide for source and response compatibility steps.

- Include a stable `code` in application and Huma problem responses. Existing
  default `type` identifiers and status mappings are preserved. Add immutable
  `apperror.Error.WithType` for optional public problem-type URIs, preserving
  sentinel identity; internal and unknown errors remain redacted. Message-only
  constructor calls remain valid. The structured log `error_code` now contains the short code
  rather than the type URI; update log queries that matched the old value.

- Add the platform `seeders.Seeder` contract with `Name` and `Seed`, registered
  through `Registry.Register`. Rename the previous name/function registration
  method to `RegisterFunc`; existing callers must update that method name.
  Both forms share validation and execution. Document module-owned `seeders/`
  implementations with constructor-injected services. No schema or queued-payload
  changes are required.

### Adoption and verification

Source is the commit identified by the annotated `v0.3.1` tag; the required
toolchain is Go 1.27.2. This owner-requested patch version is an explicit exception
to the normal patch compatibility policy: error classification APIs and seeder
registration contain source breaks, and problem response fields change. Follow
the [adoption steps](docs/upgrading.md#error-kinds-public-codes-and-instances)
and the seeder registration section before updating an existing application.
No new schema migrations, queued-payload conversions or worker ordering changes
are required by these fixes.

Verification covers formatting, vet, race-enabled Go tests, command/example
builds, and documentation generation/tests. PostgreSQL/Redis integration,
container builds and live provider checks were not run for this patch; no new
backend or deployment-target verification is claimed.

## v0.3.0 — 2026-10-25

- Add owner-authorized media visibility changes through `Library.SetVisibility`,
  `PATCH /media/{id}/visibility` and the media CLI. Preserve asset IDs while moving
  storage objects across the public/private boundary. An additive migration and
  existing recovery worker retain interrupted changes; see the
  [adoption notes](docs/upgrading.md#mutable-media-visibility).
- Align `AGENTS.md` and `CLAUDE.md` with mandatory module interface locations,
  plural entity filenames, private implementations and interface-returning
  constructors. Newly generated applications include both guidance files.
- Allow all CORS request headers by default, including custom application headers
  and `Authorization`. Explicit header allowlists remain supported; origin,
  method, authentication and administrative endpoint policies are unchanged.
- Complete Labena naming across source, documentation, runtime defaults and
  deployment examples. Existing applications should review the
  [rename compatibility notes](docs/upgrading.md#labena-rename-compatibility)
  before changing token settings, rate-limit namespaces or deployed resources.

### Adoption and release scope

Source is the commit identified by the annotated `v0.3.0` tag. Go 1.27.2 is
required. Apply the additive media migration before starting updated API and
worker processes; keep recovery scheduled. Drain pending visibility changes before
rollback. Existing queued task and event payloads need no conversion. Review the
linked rename notes for changed operational defaults when upgrading from v0.2.0.

Verification on macOS ARM64 with Go 1.27.2 passed: the full race-enabled test
suite, the required integration suite with disposable PostgreSQL 17 and Redis 7,
dependency tidy checks, vet, command/example builds, and documentation tests and
regeneration. Container image builds, vulnerability scanning, browser checks and
live cloud providers were not reverified for this release.

## v0.2.0 — 2026-10-19

Labena is distributed as a modular Go application template. Application owners
retain and customize the generated source, then review and port upstream fixes.
See [support and compatibility](SUPPORT.md) and the [update guide](docs/upgrading.md).

### Push-subscription schema

- The initial notification migration creates `push_subscriptions` directly, with
  matching index names. There is no additional alteration migration.
- The table stores push destinations; `notifications` remains the inbox and
  `notification_deliveries` remains the delivery ledger.
- HTTP routes, Go subscription APIs, configuration keys, task names and persisted
  delivery payloads are unchanged by this naming change.
- This release establishes the initial schema for fresh applications. It does not
  automatically upgrade databases created from earlier development snapshots;
  review the [schema adoption notes](docs/upgrading.md#version-02-push-subscription-schema).

This is the first GitHub release of the current template and installer. The earlier
local `v0.1.0` tag remains unchanged; it predates the installer and was not published.

### Included capabilities

- Explicit module registration and application-owned process and resource lifecycles.
- Typed HTTP APIs, live OpenAPI documentation, email OTP and Google sign-in,
  persisted sessions, token rotation and rate limits.
- PostgreSQL/Bun persistence, migrations, seeders and transactions.
- Redis/Asynq background jobs and schedules, process-local Watermill events, SMTP
  mail, durable notification intent and Expo push delivery.
- Named storage disks, managed media, cache, structured logging, an authenticated
  log viewer and optional OpenTelemetry integration.
- Application, worker, scheduler and CLI commands with local development and
  container deployment examples.

### Application creation and branding

- Install `cmd/labena` and use `labena new myapp --module example.com/myapp --name MyApp`.
  The installer accepts a matching versioned template or an explicit local checkout,
  rewrites imports, records provenance and verifies the generated application.
- Generated projects include `.env.example` and all example profiles, personalized
  for the chosen application name. A private, Git-ignored `.env` starts from the
  quickstart profile. Local secrets, Git history and the installer are not copied.
- `APP_NAME` supplies API, log viewer, telemetry and default mail identity. Explicit
  mail sender and brand overrides take precedence; the default mail logo is empty.
- Docker release linker flags follow the application's Go module path.

### Documentation

- Searchable static guides with topic navigation, responsive layouts, themes,
  diagrams, Markdown downloads and complete development workflows.
- A manual GitHub Actions workflow builds and publishes only documentation-site
  files after GitHub Pages is configured.

### Adoption notes

For applications based on earlier development snapshots, review
[application branding](docs/upgrading.md#application-branding) and the remaining
update guide before porting changes. `APP_NAME` must be nonblank. Go callers now
pass the application name to mail configuration translation and validation.
These installer and branding changes require no schema or task-payload migration.
Operational identifiers such as database names, token issuers, queue names and
deployment namespaces remain explicit settings.

### Verification and scope

The 2026-10-18 candidate verification used Go 1.27.2, disposable PostgreSQL 17 and
standalone Redis 7. The full race-enabled integration suite, vet, dependency tidy,
command/example builds, Linux AMD64 container build, documentation checks and
browser checks passed. A locally packaged, version-installed CLI generated and
verified an independent application, including its example environment files.
The vulnerability scan found no reachable or imported-package findings; one
module-only advisory affects unused code.

Those checks describe the earlier candidate. The version 0.2 release evidence is
recorded separately in [release readiness](docs/release-readiness.md) and the
[GitHub release](https://github.com/wisalgroup/labena/releases/tag/v0.2.0).
The public repository's private-reporting route in [SECURITY.md](SECURITY.md) is enabled.
Live providers, additional backends and deployment-specific recovery or load
guarantees remain outside this verification.
