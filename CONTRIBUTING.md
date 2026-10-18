# Contributing

Read [project conventions](docs/conventions.md) before changing architecture or lifecycle behavior.
Use the Go toolchain declared in go.mod. Keep changes focused and preserve unrelated
work. Add regression tests for observable behavior and update affected examples.

Run `go test -race ./...` and `go vet ./...`. Storage and delivery changes also
require disposable PostgreSQL and Redis integration tests; see the environment
variables in [testing](docs/testing.md). Never point integration tests at production data.
Report skipped backend tests explicitly. Run `govulncheck ./...` for dependency
or security changes. Documentation changes require content and link checks.

Shared entities, enums and event contracts are an intentional monolith convention.
Their owning modules retain writes and business operations. Reusable packages must
not import internal packages. Prefer explicit constructors and small interfaces
over framework machinery.

Follow the [support and compatibility policy](SUPPORT.md). Maintenance targets
the latest published release; older versions receive no backports. Describe
changes to public Go APIs, HTTP contracts, configuration/defaults, schemas, task
names/payloads and event contracts in release notes with concrete migration steps.
Preserve deployed migrations and describe any required worker/scheduler rollout
order. Customized applications need instructions they can apply to their own code.

Before a public release, the owner must verify the private reporting channel in
[SECURITY.md](SECURITY.md), clean installation and the documented update paths.
The selected distribution is an application template with module path
`github.com/wisalgroup/labena`. This repository path does not establish independent
package releases or support for external dependencies. Follow
[adoption and updates](docs/upgrading.md) for that distinction.

CI separates fast checks from the required PostgreSQL/Redis integration job.
Use `scripts/verify-integration.sh` to reproduce the core integration gate; all four
backend settings are mandatory. See [release readiness](docs/release-readiness.md)
for the current support matrix and outstanding publication gates.

Start a new module from the compiled [Catalog example](examples/module/catalog/register.go)
and follow [your first module](docs/modules.md). Use `go test ./tests/architecture`
for a quick check of dependency direction and test placement. The first-module
smoke test (`go test ./tests/modules -run TestFirstModule`) also keeps the guide
aligned with its compiled example. Both run in the existing CI test job.
