// Run from the repository root: APP_ENV_FILE= go run ./examples/storage
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/pkg/storage"

	"gocloud.dev/blob"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (err error) {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Filesystem.Default == "disabled" {
		return errors.New("select a filesystem disk to run this example")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	options, err := cfg.Filesystem.StorageConfig(cfg.Filesystem.Default)
	if err != nil {
		return err
	}
	connection, err := storage.Open(ctx, options)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, connection.Close()) }()
	bucket := connection.Bucket
	key := "examples/" + rand.Text() + ".txt"
	if err := upload(ctx, bucket, key, strings.NewReader("Hello from the configured filesystem.\n")); err != nil {
		return err
	}
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err = errors.Join(err, bucket.Delete(cleanupCtx, key))
	}()
	reader, err := bucket.NewReader(ctx, key, nil)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(os.Stdout, reader)
	return errors.Join(copyErr, reader.Close())
}

func upload(ctx context.Context, bucket *blob.Bucket, key string, src io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	writer, err := bucket.NewWriter(ctx, key, &blob.WriterOptions{ContentType: "text/plain"})
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(writer, src)
	if copyErr != nil {
		cancel()
	}
	return errors.Join(copyErr, writer.Close())
}
