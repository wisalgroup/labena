package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemEnvironmentAndIsolation(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{
		"FILESYSTEM_DISK":                    "minio",
		"FILESYSTEM_MINIO_BUCKET":            "uploads",
		"FILESYSTEM_MINIO_ENDPOINT":          "http://localhost:19000",
		"FILESYSTEM_MINIO_ACCESS_KEY_ID":     "test-key",
		"FILESYSTEM_MINIO_SECRET_ACCESS_KEY": "test-secret",
		"FILESYSTEM_S3_BUCKET":               "archive",
		"FILESYSTEM_SPACES_REGION":           "ams3",
		"FILESYSTEM_MINIO_USE_PATH_STYLE":    "false",
	})
	if err != nil {
		t.Fatal(err)
	}
	minio := cfg.Filesystem.Disks[cfg.Filesystem.Default].StorageConfig()
	if minio.Driver != "s3" || minio.Bucket != "uploads" || minio.S3.Endpoint != "http://localhost:19000" || minio.S3.AccessKeyID != "test-key" || minio.S3.SecretAccessKey != "test-secret" || minio.S3.UsePathStyle {
		t.Fatal("named settings did not reach storage options")
	}
	if cfg.Filesystem.Disks["s3"].Bucket != "archive" || cfg.Filesystem.Disks["s3"].AccessKeyID != "" || cfg.Filesystem.Disks["spaces"].Region != "ams3" {
		t.Fatal("filesystem settings leaked between connections")
	}
	first, second := Defaults(), Defaults()
	first.Filesystem.Disks["minio"].Bucket = "changed"
	if second.Filesystem.Default != "minio" || second.Filesystem.Disks["minio"].Bucket != "uploads" || first.Filesystem.Disks["s3"].Bucket != "" || second.Filesystem.Disks["minio"].Endpoint != "http://127.0.0.1:9000" || !second.Filesystem.Disks["minio"].UsePathStyle {
		t.Fatal("defaults read environment or share settings")
	}
}

func TestFilesystemValidation(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Filesystem.Default = "missing" },
		func(c *Config) { c.Filesystem.Default = "s3" },
		func(c *Config) { c.Filesystem.Default = "local"; c.Filesystem.Disks["local"].Root = "" },
		func(c *Config) { c.Filesystem.Disks["local"] = nil },
		func(c *Config) { c.Filesystem.Disks["bad-name"] = &DiskConfig{Driver: "local"} },
		func(c *Config) { c.Filesystem.Disks["spare"] = &DiskConfig{Driver: "unknown"} },
		func(c *Config) { c.Filesystem.Disks["disabled"] = &DiskConfig{Driver: "local"} },
	} {
		cfg := Defaults()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatal("invalid filesystem accepted")
		}
	}
	cfg := Defaults()
	cfg.Filesystem.Default = "disabled"
	cfg.Filesystem.Disks = nil
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	_, err := loadTestConfig(t, map[string]string{"FILESYSTEM_MINIO_USE_PATH_STYLE": "secret-value"})
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatal("expected sanitized parsing error")
	}
}

func TestFilesystemLoadDoesNotAcquireResources(t *testing.T) {
	root := filepath.Join(t.TempDir(), "unopened")
	cfg, err := loadTestConfig(t, map[string]string{"FILESYSTEM_DISK": "local", "FILESYSTEM_LOCAL_ROOT": root})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Filesystem.Disks["local"].Root != root {
		t.Fatal("root not loaded")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("configuration created a storage directory")
	}
	// Incomplete cloud definitions are permitted until selected/opened.
	if cfg.Filesystem.Disks["azure"].Bucket != "" {
		t.Fatal("unexpected configured cloud storage")
	}
}

func TestAdditionalDisksAreExplicitAndValidated(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"FILESYSTEM_DISK": "disabled", "FILESYSTEM_ADDITIONAL_DISKS": "local,s3", "FILESYSTEM_LOCAL_ROOT": t.TempDir(), "FILESYSTEM_S3_BUCKET": "archive"})
	if err != nil || len(cfg.Filesystem.Additional) != 2 {
		t.Fatal(cfg.Filesystem.Additional, err)
	}
	for _, names := range []string{"missing", "disabled", "s3"} {
		_, err := loadTestConfig(t, map[string]string{"FILESYSTEM_DISK": "disabled", "FILESYSTEM_ADDITIONAL_DISKS": names})
		if err == nil {
			t.Fatal("invalid additional disk accepted", names)
		}
	}
}

func TestFilesystemMediaSettings(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{
		"FILESYSTEM_MINIO_VISIBILITY": "private",
		"FILESYSTEM_MINIO_URL_MODE":   "direct",
		"FILESYSTEM_MINIO_PUBLIC_URL": "https://files.example/uploads",
	})
	if err != nil {
		t.Fatal(err)
	}
	options := cfg.Filesystem.MediaConfigs()
	if options["minio"].Visibility != "private" || options["minio"].URLMode != "direct" || options["minio"].PublicURL != "https://files.example/uploads" || options["local"].Visibility != "public" || options["local"].URLMode != "internal" {
		t.Fatal("disk settings not isolated")
	}
	for _, values := range []map[string]string{
		{"FILESYSTEM_MINIO_VISIBILITY": "bad"}, {"FILESYSTEM_MINIO_URL_MODE": "bad"},
		{"FILESYSTEM_MINIO_URL_MODE": "direct"},
		{"FILESYSTEM_LOCAL_URL_MODE": "direct", "FILESYSTEM_LOCAL_PUBLIC_URL": "https://files.example"},
	} {
		if _, err := loadTestConfig(t, values); err == nil {
			t.Fatal("invalid disk settings accepted")
		}
	}
}
