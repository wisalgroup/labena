package adminhttp_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wisalgroup/labena/internal/platform/adminhttp"
)

func TestProtection(t *testing.T) {
	for _, tc := range []struct {
		name, method, origin, site string
		auth, tls                  bool
		status                     int
	}{
		{name: "anonymous", method: "GET", status: 401},
		{name: "native client", method: "POST", auth: true, status: 204},
		{name: "same origin", method: "POST", origin: "http://app.example", site: "same-origin", auth: true, status: 204},
		{name: "TLS termination", method: "POST", origin: "https://app.example", site: "same-origin", auth: true, status: 204},
		{name: "foreign form", method: "POST", origin: "https://attacker.example", site: "cross-site", auth: true, status: 403},
		{name: "older browser", method: "POST", origin: "https://attacker.example", auth: true, status: 403},
		{name: "foreign read", method: "GET", origin: "https://attacker.example", auth: true, status: 403},
		{name: "foreign preflight", method: "OPTIONS", origin: "https://attacker.example", auth: true, status: 403},
		{name: "same site subdomain", method: "GET", site: "same-site", auth: true, status: 403},
		{name: "null origin", method: "POST", origin: "null", auth: true, status: 403},
		{name: "malformed origin", method: "POST", origin: "https://app.example?", auth: true, status: 403},
		{name: "HTTP to TLS origin", method: "GET", origin: "http://app.example", tls: true, auth: true, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			handler, err := adminhttp.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }), adminhttp.Config{Realm: "Test", Username: "operator", Password: "test-only"})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(tc.method, "http://app.example/admin/", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			if tc.auth {
				r.SetBasicAuth("operator", "test-only")
			}
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			w := httptest.NewRecorder()
			w.Header().Set("Access-Control-Allow-Origin", "https://attacker.example")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			handler.ServeHTTP(w, r)
			if w.Code != tc.status || (calls == 1) != (tc.status == 204) {
				t.Fatalf("status=%d calls=%d", w.Code, calls)
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Fatal(w.Header())
			}
			if tc.status == 401 && w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing Basic Auth challenge")
			}
		})
	}
}

func TestCredentialsFailClosed(t *testing.T) {
	handler, err := adminhttp.Protect(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), adminhttp.Config{Username: "operator", Password: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	for _, credentials := range [][2]string{{"wrong", "test-only"}, {"operator", "wrong"}, {"", ""}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetBasicAuth(credentials[0], credentials[1])
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
}
