// Package storage constructs Go CDK blob buckets. Object operations use the
// native blob API; application upload policy and HTTP handlers belong to modules.
package storage

import (
	"context"
	"errors"
	"sync"

	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"
)

// Connection owns Bucket and its native clients. Share Bucket with consumers;
// call Connection.Close only after all readers, writers and operations finish.
// Do not copy a Connection. Bucket supports concurrent use.
type Connection struct {
	Bucket      *blob.Bucket
	closeClient func() error
	once        sync.Once
	err         error
}

// Close releases the bucket and native clients exactly once, preserving errors.
// It is safe to call concurrently, but callers must first stop bucket operations.
func (c *Connection) Close() error {
	c.once.Do(func() {
		c.err = c.Bucket.Close()
		if c.closeClient != nil {
			c.err = errors.Join(c.err, c.closeClient())
		}
	})
	return c.err
}

// Open constructs one connection without creating remote buckets or containers.
// Local roots are created with private permissions. Opening a cloud connection
// does not establish bucket existence or authorization; operations can still fail.
// The caller owns the returned connection. No application workers are started.
func Open(ctx context.Context, cfg Config) (*Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	switch cfg.Driver {
	case "local":
		bucket, err := fileblob.OpenBucket(cfg.Root, &fileblob.Options{
			CreateDir: true, DirFileMode: 0700, NoTempDir: true,
		})
		if err != nil {
			return nil, err
		}
		return &Connection{Bucket: bucket}, nil
	case "s3":
		return openS3(ctx, cfg)
	case "gcs":
		return openGCS(ctx, cfg)
	case "azure":
		return openAzure(ctx, cfg)
	default:
		return nil, errors.New("storage: unsupported driver")
	}
}

// credentialError avoids exposing SDK credential parsing errors and secrets.
// Cancellation remains inspectable. Never log entire configuration structs.
func credentialError(ctx context.Context, provider string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("storage: cannot initialize " + provider + " credentials or client; check connection settings and credential source")
}
