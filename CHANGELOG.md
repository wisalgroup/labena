# Changelog

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
