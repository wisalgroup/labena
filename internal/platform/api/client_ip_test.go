package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/internal/platform/api/middleware"
	"github.com/wisalgroup/labena/pkg/ratelimit"
)

func TestTrustedProxyClientIP(t *testing.T) {
	for _, tc := range []struct {
		name, remote, forwarded, want string
		proxies                       []string
		duplicate                     bool
	}{
		{name: "default ignores forwarding", remote: "192.0.2.1:80", forwarded: "198.51.100.2", want: "192.0.2.1"},
		{name: "untrusted ignores malformed forwarding", remote: "192.0.2.1:80", forwarded: "bad", proxies: []string{"10.0.0.1/32"}, want: "192.0.2.1"},
		{name: "trusted ingress", remote: "10.0.0.1:80", forwarded: "198.51.100.2", proxies: []string{"10.0.0.1/32"}, want: "198.51.100.2"},
		{name: "trusted chain ignores spoofed prefix", remote: "10.0.0.1:80", forwarded: "203.0.113.99, 198.51.100.2, 10.0.0.2", proxies: []string{"10.0.0.0/24"}, want: "198.51.100.2"},
		{name: "untrusted intermediate", remote: "10.0.0.1:80", forwarded: "203.0.113.99, 192.0.2.20", proxies: []string{"10.0.0.1/32"}, want: "192.0.2.20"},
		{name: "IPv6 chain", remote: "[2001:db8::1]:80", forwarded: "2001:db8:1::2", proxies: []string{"2001:db8::1/128"}, want: "2001:db8:1::2"},
		{name: "IPv4 mapped peer", remote: "[::ffff:10.0.0.1]:80", forwarded: "::ffff:198.51.100.2", proxies: []string{"10.0.0.1/32"}, want: "198.51.100.2"},
		{name: "missing forwarding", remote: "10.0.0.1:80", proxies: []string{"10.0.0.1/32"}, want: "10.0.0.1"},
		{name: "malformed trusted forwarding", remote: "10.0.0.1:80", forwarded: "198.51.100.2, bad", proxies: []string{"10.0.0.1/32"}},
		{name: "duplicate trusted forwarding", remote: "10.0.0.1:80", forwarded: "198.51.100.2", duplicate: true, proxies: []string{"10.0.0.1/32"}},
		{name: "excessive chain", remote: "10.0.0.1:80", forwarded: strings.Repeat("198.51.100.2,", 32) + "198.51.100.2", proxies: []string{"10.0.0.1/32"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefixes, err := ParseTrustedProxies(tc.proxies)
			if err != nil {
				t.Fatal(err)
			}
			server, err := New(Options{TrustedProxies: prefixes})
			if err != nil {
				t.Fatal(err)
			}
			called := false
			server.Mux.HandleFunc("GET /client", func(w http.ResponseWriter, r *http.Request) {
				called = true
				client, err := ClientIP(r.Context())
				if err != nil || client != tc.want {
					t.Errorf("client=%q err=%v, want %q", client, err, tc.want)
				}
				peer, err := PeerIP(r.Context())
				if err != nil || peer == "" {
					t.Errorf("missing original peer: %v", err)
				}
				w.WriteHeader(204)
			})
			r := httptest.NewRequest("GET", "/client", nil)
			r.RemoteAddr = tc.remote
			if tc.forwarded != "" {
				r.Header.Set("X-Forwarded-For", tc.forwarded)
			}
			if tc.duplicate {
				r.Header.Add("X-Forwarded-For", tc.forwarded)
			}
			r.Header.Set("X-Real-IP", "203.0.113.250")
			r.Header.Set("Forwarded", "for=203.0.113.250")
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			if tc.want == "" {
				if w.Code != 400 || called {
					t.Fatalf("malformed trusted metadata reached handler: %d", w.Code)
				}
			} else if w.Code != 204 || !called {
				t.Fatalf("status=%d called=%v", w.Code, called)
			}
		})
	}
}

func TestClientIPRateLimitUsesAcceptedChain(t *testing.T) {
	prefixes, err := ParseTrustedProxies([]string{"10.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{TrustedProxies: prefixes})
	if err != nil {
		t.Fatal(err)
	}
	limiter := &admissionStub{decision: ratelimit.Decision{Allowed: true}}
	server.UseMiddleware(middleware.RateLimit(limiter, ratelimit.Limit{Rate: 1, Burst: 1, Period: time.Minute}, ClientIPKey))
	huma.Register(server, huma.Operation{Method: "GET", Path: "/", DefaultStatus: 204}, func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
	for _, chain := range []string{"198.51.100.1", "203.0.113.99, 198.51.100.1", "198.51.100.2"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "10.0.0.1:80"
		r.Header.Set("X-Forwarded-For", chain)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if len(limiter.keys) != 3 || limiter.keys[0] != limiter.keys[1] || limiter.keys[0] == limiter.keys[2] {
		t.Fatal("incorrect client budgets", limiter.keys)
	}
}
