package http

import (
	"github.com/danielgtaylor/huma/v2"
	"net/http/httptest"
	"testing"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/platform/api"
)

func TestRouteGroups(t *testing.T) {
	router := newAPI(t)
	protected := api.Authenticated(router, nil)
	RegisterRoutes(router, protected, Routes{Users: &userService{}, Auth: authServiceStub{}, SignIn: true, Social: true})
	routes := make(map[string]bool)
	for path, item := range router.OpenAPI().Paths {
		for method, op := range map[string]*huma.Operation{"GET": item.Get, "POST": item.Post, "PATCH": item.Patch, "DELETE": item.Delete} {
			if op != nil {
				routes[method+" "+path] = true
			}
		}
	}
	for _, route := range []struct{ method, path string }{
		{"GET", "/users/me"}, {"PATCH", "/users/me"}, {"GET", "/users/{id}"},
		{"POST", "/auth/logout"},
		{"GET", "/auth/sessions"}, {"DELETE", "/auth/sessions"}, {"DELETE", "/auth/sessions/{id}"},
	} {
		if !routes[route.method+" "+route.path] {
			t.Fatalf("missing route %v", route)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
		if response.Code != 401 {
			t.Fatalf("%v bypassed authentication: %d", route, response.Code)
		}
	}
	for _, route := range []string{"POST /auth/otp/send", "POST /auth/otp/verify", "POST /auth/refresh", "POST /auth/social/{provider}"} {
		if !routes[route] {
			t.Fatalf("missing public route %s", route)
		}
	}
}

func TestOptionalAuthRoutes(t *testing.T) {
	router := newAPI(t)
	RegisterRoutes(router, nil, Routes{Users: &userService{}})
	routes := router.OpenAPI().Paths
	if len(routes) != 0 {
		t.Fatalf("unexpected routes %v", routes)
	}
	// A transport-free process can register without creating an HTTP router.
	RegisterRoutes(nil, nil, Routes{})
}

// Embedded interfaces panic if a protected handler reaches a service in these
// routing tests; authentication middleware must stop the request first.
type authServiceStub struct{ service.AuthService }

func TestRetiredAuthRoutesAreAbsent(t *testing.T) {
	router := newAPI(t)
	RegisterRoutes(router, api.Authenticated(router, nil), Routes{Users: &userService{}, Auth: authServiceStub{}, SignIn: true, Social: true})
	for _, path := range []string{"/users", "/auth/email/verification-code", "/auth/email/verify", "/auth/login", "/auth/register", "/auth/register/verification-code", "/auth/forgot-password", "/auth/reset-password", "/auth/password", "/auth/password/set", "/auth/password/verification-code"} {
		for _, method := range []string{"POST", "PUT"} {
			out := httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest(method, path, nil))
			if out.Code != 404 {
				t.Errorf("removed route %s %s returned %d", method, path, out.Code)
			}
		}
	}
}
