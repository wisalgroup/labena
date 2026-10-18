package config

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/wisalgroup/labena/internal/platform/media"
	"github.com/wisalgroup/labena/pkg/storage"
)

// FilesystemConfig selects one default disk and defines available named disks.
type FilesystemConfig struct {
	Additional []string               `env:"FILESYSTEM_ADDITIONAL_DISKS" envSeparator:","`
	Default    string                 `env:"FILESYSTEM_DISK" envDefault:"minio"`
	Disks      map[string]*DiskConfig `env:"-"`
}

// DiskConfig defines one disk with exactly one driver. Definitions live
// below; FILESYSTEM_DISK selects a name, not a driver. Provider settings are
// decoded independently under FILESYSTEM_<NAME>_. Unused fields are ignored.
type DiskConfig struct {
	Visibility media.Visibility `env:"VISIBILITY" envDefault:"public"`
	URLMode    string           `env:"URL_MODE" envDefault:"internal"`
	PublicURL  string           `env:"PUBLIC_URL"`

	Driver          string `env:"-"`
	Root            string `env:"ROOT" envDefault:"storage/app"`
	Bucket          string `env:"BUCKET"`
	Region          string `env:"REGION" envDefault:"us-east-1"`
	Endpoint        string `env:"ENDPOINT"`
	UsePathStyle    bool   `env:"USE_PATH_STYLE"`
	AccessKeyID     string `env:"ACCESS_KEY_ID" json:"-"`
	SecretAccessKey string `env:"SECRET_ACCESS_KEY" json:"-"`
	SessionToken    string `env:"SESSION_TOKEN" json:"-"`
	CredentialsFile string `env:"CREDENTIALS_FILE" json:"-"`
	AccountName     string `env:"ACCOUNT_NAME"`
	AccountKey      string `env:"ACCOUNT_KEY" json:"-"`
}

// load is the application's filesystem configuration, analogous to
// Laravel's config/filesystems.php. Add named entries here to use more than one
// connection with the same driver. Defining a disk acquires no resources.
func (c *FilesystemConfig) load(values environment) error {
	c.Disks = map[string]*DiskConfig{
		"local": {Driver: "local"},
		"s3":    {Driver: "s3"},
		// Development fixture shared with Compose and Tilt. Use managed credentials
		// and a separate connection for deployments outside local development.
		"minio": {Driver: "s3", Bucket: "uploads", Endpoint: "http://127.0.0.1:9000",
			UsePathStyle: true, AccessKeyID: "demo", SecretAccessKey: "local-demo-minio"},
		"spaces": {Driver: "s3"},
		"gcs":    {Driver: "gcs"},
		"azure":  {Driver: "azure"},
	}
	for _, name := range slices.Sorted(maps.Keys(c.Disks)) {
		if err := values.parse(c.Disks[name], "FILESYSTEM_"+strings.ToUpper(name)+"_"); err != nil {
			return fmt.Errorf("config: filesystem %s: %w", name, err)
		}
	}
	return nil
}

// StorageConfig resolves a named disk and validates its owner settings without I/O.
// It neither opens a bucket nor modifies the caller's disk definitions.
func (c FilesystemConfig) StorageConfig(name string) (storage.Config, error) {
	disk := c.Disks[name]
	if disk == nil {
		return storage.Config{}, fmt.Errorf("config: unknown filesystem connection %q", name)
	}
	options := disk.StorageConfig()
	if err := options.Validate(); err != nil {
		return storage.Config{}, fmt.Errorf("config: filesystem %q: %w", name, err)
	}
	return options, nil
}

// StorageConfig returns the reusable storage package's settings without reading env.
func (c DiskConfig) StorageConfig() storage.Config {
	return storage.Config{
		Driver: c.Driver, Root: c.Root, Bucket: c.Bucket,
		S3: storage.S3Config{Region: c.Region, Endpoint: c.Endpoint,
			UsePathStyle: c.UsePathStyle, AccessKeyID: c.AccessKeyID,
			SecretAccessKey: c.SecretAccessKey, SessionToken: c.SessionToken},
		GCS:   storage.GCSConfig{CredentialsFile: c.CredentialsFile},
		Azure: storage.AzureConfig{ServiceURL: c.Endpoint, AccountName: c.AccountName, AccountKey: c.AccountKey},
	}
}

var diskName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func (c FilesystemConfig) Validate() error {
	for name, disk := range c.Disks {
		if name == "disabled" || !diskName.MatchString(name) || disk == nil {
			return fmt.Errorf("config: invalid filesystem connection %q", name)
		}
		if err := disk.mediaConfig().Validate(); err != nil {
			return fmt.Errorf("config: filesystem %q: %w", name, err)
		}
		if disk.Driver == "local" && disk.URLMode == "direct" {
			return fmt.Errorf("config: filesystem %q: local disks require internal URL mode", name)
		}
		if !slices.Contains([]string{"local", "s3", "gcs", "azure"}, disk.Driver) {
			return fmt.Errorf("config: filesystem %q has an unsupported driver", name)
		}
	}
	for _, name := range c.Additional {
		disk := c.Disks[name]
		if disk == nil {
			return fmt.Errorf("config: unknown additional filesystem disk %q", name)
		}
		if err := disk.StorageConfig().Validate(); err != nil {
			return fmt.Errorf("config: filesystem %q: %w", name, err)
		}
	}

	if c.Default == "disabled" {
		return nil
	}
	disk := c.Disks[c.Default]
	if disk == nil {
		return fmt.Errorf("config: unknown filesystem disk %q", c.Default)
	}
	if err := disk.StorageConfig().Validate(); err != nil {
		return fmt.Errorf("config: filesystem %q: %w", c.Default, err)
	}
	return nil
}

func (c DiskConfig) mediaConfig() media.DiskConfig {
	return media.DiskConfig{Visibility: c.Visibility, URLMode: c.URLMode, PublicURL: c.PublicURL}
}

// MediaConfigs maps filesystem link and visibility settings without opening disks.
func (c FilesystemConfig) MediaConfigs() map[string]media.DiskConfig {
	disks := make(map[string]media.DiskConfig, len(c.Disks))
	for name, disk := range c.Disks {
		if disk != nil {
			disks[name] = disk.mediaConfig()
		}
	}
	return disks
}
