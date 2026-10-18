package identity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	identityhttp "github.com/wisalgroup/labena/internal/modules/identity/delivery/http"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/auth"
	"github.com/wisalgroup/labena/internal/platform/media"
)

type mediaService struct {
	identityhttp.Assets
	urlError  error
	expiresAt time.Time
	result    *media.Asset
	err       error
	calls     int
}

func (s *mediaService) Get(context.Context, media.Owner, uuid.UUID) (*media.Asset, error) {
	s.calls++
	return s.result, s.err
}
func TestMediaHTTP(t *testing.T) {
	asset := &media.Asset{ID: uuid.New(), Owner: media.Owner{Type: "user", ID: uuid.NewString()}}
	for _, tc := range []struct {
		path   string
		err    error
		status int
		code   string
	}{
		{"invalid", nil, 422, "validation_failed"},
		{asset.ID.String(), nil, 200, ""},
		{asset.ID.String(), media.ErrAssetNotFound, 404, "not_found"},
		{asset.ID.String(), errors.New("private SQL detail"), 500, "internal_error"},
	} {
		t.Run(tc.path+tc.code, func(t *testing.T) {
			svc := &mediaService{result: asset, err: tc.err}
			router := newCapabilityAPI(t)

			identityhttp.RegisterRoutes(nil, api.Authenticated(router, mediaAuth{uuid.MustParse(asset.Owner.ID)}), identityhttp.Routes{Media: svc})
			out := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/media/"+tc.path, nil)
			req.Header.Set("Authorization", "Bearer test")
			router.ServeHTTP(out, req)
			if out.Code != tc.status || strings.Contains(out.Body.String(), "private SQL") {
				t.Fatal(out.Code, out.Body.String())
			}
			if tc.code != "" && !strings.Contains(out.Body.String(), tc.code) {
				t.Fatal(out.Body.String())
			}
			if tc.path == "invalid" && svc.calls != 0 {
				t.Fatal("invalid ID reached service")
			}
		})
	}
}

type mediaAuth struct{ id uuid.UUID }

func (a mediaAuth) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{UserID: a.id, SessionID: uuid.New()}, nil
}

func TestUploadRejectsOversizeAndUnexpectedParts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		extra  bool
		status int
	}{
		{"too large", 21 << 20, false, 413},
		{"unexpected field", 5, true, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", "test.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(part, strings.NewReader(strings.Repeat("x", tc.size))); err != nil {
				t.Fatal(err)
			}
			if tc.extra {
				if err := writer.WriteField("user_id", uuid.NewString()); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			router := newCapabilityAPI(t)

			// No service may be called for rejected uploads.
			typed := api.Authenticated(router, mediaAuth{uuid.New()})
			identityhttp.RegisterRoutes(nil, huma.NewGroup(typed, "/nested"), identityhttp.Routes{Media: &mediaService{}})
			if typed.OpenAPI().Paths["/nested/media"].Post.RequestBody == nil {
				t.Fatal("nested upload is missing its multipart schema")
			}
			req := httptest.NewRequest("POST", "/nested/media", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.Header.Set("Authorization", "Bearer test")
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != tc.status {
				t.Fatal(out.Code, out.Body.String())
			}
		})
	}
}

func newCapabilityAPI(t testing.TB, options ...api.Options) *api.Server {
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

func (s *mediaService) URL(ctx context.Context, a *media.Asset) (*media.Link, error) {
	return &media.Link{URL: "/media/" + a.ID.String() + "/download", ExpiresAt: s.expiresAt}, s.urlError
}

func (s *mediaService) Upload(_ context.Context, in media.UploadInput) (*media.Asset, error) {
	s.calls++
	visibility := in.Visibility
	if visibility == "" {
		visibility = media.Public
	}
	if !visibility.Valid() {
		return nil, media.ErrInvalidUpload
	}
	s.result.Visibility = visibility
	return s.result, nil
}
func TestUploadVisibilityAndURL(t *testing.T) {
	for _, visibility := range []string{"", "public", "private", "invalid"} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "file.txt")
		_, _ = part.Write([]byte("hello"))
		if visibility != "" {
			_ = writer.WriteField("visibility", visibility)
		}
		_ = writer.Close()
		svc := &mediaService{result: &media.Asset{ID: uuid.New(), Status: media.AssetReady}}
		router := newCapabilityAPI(t)
		identityhttp.RegisterRoutes(nil, api.Authenticated(router, mediaAuth{uuid.New()}), identityhttp.Routes{Media: svc})
		request := httptest.NewRequest("POST", "/media", &body)
		request.Header.Set("Authorization", "Bearer test")
		request.Header.Set("Content-Type", writer.FormDataContentType())
		out := httptest.NewRecorder()
		router.ServeHTTP(out, request)
		if visibility == "invalid" {
			if out.Code != 422 {
				t.Fatal(out.Code)
			}
			continue
		}
		if out.Code != 201 {
			t.Fatal(out.Code, out.Body.String())
		}
		var response struct {
			Data identityhttp.AssetResponse `json:"data"`
		}
		if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		expected := visibility
		if expected == "" {
			expected = "public"
		}
		if string(response.Data.Visibility) != expected || response.Data.URL == "" {
			t.Fatal(response)
		}
	}
}

func TestUploadSurvivesSigningFailure(t *testing.T) {
	for _, signingFails := range []bool{false, true} {
		svc := &mediaService{result: &media.Asset{ID: uuid.New(), Status: media.AssetReady}}
		if signingFails {
			svc.urlError = errors.New("private signing detail")
		} else {
			svc.expiresAt = time.Now().UTC().Add(15 * time.Minute).Truncate(time.Second)
		}
		router := newCapabilityAPI(t)
		identityhttp.RegisterRoutes(nil, api.Authenticated(router, mediaAuth{uuid.New()}), identityhttp.Routes{Media: svc})
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "test.txt")
		part.Write([]byte("hello"))
		writer.WriteField("visibility", "private")
		writer.Close()
		request := httptest.NewRequest("POST", "/media", &body)
		request.Header.Set("Authorization", "Bearer test")
		request.Header.Set("Content-Type", writer.FormDataContentType())
		out := httptest.NewRecorder()
		router.ServeHTTP(out, request)
		if out.Code != 201 || strings.Contains(out.Body.String(), "private signing detail") {
			t.Fatal(out.Code, out.Body.String())
		}
		var result struct {
			Data identityhttp.AssetResponse `json:"data"`
		}
		if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Data.URL == "" || !result.Data.URLExpiresAt.Equal(svc.expiresAt) {
			t.Fatal("missing link or expiry")
		}
		if signingFails && strings.Contains(out.Body.String(), "url_expires_at") {
			t.Fatal("fallback must not claim expiration")
		}
		if signingFails {
			request = httptest.NewRequest("GET", "/media/"+svc.result.ID.String(), nil)
			request.Header.Set("Authorization", "Bearer test")
			out = httptest.NewRecorder()
			router.ServeHTTP(out, request)
			if out.Code != 500 || strings.Contains(out.Body.String(), "private signing detail") {
				t.Fatal("metadata signing error not sanitized")
			}
		}
	}
}
