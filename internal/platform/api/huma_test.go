package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/auth"
	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/logging"
	"github.com/wisalgroup/labena/pkg/ratelimit"
)

type typedBody struct {
	Email string `json:"email" format:"email" maxLength:"254"`
	Code  string `json:"code" minLength:"6" maxLength:"6" pattern:"^[0-9]{6}$"`
}

func TestHumaValidationAndProblemDetails(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
		code                    string
		failure                 error
	}{
		{name: "valid", body: `{"email":"ada@example.com","code":"012345"}`, status: 200},
		{name: "missing", body: `{}`, status: 422, code: "validation_failed"},
		{name: "invalid", body: `{"email":"secret-not-email","code":"secret"}`, status: 422, code: "validation_failed"},
		{name: "unknown", body: `{"extra":"secret"}`, status: 422, code: "validation_failed"},
		{name: "duplicate", body: `{"email":"ada@example.com","email":"secret","code":"012345"}`, status: 400, code: "invalid_input"},
		{name: "case duplicate", body: `{"email":"ada@example.com","Email":"secret","code":"012345"}`, status: 400, code: "invalid_input"},
		{name: "trailing", body: `{} {}`, status: 400, code: "invalid_input"},
		{name: "array", body: `[]`, status: 422, code: "validation_failed"},
		{name: "null", body: `null`, status: 422, code: "validation_failed"},
		{name: "wrong type", body: `{"email":42}`, status: 422, code: "validation_failed"},
		{name: "oversized whitespace", body: `{}` + strings.Repeat(" ", 128), status: 413, code: "request_too_large"},
		{name: "unsupported media", body: `<secret/>`, contentType: "application/xml", status: 415, code: "unsupported_media_type"},
		{name: "safe business error", body: `{"email":"ada@example.com","code":"012345"}`, status: 409, code: "conflict", failure: apperror.Conflict("Already exists.")},
		{name: "private failure", body: `{"email":"ada@example.com","code":"012345"}`, status: 500, code: "internal_error", failure: errors.New("database password=secret")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newAPI(t)
			h := huma.NewGroup(r, "/api/v1")
			calls := 0
			huma.Register(h, huma.Operation{Method: "POST", Path: "/proof", MaxBodyBytes: 128}, func(ctx context.Context, input *struct{ Body typedBody }) (*struct{ Body string }, error) {
				calls++
				if tc.failure != nil {
					return nil, response.Error(ctx, tc.failure)
				}
				return &struct{ Body string }{Body: "accepted"}, nil
			})
			req := httptest.NewRequest("POST", "/api/v1/proof", strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			if out.Code != tc.status {
				t.Fatalf("got %d: %s", out.Code, out.Body.String())
			}
			contentType := "application/json"
			if tc.status >= 400 {
				contentType = "application/problem+json"
			}
			if out.Header().Get("Content-Type") != contentType {
				t.Fatal(out.Header())
			}
			if out.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request ID")
			}
			if tc.code != "" {
				var body huma.ErrorModel
				if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Type != "urn:labena:error:"+tc.code || body.Status != tc.status {
					t.Fatal(out.Body.String())
				}
				if tc.status == 422 && len(body.Errors) == 0 {
					t.Fatal("missing validation details")
				}
			}
			if tc.status >= 400 && strings.Contains(out.Body.String(), "secret") {
				t.Fatal("private input/error leaked")
			}
			wantCalls := 0
			if tc.status == 200 || tc.failure != nil {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("handler calls %d, want %d", calls, wantCalls)
			}
		})
	}
}

type rejectBody struct{ testing *testing.T }

func (b rejectBody) Read([]byte) (int, error) {
	b.testing.Error("body read before admission")
	return 0, io.EOF
}
func (rejectBody) Close() error { return nil }

type denyLimiter struct{}

func (denyLimiter) Allow(context.Context, string, ratelimit.Limit) (ratelimit.Decision, error) {
	return ratelimit.Decision{RetryAfter: time.Second}, nil
}

