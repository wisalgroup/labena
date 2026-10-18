package storage

import (
	"context"
	"encoding/json"
	"errors"

	"cloud.google.com/go/auth/credentials"
	gcs "cloud.google.com/go/storage"
	"gocloud.dev/blob/gcsblob"
	"google.golang.org/api/option"
)

func openGCS(ctx context.Context, cfg Config) (*Connection, error) {
	// The native auth provider takes the operation context when fetching tokens;
	// do not bind a long-lived token source to the short-lived startup context.
	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		CredentialsFile:     cfg.GCS.CredentialsFile,
		Scopes:              []string{gcs.ScopeReadWrite},
		DisableAsyncRefresh: true,
	})
	if err != nil {
		return nil, credentialError(ctx, "GCS")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, err := gcs.NewClient(ctx, option.WithAuthCredentials(creds))
	if err != nil {
		return nil, credentialError(ctx, "GCS")
	}
	// Go CDK requires explicit signing credentials. Service account files can
	// sign locally; other ADC identities support object I/O but not SignedURL
	// through this constructor. No implicit IAM signing client is created.
	var signing struct {
		Email string `json:"client_email"`
		Key   string `json:"private_key"`
	}
	_ = json.Unmarshal(creds.JSON(), &signing)
	bucket, err := gcsblob.OpenBucket(ctx, nil, cfg.Bucket, &gcsblob.Options{
		Client: client, GoogleAccessID: signing.Email, PrivateKey: []byte(signing.Key),
	})
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	// Go CDK borrows this client; closing the blob bucket alone does not close it.
	return &Connection{Bucket: bucket, closeClient: client.Close}, nil
}
