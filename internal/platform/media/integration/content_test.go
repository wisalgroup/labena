package integration

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"gocloud.dev/blob"
	"gocloud.dev/blob/memblob"

	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/media"
)

func newAPI(t testing.TB, options ...api.Options) *api.Server {
	t.Helper()
	var opts api.Options
	if len(options) > 0 {
		opts = options[0]
	}
	server, err := api.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

type contentService struct {
	asset     *media.Asset
	bucket    *blob.Bucket
	expires   int64
	signature string
}

func (s *contentService) OpenContent(ctx context.Context, id uuid.UUID, expires int64, signature string) (*media.Asset, *blob.Reader, error) {
	if id != s.asset.ID {
		return nil, nil, media.ErrAssetNotFound
	}
	s.expires, s.signature = expires, signature
	reader, err := s.bucket.NewReader(ctx, "file", nil)
	return s.asset, reader, err
}
func TestMediaContentHTTP(t *testing.T) {
	bucket := memblob.OpenBucket(nil)
	defer bucket.Close()
	if err := bucket.WriteAll(t.Context(), "file", []byte("content"), nil); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"image/png", "text/html", "image/svg+xml"} {
		svc := &contentService{asset: &media.Asset{ID: uuid.New(), Filename: "file", ContentType: kind}, bucket: bucket}
		router := newAPI(t)
		RegisterContentHTTP(huma.NewGroup(router, "/nested"), svc)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest("GET", "/nested/media/"+svc.asset.ID.String()+"/content?expires=123&signature=test", nil))
		if out.Code != 200 || out.Body.String() != "content" || svc.expires != 123 || svc.signature != "test" {
			t.Fatal(out.Code, out.Body.String())
		}
		disposition := "attachment"
		if kind == "image/png" {
			disposition = "inline"
			if out.Header().Get("Content-Type") != kind {
				t.Fatal(out.Header())
			}
		}
		if !strings.HasPrefix(out.Header().Get("Content-Disposition"), disposition) || out.Header().Get("X-Content-Type-Options") != "nosniff" || out.Header().Get("Cache-Control") != "no-store" || out.Header().Get("Content-Security-Policy") == "" {
			t.Fatal(out.Header())
		}
		out = httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest("GET", "/nested/media/"+uuid.NewString()+"/content", nil))
		if out.Code != 404 {
			t.Fatal(out.Code)
		}
	}
}
