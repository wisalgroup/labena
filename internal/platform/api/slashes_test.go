package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestTrailingSlashCanonicalization(t *testing.T) {
	server := newAPI(t)
	huma.Register(server, huma.Operation{Method: "POST", Path: "/items/{id}", DefaultStatus: 201}, func(_ context.Context, in *struct {
		ID   string `path:"id"`
		Body struct {
			Name string `json:"name"`
		}
	}) (*struct{ Body string }, error) {
		return &struct{ Body string }{Body: in.ID + ":" + in.Body.Name}, nil
	})
	server.Mux.HandleFunc("GET /files/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	server.Mux.HandleFunc("GET /explicit/{$}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, tc := range []struct {
		method, path string
		status       int
		location     string
	}{
		{"GET", "/docs/", 308, "/docs"},
		{"GET", "/openapi.json/?download=true", 308, "/openapi.json?download=true"},
		{"POST", "/items/one/?source=a%2Fb", 308, "/items/one?source=a%2Fb"},
		{"POST", "/items/one%2Ftwo/", 308, "/items/one%2Ftwo"},
		{"GET", "/unknown/", 404, ""},
		{"POST", "/items/one%2F", 400, ""},
		{"GET", "/files/", 204, ""},
		{"GET", "/explicit/", 204, ""},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			out := httptest.NewRecorder()
			server.ServeHTTP(out, httptest.NewRequest(tc.method, tc.path, nil))
			if out.Code != tc.status || out.Header().Get("Location") != tc.location {
				t.Fatalf("status=%d location=%q body=%s", out.Code, out.Header().Get("Location"), out.Body.String())
			}
		})
	}
	// Exercise the client redirect as well: POST and its body must survive.
	live := httptest.NewServer(server)
	defer live.Close()
	res, err := live.Client().Post(live.URL+"/items/one%2Ftwo/", "application/json", strings.NewReader(`{"name":"Ada"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != 201 || strings.TrimSpace(string(body)) != `"one/two:Ada"` {
		t.Fatalf("status=%d body=%s err=%v", res.StatusCode, body, err)
	}
}
