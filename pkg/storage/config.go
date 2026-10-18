package storage

import (
	"errors"
	"net/url"
	"strings"
)

// Config selects exactly one driver. Only settings for that driver are used.
// Credentials are optional when the provider's default identity chain is used.
// Values are copied by Open. Endpoints and roots are trusted operator settings,
// never values supplied by upload clients.
type Config struct {
	Driver string
	Root   string
	Bucket string // Bucket name, or Azure container name.
	S3     S3Config
	GCS    GCSConfig
	Azure  AzureConfig
}

type S3Config struct {
	Region          string
	Endpoint        string
	UsePathStyle    bool
	AccessKeyID     string `json:"-"`
	SecretAccessKey string `json:"-"`
	SessionToken    string `json:"-"`
}

type GCSConfig struct {
	// CredentialsFile selects a per-connection service account or workload
	// identity credential file. Empty uses Application Default Credentials.
	CredentialsFile string `json:"-"`
}

type AzureConfig struct {
	// ServiceURL is the account URL, including any emulator account path.
	// Queries (including SAS tokens) and embedded credentials are not accepted.
	ServiceURL  string
	AccountName string
	AccountKey  string `json:"-"`
}

// Validate checks the selected driver's settings without I/O. Inactive named
// connections may remain incomplete until selected or explicitly opened.
func (c Config) Validate() error {
	switch c.Driver {
	case "local":
		if strings.TrimSpace(c.Root) == "" {
			return errors.New("storage: local root is required")
		}
	case "s3":
		if !validBucket(c.Bucket) || strings.TrimSpace(c.S3.Region) == "" {
			return errors.New("storage: S3 bucket and region are required")
		}
		if (c.S3.AccessKeyID == "") != (c.S3.SecretAccessKey == "") || (c.S3.SessionToken != "" && c.S3.AccessKeyID == "") {
			return errors.New("storage: S3 access key and secret must be supplied together; session token requires both")
		}
		if c.S3.Endpoint != "" && !validEndpoint(c.S3.Endpoint) {
			return errors.New("storage: invalid S3 endpoint (expected HTTP(S) URL without credentials, query or fragment)")
		}
	case "gcs":
		if !validBucket(c.Bucket) {
			return errors.New("storage: GCS bucket is required")
		}
	case "azure":
		if !validBucket(c.Bucket) || !validEndpoint(c.Azure.ServiceURL) {
			return errors.New("storage: Azure container and valid HTTP(S) service URL are required")
		}
		if c.Azure.AccountKey != "" && c.Azure.AccountName == "" {
			return errors.New("storage: Azure account name is required with an account key")
		}
	default:
		return errors.New("storage: unsupported driver (expected local, s3, gcs or azure)")
	}
	return nil
}

func validBucket(name string) bool {
	return name != "" && name != "." && name != ".." && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "/\\?#\x00\r\n")
}

func validEndpoint(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}
