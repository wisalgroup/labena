package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	identityhttp "github.com/wisalgroup/labena/internal/modules/identity/delivery/http"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/media"
)

type visibilityService struct {
	mediaService
	owner      media.Owner
	id         uuid.UUID
	visibility media.Visibility
}

func (s *visibilityService) SetVisibility(_ context.Context, owner media.Owner, id uuid.UUID, visibility media.Visibility) (*media.Asset, error) {
	s.calls++
	s.owner, s.id, s.visibility = owner, id, visibility
	return s.result, s.err
}

func TestSetMediaVisibilityHTTP(t *testing.T) {
	userID, assetID := uuid.New(), uuid.New()
	for _, test := range []struct {
		name, body, id string
		authenticated  bool
		serviceErr     error
		urlErr         error
		status, calls  int
	}{
		{name: "private", body: `{"visibility":"private"}`, id: assetID.String(), authenticated: true, status: 200, calls: 1},
		{name: "public", body: `{"visibility":"public"}`, id: assetID.String(), authenticated: true, status: 200, calls: 1},
		{name: "signing fallback", body: `{"visibility":"private"}`, id: assetID.String(), authenticated: true, urlErr: errors.New("signing unavailable"), status: 200, calls: 1},
		{name: "missing", body: `{}`, id: assetID.String(), authenticated: true, status: 422},
		{name: "invalid", body: `{"visibility":"hidden"}`, id: assetID.String(), authenticated: true, status: 422},
		{name: "invalid id", body: `{"visibility":"private"}`, id: "invalid", authenticated: true, status: 422},
		{name: "unauthenticated", body: `{"visibility":"private"}`, id: assetID.String(), status: 401},
		{name: "other owner", body: `{"visibility":"private"}`, id: assetID.String(), authenticated: true, serviceErr: media.ErrAssetNotFound, status: 404, calls: 1},
		{name: "busy", body: `{"visibility":"private"}`, id: assetID.String(), authenticated: true, serviceErr: media.ErrNotReady, status: 409, calls: 1},
		{name: "recorded failure", body: `{"visibility":"private"}`, id: assetID.String(), authenticated: true, serviceErr: media.ErrVisibilityPending, status: 503, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := &visibilityService{mediaService: mediaService{result: &media.Asset{ID: assetID, Visibility: media.Private, Status: media.AssetReady}, err: test.serviceErr, urlError: test.urlErr}}
			router := newCapabilityAPI(t)
			identityhttp.RegisterRoutes(nil, api.Authenticated(router, mediaAuth{userID}), identityhttp.Routes{Media: svc})
			request := httptest.NewRequest("PATCH", "/media/"+test.id+"/visibility", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.authenticated {
				request.Header.Set("Authorization", "Bearer test")
			}
			out := httptest.NewRecorder()
			router.ServeHTTP(out, request)
			if out.Code != test.status || svc.calls != test.calls {
				t.Fatalf("status=%d calls=%d body=%s", out.Code, svc.calls, out.Body.String())
			}
			if test.calls > 0 {
				var body struct {
					Visibility media.Visibility `json:"visibility"`
				}
				if err := json.Unmarshal([]byte(test.body), &body); err != nil {
					t.Fatal(err)
				}
				if svc.owner != (media.Owner{Type: "user", ID: userID.String()}) || svc.id != assetID || svc.visibility != body.Visibility {
					t.Fatal("request did not use the authenticated owner and requested visibility")
				}
			}
			if test.status == 200 {
				var body struct {
					Data identityhttp.AssetResponse `json:"data"`
				}
				if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Data.ID != assetID || body.Data.Visibility != svc.result.Visibility || body.Data.URL == "" {
					t.Fatal("missing updated asset representation", out.Body.String())
				}
			}
		})
	}
}
