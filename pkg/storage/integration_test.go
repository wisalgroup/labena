package storage

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gocloud.dev/blob"
)

// This suite creates and deletes its own bucket on a disposable S3-compatible
// server. Never point it at production: the credentials need bucket-create rights.
func TestS3Integration(t *testing.T) {
	endpoint := os.Getenv("STORAGE_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_TEST_S3_ENDPOINT not set (disposable MinIO)")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	cfg := Config{Driver: "s3", Bucket: "storage-test-" + strings.ToLower(rand.Text()), S3: S3Config{
		Region: "us-east-1", Endpoint: endpoint, UsePathStyle: true,
		AccessKeyID: os.Getenv("STORAGE_TEST_S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("STORAGE_TEST_S3_SECRET_ACCESS_KEY"),
	}}
	c, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	var native *s3.Client
	if !c.Bucket.As(&native) {
		t.Fatal("missing native S3 client")
	}
	if _, err := native.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(cfg.Bucket)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, err := native.DeleteBucket(cleanupCtx, &s3.DeleteBucketInput{Bucket: aws.String(cfg.Bucket)}); err != nil {
			t.Error(err)
		}
	})
	exerciseObjects(t, c.Bucket)
	client := &http.Client{Timeout: 10 * time.Second}
	for _, method := range []string{http.MethodPut, http.MethodGet} {
		u, err := c.Bucket.SignedURL(ctx, "signed.txt", &blob.SignedURLOptions{Method: method, Expiry: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		var body io.Reader
		if method == http.MethodPut {
			body = strings.NewReader("signed upload")
		}
		req, err := http.NewRequestWithContext(ctx, method, u, body)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("signed %s: status %d, read %v, close %v", method, response.StatusCode, readErr, closeErr)
		}
		if method == http.MethodPut {
			t.Cleanup(func() {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
				defer stop()
				if err := c.Bucket.Delete(cleanupCtx, "signed.txt"); err != nil {
					t.Error(err)
				}
			})
		} else if string(data) != "signed upload" {
			t.Fatalf("signed download: %q", data)
		}
	}
}
