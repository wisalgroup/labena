# File storage

A disk is a named storage connection. App opens selected disks and shares native
Go CDK `*blob.Bucket` values. Use those buckets for exports, documents or other raw
objects. For owned uploads, public/private asset URLs and deletion recovery, use
[Media](media.html) above these primitives.

## Select a disk

Start locally without a cloud provider:

```dotenv
FILESYSTEM_DISK=local
FILESYSTEM_LOCAL_ROOT=storage/app
```

`FILESYSTEM_DISK` selects a **name**, not a driver. Named definitions live in
`internal/app/config/filesystems.go`:

| Disk name | Driver | Environment prefix |
| --- | --- | --- |
| `local` | Local filesystem | `FILESYSTEM_LOCAL_` |
| `s3` | Amazon S3 | `FILESYSTEM_S3_` |
| `minio` | S3-compatible MinIO | `FILESYSTEM_MINIO_` |
| `spaces` | S3-compatible Spaces | `FILESYSTEM_SPACES_` |
| `gcs` | Google Cloud Storage | `FILESYSTEM_GCS_` |
| `azure` | Azure Blob Storage | `FILESYSTEM_AZURE_` |

App opens the default plus `FILESYSTEM_ADDITIONAL_DISKS` once. `disabled` omits the
default, but explicitly selected additional disks still open. Unselected providers
need no credentials/bucket. Media URL settings are validated for all definitions;
provider requirements are validated when a disk is selected or explicitly opened.

