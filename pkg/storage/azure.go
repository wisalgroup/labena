package storage

import (
	"context"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"gocloud.dev/blob/azureblob"
)

func openAzure(ctx context.Context, cfg Config) (*Connection, error) {
	containerURL := strings.TrimRight(cfg.Azure.ServiceURL, "/") + "/" + cfg.Bucket
	var client *container.Client
	var err error
	if cfg.Azure.AccountKey != "" {
		credential, credentialErr := container.NewSharedKeyCredential(cfg.Azure.AccountName, cfg.Azure.AccountKey)
		if credentialErr != nil {
			return nil, credentialError(ctx, "Azure")
		}
		client, err = container.NewClientWithSharedKeyCredential(containerURL, credential, nil)
	} else {
		credential, credentialErr := azidentity.NewDefaultAzureCredential(nil)
		if credentialErr != nil {
			return nil, credentialError(ctx, "Azure")
		}
		client, err = container.NewClient(containerURL, credential, nil)
	}
	if err != nil {
		return nil, credentialError(ctx, "Azure")
	}
	bucket, err := azureblob.OpenBucket(ctx, client, nil)
	if err != nil {
		return nil, err
	}
	return &Connection{Bucket: bucket}, nil
}
