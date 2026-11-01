# Create an application

Use the standalone `labena` installer to create your own application. It copies a
versioned template, changes the Go module and imports, and configures your name.
The resulting repository owns its source and can evolve independently.

## Install the command

Install the released CLI:

```sh
go install github.com/wisalgroup/labena/cmd/labena@latest
```

For repeatable team setup, replace `@latest` with `@v0.4.0`.
Go installs the binary into `GOBIN`, or `$(go env GOPATH)/bin` when `GOBIN` is unset.
Put that directory on your shell's `PATH`, then check `labena --version`.
Use the Go version declared in the release's `go.mod`.

If your shell reports `command not found`, add the installation directory to the
current session:

```sh
go_bin="$(go env GOBIN)"
export PATH="${go_bin:-$(go env GOPATH)/bin}:$PATH"
labena --version
```

Add the resolved directory to your shell configuration (for example, `~/.zshrc`)
to keep it available in future terminals.

While the repository is private, configure Git authentication for GitHub and add
`github.com/wisalgroup/labena` to your existing `GOPRIVATE` patterns. For a shell
without other private-module patterns:

```sh
export GOPRIVATE=github.com/wisalgroup/labena
go install github.com/wisalgroup/labena/cmd/labena@latest
```

Keep `GOPRIVATE` set when running the installer: it downloads the template too.
This setting bypasses the public proxy and checksum database for this module; it
does not grant repository access. Git still needs your credential helper or SSH
configuration. Never put access tokens in a command or committed file.

## Create a project

Run from the parent directory where the new project should live:

```sh
labena new myapp \
  --module github.com/your-org/myapp \
  --name MyApp
```

Replace `github.com/your-org/myapp` with your application's Go module path.

`myapp` must not already exist, and its parent must exist. The module path identifies
the application's Go source. `MyApp` is its display name; these are separate choices.
The command creates:

- Application source, tests, examples, deployment files and documentation.
- A new `go.mod` module declaration and matching Go imports, including tests and examples.
- An `APP_NAME` default and example profiles using `MyApp`, plus initial Compose and Kubernetes branding.
- A private local `.env` copied from the quickstart example, with optional capabilities disabled.
- `.labena.json`, recording the source version and checksum, plus its commit when available.

Commit the generated `.env.example` and `.env.*.example` profiles with your
application. They contain example settings with your application name. The generated
local `.env` remains ignored by Git; the template's local secrets are never copied.

It runs `go mod tidy`, regenerates the documentation, and builds the application
commands and examples. These checks need access to Go dependencies. It does not
start services, apply migrations, initialize Git, or create a GitHub repository.
On failure, it removes the new incomplete directory; an existing destination is
never overwritten. Use `--skip-checks` to generate source without those checks,
then run them yourself before using it.

The installer itself is not copied into your application. `labena` creates
projects; your application's `go run ./cmd/cli` manages its routes, migrations and
other commands. Application CLI help still constructs the configured application.
Installer help does not load application configuration or connect to infrastructure.

## Run your application

```sh
cd myapp
```

Follow [installation](index.html) to start the tutorial PostgreSQL and Redis
containers. Then run:

```sh
go run ./cmd/cli migrate up
go run ./cmd/app
```

The generated `.env` already selects the tutorial settings. Replace example
credentials and configure the features you need before deployment. Database names,
JWT issuers/audiences, Redis namespaces, queues, image names and Kubernetes
namespaces remain explicit operational settings. Review those when provisioning
an independent environment; changing `APP_NAME` never changes them automatically.
Likewise, update observability dashboards' service-name filters for your deployment.

Start your own Git history when ready, preserving `.labena.json` and the supplied
license. Upstream fixes are reviewed and ported into your copy; see
[adoption and updates](upgrading.html).

## Try an unpublished checkout

From a Labena checkout, install the current CLI locally:

```sh
go install ./cmd/labena
labena new ../myapp \
  --module github.com/your-org/myapp \
  --name MyApp \
  --source .
```

If you are in another directory, `--source .` points there rather than to Labena.
Pass the absolute checkout path instead. For example, from your chosen projects
directory, replace the source path below with your actual checkout:

```sh
labena new myapp \
  --module github.com/your-org/myapp \
  --name MyApp \
  --source /absolute/path/to/labena
```

The checkout folder may have any name; what matters
is that it contains Labena's `go.mod`, source and Git metadata. The destination
directory must be new; the installer never overwrites an existing application.

`--source` must point to the root of a Labena Git checkout. It includes tracked and
non-ignored source changes, excludes local environment files, runtime data, build
artifacts and Git history, and rejects copied symlinks. Provenance marks this as a
local snapshot and records whether the checkout was modified. Treat local source
as trusted code: verification runs its Go documentation builder and compiles it.

Use the default release path for reproducible adoption. A released CLI downloads
its own exact template version through the Go module system. To select another
compatible release explicitly, use `--template-version vX.Y.Z`; branches and
`latest` are not accepted as template versions. Incompatible templates are rejected.
Prefer installing the CLI from the desired release instead of mixing versions.

## Publish the installer

Maintainers commit `cmd/labena` with the matching template and verification results,
then publish a new immutable module release tag. Tags made before this command
was added cannot install it; do not move an existing tag to add it.
No second repository, package manager, or installer service is required.
`go install` fetches the command from that version, and `labena new` uses the same
version as its application template.
