package app_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/cors"

	"github.com/wisalgroup/labena/internal/app/config"
)

func TestCORSPreflight(t *testing.T) {
	const customHeaders = "authorization,content-type,x-client-version,x-platform,x-tenant-id"
	for _, test := range []struct {
		name           string
		method         string
		requestHeaders string
		allowedOrigins []string
		allowedHeaders []string
		credentials    bool
		allowed        bool
	}{
		{name: "load with custom headers", method: "GET", requestHeaders: customHeaders, allowed: true},
		{name: "save with custom headers", method: "PATCH", requestHeaders: customHeaders, allowed: true},
		{name: "credentialed custom headers", method: "PATCH", requestHeaders: customHeaders, allowedOrigins: []string{"https://client.example"}, credentials: true, allowed: true},
		{name: "explicit header allowlist", method: "PATCH", requestHeaders: "authorization,x-client-version", allowedHeaders: []string{"Authorization", "X-Client-Version"}, allowed: true},
		{name: "unlisted header", method: "PATCH", requestHeaders: customHeaders, allowedHeaders: []string{"Authorization", "Content-Type"}},
		{name: "unlisted origin", method: "PATCH", requestHeaders: customHeaders, allowedOrigins: []string{"https://other.example"}, credentials: true},
		{name: "unlisted method", method: "TRACE", requestHeaders: customHeaders},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Defaults().HTTP.CORS
			if test.allowedOrigins != nil {
				cfg.AllowOrigins = test.allowedOrigins
			}
			if test.allowedHeaders != nil {
				cfg.AllowHeaders = test.allowedHeaders
			}
			cfg.AllowCredentials = test.credentials
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
			handler := cors.New(cfg.Options()).Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("preflight reached application handler")
			}))
			request := httptest.NewRequest(http.MethodOptions, "/api/v1/users", nil)
			request.Header.Set("Origin", "https://client.example")
			request.Header.Set("Access-Control-Request-Method", test.method)
			request.Header.Set("Access-Control-Request-Headers", test.requestHeaders)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("preflight status = %d", response.Code)
			}
			if !test.allowed {
				for _, name := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Headers", "Access-Control-Allow-Credentials"} {
					if got := response.Header().Get(name); got != "" {
						t.Fatalf("rejected preflight included %s: %s", name, got)
					}
				}
				return
			}
			origin := "*"
			if test.allowedOrigins != nil {
				origin = "https://client.example"
			}
			credentials := ""
			if test.credentials {
				credentials = "true"
			}
			for name, want := range map[string]string{
				"Access-Control-Allow-Origin":      origin,
				"Access-Control-Allow-Methods":     test.method,
				"Access-Control-Allow-Headers":     test.requestHeaders,
				"Access-Control-Allow-Credentials": credentials,
			} {
				if got := response.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}
