package media

import (
	"io"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/platform/internal/reference"
	"github.com/wisalgroup/labena/pkg/apperror"
)

// Asset is an object owned by a model, grouped into a named collection.
type Asset struct {
	bun.BaseModel `bun:"table:media_assets,alias:a"`
	ID            uuid.UUID   `bun:",pk,type:uuid" json:"id"`
	Owner         Owner       `bun:"embed:owner_" json:"owner"`
	Collection    string      `json:"collection"`
	Visibility    Visibility  `json:"visibility"`
	Disk          string      `json:"-"`
	Key           string      `json:"-"`
	Filename      string      `json:"filename"`
	ContentType   string      `json:"content_type"`
	Size          int64       `json:"size"`
	Status        AssetStatus `json:"status"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

// AssetStatus is owned by platform Media. Pending assets cannot be downloaded.
type AssetStatus string

const (
	AssetPending  AssetStatus = "pending"
	AssetReady    AssetStatus = "ready"
	AssetDeleting AssetStatus = "deleting"
	AssetDeleted  AssetStatus = "deleted"
)

func AssetStatusValues() []AssetStatus {
	return []AssetStatus{AssetPending, AssetReady, AssetDeleting, AssetDeleted}
}
func (s AssetStatus) Valid() bool { return slices.Contains(AssetStatusValues(), s) }

// Policy is a business-owned upload policy. MIME types are detected from bytes;
// this validation is not malware scanning or proof that an image can be decoded.
// Empty ContentTypes permits any detected type. MaxBytes must be 1..1 GiB.
type Policy struct {
	Disk         string
	MaxBytes     int64
	ContentTypes []string
}

// UploadInput borrows Body for the call. The caller owns closing it and must
// supply a reader that respects its request deadline. Filename is display-only.
type UploadInput struct {
	// Visibility inherits the disk default when omitted (Public unless configured). Private assets require owner authorization or a signed link.
	Visibility Visibility
	Owner      Owner
	Collection string
	Filename   string
	Body       io.Reader
	Policy     Policy
}

var (
	ErrAssetNotFound        = apperror.NotFound("The asset was not found.")
	ErrInvalidUpload        = apperror.Validation("Provide a filename, nonempty file, owner and valid upload policy.")
	ErrFileTooLarge         = apperror.Validation("The file exceeds the allowed size.")
	ErrContentType          = apperror.Validation("This file type is not allowed.")
	ErrUnavailable          = apperror.Unavailable("The asset disk is not configured.")
	ErrNotReady             = apperror.Conflict("The asset is not ready.")
	ErrDependenciesRequired = apperror.Internal("media: repository and valid named disks required")
	ErrLibraryRequired      = apperror.Internal("media: library required")
)

// Owner is the model an asset belongs to, independent of who uploaded it.
// Type is a stable application name; ID is the model's canonical string ID.
type Owner struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

var collectionPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

func (o Owner) Valid() bool {
	return reference.Valid(o.Type, o.ID)
}

const DefaultCollection = "default"

func validCollection(name string) bool { return collectionPattern.MatchString(name) }

var ErrInvalidCollection = apperror.Validation("Provide a collection name of 1 to 64 lowercase letters, digits, dots, hyphens or underscores, starting with a letter.")

// Visibility controls anonymous access, independently of ownership and storage ACLs.
type Visibility string

const (
	Public  Visibility = "public"
	Private Visibility = "private"
)

func (v Visibility) Valid() bool { return v == Public || v == Private }
