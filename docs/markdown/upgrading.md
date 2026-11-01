# Adopt and update the template

[Labena](https://github.com/wisalgroup/labena) supplies the starting source for an application. Your repository
owns the customized application, its deployments and its updates. Read the
[support policy](../SUPPORT.md) for release coverage and compatibility.

## Native HTTP/2 and WebSocket sessions

Use Go 1.27.3 or newer in the supported Go line and rebuild deployed binaries/images.
This fixes the prior Go 1.27.2 certificate-verification advisory
[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809); the pinned container toolchain
has been updated as well.

HTTP/1.1 defaults are unchanged. To enable native HTTPS, set both
`HTTP_TLS_CERT_FILE` and `HTTP_TLS_KEY_FILE`; add `HTTP_HTTP2_ENABLED=true` for
TLS-negotiated HTTP/2 on that listener. Certificates are loaded at startup and
require a restart for renewal. Update clients, published ports and health probes
to use HTTPS when switching an existing listener to TLS. HTTP/1.1 remains
available, including for WebSocket upgrades.

Socket endpoints are opt-in: construct `platform/websocket.New` and mount it on
the native mux with authentication. `middleware.BearerHTTP` reuses the existing
authenticator for clients supporting Authorization headers. Browser clients need
an explicit authenticated session/ticket adapter. No automatic route, message
contract, database migration, worker ordering or queued-payload change is introduced.
The integration adds the pinned `github.com/coder/websocket` protocol dependency;
TLS and HTTP/2 use the Go standard library. See the [setup guide](websockets.md).

## HTTP middleware and authentication helpers

Import reusable HTTP policies from `internal/platform/api/middleware` and
principal helpers from `internal/platform/auth`:

| Previous API | Replacement |
| --- | --- |
| `api.Bearer` | `middleware.Bearer` |
| `api.RateLimit` | `middleware.RateLimit` |
| `api.Multipart` | `middleware.Multipart` |
| `api.Authenticator` | `auth.Authenticator` |
| `api.Principal(ctx)` | `auth.FromContext(ctx)` |
| `api.CurrentUserID(ctx)` | `auth.CurrentUserID(ctx)` |
| `api.RequireCurrentUser(ctx, id)` | `auth.RequireCurrentUser(ctx, id)` |

Replace repeated no-store Huma closures with `middleware.NoStore`.
`api.Authenticated` remains the protected-group/OpenAPI composition helper;
`api.ClientIPKey` and `api.PeerIPKey` retain their server-owned behavior.
Bearer middleware still sets no-store on both success and rejection. Routes,
statuses, authentication rules, rate-limit keys and multipart cleanup are unchanged.
Custom trusted authentication adapters can use `auth.WithPrincipal` after
verifying credentials; it is not an authentication bypass for request input.
This source move requires no schema migration or queued-payload conversion.

## Error kinds, public codes and instances

Classification is now `Kind`: replace `CodeNotFound` with `KindNotFound` (and
likewise for the other constants), `New(code, detail)` with `New(kind, detail)`,
and category comparisons on `Code()` with `Kind()`. `Code` remains the type
of an optional public identifier; `Code()` returns that override or the kind's
default string. Remove uses of the mutable code registry. Use `Define` for a
complete domain definition or individual fluent overrides; see [examples](errors.md#customize-public-errors).

Existing generic constructors keep their code/type/status defaults. Problem JSON
adds `code` and a request-correlated UUID-URN `instance`; empty application detail
and native Huma 4xx detail are omitted. Review strict response schemas and clients
that assumed detail was always present. Custom code/type/title overrides are opt-in;
review consumers before changing an existing public error identifier.
Structured log `error_code` now holds the public code instead of the type URI.
Update matching log filters. No database migration, worker ordering or queued
payload conversion is required.

## Seeder registration forms

Replace existing `registry.Register(name, fn)` calls with
`registry.RegisterFunc(name, fn)`. `Register` now accepts a `seeders.Seeder`
implementing `Name() string` and `Seed(context.Context) error`. Names and CLI
commands are unchanged; do not register the same name through both forms.
Dedicated implementations can live in the owning module's `seeders/` directory;
see the [constructor example](seeders.md#register-a-module-seeder). This source API
change requires no database migration or queued-payload conversion.

## Mutable media visibility

Adopting the v0.3.0 visibility-change operation requires the additive media
migration `20261022000000_media_visibility_changes.sql`. Apply `migrate up` before
serving the updated application. It preserves `media_assets.visibility`, adds
`visibility_target` for interrupted changes, and permits the `changing_visibility`
status. Published migrations are unchanged; no database reset is needed.

Port the media library/store/recovery changes together with Identity's media HTTP
interface, request, handler, route and response status enum. The new owner-authorized
operation is `PATCH /api/v1/media/{id}/visibility` with `{"visibility":"public"}`
or `{"visibility":"private"}`. Go callers use `Library.SetVisibility` outside SQL
transactions. Existing upload defaults remain unchanged.

The operation copies and deletes storage objects when crossing the `public/`
boundary. Verify permissions on both paths, restrict anonymous reads to `public/*`,
and keep the media recovery schedule/workers running (or schedule `media recover`).
Interrupted changes retain their intent and return 503; they are retried after a
ten-minute grace. Do not report a failed private change as completed privacy.
Review [visibility and link behavior](media.html#change-an-existing-assets-visibility),
including cached copies and existing signed links, before adopting the endpoint.

Drain all processes using this operation and finish pending visibility changes
before downgrading code or schema. Migration rollback rejects pending work so its
recovery intent cannot be silently lost.

## Version 0.2: push-subscription schema

The initial notification migration, `20261001120100_notifications.sql`, creates
`push_subscriptions` directly, alongside `notifications` and
`notification_deliveries`. Its name identifies device registrations rather than
notification preferences. There is no separate table-renaming migration.

Fresh applications run `migrate up` normally. This release updates the initial
schema; it does not automatically migrate databases created from earlier development
snapshots. Editing a migration file does not rerun a version already applied by
Goose. Review any existing database against the current schema before updating its
application; retained data needs an application-specific migration plan.

The table exists even when push is disabled, so every fresh installation uses the
same schema history. HTTP subscription routes, Go APIs, configuration, task names
and event contracts are unchanged by the table naming change.

## Record your starting point

Prefer [the installer](creating-applications.html) to create an application from a
published release. It records the upstream version and checksum in `.labena.json`
and changes imports and branding automatically. Keep that provenance file.

For a manual clone or template copy, record the source yourself. Before changing
the source, record the upstream repository URL, release tag and exact commit in
your application's maintenance notes. A template copy may not retain upstream
history, so keep this record even if its Git history starts at your own first commit.
For an evaluation snapshot, record its commit and note that it is unreleased.

Keep a clean initial snapshot or access to the original release so later fixes can
be compared against the source you adopted. Preserve the project's license and
applicable third-party notices. Follow upstream releases and security advisories;
copied applications do not receive automatic updates.

You can run the [installation tutorial](getting-started.md) with the existing
`github.com/wisalgroup/labena` path before adopting the source for your own application.

## Choose your application module path

The installer handles this step. For manual adoption, the template uses
`github.com/wisalgroup/labena`. Replace it with the path belonging to
your application repository. For example, substituting your actual owner and name:

```sh
go mod edit -module=github.com/your-owner/your-app
```

In your editor, replace the `github.com/wisalgroup/labena/` prefix with that same path plus `/` in all
Go files, including examples and tests. Update active documentation imports too.
Include any module-path strings in custom tests; the supplied architecture checks
read the path from `go.mod`. The module declaration command does not rewrite imports.

Avoid replacing the bare project name throughout every file: database names,
container names and persisted identifiers have separate meanings. Configure your
application name and deployment resources deliberately. Historical review records
describe earlier source and need not be rewritten.

Check for remaining old imports, tidy dependencies and build the application:

```sh
rg -n '"github.com/wisalgroup/labena/' --glob '*.go'
go mod tidy
go test ./tests/architecture
go build ./cmd/... ./examples/...
go run docs/build.go
```

The search should have no matches. Rebuild the documentation after editing its
Markdown sources; do not edit generated HTML or Markdown downloads directly.
Then run the required [verification suites](testing.md) for your application.

`pkg` contains reusable source maintained within this template release. No package
currently has an independent external release or support promise. Adoption does
not require adding this repository as a dependency or introducing a second module.

## Labena rename compatibility

The Go module and instrumentation scopes use `github.com/wisalgroup/labena`.
The default `APP_NAME` is `labena`; API titles, mail defaults, the log viewer and
telemetry inherit it. Explicit application names still take precedence. The
supplied log queries use `service_name="labena"`.

The changes after version 0.2.0 complete the rename in runtime defaults and
deployment examples. When porting them into an existing application:

- JWT defaults become issuer `labena` and audience `labena-api`. Keep your previous
  values explicitly in `AUTH_TOKEN_ISSUER` and `AUTH_TOKEN_AUDIENCE` to preserve existing token
  validation, or plan for clients to authenticate again when adopting new values.
- `RATE_LIMIT_NAMESPACE` defaults to `labena`. Changing it selects separate rate
  budgets; retain your existing explicit namespace if counters must carry over.
- Application problem types use `urn:labena:error:<code>`. Update clients that
  match the full type URI. HTTP statuses and the error-code suffixes are unchanged.
- Database examples, image names, Kubernetes namespaces, Prometheus job names and
  dashboard identifiers use Labena naming. This does not rename an existing
  database or move persistent volumes. Preserve deployed values or plan their
  migration before applying the updated examples.
- MySQL migration locks use the new `labena:` prefix. Stop older migration tools
  before running the updated tools; the two prefixes do not coordinate with each
  other. This changes no SQL schema.

`APP_NAME` controls branding, not these operational identifiers. Check deployment
configuration and monitoring filters when adopting the new defaults.

## CORS request headers

After version 0.2.0, `HTTP_CORS_ALLOW_HEADERS` defaults to `*`, allowing custom
request headers without listing each one. The middleware returns the requested
names in preflight responses, including `Authorization`. Origin and method
restrictions still apply; credentialed requests still require explicit origins.
Administrative endpoints remain outside business CORS.

An existing explicit header list continues to restrict headers. To adopt the new
behavior, set `HTTP_CORS_ALLOW_HEADERS=*` in your application's environment too.
Keep `HTTP_CORS_EXPOSE_HEADERS` as explicit response-header names; it is a separate
setting. No schema or retained-state migration is needed for this CORS change.

## Application branding

`APP_NAME` must be nonblank and supplies API titles, the log viewer and mail names,
as well as telemetry service identity. Empty `MAIL_FROM_NAME` and `MAIL_BRAND_NAME`
inherit it; explicit mail overrides continue to work. The default logo is now empty,
so mail renders text branding without fetching placeholder artwork. Remove old
example mail-name overrides if you want them to follow your application name.

For Go configuration callers, pass `cfg.App.AppName` to `cfg.Mail.Brand(...)`,
`SMTPConfig(...)` and `Validate(...)`. Inheritance is resolved during translation,
so changing an app name after `config.Defaults()` does not leave stale mail defaults.
Standalone `api.New` falls back to “Application API”; app always supplies its own title.
Standalone log viewers can set `logviewer.Config.AppName`.

These changes require no retained-state migration. If you change `APP_NAME`, update
telemetry filters and dashboards that select its previous service identity.

## Platform and Identity ownership

The authenticated principal moved from `internal/modules/shared/contracts/auth`
to `internal/platform/auth`. Update imports; its user/session fields are unchanged.
Platform packages no longer import any business definitions.

Identity now registers the user media, notification and push-subscription HTTP
operations. Supply constructed `Resources.Media` and `Resources.Notifier` before
calling `identity.Register`. Custom HTTP composition can use
`identityhttp.RegisterRoutes` with `Routes.Media`, `Routes.Notifications` and
`Routes.Auth`. Omitted capability fields omit their routes. Keep the protected
API authenticated; subscription registration also rejects a missing AuthService.

Remove calls to the former media/notification `RegisterHTTP` helpers and
`RegisterSubscriptionsHTTP`. Platform media's `RegisterContentHTTP` remains for
public/signed content. Notification `integration.Register` now takes only the
notifier and targets; the subscription callback was removed. The Identity handler
calls `AuthService.SubscribePush` directly. Custom non-Identity code can still use
`Notifier.Subscribe`, with its own recipient authorization and session coordination.

HTTP paths, operation IDs, payloads and authentication requirements are unchanged.
No database, task, event or retained-state migration is needed for this move.

## Response metadata moves to headers

Success and problem bodies no longer include `request_id` or `api_version`.
Read `X-Request-ID` and `X-API-Version` instead. These headers were already present
and remain available for successes, errors, empty responses and streams. Capture
the request ID with client-side errors so logs can still be correlated.

This is a breaking HTTP body and OpenAPI schema change. Update clients to read
headers before rolling out the server change; they can use those headers with
both the old and new server. Regenerate clients that rely on the removed body
fields. Custom CORS configurations must expose both headers to browser clients;
the supplied defaults already do. No database or retained-state migration is needed.

For customized Go response code, `response.Metadata` was removed. Optional `Meta`
now belongs directly to `response.Envelope[T]` and `response.Problem`; replace
embedded `Metadata` literals with the direct field. Custom middleware using
`response.WithMetadata(ctx, id, version)` should use `response.WithRequestID(ctx, id)`
and continue setting the response headers. Ordinary handlers still use
`response.OK`, `Created`, `Accepted`, `WithMeta` and `response.Error` unchanged.

## Optional local log viewer

The viewer is disabled by default. Set `LOG_VIEWER_ENABLED=true` with
`LOG_VIEWER_USERNAME` and `LOG_VIEWER_PASSWORD` to mount `/admin/logs/` on the
application HTTP process. No collector or migration is required. Applications
porting custom logging construction can install the viewer's `WrapHandler` through
`logging.Config.WrapHandler`; keep the manager's handler intact so named-channel
selection continues to work. Read the [scope and limits](logging.md#local-log-viewer)
before relying on live or file history across processes.

## Apply an upstream update

### Version 0.1 hardening changes

- Port `internal/platform/adminhttp`, queue-dashboard wrapping, and the log viewer
  integration together. Business CORS now excludes `/admin`; administrative reads,
  preflights and mutations reject cross-origin browser requests. Preserve Host at
  TLS termination and update clients that embedded dashboards across origins.
- Port the subscription-pruning query and its concurrency regression. It skips
  locked registrations and rechecks staleness, preserving concurrent refreshes.
  No schema migration, task-payload change or queue drain is required. Deploy the
  corrected maintenance worker before resuming scheduled pruning on older builds.
- Rename calls to `Notifier.Delivery` to `Notifier.GetDelivery`. The returned
  metadata, HTTP operations and stored delivery representation are unchanged.
- Configure `HTTP_TRUSTED_PROXIES` only for a secured, sanitized ingress path.
  Empty preserves direct-peer behavior. Application HTTP/OTP limits now use
  `api.ClientIP`; `api.PeerIP` still returns the immediate connection peer.
  Behind an enabled proxy policy, clients get separate budgets instead of sharing
  the ingress address. Existing session/token formats remain unchanged.
- Compose now explicitly enables dependency readiness, matching the full example
  and Kubernetes profile. A database or required Redis outage makes `/readyz`
  unavailable; `/healthz` stays live. Update probe configuration before rollout.
- Release images accept `VERSION` and `REVISION` build arguments. Update the
  custom build tooling when renaming the Go module; the supplied Dockerfile
  derives its linker package path from `go.mod`. OpenAPI and CLI/startup output share the release identity;
  `X-API-Version` remains `v1`.

Delivery records and media tombstones have no automatic purge in version 0.1.
Follow the [retention contract](../deployments/README.md#retention-for-version-01)
before implementing application-specific erasure or expiration.

### Update workflow

1. **Identify the change.** Compare your last incorporated upstream tag/commit with
   the target release. Read all intervening release notes and security advisories,
   including affected versions, configuration changes and migration instructions.
2. **Port it on a branch.** Review the upstream patch against your customized code.
   Apply the relevant implementation, tests and dependency changes, adapting import
   paths and local business behavior. A clean cherry-pick alone is not proof that
   a fix works in your application.
3. **Plan retained-state changes.** Add new migrations instead of editing ones
   already applied. Account for queued task payloads, stored notification intent,
   event contracts and old/new worker compatibility. Follow the release's rollout
   order; stop incompatible producers or drain old work when its instructions
   require that.
4. **Verify your result.** Run regression tests, the PostgreSQL/Redis integration
   gate, builds and the dependency vulnerability scan. Check the providers and
   deployment targets your application actually uses. Rehearse schema and data
   changes on a disposable restored copy before production deployment.
5. **Record and deploy.** Record the target upstream release, patches incorporated,
   any deferred changes and verification results. Deploy with the migration and
   recovery plan appropriate to your application. Retain its own release artifacts.

Keep security corrections traceable even when you port them without adopting all
other upstream changes. Upstream maintains the latest release; you own fixes to
customized or older application copies. Merely running `go get -u` does not update
copied template source.

## Prepare a template release

Maintainers publish migration notes alongside each release. Use the
[compatibility policy](../SUPPORT.md#compatibility-and-release-notes) to cover API,
configuration, schema, task and event changes, then record candidate verification
in [release readiness](release-readiness.md). Enable and verify the private
[security reporting channel](../SECURITY.md) before inviting public adoption.
