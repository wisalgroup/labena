package http

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gocloud.dev/blob"

	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/auth"
	"github.com/wisalgroup/labena/internal/platform/media"
	mediaintegration "github.com/wisalgroup/labena/internal/platform/media/integration"
	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/pagination"
)

// Assets provides owner-scoped media operations for Identity users.
type Assets interface {
	URL(ctx context.Context, asset *media.Asset) (*media.Link, error)
	TemporaryURL(ctx context.Context, owner media.Owner, assetID uuid.UUID, ttl time.Duration) (*media.Link, error)
	Upload(ctx context.Context, input media.UploadInput) (*media.Asset, error)
	SetVisibility(ctx context.Context, owner media.Owner, assetID uuid.UUID, visibility media.Visibility) (*media.Asset, error)
	Get(ctx context.Context, owner media.Owner, assetID uuid.UUID) (*media.Asset, error)
	List(ctx context.Context, owner media.Owner, collection string, page pagination.Pagination) (*pagination.Page[*media.Asset], error)
	Open(ctx context.Context, owner media.Owner, assetID uuid.UUID) (*media.Asset, *blob.Reader, error)
	Delete(ctx context.Context, owner media.Owner, assetID uuid.UUID) error
}
type mediaHandler struct{ svc Assets }

const uploadLimit int64 = 20 << 20

func ownerAndID(ctx context.Context, id string) (media.Owner, uuid.UUID, error) {
	owner, err := currentOwner(ctx)
	if err != nil {
		return media.Owner{}, uuid.Nil, err
	}
	return owner, uuid.MustParse(id), nil
}

func (h *mediaHandler) upload(ctx context.Context, in *UploadRequest) (*response.JSON[AssetResponse], error) {
	owner, err := currentOwner(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	form := &in.RawBody
	if len(form.File) != 1 || len(form.File["file"]) != 1 || len(form.Value) > 1 || len(form.Value["visibility"]) > 1 || (len(form.Value) == 1 && len(form.Value["visibility"]) == 0) {
		return nil, response.Error(ctx, apperror.Validation("Provide one file part named file and at most one visibility field."))
	}
	visibility := ""
	if values := form.Value["visibility"]; len(values) == 1 {
		visibility = values[0]
	}
	header := form.File["file"][0]
	file, err := header.Open()
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	defer file.Close()
	a, err := h.svc.Upload(ctx, media.UploadInput{Visibility: media.Visibility(visibility), Owner: owner, Filename: header.Filename, Body: file, Policy: media.Policy{MaxBytes: uploadLimit}})
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	// URL signing must not turn an already committed upload into a failure.
	// The resolver supplies an authenticated internal fallback on signing failure.
	out, _ := h.assetResponse(ctx, a)
	return response.Created(out), nil
}

func (h *mediaHandler) list(ctx context.Context, in *ListAssetsRequest) (*response.JSON[*pagination.Page[AssetResponse]], error) {
	owner, err := currentOwner(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	p := in.Pagination()
	page, err := h.svc.List(ctx, owner, in.Collection, p)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	items := make([]AssetResponse, len(page.Items))
	for i, a := range page.Items {
		items[i], err = h.assetResponse(ctx, a)
		if err != nil {
			return nil, response.Error(ctx, err)
		}
	}
	return response.OK(pagination.NewPage(items, p, page.Total)), nil
}

func (h *mediaHandler) get(ctx context.Context, in *AssetRequest) (*response.JSON[AssetResponse], error) {
	owner, id, err := ownerAndID(ctx, in.ID)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	a, err := h.svc.Get(ctx, owner, id)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	out, err := h.assetResponse(ctx, a)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.OK(out), nil
}

func (h *mediaHandler) setVisibility(ctx context.Context, in *SetAssetVisibilityRequest) (*response.JSON[AssetResponse], error) {
	owner, id, err := ownerAndID(ctx, in.ID)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	a, err := h.svc.SetVisibility(ctx, owner, id, in.Body.Visibility)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	// As with upload, signing failure must not disguise a committed change.
	out, _ := h.assetResponse(ctx, a)
	return response.OK(out), nil
}

func (h *mediaHandler) download(ctx context.Context, in *AssetRequest) (*mediaintegration.ContentResponse, error) {
	owner, id, err := ownerAndID(ctx, in.ID)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	a, reader, err := h.svc.Open(ctx, owner, id)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return mediaintegration.NewDownloadResponse(ctx, a, reader), nil
}

func (h *mediaHandler) temporaryURL(ctx context.Context, in *TemporaryURLRequest) (*response.JSON[media.Link], error) {
	owner, id, err := ownerAndID(ctx, in.ID)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	link, err := h.svc.TemporaryURL(ctx, owner, id, time.Duration(in.Body.ExpiresIn)*time.Second)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.OK(*link), nil
}

func (h *mediaHandler) delete(ctx context.Context, in *AssetRequest) (*response.Empty, error) {
	owner, id, err := ownerAndID(ctx, in.ID)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	if err := h.svc.Delete(ctx, owner, id); err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.NoContent(), nil
}

// Management routes authorize Identity users, never arbitrary request owners.
func currentOwner(ctx context.Context) (media.Owner, error) {
	id, err := auth.CurrentUserID(ctx)
	if err != nil {
		return media.Owner{}, err
	}
	return media.Owner{Type: string(entities.UserEntityType), ID: id.String()}, nil
}

func (h *mediaHandler) assetResponse(ctx context.Context, a *media.Asset) (AssetResponse, error) {
	link, err := h.svc.URL(ctx, a)
	return newAssetResponse(a, link), err
}
