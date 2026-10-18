package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/platform/api"
)

type verifierStub struct {
	service.AuthService
	calls int
	ip    string
	err   error
}

func (v *verifierStub) RequestSignInCode(_ context.Context, _, ip string) (uuid.UUID, error) {
	v.calls++
	v.ip = ip
	return uuid.New(), v.err
}
func (v *verifierStub) SignInWithCode(_ context.Context, input service.SignInInput) (*service.SignInResult, error) {
	v.calls++
	v.ip = input.Proof.ClientIP
	if v.err != nil {
		return nil, v.err
	}
	return &service.SignInResult{Tokens: &service.Tokens{}, User: &entities.User{}}, nil
}
func TestOTPHTTP(t *testing.T) {
	for _, tc := range []struct {
		path, body string
		err        error
		status     int
	}{
		{"send", `{"email":"person@example.com"}`, nil, 202},
		{"send", `{"email":"invalid"}`, nil, 422},
		{"send", `{"email":"person@example.com"}`, domain.ErrVerificationLimited, 429},
		{"verify", `{"challenge_id":"` + uuid.NewString() + `","code":"123456","platform":"web"}`, nil, 200},
		{"verify", `{"challenge_id":"` + uuid.NewString() + `","code":"123456","platform":"web"}`, domain.ErrVerificationInvalid, 422},
		{"verify", `{"challenge_id":"invalid","code":"123456","platform":"web"}`, nil, 422},
	} {
		t.Run(tc.path+tc.body, func(t *testing.T) {
			svc := &verifierStub{err: tc.err}
			router := newAPI(t)

			registerAuth(router, nil, svc, false)
			req := httptest.NewRequest("POST", "/auth/otp/"+tc.path, strings.NewReader(tc.body))
			req.RemoteAddr = "192.0.2.1:1234"
			req.Header.Set("X-Forwarded-For", "192.0.2.200")
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != tc.status || out.Header().Get("Cache-Control") != "no-store" || strings.Contains(out.Body.String(), "123456") {
				t.Fatal(out.Code, out.Body.String())
			}
			if svc.calls > 0 && svc.ip != "192.0.2.1" {
				t.Fatal("trusted spoofed header", svc.ip)
			}
		})
	}
}

func newAPI(t testing.TB, options ...api.Options) *api.Server {
	t.Helper()
	var opts api.Options
	if len(options) > 0 {
		opts = options[0]
	}
	server, err := api.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}