func TestHumaAdmissionPrecedesBodyParsing(t *testing.T) {
	for _, kind := range []string{"bearer", "rate limit"} {
		t.Run(kind, func(t *testing.T) {
			r := newAPI(t)
			public := r
			if kind == "rate limit" {
				public.UseMiddleware(api.RateLimit(denyLimiter{}, ratelimit.Limit{Rate: 1, Burst: 1, Period: time.Second}, api.PeerIPKey))
			}
			h := api.Authenticated(public, nil)
			huma.Register(h, huma.Operation{Method: "POST", Path: "/private"}, func(context.Context, *struct{ Body typedBody }) (*struct{}, error) {
				t.Fatal("unauthenticated handler invoked")
				return nil, nil
			})
			req := httptest.NewRequest("POST", "/private", rejectBody{t})
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			expected := 401
			if kind == "rate limit" {
				expected = 429
			}
			if out.Code != expected {
				t.Fatalf("got %d: %s", out.Code, out.Body.String())
			}
			if kind == "bearer" && out.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("missing auth challenge")
			}
			if kind == "rate limit" && out.Header().Get("Retry-After") != "1" {
				t.Fatal("missing retry header")
			}
		})
	}
	// A protected group with no authenticator fails closed.
	r := newAPI(t)
	h := api.Authenticated(r, nil)
	huma.Register(h, huma.Operation{Method: "GET", Path: "/private"}, func(context.Context, *struct{}) (*struct{}, error) { t.Fatal("handler invoked"); return nil, nil })
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest("GET", "/private", nil))
	if out.Code != 401 {
		t.Fatal(out.Code, out.Body.String())
	}
}

type typedAuth struct{ user, session uuid.UUID }

func (a typedAuth) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{UserID: a.user, SessionID: a.session}, nil
}

func TestHumaContextAndEmptyResponse(t *testing.T) {
	r := newAPI(t)
	identity := typedAuth{uuid.New(), uuid.New()}
	public := r
	protected := api.Authenticated(public, identity)
	group := huma.NewGroup(protected, "/nested")
	huma.Register(group, huma.Operation{Method: "DELETE", Path: "/resource", DefaultStatus: 204}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("request cancellation lost")
		}
		principal, err := api.Principal(ctx)
		if err != nil || principal.UserID != identity.user || principal.SessionID != identity.session {
			t.Fatal("principal lost", err)
		}
		return nil, nil
	})
	op := public.OpenAPI().Paths["/nested/resource"].Delete
	if len(op.Security) != 1 {
		t.Fatal("security declaration lost through groups")
	}
	if len(op.Responses["204"].Content) != 0 {
		t.Fatal("204 schema has a body")
	}
	req := httptest.NewRequest("DELETE", "/nested/resource", nil)
	req.Header.Set("Authorization", "Bearer test")
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	if out.Code != 204 || out.Body.Len() != 0 || out.Header().Get("X-Request-ID") == "" {
		t.Fatal(out.Code, out.Body.String(), out.Header())
	}
}

func TestHumaStreamAbortPreservesCleanupAndAccessLogging(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	r := newAPI(t)
	closed := false
	huma.Register(r, huma.Operation{Method: "GET", Path: "/stream"}, func(ctx context.Context, _ *struct{}) (*struct{ Body func(huma.Context) }, error) {
		return &struct{ Body func(huma.Context) }{Body: func(c huma.Context) {
			defer func() { closed = true }()
			c.SetHeader("Content-Type", "application/octet-stream")
			_, _ = c.BodyWriter().Write([]byte("partial"))
			c.BodyWriter().(http.Flusher).Flush()
			response.LogError(ctx, errors.New("private source failure"))
			panic(http.ErrAbortHandler)
		}}, nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, rq *http.Request) {
		r.ServeHTTP(w, rq.WithContext(logging.WithLogger(rq.Context(), logger)))
	}))
	defer server.Close()
	res, err := server.Client().Get(server.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.ReadAll(res.Body)
	_ = res.Body.Close()
	// Close waits for handlers to exit before reading their cleanup/logging state.
	server.Close()
	if _, err := uuid.Parse(res.Header.Get("X-Request-ID")); err != nil || res.Header.Get("X-API-Version") != "v1" {
		t.Fatal("stream lost response headers", res.Header)
	}
	if readErr == nil || !closed {
		t.Fatal("truncated stream looked complete or cleanup did not run", readErr, closed)
	}
	if strings.Count(logs.String(), `"msg":"HTTP operation failed"`) != 1 || strings.Count(logs.String(), `"msg":"HTTP request"`) != 1 || strings.Contains(logs.String(), "private source") {
		t.Fatal(logs.String())
	}
}

