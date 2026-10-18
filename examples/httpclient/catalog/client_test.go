package catalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/wisalgroup/labena/examples/httpclient/catalog"
	"github.com/wisalgroup/labena/pkg/httpclient"
)

func TestProductOperations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer example-token" || r.Header.Get("Accept") != "application/json" || r.Header.Get("X-Request-ID") != "example-request" {
			t.Error("missing integration headers or middleware")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/v1/products/a%2Fb":
			if !reflect.DeepEqual(r.URL.Query()["fields"], []string{"id", "name"}) {
				t.Errorf("fields = %v", r.URL.Query())
			}
			_ = json.NewEncoder(w).Encode(catalog.ProductResponse{ID: "a/b", Name: "Keyboard"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/products":
			var body catalog.CreateProductBody
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("content type = %s", r.Header.Get("Content-Type"))
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name != "Mouse" {
				t.Errorf("body = %+v, err = %v", body, err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(catalog.ProductResponse{ID: "456", Name: body.Name})
		default:
			http.Error(w, `{"message":"unknown product"}`, http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := catalog.New(server.URL+"/v1", "example-token")
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	cfg := client.Config()
	cfg.Middleware = []httpclient.Middleware{func(r *http.Request, next httpclient.Next) (*http.Response, error) {
		r.Header.Set("X-Request-ID", "example-request")
		return next(r)
	}}
	if err := client.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		request any
		status  int
		want    catalog.ProductResponse
	}{
		{"get", catalog.GetProductRequest{ID: "a/b", Fields: []string{"id", "name"}}, http.StatusOK, catalog.ProductResponse{ID: "a/b", Name: "Keyboard"}},
		{"create", catalog.CreateProductRequest{Body: catalog.CreateProductBody{Name: "Mouse"}}, http.StatusCreated, catalog.ProductResponse{ID: "456", Name: "Mouse"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := client.Do(context.Background(), tc.request)
			if err != nil {
				t.Fatal(err)
			}
			if err := response.CheckStatus(); err != nil || response.StatusCode != tc.status {
				t.Fatalf("status = %d, err = %v", response.StatusCode, err)
			}
			var product catalog.ProductResponse
			if err := response.DecodeJSON(&product); err != nil || product != tc.want {
				t.Fatalf("product = %+v, err = %v", product, err)
			}
		})
	}
	response, err := client.Do(context.Background(), catalog.GetProductRequest{ID: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	var status *httpclient.StatusError
	if !errors.As(response.CheckStatus(), &status) || status.Response.StatusCode != http.StatusNotFound {
		t.Fatal("missing product should retain its HTTP response")
	}
}
