package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/wisalgroup/labena/pkg/storage"
	"gocloud.dev/blob"
)

// Uses only a uniquely named bucket on an explicitly supplied disposable server.
func TestDirectStorageURLs(t *testing.T) {
	endpoint := os.Getenv("STORAGE_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_TEST_S3_ENDPOINT not set (disposable MinIO)")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	name := "media-test-" + uuid.NewString()
	connection, err := storage.Open(ctx, storage.Config{Driver: "s3", Bucket: name, S3: storage.S3Config{
		Region: "us-east-1", Endpoint: endpoint, UsePathStyle: true,
		AccessKeyID: os.Getenv("STORAGE_TEST_S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("STORAGE_TEST_S3_SECRET_ACCESS_KEY"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var client *s3.Client
	if !connection.Bucket.As(&client) {
		t.Fatal("missing S3 client")
	}
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(name)}); err != nil {
		t.Fatal(err)
	}
	var keys []string
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		for _, key := range keys {
			if err := connection.Bucket.Delete(cleanup, key); err != nil {
				t.Error(err)
			}
		}
		if _, err := client.DeleteBucket(cleanup, &s3.DeleteBucketInput{Bucket: aws.String(name)}); err != nil {
			t.Error(err)
		}
	}()
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::%s/public/*"}]}`, name)
	if _, err := client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(name), Policy: &policy}); err != nil {
		t.Fatal(err)
	}
	base := endpoint + "/" + name
	library, err := newWithStore(&assetRepo{}, map[string]*blob.Bucket{"test": connection.Bucket}, "test", Config{Disks: map[string]DiskConfig{"test": {URLMode: "direct", PublicURL: base}}})
	if err != nil {
		t.Fatal(err)
	}
	owner := Owner{Type: "user", ID: uuid.NewString()}
	httpClient := &http.Client{Timeout: 5 * time.Second}
	read := func(address string, want int) {
		t.Helper()
		response, err := httpClient.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != want {
			t.Fatalf("storage response status %d, want %d: %v", response.StatusCode, want, err)
		}
		if want == http.StatusOK && string(body) != "hello" {
			t.Fatal("wrong file content")
		}
	}
	for _, visibility := range []Visibility{Public, Private} {
		a, err := library.Upload(ctx, UploadInput{Owner: owner, Visibility: visibility, Filename: "test.txt", Body: strings.NewReader("hello"), Policy: Policy{MaxBytes: 100}})
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, a.Key)
		link, err := library.URL(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		read(link.URL, http.StatusOK)
		if visibility == Public {
			if link.URL != base+"/"+a.Key || !link.ExpiresAt.IsZero() {
				t.Fatal("public URL is not permanent")
			}
		} else {
			if link.ExpiresAt.Before(time.Now().Add(14 * time.Minute)) {
				t.Fatal("missing private expiry")
			}
			read(base+"/"+a.Key, http.StatusForbidden)
		}
		if _, err := library.TemporaryURL(ctx, Owner{Type: "user", ID: "other"}, a.ID, time.Minute); err == nil {
			t.Fatal("cross-owner signing")
		}
		temporary, err := library.TemporaryURL(ctx, owner, a.ID, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		read(temporary.URL, http.StatusOK)
	}
}
