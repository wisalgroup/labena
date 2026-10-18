package media

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wisalgroup/labena/pkg/apperror"
	"gocloud.dev/blob"
)

// Config configures media routes and per-disk defaults and URL delivery.
// BaseURL must match the registered media routes; empty uses /api/v1/media.
// SigningKey is an optional, independent 32-byte secret shared across replicas.
// Rotating it invalidates outstanding links. The library copies it on construction.
type Config struct {
	BaseURL    string
	SigningKey []byte `json:"-"`
	Disks      map[string]DiskConfig
}

// DiskConfig controls media defaults and link delivery for one named disk.
// PublicURL is the externally reachable bucket root (including the bucket when
// using path-style S3). It is never inferred from an internal network endpoint.
// Visibility affects new uploads only; it does not change provider permissions.
type DiskConfig struct {
	Visibility Visibility
	URLMode    string
	PublicURL  string
}

func (d DiskConfig) Validate() error {
	if d.Visibility != "" && !d.Visibility.Valid() {
		return errors.New("media: visibility must be public or private")
	}
	if d.URLMode != "" && d.URLMode != "internal" && d.URLMode != "direct" {
		return errors.New("media: URL mode must be internal or direct")
	}
	if d.PublicURL != "" {
		u, err := url.Parse(d.PublicURL)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(d.PublicURL, "\\\r\n") {
			return errors.New("media: public URL must be an HTTP(S) bucket root without credentials, query or fragment")
		}
	}
	if d.URLMode == "direct" && d.PublicURL == "" {
		return errors.New("media: direct URL mode requires a public URL")
	}
	return nil
}

func (o Config) Validate() error {
	for _, disk := range o.Disks {
		if err := disk.Validate(); err != nil {
			return err
		}
	}
	u, err := url.Parse(o.BaseURL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(o.BaseURL, "\\\r\n") {
		return errors.New("media: invalid base URL")
	}
	if o.BaseURL != "" && !((u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" || u.Scheme == "" && u.Host == "" && strings.HasPrefix(u.Path, "/") && !strings.HasPrefix(u.Path, "//")) {
		return errors.New("media: base URL must be HTTP(S) or an absolute path")
	}
	if len(o.SigningKey) != 0 && len(o.SigningKey) != 32 {
		return errors.New("media: signing key must contain 32 bytes")
	}
	return nil
}

func (o Config) normalized() Config {
	if o.BaseURL == "" {
		o.BaseURL = "/api/v1/media"
	}
	o.BaseURL = strings.TrimRight(o.BaseURL, "/")
	o.SigningKey = slices.Clone(o.SigningKey)
	o.Disks = maps.Clone(o.Disks)
	return o
}

// URL resolves a ready asset's configured link. Call only after authorizing the
// caller to view this asset: private direct links are bearer credentials.
// If provider signing fails, the result contains an authenticated internal fallback
// alongside the error, allowing an already committed upload to return successfully.
// Pending assets return an empty link. Legacy public objects without the public/
// prefix retain internal URLs until explicitly migrated in storage and metadata.
func (s *Library) URL(ctx context.Context, a *Asset) (*Link, error) {
	if a == nil || a.ID == uuid.Nil || a.Status != AssetReady {
		return &Link{}, nil
	}
	disk := s.options.Disks[a.Disk]
	if disk.URLMode == "direct" {
		if a.Visibility == Private {
			link, err := s.storageURL(ctx, a, 15*time.Minute)
			if err == nil {
				return link, nil
			}
			return &Link{URL: s.options.BaseURL + "/" + a.ID.String() + "/download"}, err
		}
		if a.Visibility == Public && strings.HasPrefix(a.Key, "public/") {
			u, _ := url.Parse(disk.PublicURL)
			u.Path = strings.TrimRight(u.Path, "/") + "/" + a.Key
			u.RawPath = ""
			return &Link{URL: u.String()}, nil
		}
	}
	suffix := "/download"
	if a.Visibility == Public {
		suffix = "/content"
	}
	return &Link{URL: s.options.BaseURL + "/" + a.ID.String() + suffix}, nil
}

func (s *Library) storageURL(ctx context.Context, a *Asset, ttl time.Duration) (*Link, error) {
	bucket := s.disks[a.Disk]
	if bucket == nil {
		return nil, ErrUnavailable
	}
	expires := time.Now().UTC().Add(ttl)
	link, err := bucket.SignedURL(ctx, a.Key, &blob.SignedURLOptions{Method: http.MethodGet, Expiry: ttl})
	if err != nil {
		return nil, fmt.Errorf("media: sign storage URL: %w", err)
	}
	return &Link{URL: link, ExpiresAt: expires}, nil
}

// Link is a resolved media URL. A zero ExpiresAt denotes a stable URL.
// Signed links grant bearer access; internal private download URLs require authentication.
type Link struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

var ErrSigningUnavailable = apperror.Unavailable("Media signed links are not configured.")
var ErrInvalidExpiry = apperror.Validation("Link lifetime must be between one second and 24 hours.")

// TemporaryURL authorizes the owner before issuing a signed content URL. A zero
// lifetime uses 15 minutes. Links never change the asset's visibility.
func (s *Library) TemporaryURL(ctx context.Context, owner Owner, id uuid.UUID, ttl time.Duration) (*Link, error) {
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	if ttl < time.Second || ttl > 24*time.Hour {
		return nil, ErrInvalidExpiry
	}
	a, err := s.Get(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if a.Status != AssetReady {
		return nil, ErrNotReady
	}
	if s.options.Disks[a.Disk].URLMode == "direct" {
		return s.storageURL(ctx, a, ttl)
	}
	if len(s.options.SigningKey) == 0 {
		return nil, ErrSigningUnavailable
	}
	expires := time.Now().UTC().Add(ttl).Truncate(time.Second)
	q := url.Values{"expires": {strconv.FormatInt(expires.Unix(), 10)}, "signature": {s.signature(id, expires.Unix())}}
	return &Link{URL: s.options.BaseURL + "/" + id.String() + "/content?" + q.Encode(), ExpiresAt: expires}, nil
}

func (s *Library) signature(id uuid.UUID, expires int64) string {
	mac := hmac.New(sha256.New, s.options.SigningKey)
	fmt.Fprintf(mac, "media-content-v1\n%s\n%s\n%d", s.options.BaseURL, id.String(), expires)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// OpenContent permits anonymous ready public assets or a valid signed link.
// Unknown, private, deleted and invalid-link assets have the same not-found result.
// The caller must close the reader. Management operations still require an owner.
func (s *Library) OpenContent(ctx context.Context, id uuid.UUID, expires int64, signature string) (*Asset, *blob.Reader, error) {
	if id == uuid.Nil {
		return nil, nil, ErrAssetNotFound
	}
	// When signature parameters are supplied, validate even for public assets.
	signed := expires != 0 || signature != ""
	if signed && (len(s.options.SigningKey) == 0 || expires <= time.Now().Unix() || !hmac.Equal([]byte(signature), []byte(s.signature(id, expires)))) {
		return nil, nil, ErrAssetNotFound
	}
	a, err := s.repo.Find(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if a.Status != AssetReady || (a.Visibility != Public && !signed) {
		return nil, nil, ErrAssetNotFound
	}
	return s.open(ctx, a)
}
