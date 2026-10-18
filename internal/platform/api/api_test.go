package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

type request struct {
	Email string `json:"email" format:"email"`
	Name  string `json:"name" minLength:"1" maxLength:"100"`
}

func TestJSONValidationAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"invalid UTF-8", "{\"email\":\"\xff\",\"name\":\"User\"}", 400},
		{"valid", `{"email":"user@example.com","name":"User"}`, 200},
		{"missing", `{}`, 422}, {"null", `null`, 422}, {"array", `[]`, 422},
		{"empty", ``, 400}, {"syntax", `{`, 400}, {"unknown", `{"password":"secret"}`, 422},
		{"trailing", `{} {}`, 400}, {"trailing garbage", `{} abc`, 400},
		{"wrong type", `{"email":17}`, 422}, {"invalid fields", `{"email":"broken","name":""}`, 422},
		{"large", `{"email":"` + strings.Repeat("x", 300) + `"}`, 413},
		{"large whitespace", `{}` + strings.Repeat(" ", 300), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newAPI(t)

			huma.Register(r, huma.Operation{Method: "POST", Path: "/", MaxBodyBytes: 256}, func(ctx context.Context, in *struct{ Body request }) (*struct{ Body request }, error) {
				return &struct{ Body request }{Body: in.Body}, nil
			})
			out := httptest.NewRecorder()
			r.ServeHTTP(out, httptest.NewRequest("POST", "/", strings.NewReader(tc.body)))
			if out.Code != tc.status {
				t.Fatal(out.Code, out.Body.String())
			}
			var result huma.ErrorModel
			if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if tc.status == 422 && len(result.Errors) == 0 {
				t.Fatal("missing field arrays", out.Body.String())
			}
			if strings.Contains(out.Body.String(), "secret") {
				t.Fatal("echoed input")
			}
		})
	}
}

func TestNestedValidationUsesJSONPaths(t *testing.T) {
	type item struct {
		DisplayName string `json:"display_name" minLength:"1"`
	}
	type input struct {
		Items []item `json:"items"`
	}
	r := newAPI(t)
	huma.Register(r, huma.Operation{Method: "POST", Path: "/"}, func(context.Context, *struct{ Body input }) (*struct{}, error) {
		t.Fatal("invalid nested body reached handler")
		return nil, nil
	})
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest("POST", "/", strings.NewReader(`{"items":[{}]}`)))
	if out.Code != 422 || !strings.Contains(out.Body.String(), "body.items[0]") {
		t.Fatal(out.Code, out.Body.String())
	}
}