The Go default selects MinIO; the tutorial profile disables storage. Tilt enables
MinIO by default, while Compose needs its storage profile. See
[local development](local-development.html#optional-object-storage).

## Try a complete storage operation

```sh
APP_ENV_FILE='' FILESYSTEM_DISK=local go run ./examples/storage
```

Expect `Hello from the configured filesystem.` The example writes a uniquely named
small object, reads it, deletes it, and closes its connection. No full app or
migration is needed. It uses the existing configuration loader but constructs only
storage; inherited process settings still affect configuration validation.

## Inject and use the bucket

During module construction, choose from `resources.Disks` and pass that bucket to
the service that needs it. Fail construction if an enabled operation requires an
unavailable disk; do not look it up from the resource bundle on every call.

For a small bounded object inside the service:

```go
if err := bucket.WriteAll(ctx, "exports/example.txt", []byte("hello"), nil); err != nil {
    return err
}
body, err := bucket.ReadAll(ctx, "exports/example.txt")
if err != nil {
    return err
}
```

This **operation fragment** assumes a borrowed `*blob.Bucket` and context. Consume
`body` as the result. App owns the connection; the service must not close it.
Generate or validate object keys in the business operation. A request filename is
not an authorization rule.

## Stream larger objects

Use readers/writers when the object should not be buffered in memory. Complete
helper for a borrowed bucket, authorized key and caller-owned reader:

```go
package files

import (
	"context"
	"errors"
	"io"

	"gocloud.dev/blob"
)

func Upload(ctx context.Context, bucket *blob.Bucket, key string, src io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	writer, err := bucket.NewWriter(ctx, key, nil)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(writer, src)
	if copyErr != nil {
		cancel()
	}
	return errors.Join(copyErr, writer.Close())
}
```

The caller bounds input size and duration. Writer `Close` finalizes an upload and
can fail; preserve its error. Cancel before closing if input copying fails.
Cancellation still cannot prove that a remote write rolled back after acceptance.
Use stable keys and reconciliation when an outcome is uncertain.

### Read and close an object

```go
reader, err := bucket.NewReader(ctx, key, nil)
if err != nil {
    return err
}
_, copyErr := io.Copy(destination, reader)
return errors.Join(copyErr, reader.Close())
```

This **function-body fragment** imports `io` and `errors`; `destination` is an
`io.Writer`. Finish all readers/writers before the owner closes the connection.
Native range reads are available when your operation needs only part of an object.

## Open another named disk

Add a name to the definitions in `FilesystemConfig.load`:

```go
"invoices": {Driver: "s3"},
```

Then supply its own settings and select it alongside the default:

```dotenv
FILESYSTEM_ADDITIONAL_DISKS=invoices
FILESYSTEM_INVOICES_BUCKET=example-invoices
FILESYSTEM_INVOICES_REGION=eu-west-1
```

Use provider-managed credentials or the disk's explicit credential fields. Names
and drivers are defined in Go; setting an environment prefix does not create a
new disk. Settings are immutable after assembly.

For a connection you own independently:

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

This **acquisition fragment** imports `github.com/wisalgroup/labena/pkg/storage`. Pass
`connection.Bucket` to consumers, then preserve `connection.Close()` errors after
all users finish. Calls are not cached. See `examples/storage/main.go` for a full
function with cleanup, or the `pkg/storage/README.md` constructor reference.

## Listing, deletion and signed URLs

| Native operation | Use |
| --- | --- |
| `Attributes` | Read size and object metadata |
| `List` | Iterate a prefix with a caller-defined result limit |
| `NewRangeReader` | Read a byte range |
| `Copy` | Copy an object within the bucket |
| `Delete` | Remove an object |
| `SignedURL` | Grant temporary provider access |

Handle iterator errors as well as the end of iteration. Use `gcerrors.Code(err)`
for portable error classification. Copy then delete is not a portable atomic
rename; blob operations do not become atomic with SQL.

After authorizing an object, issue a native download link:

```go
url, err := bucket.SignedURL(ctx, key, &blob.SignedURLOptions{
    Expiry: 5 * time.Minute,
})
if err != nil {
    return err
}
```

This **fragment** imports `time` and `gocloud.dev/blob`. Its `url` is a bearer
credential. Store the disk/key, not the expiring link. A signed PUT URL is possible
through native options, but does not verify that an uploaded file is safe or create
a managed Media asset.

| Provider | Signing requirement in this integration |
| --- | --- |
| S3-compatible | SDK signing credentials and a client-reachable endpoint |
| GCS | Service-account credentials containing email/private key; IAM remote signing is not configured |
| Azure | Shared-key client; signed PUT also needs `x-ms-blob-type: BlockBlob` |
| Local | Native signing is not configured; use Media's internal links |

## Media visibility and URLs

These disk settings apply to **Media assets**, not raw bucket writes or operating
system permissions:

```dotenv
FILESYSTEM_MINIO_VISIBILITY=public
FILESYSTEM_MINIO_URL_MODE=internal
FILESYSTEM_MINIO_PUBLIC_URL=http://localhost:9000/uploads
MEDIA_BASE_URL=http://localhost:8080/api/v1/media
```

| Setting | Controls |
| --- | --- |
| `VISIBILITY=public\|private` | Default for new uploads; an explicit upload value overrides it |
| `URL_MODE=internal` | Return application content/download routes |
| `URL_MODE=direct` | Public permanent provider URLs and private signed provider URLs |
| `PUBLIC_URL` | Browser-reachable public bucket/CDN root, including any bucket path |
| `MEDIA_BASE_URL` | Application media route origin/prefix; not the storage endpoint |

Direct mode requires `PUBLIC_URL`, even when the default visibility is private.
Local disks require internal mode. The [Media URL matrix](media.html#public-urls-and-private-links)
shows precisely which links require a bearer header and which expire.

### Configure a direct public root

Examples:

```text
S3 virtual-host: https://BUCKET.s3.REGION.amazonaws.com
MinIO path-style: https://storage.example.com/BUCKET
CDN:             https://media.example.com
```

This setting builds public URLs. Private signatures use the native client's
**Endpoint**, not PublicURL. Compose/Tilt's internal `http://minio:9000` is useful
for app-to-storage traffic but inaccessible to an ordinary browser outside that
network. Choose an endpoint reachable by both before enabling private direct links.

Do not rewrite the hostname after signing: the signature includes it. Temporary
provider credentials may expire sooner than the requested link lifetime.

### Grant public access to public objects only

Media writes public assets under `public/assets/<id>` and private assets under
`assets/<id>`. For permanent public links, provision anonymous read only on
`public/*`, leaving uploads/deletes authenticated. Example S3/MinIO statement for
an `uploads` bucket:

```json
{
  "Effect": "Allow",
  "Principal": "*",
  "Action": "s3:GetObject",
  "Resource": "arn:aws:s3:::uploads/public/*"
}
```

Place this in a bucket policy's `Statement` array with version `2012-10-17` and
check account-level public-access restrictions. The framework does not change
ACLs or policies. For providers lacking suitable prefix restrictions, use separate
public and private disks instead of exposing a mixed container.

Direct reads bypass app headers, visibility/deletion status checks and rate limits.
Objects may be readable before metadata is ready, and CDN caches may retain deleted
bytes. Use provider/CDN controls and a separate media origin for untrusted content.
Changing to internal URL mode does not revoke existing public storage permissions.

Legacy public objects outside `public/` retain internal URLs until explicitly
copied and their keys updated. Configuration changes do not move objects.

## Storage limits and verification

Remote buckets/containers must already exist. Constructing a client does not probe
bucket existence or permissions; the first operation can still fail. Use HTTPS
for remote endpoints. Workload identity or operator-managed credentials belong in
configuration, never upload inputs.

Local roots are created with private permissions; existing permissions are not
rewritten. Keep Go CDK attribute sidecars when backing up. Protect the root against
untrusted writes/symlinks and provision shared storage deliberately for replicas.

| Symptom | Check |
| --- | --- |
| App starts but upload is denied | Bucket existence and operation-specific credentials |
| An additional disk is missing | Add its Go definition and select it in Additional disks |
| File exists on one replica only | Local storage is host-specific without a shared volume |
| Read succeeds but signing fails | Provider-specific signing credentials |
| Direct URL opens only inside a container | Endpoint/PublicURL reachability |

Local and MinIO tests do not prove real-cloud permissions or recovery semantics.
Use disposable provider acceptance resources for the operations you rely on.

## Media asset URLs

For database-backed uploads, use [Media](media.html) and its HTTP `url` field or
`Library.URL(ctx, asset)` after authorization. The library returns a `Link`;
HTTP asset responses map its expiry to `url_expires_at`. Explicit temporary links
return `expires_at`. See [private-link examples](media.html#request-a-temporary-link)
for client code and signing-failure behavior.