func TestNativeHumaOutputsAndErrors(t *testing.T) {
	r := newAPI(t)
	type output struct {
		Location string `header:"Location"`
		Body     []string
	}
	huma.Register(r, huma.Operation{Method: "POST", Path: "/items", DefaultStatus: 201}, func(ctx context.Context, in *struct{ Body []string }) (*output, error) {
		return &output{Location: "/items/1", Body: in.Body}, nil
	})
	huma.Register(r, huma.Operation{Method: "GET", Path: "/limited"}, func(context.Context, *struct{}) (*struct{}, error) {
		return nil, huma.ErrorWithHeaders(huma.Error429TooManyRequests("Try later", errors.New("private diagnostic")), http.Header{"Retry-After": {"7"}})
	})
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest("POST", "/items", strings.NewReader(`["first","second"]`)))
	if out.Code != 201 || out.Header().Get("Location") != "/items/1" || strings.TrimSpace(out.Body.String()) != `["first","second"]` {
		t.Fatal(out.Code, out.Header(), out.Body.String())
	}
	out = httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest("GET", "/limited", nil))
	var problem huma.ErrorModel
	if err := json.Unmarshal(out.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if out.Code != 429 || problem.Type != "urn:labena:error:rate_limited" || out.Header().Get("Retry-After") != "7" || strings.Contains(out.Body.String(), "private") {
		t.Fatal(out.Code, out.Header(), out.Body.String())
	}
}

func TestRequestEncodingAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, encoding string
		status                      int
	}{
		{name: "UTF8", contentType: "application/json; charset=utf-8", status: 204},
		{name: "structured JSON", contentType: "application/example+json", status: 204},
		{name: "identity", encoding: "identity", status: 204},
		{name: "gzip", encoding: "gzip", status: 415},
		{name: "unsupported charset", contentType: "application/json; charset=utf-16", status: 415},
		{name: "invalid charset", contentType: "application/json; charset=", status: 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newAPI(t)
			calls := 0
			huma.Register(server, huma.Operation{Method: "POST", Path: "/"}, func(context.Context, *struct{ Body typedBody }) (*struct{}, error) { calls++; return nil, nil })
			var body io.Reader = strings.NewReader(`{"email":"ada@example.com","code":"012345"}`)
			if tc.status == 415 {
				body = rejectBody{t}
			}
			req := httptest.NewRequest("POST", "/", body)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			if tc.encoding != "" {
				req.Header.Set("Content-Encoding", tc.encoding)
			}
			out := httptest.NewRecorder()
			server.ServeHTTP(out, req)
			if out.Code != tc.status || tc.status == 415 && calls != 0 {
				t.Fatal(out.Code, calls, out.Body.String())
			}
		})
	}
}

func TestServiceErrorConversionPreservesCauseAndSafeDetails(t *testing.T) {
	if response.Error(context.Background(), nil) != nil {
		t.Fatal("nil must remain nil")
	}
	cause := apperror.Conflict("That email is already registered.")
	converted := response.Error(context.Background(), cause)
	if !errors.Is(converted, cause) {
		t.Fatal("service error cause was lost")
	}
	server := newAPI(t)
	huma.Register(server, huma.Operation{Method: "GET", Path: "/conflict"}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		return nil, response.Error(ctx, cause)
	})
	out := httptest.NewRecorder()
	server.ServeHTTP(out, httptest.NewRequest("GET", "/conflict", nil))
	var body huma.ErrorModel
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if out.Code != 409 || body.Detail != cause.Message() || body.Type != "urn:labena:error:conflict" {
		t.Fatal(out.Code, out.Body.String())
	}
}
