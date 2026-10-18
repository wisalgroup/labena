package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestTypedParameterSources(t *testing.T) {
	type input struct {
		Name   string `path:"name" minLength:"1"`
		Query  string `query:"query" required:"true"`
		Header string `header:"X-Name" required:"true"`
		Body   struct {
			Name string `json:"name"`
		}
	}
	r := newAPI(t)
	huma.Register(r, huma.Operation{Method: "POST", Path: "/{name}"}, func(_ context.Context, in *input) (*struct{}, error) {
		if in.Name != "path" || in.Query != "query" || in.Header != "header" || in.Body.Name != "body" {
			t.Fatalf("sources mixed: %+v", in)
		}
		return nil, nil
	})
	req := httptest.NewRequest("POST", "/path?query=query", strings.NewReader(`{"name":"body"}`))
	req.Header.Set("X-Name", "header")
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	if out.Code != 204 {
		t.Fatal(out.Code, out.Body.String())
	}
}

func TestPaginationBoundary(t *testing.T) {
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"", 204}, {"page=2&per_page=5", 204}, {"page=0", 422}, {"page=1&page=2", 422}, {"per_page=", 422}, {"per_page=101", 422}, {"page=wrong", 422}, {"page=99999999999999999999999", 422}, {"page=1;ignored=2", 422},
	} {
		r := newAPI(t)
		huma.Register(r, huma.Operation{Method: "GET", Path: "/"}, func(_ context.Context, in *PaginationInput) (*struct{}, error) {
			if tc.status != 204 {
				t.Fatal("invalid pagination reached handler")
			}
			p := in.Pagination()
			if p.Page < 1 || p.PerPage < 1 {
				t.Fatal(p)
			}
			return nil, nil
		})
		out := httptest.NewRecorder()
		r.ServeHTTP(out, httptest.NewRequest("GET", "/?"+tc.query, nil))
		if out.Code != tc.status {
			t.Fatal(tc.query, out.Code, out.Body.String())
		}
	}
}

func newAPI(t testing.TB, options ...Options) *Server {
	t.Helper()
	var opts Options
	if len(options) > 0 {
		opts = options[0]
	}
	server, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}
