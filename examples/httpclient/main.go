// Run with go run ./examples/httpclient. The example starts a local provider;
// it needs no credentials, environment file or application infrastructure.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/wisalgroup/labena/examples/httpclient/catalog"
	"github.com/wisalgroup/labena/pkg/httpclient"
)

func requestID(request *http.Request, next httpclient.Next) (*http.Response, error) {
	request.Header.Set("X-Request-ID", "example-request")
	return next(request)
}

func run() error {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/products/123" || r.Header.Get("X-Request-ID") != "example-request" {
			http.Error(w, "unexpected request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(catalog.ProductResponse{ID: "123", Name: "Keyboard"})
	}))
	defer server.Close()
	client, err := catalog.New(server.URL+"/v1/", "")
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	// Embedded framework methods remain available without forwarding code.
	cfg := client.Config()
	cfg.Middleware = []httpclient.Middleware{requestID}
	if err := client.Configure(cfg); err != nil {
		return err
	}
	response, err := client.Do(context.Background(), catalog.GetProductRequest{ID: "123"})
	if err != nil {
		return err
	}
	if err := response.CheckStatus(); err != nil {
		return err
	}
	var result catalog.ProductResponse
	if err := response.DecodeJSON(&result); err != nil {
		return err
	}
	fmt.Printf("Product %s: %s (local sample data)\n", result.ID, result.Name)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
