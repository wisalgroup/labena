package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/auth"
)

type userService struct {
	service.UserService
	user  *entities.User
	err   error
	calls int
}

func (s *userService) Get(context.Context, uuid.UUID) (*entities.User, error) {
	s.calls++
	return s.user, s.err
}
func TestUserResponses(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		user                     *entities.User
		err                      error
		status                   int
		code                     string
	}{
		{name: "missing", method: "GET", path: "/users/00000000-0000-4000-8000-000000000001", err: domain.ErrUserNotFound, status: 404, code: "not_found"},
		{name: "unknown", method: "GET", path: "/users/00000000-0000-4000-8000-000000000001", err: errors.New("private database failure"), status: 500, code: "internal_error"},
		{name: "invalid UUID", method: "GET", path: "/users/not-a-uuid", status: 422, code: "validation_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &userService{user: tc.user, err: tc.err}
			r := newAPI(t)

			registerAccount(api.Authenticated(r, testAuth{}), svc)
			out := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer test")
			r.ServeHTTP(out, req)
			if out.Code != tc.status {
				t.Fatal(out.Code, out.Body.String())
			}
			var result struct {
				Data UserResponse `json:"data"`
				Type string       `json:"type"`
			}
			if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if tc.code != "" && result.Type != "urn:labena:error:"+tc.code {
				t.Fatal(out.Body.String())
			}
			if tc.user != nil && result.Data.ID != tc.user.ID {
				t.Fatal("lost committed user", out.Body.String())
			}

			if (tc.name == "nil request" || tc.name == "empty request") && svc.calls != 0 {
				t.Fatal("invalid request reached service")
			}
			if strings.Contains(out.Body.String(), "private") {
				t.Fatal("leaked diagnostic")
			}
		})
	}
}

type testAuth struct{}

func (testAuth) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{UserID: uuid.MustParse("00000000-0000-4000-8000-000000000001"), SessionID: uuid.New()}, nil
}

// Keep HTTP workflow coverage on the public service implementation.
type registrationRepo struct {
	repository.UserRepository
	user *entities.User
	err  error
}

func (r *registrationRepo) Create(_ context.Context, user *entities.User) error {
	if r.err != nil {
		return r.err
	}
	user.ID = r.user.ID
	return nil
}

func (r *registrationRepo) Edit(_ context.Context, id uuid.UUID, change func(*entities.User) error) (*entities.User, error) {
	if id != r.user.ID {
		return nil, domain.ErrUserNotFound
	}
	if err := change(r.user); err != nil {
		return nil, err
	}
	return r.user, nil
}

func TestProfileUpdate(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"name":"  Ada Lovelace  "}`, 200},
		{`{}`, 422}, {`{"name":null}`, 422}, {`{"name":"  "}`, 422},
		{`{"name":"` + strings.Repeat("a", 101) + `"}`, 422},
		{`{"name":"Ada","email":"other@example.com"}`, 422},
		{`{"id":"00000000-0000-4000-8000-000000000002"}`, 422},
	} {
		t.Run(tc.body, func(t *testing.T) {
			email, _ := valueobjects.NewEmail("ada@example.com")
			user, _ := entities.NewUser("Before", email)
			user.ID = uuid.MustParse("00000000-0000-4000-8000-000000000001")
			svc, err := service.NewUserService(&registrationRepo{user: user})
			if err != nil {
				t.Fatal(err)
			}
			router := newAPI(t)

			registerAccount(api.Authenticated(router, testAuth{}), svc)
			req := httptest.NewRequest("PATCH", "/users/me", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer test")
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != tc.status {
				t.Fatal(out.Code, out.Body.String())
			}
			if tc.status == 200 && (user.Name != "Ada Lovelace" || out.Header().Get("Cache-Control") != "no-store") {
				t.Fatal(user, out.Body.String())
			}
			if tc.status != 200 && user.Name != "Before" {
				t.Fatal("invalid request changed profile")
			}
			if user.Email.String() != "ada@example.com" {
				t.Fatal("contact changed")
			}
		})
	}
}
