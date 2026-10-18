package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
)

func TestLocalObjectLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "objects")
	c, err := Open(t.Context(), Config{Driver: "local", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	exerciseObjects(t, c.Bucket)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("storage root is not private")
	}
}

func exerciseObjects(t *testing.T, bucket *blob.Bucket) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	const key = "documents/example.txt"
	w, err := bucket.NewWriter(ctx, key, &blob.WriterOptions{ContentType: "text/plain", Metadata: map[string]string{"owner": "example"}})
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(w, strings.NewReader("hello storage"))
	if err := errors.Join(copyErr, w.Close()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bucket.Delete(ctx, key)
	})
	r, err := bucket.NewReader(ctx, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(r)
	if err := errors.Join(readErr, r.Close()); err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello storage" {
		t.Fatalf("read %q", data)
	}
	attrs, err := bucket.Attributes(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Size != 13 || attrs.ContentType != "text/plain" || attrs.Metadata["owner"] != "example" {
		t.Fatalf("attributes: %+v", attrs)
	}
	r, err = bucket.NewRangeReader(ctx, key, 6, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr = io.ReadAll(r)
	if err := errors.Join(readErr, r.Close()); err != nil {
		t.Fatal(err)
	}
	if string(data) != "storage" {
		t.Fatalf("range: %q", data)
	}
	iterator := bucket.List(&blob.ListOptions{Prefix: "documents/"})
	object, err := iterator.Next(ctx)
	if err != nil || object.Key != key {
		t.Fatalf("list: %v %v", object, err)
	}
	if _, err := iterator.Next(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("list end: %v", err)
	}
	if err := bucket.Copy(ctx, "copy.txt", key, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bucket.Delete(ctx, "copy.txt")
	})
	if err := bucket.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if exists, err := bucket.Exists(ctx, key); err != nil || exists {
		t.Fatalf("deleted key exists: %v %v", exists, err)
	}
	if _, err := bucket.NewReader(ctx, key, nil); gcerrors.Code(err) != gcerrors.NotFound {
		t.Fatalf("missing read: %v", err)
	}
	if err := bucket.Delete(ctx, "copy.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestLocalCancellationAndClose(t *testing.T) {
	c, err := Open(t.Context(), Config{Driver: "local", Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Open(ctx, Config{Driver: "local", Root: t.TempDir()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("open: %v", err)
	}
	if err := c.Bucket.WriteAll(ctx, "canceled", []byte("value"), nil); !errors.Is(err, context.Canceled) && gcerrors.Code(err) != gcerrors.Canceled {
		t.Fatalf("write: %v", err)
	}
	if exists, err := c.Bucket.Exists(t.Context(), "canceled"); err != nil || exists {
		t.Fatal("canceled write committed")
	}
	var closed atomic.Int32
	want := errors.New("client close failed")
	c.closeClient = func() error { closed.Add(1); return want }
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if err := c.Close(); !errors.Is(err, want) {
				t.Errorf("lost close error: %v", err)
			}
		})
	}
	wg.Wait()
	if closed.Load() != 1 {
		t.Fatal("native client closed more than once")
	}
	if err := c.Bucket.WriteAll(t.Context(), "closed", nil, nil); err == nil {
		t.Fatal("closed bucket accepted write")
	}
}

func TestInvalidStorageConfiguration(t *testing.T) {
	for _, cfg := range []Config{
		{}, {Driver: "minio"}, {Driver: "local"}, {Driver: "s3"},
		{Driver: "s3", Bucket: "uploads", S3: S3Config{Region: "us-east-1", AccessKeyID: "key"}},
		{Driver: "s3", Bucket: "uploads", S3: S3Config{Region: "us-east-1", Endpoint: "https://user:secret@example.com"}},
		{Driver: "s3", Bucket: "uploads", S3: S3Config{Region: "us-east-1", Endpoint: "https://example.com?secret=value"}},
		{Driver: "gcs"}, {Driver: "azure", Bucket: "../bad"},
	} {
		if _, err := Open(t.Context(), cfg); err == nil || strings.Contains(err.Error(), "secret") && strings.Contains(err.Error(), "example.com") {
			t.Fatal("expected safe validation error")
		}
	}
	if _, err := Open(t.Context(), Config{Driver: "gcs", Bucket: "uploads", GCS: GCSConfig{CredentialsFile: filepath.Join(t.TempDir(), "secret-credentials")}}); err == nil || strings.Contains(err.Error(), "secret-credentials") {
		t.Fatal("credential path exposed")
	}
}
