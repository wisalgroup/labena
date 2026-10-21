package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/logging"
)

type responseItem struct {
	ID int `json:"id"`
}

func TestResponseHelpersAndSchemas(t *testing.T) {
	for _, tc := range []struct {
		status int
		output *response.JSON[responseItem]
	}{
		{200, response.OK(responseItem{ID: 7})},
		{201, response.Created(responseItem{ID: 7})},
		{202, response.Accepted(responseItem{ID: 7}).WithMeta(map[string]any{"queued": true})},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			server := newAPI(t, api.Options{Version: "2.3.4", APIVersion: "v2"})
			huma.Register(server, huma.Operation{Method: "POST", Path: "/items", DefaultStatus: tc.status},
				func(context.Context, *struct{}) (*response.JSON[responseItem], error) { return tc.output, nil })
			out := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/items", nil)
			req.Header.Set("X-Request-ID", "untrusted-client-id")
			server.ServeHTTP(out, req)
			if out.Code != tc.status || out.Header().Get("Content-Type") != "application/json" {
				t.Fatal(out.Code, out.Header(), out.Body.String())
			}
			var body response.Envelope[responseItem]
			if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil || body.Data.ID != 7 {
				t.Fatal(err, out.Body.String())
			}
			assertResponseHeaders(t, out, "v2")
			assertNoBodyMetadata(t, out)
			if (body.Meta != nil) != (tc.status == 202) {
				t.Fatal("optional metadata presence", out.Body.String())
			}
			op := server.OpenAPI().Paths["/items"].Post
			schema := resolveResponseSchema(server, op.Responses[fmt.Sprint(tc.status)].Content["application/json"].Schema)
			if !slices.Equal(schema.Required, []string{"data"}) {
				t.Errorf("required fields: %v", schema.Required)
			}
			for _, field := range []string{"request_id", "api_version"} {
				if _, exists := schema.Properties[field]; exists {
					t.Errorf("header-only field %s remains in the body schema", field)
				}
			}
			if slices.Contains(schema.Required, "meta") || schema.Properties["meta"].Type != "object" {
				t.Fatal("meta must be an optional JSON object")
			}
			data := resolveResponseSchema(server, schema.Properties["data"])
			if data.Properties["id"].Type != "integer" {
				t.Fatal("data lost its concrete schema")
			}
		})
	}
	server := newAPI(t)
	huma.Register(server, huma.Operation{Method: "DELETE", Path: "/items/7"},
		func(context.Context, *struct{}) (*response.Empty, error) { return response.NoContent(), nil })
	out := httptest.NewRecorder()
	server.ServeHTTP(out, httptest.NewRequest("DELETE", "/items/7", nil))
	if out.Code != 204 || out.Body.Len() != 0 || out.Header().Get("Content-Type") != "" {
		t.Fatal("204 must be bodyless", out.Code, out.Header(), out.Body.String())
	}
	assertResponseHeaders(t, out, "v1")
	if len(server.OpenAPI().Paths["/items/7"].Delete.Responses["204"].Content) != 0 {
		t.Fatal("204 must not document a body")
	}
}

func TestApplicationValidationMatchesHumaDetails(t *testing.T) {
	cause := apperror.InvalidFields(map[string][]string{
		"email":        {"Use your work email.", "This address is not eligible."},
		"profile.name": {"Choose a display name."},
		"query.page":   {"This page is unavailable."},
	})
	server := newAPI(t)
	huma.Register(server, huma.Operation{Method: "POST", Path: "/validate"},
		func(ctx context.Context, _ *struct{ Body typedBody }) (*response.Empty, error) {
			converted := response.Error(ctx, fmt.Errorf("private operation: %w", cause))
			if !errors.Is(converted, cause) {
				t.Error("conversion lost the original error")
			}
			return nil, converted
		})
	for _, input := range []string{`{"email":"ada@example.com","code":"012345"}`, `{"email":"private-input","code":"x"}`} {
		out := httptest.NewRecorder()
		server.ServeHTTP(out, httptest.NewRequest("POST", "/validate", strings.NewReader(input)))
		var body response.Problem
		if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if out.Code != 422 || body.Status != 422 || body.Type != "urn:labena:error:validation_failed" || len(body.Errors) < 2 {
			t.Fatal(out.Code, out.Body.String())
		}
		assertResponseHeaders(t, out, "v1")
		assertNoBodyMetadata(t, out)
		if strings.Contains(out.Body.String(), "private") || strings.Contains(out.Body.String(), `"value"`) {
			t.Fatal("validation leaked diagnostic/input", out.Body.String())
		}
		if strings.Contains(input, "ada@example.com") {
			if len(body.Errors) != 4 || body.Detail != cause.Message() {
				t.Fatal("lost application validation messages", out.Body.String())
			}
			for i, location := range []string{"body.email", "body.email", "body.profile.name", "query.page"} {
				if body.Errors[i].Location != location {
					t.Fatalf("location %d: %s", i, body.Errors[i].Location)
				}
			}
			if body.Errors[0].Message != "Use your work email." || body.Errors[1].Message != "This address is not eligible." {
				t.Fatal("safe application messages lost")
			}
		}
	}
}

