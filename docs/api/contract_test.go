package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	identityhttp "github.com/wisalgroup/labena/internal/modules/identity/delivery/http"
	identityservice "github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/platform/api"
	mediahttp "github.com/wisalgroup/labena/internal/platform/media/integration"
)

type schema struct {
	Ref        string            `json:"$ref"`
	Type       string            `json:"type"`
	Format     string            `json:"format"`
	Nullable   bool              `json:"-"`
	Properties map[string]schema `json:"properties"`
	Required   []string          `json:"required"`
	AllOf      []schema          `json:"allOf"`
	Items      *schema           `json:"items"`
	MaxLength  int               `json:"maxLength"`
	Enum       []string          `json:"enum"`
}
type operation struct {
	RequestBody struct {
		Content map[string]struct {
			Schema schema `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
	ID         string                `json:"operationId"`
	Security   []map[string][]string `json:"security"`
	Parameters []struct {
		Name     string `json:"name"`
		In       string `json:"in"`
		Required bool   `json:"required"`
		Schema   schema `json:"schema"`
	} `json:"parameters"`
	Responses map[string]struct {
		Content map[string]struct {
			Schema schema `json:"schema"`
		} `json:"content"`
	} `json:"responses"`
}
type specification struct {
	OpenAPI string `json:"openapi"`
	Servers []struct {
		URL string `json:"url"`
	} `json:"servers"`
	Paths      map[string]map[string]operation `json:"paths"`
	Components struct {
		Schemas         map[string]schema `json:"schemas"`
		SecuritySchemes map[string]struct {
			Type   string `json:"type"`
			Scheme string `json:"scheme"`
		} `json:"securitySchemes"`
	} `json:"components"`
}

// OpenAPI 3.1 uses a type union to express nullable fields.
func (s *schema) UnmarshalJSON(data []byte) error {
	type plain schema
	var wire struct {
		Type json.RawMessage `json:"type"`
		*plain
	}
	wire.plain = (*plain)(s)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if len(wire.Type) == 0 {
		return nil
	}
	if wire.Type[0] != '[' {
		return json.Unmarshal(wire.Type, &s.Type)
	}
	var types []string
	if err := json.Unmarshal(wire.Type, &types); err != nil {
		return err
	}
	for _, typ := range types {
		if typ == "null" {
			s.Nullable = true
		} else {
			s.Type = typ
		}
	}
	return nil
}

func loadSpec(t *testing.T) specification {
	t.Helper()
	server := referenceServer(t)
	out := httptest.NewRecorder()
	server.ServeHTTP(out, httptest.NewRequest("GET", "/openapi.json", nil))
	if out.Code != 200 {
		t.Fatal(out.Code)
	}
	data := out.Body.Bytes()
	var spec specification
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAPI != "3.1.0" || spec.Paths["/api/v1/auth/otp/send"] == nil {
		t.Fatal("unexpected specification version/base path")
	}
	bearer := spec.Components.SecuritySchemes["Bearer"]
	if bearer.Type != "http" || bearer.Scheme != "bearer" {
		t.Fatal("bearer scheme does not match the middleware")
	}
	// Check every reference, including ones outside the representative response tests.
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatal(err)
	}
	var check func(any)
	check = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if ref, ok := node["$ref"].(string); ok {
				spec.resolve(t, schema{Ref: ref})
			}
			for _, child := range node {
				check(child)
			}
		case []any:
			for _, child := range node {
				check(child)
			}
		}
	}
	check(tree)
	return spec
}

type authStub struct{ identityservice.AuthService }
type assetsStub struct{ identityhttp.Assets }
type notificationsStub struct{ identityhttp.Notifications }

// Register the real module routes without constructing infrastructure. The
// embedded service interfaces panic if middleware lets a protected call through.
func referenceServer(t *testing.T) *api.Server {
	server := newAPI(t)
	public := huma.NewGroup(server, "/api/v1")
	protected := api.Authenticated(public, nil)
	identityhttp.RegisterRoutes(public, protected, identityhttp.Routes{Users: usersStub{}, Auth: authStub{}, SignIn: true, Social: true, Media: assetsStub{}, Notifications: notificationsStub{}})
	mediahttp.RegisterContentHTTP(public, nil)
	return server
}
func TestSpecificationMatchesRoutes(t *testing.T) {
	spec := loadSpec(t)
	server := referenceServer(t)
	ids := map[string]bool{}
	for path, methods := range spec.Paths {
		for method, op := range methods {
			if op.ID == "" || ids[op.ID] {
				t.Errorf("missing or duplicate operation ID %s", op.ID)
			}
			ids[op.ID] = true
			if len(op.Security) > 0 {
				if _, ok := op.Security[0]["Bearer"]; !ok {
					t.Errorf("unknown security for %s", path)
				}
				out := httptest.NewRecorder()
				server.ServeHTTP(out, httptest.NewRequest(strings.ToUpper(method), strings.ReplaceAll(path, "{id}", uuid.NewString()), nil))
				if out.Code != 401 {
					t.Errorf("%s %s bypassed authentication: %d", method, path, out.Code)
				}
			}
			for _, part := range strings.Split(path, "/") {
				if strings.HasPrefix(part, "{") {
					name := strings.Trim(part, "{}")
					found := false
					for _, param := range op.Parameters {
						if param.Name == name && param.In == "path" && param.Required {
							found = true
						}
					}
					if !found {
						t.Errorf("missing path parameter %s in %s", name, path)
					}
				}
			}
			success := false
			for code, response := range op.Responses {
				if strings.HasPrefix(code, "2") {
					success = true
				}
				if code == "204" && len(response.Content) != 0 {
					t.Errorf("204 body on %s", path)
				}
				for _, content := range response.Content {
					fields := spec.properties(t, content.Schema)
					for _, field := range []string{"request_id", "api_version"} {
						if _, exists := fields[field]; exists {
							t.Errorf("%s %s: header-only field %s remains in response schema %s", method, path, field, code)
						}
					}
				}
			}
			if !success {
				t.Errorf("missing success on %s", path)
			}
		}
	}
}
func TestLiveDocsConcurrentFirstRequests(t *testing.T) {
	server := referenceServer(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			out := httptest.NewRecorder()
			server.ServeHTTP(out, httptest.NewRequest("GET", "/openapi.json", nil))
			if out.Code != 200 || !json.Valid(out.Body.Bytes()) || !strings.Contains(out.Body.String(), "/api/v1/users") {
				t.Error("invalid live spec")
			}
		})
	}
	wg.Wait()
	out := httptest.NewRecorder()
	server.ServeHTTP(out, httptest.NewRequest("GET", "/docs", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), "./openapi.json") {
		t.Fatal("missing live docs")
	}
	disabled := newAPI(t, api.Options{DisableDocs: true})
	for _, path := range []string{"/docs", "/openapi.json"} {
		out := httptest.NewRecorder()
		disabled.ServeHTTP(out, httptest.NewRequest("GET", path, nil))
		if out.Code != 404 {
			t.Fatal("docs still exposed")
		}
	}
}

// Check high-value schema details that generators can otherwise misrepresent.
// This is not a second implementation of JSON Schema or request validation.
func TestWireSchemaDetails(t *testing.T) {
	spec := loadSpec(t)
	registration := spec.resolve(t, spec.Paths["/api/v1/auth/otp/send"]["post"].RequestBody.Content["application/json"].Schema)
	if !slices.Contains(registration.Required, "email") {
		t.Fatal("binding-required fields were lost")
	}
	if registration.Properties["email"].MaxLength != 254 {
		t.Fatal("email bound was lost")
	}
	signIn := spec.resolve(t, spec.Paths["/api/v1/auth/otp/verify"]["post"].RequestBody.Content["application/json"].Schema)
	if slices.Contains(signIn.Required, "name") {
		t.Fatal("sign-in must not require a name")
	}
	for _, path := range []string{"/api/v1/users", "/api/v1/auth/email/verification-code", "/api/v1/auth/email/verify"} {
		if spec.Paths[path]["post"].ID != "" {
			t.Fatal("retired route in OpenAPI", path)
		}
	}
	if spec.Components.Schemas["UserResponse"].Properties["profile_complete"].Type != "boolean" {
		t.Fatal("missing profile completion flag")
	}
	platform := spec.resolve(t, signIn.Properties["platform"])
	if !slices.Equal(platform.Enum, []string{"ios", "android", "web"}) {
		t.Fatalf("platform values: %v", platform.Enum)
	}
	for _, name := range []string{"UserResponse", "AssetResponse", "NotificationResponse", "SessionResponse"} {
		fields := spec.Components.Schemas[name].Properties
		if fields["id"].Type != "string" || fields["id"].Format != "uuid" {
			t.Errorf("%s: UUID must be a JSON string", name)
		}
		if fields["created_at"].Format != "date-time" {
			t.Errorf("%s: missing timestamp format", name)
		}
	}
	if !spec.Components.Schemas["UserResponse"].Properties["email_verified_at"].Nullable {
		t.Fatal("unverified contact must allow null")
	}
	for name, field := range map[string]string{"SessionResponse": "expires_at", "TokensResponse": "refresh_expires_at"} {
		property := spec.Components.Schemas[name].Properties[field]
		if !property.Nullable || property.Type != "string" || property.Format != "date-time" {
			t.Fatalf("%s.%s must allow a null expiry", name, field)
		}
	}
	if !spec.Components.Schemas["NotificationResponse"].Properties["read_at"].Nullable {
		t.Fatal("unread timestamp must allow null")
	}
	notificationFields := spec.Components.Schemas["NotificationResponse"].Properties
	if notificationFields["type"].Type != "string" || notificationFields["data"].Type != "object" {
		t.Fatal("notification type and JSON object payload are required")
	}
	for _, legacy := range []string{"kind", "body"} {
		if _, exists := notificationFields[legacy]; exists {
			t.Fatal("text-only notification field remains", legacy)
		}
	}
	if notificationFields["recipient_id"].Type != "string" || notificationFields["recipient_type"].Type != "string" {
		t.Fatal("polymorphic recipient fields missing")
	}
	if _, exists := notificationFields["user_id"]; exists {
		t.Fatal("user-only recipient leaked")
	}
	fields := spec.properties(t, spec.Paths["/api/v1/auth/sessions"]["get"].Responses["200"].Content["application/json"].Schema)
	page := spec.properties(t, fields["data"])
	items := page["items"]
	if items.Type != "array" || items.Items == nil || items.Items.Ref != "#/components/schemas/SessionResponse" {
		t.Fatalf("session page lost item type: %+v", items)
	}
	for _, s := range spec.Components.Schemas {
		for _, private := range []string{"password_hash", "refresh_token_hash", "code_digest", "provider_subject"} {
			if _, ok := s.Properties[private]; ok {
				t.Errorf("private field leaked into schemas: %s", private)
			}
		}
	}
}

type usersStub struct{ identityservice.UserService }

func (authStub) SignInWithCode(_ context.Context, input identityservice.SignInInput) (*identityservice.SignInResult, error) {
	return &identityservice.SignInResult{Tokens: &identityservice.Tokens{}, User: &entities.User{}, Created: input.Proof.Code == "123456"}, nil
}
func (usersStub) Get(context.Context, uuid.UUID) (*entities.User, error) { panic("unexpected read") }

// Exercise the real decoding, handler, body and error renderer. Business
// persistence is intentionally outside this HTTP contract test.
func TestSignInResponsesMatchSpecification(t *testing.T) {
	spec := loadSpec(t)
	server := referenceServer(t)
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"challenge_id":"` + uuid.NewString() + `","code":"123456","platform":"web"}`, 201},
		{`{"challenge_id":"` + uuid.NewString() + `","code":"654321","platform":"web"}`, 200},
		{`{"email":"invalid","name":""}`, 422},
		{`{"unexpected":true}`, 422},
		{`{}` + strings.Repeat(" ", 4096), 413},
	} {
		out := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/auth/otp/verify", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		server.ServeHTTP(out, req)
		if out.Code != tc.status {
			t.Fatalf("got %d want %d: %s", out.Code, tc.status, out.Body.String())
		}
		contentType := out.Header().Get("Content-Type")
		responses := spec.Paths["/api/v1/auth/otp/verify"]["post"].Responses
		documented, ok := responses[strconv.Itoa(tc.status)]
		if !ok {
			documented, ok = responses["default"]
		}
		if !ok {
			t.Fatalf("undocumented status %d", tc.status)
		}
		fields := spec.properties(t, documented.Content[contentType].Schema)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for name := range body {
			if _, ok := fields[name]; !ok {
				t.Errorf("undocumented response property %s", name)
			}
		}
		if _, err := uuid.Parse(out.Header().Get("X-Request-ID")); err != nil {
			t.Fatal("missing server-generated request ID", err)
		}
		if out.Header().Get("X-API-Version") != "v1" {
			t.Fatal("missing API version header", out.Header())
		}
		for _, field := range []string{"request_id", "api_version"} {
			if _, exists := body[field]; exists {
				t.Errorf("header-only field %s remains in the response body", field)
			}
		}
		if tc.status < 300 && body["data"] == nil || tc.status >= 400 && body["data"] != nil {
			t.Fatal("success and problem body contracts changed", out.Body.String())
		}
		if tc.status == 422 {
			var problem huma.ErrorModel
			if err := json.Unmarshal(out.Body.Bytes(), &problem); err != nil || len(problem.Errors) == 0 {
				t.Fatal("missing validation details", err)
			}
		}
	}
}
func (s specification) resolve(t *testing.T, v schema) schema {
	t.Helper()
	if v.Ref == "" {
		return v
	}
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(v.Ref, prefix) {
		t.Fatalf("unsupported reference %s", v.Ref)
	}
	result, ok := s.Components.Schemas[strings.TrimPrefix(v.Ref, prefix)]
	if !ok {
		t.Fatalf("unresolved reference %s", v.Ref)
	}
	return result
}
func (s specification) properties(t *testing.T, v schema) map[string]schema {
	t.Helper()
	v = s.resolve(t, v)
	result := map[string]schema{}
	for name, value := range v.Properties {
		result[name] = value
	}
	for _, part := range v.AllOf {
		for name, value := range s.properties(t, part) {
			result[name] = value
		}
	}
	return result
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
