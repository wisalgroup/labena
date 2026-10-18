# Typed application configuration

This package owns the application's environment schema, defaults and validation.
It opens no connections. Read the [configuration guide](../../../docs/configuration.md)
for profiles, precedence and common changes, or the
[complete key reference](../../../deployments/configuration.md) for fields/defaults.

## Choose a loading entry point

| Function | Reads files/process? | Validates? |
| --- | --- | --- |
| `Load()` | Selected dotenv plus process environment | Yes |
| `FromEnvironment(values)` | No; uses the supplied snapshot | Yes |
| `Defaults()` | No | No; returns an editable baseline |

```go
cfg, err := config.Load()
if err != nil {
    return err
}
return app.Serve(ctx, cfg, app.Processes{HTTP: true, Workers: true})
```

This **entry-point fragment** assumes a context and imports app/config. `Serve`
also validates programmatically supplied settings before acquiring resources.
Treat returned maps and slices as immutable after assembly; there is no live reload.

## Environment precedence

`APP_ENV_FILE` unset selects an optional `.env`. A nonempty selector requires that
file; an explicitly empty selector disables dotenv. Process variables override
file values, including explicit empty strings. Tagged defaults may still apply to
empty fields. Loading takes an isolated snapshot and never modifies process values.

Parsing errors identify the setting without echoing its supplied value.
`.env.quickstart.example` supports a first run without external provider credentials;
`.env.example` enables the full implemented profile and requires credentials.

## File ownership

| File | Owns |
| --- | --- |
| `config.go` | Composition of the single typed schema |
| `load.go` | Snapshot reading, parsing and defaults |
| `validate.go` | Combined checks and cross-capability constraints |
| Capability files | Settings, native-option conversion and local validation |
| `observability.go` | Logging, tracing and metrics together |
| `filesystems.go` | Named disk definitions and selection |

HTTP, CORS and events declare complete environment names on their fields. Queue
priorities and disk definitions remain explicit Go configuration. Add settings to
their owner instead of creating another provider registry or reading environment
variables in business code.

## Cross-capability checks

- Active queue, cache and rate-limit stores use different Redis databases or servers.
- Queue drain timeout is shorter than the application's overall shutdown budget.
- Enabled authentication needs its database, keys, mail configuration and rate limits.
- `App.AppName` (`APP_NAME`) must be nonblank. It supplies API/viewer branding,
  telemetry identity and mail-name defaults. Mail translation methods accept the
  app name explicitly, preserving independent overrides.
- Selected disk and route names must reference configured definitions.

Owner policy validation stays with the owner. For example,
`cfg.Auth.SessionPolicy()` translates settings to Identity's pure policy,
whose validation is shared by config and service construction. AuthConfig is the
single authentication settings schema; app constructs the service from it.
Execution choices belong to Processes in main,
not this environment schema. The default access
lifetime is **7 days**; zero refresh/session lifetime has no absolute time limit.
See [Identity](../../modules/identity/README.md) for revocation and inactivity semantics.

Mail is always constructed; SMTP connects on send. Queue CLI administration is
always registered; `QUEUE_DASHBOARD_ENABLED` controls only the web UI. The optional
`FILESYSTEM_ADDITIONAL_DISKS` list opens named disks alongside the default.

## Verify a settings change

Update the key/default reference whenever changing the environment schema. The
reference parity check runs with:

```sh
go test ./internal/app/config -run TestConfigurationReferenceMatchesDefaults -count=1
```

Use ordinary config tests for validation and precedence. The owning component's
behavior tests establish what the setting actually does. See
[conventions](../../../docs/conventions.md) for lifecycle ownership.
