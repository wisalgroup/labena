package integration

import (
	"context"
	"io"
	"mime"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"gocloud.dev/blob"

	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/media"
)

// Content authorizes public or signed reads without a business identity model.
type Content interface {
	OpenContent(ctx context.Context, assetID uuid.UUID, expires int64, signature string) (*media.Asset, *blob.Reader, error)
}

// ContentResponse streams an already authorized asset and closes its reader.
type ContentResponse struct{ Body func(huma.Context) }

// NewDownloadResponse transfers reader ownership to the response body and forces
// attachment delivery. The caller must authorize and open the asset first.
func NewDownloadResponse(ctx context.Context, asset *media.Asset, reader *blob.Reader) *ContentResponse {
	return streamAsset(ctx, asset, reader, false)
}

var inlineContentTypes = []string{"image/jpeg", "image/png", "image/gif", "image/webp", "image/avif", "image/x-icon", "audio/mpeg", "audio/wave", "audio/ogg", "video/mp4", "video/webm"}

func streamAsset(ctx context.Context, a *media.Asset, reader *blob.Reader, inline bool) *ContentResponse {
	return &ContentResponse{Body: func(c huma.Context) {
		defer func() { response.LogError(ctx, reader.Close()) }()
		disposition, contentType := "attachment", "application/octet-stream"
		if inline && slices.Contains(inlineContentTypes, a.ContentType) {
			disposition, contentType = "inline", a.ContentType
		}
		c.SetHeader("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.Filename}))
		c.SetHeader("Content-Type", contentType)
		c.SetHeader("X-Content-Type-Options", "nosniff")
		c.SetHeader("Content-Security-Policy", "sandbox; default-src 'none'")
		c.SetHeader("Referrer-Policy", "no-referrer")
		c.SetHeader("Cache-Control", "no-store")
		if _, err := io.Copy(c.BodyWriter(), reader); err != nil {
			response.LogError(ctx, err)
			panic(http.ErrAbortHandler)
		}
	}}
}

// RegisterContentHTTP installs the anonymous public/signed read route separately
// from owner-authenticated upload, list, metadata and deletion routes.
func RegisterContentHTTP(router huma.API, svc Content) {
	content := map[string]*huma.MediaType{"application/octet-stream": {Schema: &huma.Schema{Type: "string", Format: "binary"}}}
	for _, kind := range inlineContentTypes {
		content[kind] = &huma.MediaType{Schema: &huma.Schema{Type: "string", Format: "binary"}}
	}
	huma.Register(router, huma.Operation{OperationID: "media.content", Method: "GET", Path: "/media/{id}/content", Summary: "Read a public asset or use a signed link", Tags: []string{"media"},
		Responses: map[string]*huma.Response{"200": {Description: "Asset content; safe media types display inline, other types download", Content: content}},
	}, func(ctx context.Context, in *ContentRequest) (*ContentResponse, error) {
		a, reader, err := svc.OpenContent(ctx, uuid.MustParse(in.ID), in.Expires, in.Signature)
		if err != nil {
			return nil, response.Error(ctx, err)
		}
		return streamAsset(ctx, a, reader, true), nil
	})
}

type ContentRequest struct {
	ID        string `path:"id" format:"uuid"`
	Expires   int64  `query:"expires" minimum:"0"`
	Signature string `query:"signature" maxLength:"64"`
}
