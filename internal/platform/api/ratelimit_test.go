package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/wisalgroup/labena/internal/platform/api/middleware"
	"github.com/wisalgroup/labena/pkg/ratelimit"
)

type admissionStub struct {
	decision ratelimit.Decision
	err      error
	keys     []string
}

func (s *admissionStub) Allow(_ context.Context, key string, _ ratelimit.Limit) (ratelimit.Decision, error) {
	s.keys = append(s.keys, key)
	return s.decision, s.err
}

func TestRateLimitAdmission(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decision ratelimit.Decision
		err      error
		status   int
		retry    string
	}{
		{name: "allowed", decision: ratelimit.Decision{Allowed: true}, status: 204},
		{name: "denied", decision: ratelimit.Decision{RetryAfter: 1500 * time.Millisecond}, status: 429, retry: "2"},
		{name: "unavailable", err: errors.New("private backend detail"), status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &admissionStub{decision: tc.decision, err: tc.err}
			router := http.NewServeMux()
			h := humago.New(router, huma.DefaultConfig("test", "1"))
			h.UseMiddleware(middleware.RateLimit(stub, ratelimit.Limit{Rate: 1, Burst: 1, Period: time.Minute}, PeerIPKey))
			calls := 0
			huma.Register(h, huma.Operation{Method: "GET", Path: "/", DefaultStatus: 204}, func(context.Context, *struct{}) (*struct{}, error) { calls++; return nil, nil })
			for _, forwarded := range []string{"1.2.3.4", "5.6.7.8"} {
				request := httptest.NewRequest("GET", "/", nil)
				request.RemoteAddr = "192.0.2.1:1234"
				request.Header.Set("X-Forwarded-For", forwarded)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != tc.status || response.Header().Get("Retry-After") != tc.retry {
					t.Fatalf("status %d retry %q body %s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
				}
			}
			if stub.keys[0] != stub.keys[1] {
				t.Fatal("untrusted forwarding header changed budget")
			}
			if tc.status != 204 && calls != 0 {
				t.Fatal("denied request reached handler")
			}
		})
	}
}
