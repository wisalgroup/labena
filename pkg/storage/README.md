# Storage connections

`pkg/storage` constructs native [Go CDK blob](https://pkg.go.dev/gocloud.dev/blob)
buckets. Use the resulting `*blob.Bucket` directly for object I/O; this package
owns connection setup and cleanup, not uploads, ownership or HTTP policy.

For application disk configuration and common operations, read
[file storage](../../docs/filesystems.md). For managed records and access URLs,
read [Media](../../internal/platform/media/README.md).

## Open a local connection independently

Complete helper with no app, configuration-loader or Resources dependency:

```go
package files

import (
    "context"
    "errors"

    "github.com/wisalgroup/labena/pkg/storage"
)

func WriteGreeting(ctx context.Context) (err error) {
    connection, err := storage.Open(ctx, storage.Config{
        Driver: "local",
        Root:   "storage/app",
    })
    if err != nil {
        return err
    }
    defer func() { err = errors.Join(err, connection.Close()) }()
    return connection.Bucket.WriteAll(ctx, "greeting.txt", []byte("hello"), nil)
}
```

This leaves a small object in the selected root. Supply a bounded context and a
trusted root/key. `Open` creates a local directory if necessary, but never creates
remote buckets. Successful cloud construction does not establish operation access.

## Assembly and ownership

`Connection` owns its bucket and native clients. Share `connection.Bucket` with
consumers; never copy Connection itself. Stop operations and close all readers and
writers before `connection.Close()`. Close runs once and preserves its result,
including native-client cleanup failures.

App owns connections it opens for `Resources.Disks`; modules must not close those
borrowed buckets. Independent callers own the connection above. Repeated Open
calls are not cached and create separate ownership obligations.

## Named disks

Named application disks belong to `internal/app/config/filesystems.go`.
`FILESYSTEM_DISK` selects one and `FILESSYSTEM_ADDITIONAL_DISKS` selects more.
The reusable storage package does not read environment variables or depend on
that config package.

Translate explicitly when using application settings:

```go
options, err := cfg.Filesystem.StorageConfig("invoices")
if err != nil {
    return err
}
connection, err := storage.Open(ctx, options)
if err != nil {
    return err
}
```

This **acquisition fragment** assumes `cfg`, context and a configured `invoices`
disk. Preserve Close errors after its users finish. See
[additional disks](../../docs/filesystems.md#open-another-named-disk).

## Provider settings

### Local filesystem

`Config{Driver: "local", Root: "storage/app"}` creates missing roots with mode
0700; existing permissions remain unchanged. Relative roots use the process working
directory. Temporary files live beside objects for mounted-volume compatibility.

Go CDK stores attributes in sidecars; back up the whole root. Keep it under trusted
application control so untrusted users cannot introduce symlinks. Opening local
storage does not expose an HTTP file server or share files across replicas.

### S3, MinIO and Spaces

```go
settings := storage.Config{
    Driver: "s3",
    Bucket: "uploads",
    S3: storage.S3Config{
        Region:       "us-east-1",
        Endpoint:     "http://127.0.0.1:9000",
        UsePathStyle: true,
        AccessKeyID:  accessKey,
        SecretAccessKey: secretKey,
    },
}
```

This **MinIO settings fragment** assumes trusted credential values. Pass it to
`storage.Open`; HTTP is for local emulators. For AWS, omit Endpoint to use the
provider endpoint. Spaces uses its region and endpoint with the same driver.

Supply access/secret keys together, optionally with a session token. When absent,
the AWS SDK default credential chain supplies credentials, including workload
roles. The region is required by the reusable Config; application named disks
supply their own `us-east-1` default.

### Google Cloud Storage

Use Driver `gcs`, Bucket and optional `GCS.CredentialsFile`. Omission uses
Application Default Credentials, including workload identity. Credential files
are trusted operator-managed inputs, never request uploads.

Ordinary object operations support those identities. Signed URLs through this
constructor require service-account credentials containing `client_email` and
`private_key`; IAM-based remote signing is not configured. Credential refresh uses
operation context rather than binding the client to a short startup deadline.

### Azure Blob Storage

Use Driver `azure`, Bucket as the container name, and `Azure.ServiceURL` as the
account URL. Explicit AccountName/AccountKey selects shared-key credentials;
otherwise the Azure SDK default chain is used. Emulator/sovereign account URLs
may be configured. SAS query URLs and connection strings are not inputs here.

Signed URLs require a shared-key client in this integration. Signed PUT requests
also send `x-ms-blob-type: BlockBlob`.

### Credential boundaries

Provider SDK credential-chain variables are read during their construction;
application settings are loaded by config separately. Never log complete Config
values, credential files, signed links or unrestricted provider errors. Cloud
permissions and bucket/container creation remain deployment responsibilities.

## Working with files

| Need | Native API |
| --- | --- |
| Small bounded content | `WriteAll`, `ReadAll` |
| Stream content | `NewWriter`, `NewReader` |
| Part of a file | `NewRangeReader` |
| Metadata | `Attributes` |
| Prefix scan | `List` |
| Copy/remove | `Copy`, `Delete` |

Writer Close completes the upload and can fail. If copying fails, cancel before
closing and preserve both errors. Close readers promptly. The
[streaming guide](../../docs/filesystems.md#stream-larger-objects) contains complete
helpers; `examples/storage/main.go` demonstrates acquisition, streaming and cleanup:

```sh
APP_ENV_FILE='' FILESYSTEM_DISK=local go run ./examples/storage
```

Bound payload size, deadlines and iterator results. Inspect `gcerrors.Code(err)`
for portable categories such as NotFound. Generate authorized keys, persist
connection name/key rather than temporary URLs, and plan for uncertain remote
outcomes. Copy/delete is not a portable atomic rename or a SQL transaction.

## Signed URLs and portability

```go
url, err := bucket.SignedURL(ctx, key, &blob.SignedURLOptions{
    Expiry: 5 * time.Minute,
})
if err != nil {
    return err
}
```

This **download-signing fragment** imports `time` and `gocloud.dev/blob`. Authorize
before issuing `url`; possession grants access. `Method: http.MethodPut` requests
a direct upload where supported. Browser upload also needs provider CORS, and
provider acceptance does not verify an application's attachment policy.

S3-compatible signatures use the configured endpoint, which must be reachable by
the client. Do not change its hostname after signing. Local signing is not set up;
use an authorized handler or [Media internal signatures](../../internal/platform/media/README.md#configure-internal-signing).
Provider credential expiry can shorten link validity.

Native `As` escape hatches remain available. Retention, ACLs, versioning, resumable
uploads and CDN management are provider-specific; this wrapper promises no shared
abstraction for them. It uses explicit constructors rather than global bucket URL
lookup or cached clients. Verify your chosen driver's operation semantics.

## Verification

Local tests exercise streaming, attributes, range reads, copy/list/delete, missing
objects, cancellation and cleanup. App/config tests cover named selection and
ownership. Run local coverage with `go test -race ./pkg/storage`.

For the opt-in MinIO suite, use its isolated test service:

```sh
docker compose -p go-modular-storage-test -f pkg/storage/compose.test.yaml up --build -d --wait
STORAGE_TEST_S3_ENDPOINT=http://127.0.0.1:19000 \
STORAGE_TEST_S3_ACCESS_KEY_ID=storage-test \
STORAGE_TEST_S3_SECRET_ACCESS_KEY=storage-test-password \
go test -race ./pkg/storage
docker compose -p go-modular-storage-test -f pkg/storage/compose.test.yaml down -v
```

The suite creates/removes a unique bucket and checks signed PUT/GET as well as
object operations. The final command deletes this test stack's volumes. These
are disposable fixtures, not production credentials.

MinIO does not establish AWS/Spaces behavior. GCS, Azure and real S3 deployments
need separate acceptance checks with scoped disposable resources, especially for
permissions, signing, retention and recovery.
