package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/pkg/apperror"
)

func TestNoStoreScopeAndRejection(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "1"))
	private := huma.NewGroup(api, "/private")
	private.UseMiddleware(NoStore)
	handler := func(context.Context, *struct{}) (*struct{}, error) { return nil, nil }
	huma.Register(api, huma.Operation{Method: "GET", Path: "/public", DefaultStatus: 204}, handler)
	huma.Register(private, huma.Operation{Method: "GET", Path: "/ok", DefaultStatus: 204}, handler)
	denied := huma.NewGroup(private, "/denied")
	denied.UseMiddleware(func(ctx huma.Context, _ func(huma.Context)) {
		response.WriteError(ctx, apperror.Forbidden("Denied."))
	})
	huma.Register(denied, huma.Operation{Method: "GET", Path: ""}, func(context.Context, *struct{}) (*struct{}, error) {
		t.Fatal("rejected request reached handler")
		return nil, nil
	})
	for _, tc := range []struct {
		path, cache string
		status      int
	}{
		{"/public", "", 204}, {"/private/ok", "no-store", 204}, {"/private/denied", "no-store", 403},
	} {
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, httptest.NewRequest("GET", tc.path, nil))
		if out.Code != tc.status || out.Header().Get("Cache-Control") != tc.cache {
			t.Fatal(tc.path, out.Code, out.Header(), out.Body.String())
		}
	}
}