func TestProblemHeadersAcrossFailurePaths(t *testing.T) {
	server := newAPI(t)
	huma.Register(server, huma.Operation{Method: "GET", Path: "/panic"},
		func(context.Context, *struct{}) (*response.Empty, error) { panic("private panic") })
	huma.Register(server, huma.Operation{Method: "GET", Path: "/native"},
		func(context.Context, *struct{}) (*response.Empty, error) {
			return nil, huma.ErrorWithHeaders(huma.Error429TooManyRequests("private diagnostic"), http.Header{"Retry-After": {"7"}})
		})
	group := huma.NewGroup(server, "/middleware")
	group.UseMiddleware(func(ctx huma.Context, _ func(huma.Context)) {
		response.WriteError(ctx, apperror.Forbidden("Access denied."))
	})
	huma.Register(group, huma.Operation{Method: "GET", Path: "/deny"},
		func(context.Context, *struct{}) (*response.Empty, error) {
			t.Error("denied handler ran")
			return nil, nil
		})
	for path, status := range map[string]int{"/panic": 500, "/native": 429, "/middleware/deny": 403} {
		out := httptest.NewRecorder()
		server.ServeHTTP(out, httptest.NewRequest("GET", path, nil))
		var body response.Problem
		if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil || out.Code != status || body.Status != status {
			t.Fatal(path, err, out.Code, out.Body.String())
		}
		assertResponseHeaders(t, out, "v1")
		assertNoBodyMetadata(t, out)
		if out.Header().Get("Content-Type") != "application/problem+json" || strings.Contains(out.Body.String(), "private") {
			t.Fatal(out.Header(), out.Body.String())
		}
		if path == "/native" && out.Header().Get("Retry-After") != "7" {
			t.Fatal("native error headers were lost")
		}
	}
}

func TestSharedResponsesKeepRequestHeadersIsolated(t *testing.T) {
	meta := map[string]any{"cached": true}
	shared := response.OK(responseItem{ID: 1}).WithMeta(meta)
	meta["cached"] = false
	var wg sync.WaitGroup
	ids := sync.Map{}
	for _, version := range []string{"v1", "v2"} {
		server := newAPI(t, api.Options{APIVersion: version})
		huma.Register(server, huma.Operation{Method: "GET", Path: "/shared"},
			func(context.Context, *struct{}) (*response.JSON[responseItem], error) { return shared, nil })
		for range 12 {
			wg.Go(func() {
				out := httptest.NewRecorder()
				server.ServeHTTP(out, httptest.NewRequest("GET", "/shared", nil))
				var body response.Envelope[responseItem]
				if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
					t.Error(err)
					return
				}
				assertResponseHeaders(t, out, version)
				assertNoBodyMetadata(t, out)
				if _, loaded := ids.LoadOrStore(out.Header().Get("X-Request-ID"), true); loaded || body.Meta["cached"] != true {
					t.Error("request ID reused or operation metadata changed")
				}
			})
		}
	}
	wg.Wait()
	if shared.Body.Data.ID != 1 || shared.Body.Meta["cached"] != true {
		t.Fatal("shared output mutated")
	}
}

func TestRequestCorrelationMatchesHeaderAndLog(t *testing.T) {
	if response.RequestID(context.Background()) != "" {
		t.Fatal("request ID present outside a request")
	}
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			server := newAPI(t)
			var id string
			huma.Register(server, huma.Operation{Method: "GET", Path: "/correlation"},
				func(ctx context.Context, _ *struct{}) (*response.JSON[responseItem], error) {
					id = response.RequestID(ctx)
					logging.FromContext(ctx).InfoContext(ctx, "handler")
					if fail {
						return nil, response.Error(ctx, errors.New("private failure"))
					}
					return response.OK(responseItem{ID: 1}), nil
				})
			request := httptest.NewRequest("GET", "/correlation", nil)
			request.Header.Set("X-Request-ID", "untrusted-client-id")
			out := httptest.NewRecorder()
			server.ServeHTTP(out, request.WithContext(logging.WithLogger(request.Context(), logger)))
			assertResponseHeaders(t, out, "v1")
			assertNoBodyMetadata(t, out)
			if id != out.Header().Get("X-Request-ID") {
				t.Fatal("handler context lost request correlation")
			}
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			wantLogs := 2 // Handler and access log, plus the operational error on failure.
			if fail {
				wantLogs++
			}
			if len(lines) != wantLogs {
				t.Fatal("unexpected logs", logs.String())
			}
			for _, line := range lines {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil || record["request_id"] != id {
					t.Fatal("log lost request correlation", err, line)
				}
			}
		})
	}
}

func TestJSONRejectsNonBodySuccessStatuses(t *testing.T) {
	for _, status := range []int{0, 100, 204, 205, 302, 400, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid JSON success status accepted")
				}
			}()
			response.NewJSON("result", status)
		})
	}
}

func assertResponseHeaders(t *testing.T, out *httptest.ResponseRecorder, version string) {
	t.Helper()
	if _, err := uuid.Parse(out.Header().Get("X-Request-ID")); err != nil {
		t.Errorf("missing server-generated request ID: %v", out.Header())
	}
	if out.Header().Get("X-API-Version") != version {
		t.Errorf("API version mismatch: %v", out.Header())
	}
}

func assertNoBodyMetadata(t *testing.T, out *httptest.ResponseRecorder) {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"request_id", "api_version"} {
		if _, exists := body[field]; exists {
			t.Errorf("header-only field %s remains in the body", field)
		}
	}
}

func resolveResponseSchema(server *api.Server, schema *huma.Schema) *huma.Schema {
	if schema.Ref != "" {
		return server.OpenAPI().Components.Schemas.Map()[strings.TrimPrefix(schema.Ref, "#/components/schemas/")]
	}
	return schema
}
