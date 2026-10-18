package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/platform/auth"
)

type authenticatorStub struct {
	calls     int
	err       error
	principal auth.Principal
}

func (s *authenticatorStub) Authenticate(context.Context, string) (auth.Principal, error) {
	s.calls++
	return s.principal, s.err
}
func TestBearerBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		duplicate    bool
		err          error
		want, calls  int
	}{
		{name: "missing", want: 401}, {name: "wrong scheme", header: "Basic secret", want: 401},
		{name: "duplicate", header: "Bearer token", duplicate: true, want: 401},
		{name: "success", header: "Bearer token", want: 204, calls: 1},
		{name: "outage", header: "Bearer token", err: errors.New("private SQL"), want: 500, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &authenticatorStub{err: tc.err, principal: auth.Principal{UserID: uuid.New(), SessionID: uuid.New()}}
			r := http.NewServeMux()
			h := humago.New(r, huma.DefaultConfig("test", "1"))
			h.UseMiddleware(Bearer(stub))
			huma.Register(h, huma.Operation{Method: "GET", Path: "/private", DefaultStatus: 204}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
				id, err := CurrentUserID(ctx)
				if err != nil || id != stub.principal.UserID {
					t.Error(err)
				}
				return nil, nil
			})
			req := httptest.NewRequest("GET", "/private?access_token=forged", nil)
			req.Header.Set("X-User-ID", uuid.NewString())
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			if tc.duplicate {
				req.Header.Add("Authorization", tc.header)
			}
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			if out.Code != tc.want || stub.calls != tc.calls {
				t.Fatal(out.Code, stub.calls, out.Body.String())
			}
		})
	}
}
