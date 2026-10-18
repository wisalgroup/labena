package storage

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gocloud.dev/blob/s3blob"
)

func openS3(ctx context.Context, cfg Config) (*Connection, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.S3.Region)}
	if cfg.S3.AccessKeyID != "" {
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey, cfg.S3.SessionToken,
		)))
	}
	native, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, credentialError(ctx, "S3")
	}
	client := s3.NewFromConfig(native, func(options *s3.Options) {
		options.UsePathStyle = cfg.S3.UsePathStyle
		if cfg.S3.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.S3.Endpoint)
		}
	})
	bucket, err := s3blob.OpenBucket(ctx, client, cfg.Bucket, &s3blob.Options{
		// Retain integrity checks where required while supporting S3-compatible
		// servers that do not implement AWS's optional streaming checksums.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	})
	if err != nil {
		return nil, err
	}
	return &Connection{Bucket: bucket}, nil
}
