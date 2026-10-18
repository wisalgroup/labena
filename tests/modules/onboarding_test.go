package modules_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/examples/module/catalog"
	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/dependencies"
)

func TestFirstModuleWorksWithoutInfrastructure(t *testing.T) {
	if err := catalog.Register(dependencies.Resources{}, dependencies.Targets{}); err != nil {
		t.Fatal(err)
	}
	router := newAPI(t)
	if err := catalog.Register(dependencies.Resources{}, dependencies.Targets{HTTP: huma.NewGroup(router, "/api/v1")}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/catalog/hello/Ada", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body response.Envelope[catalog.GreetingResponse]
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Message != "Hello, Ada" {
		t.Fatal(recorder.Body.String())
	}
}

// Keep the copyable guide tied to the source compiled by the normal Go suite.
func TestFirstModuleGuideMatchesCompiledExample(t *testing.T) {
	guide, err := os.ReadFile("../../docs/modules.md")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../examples/module/catalog/register.go")
	if err != nil {
		t.Fatal(err)
	}
	_, code, ok := strings.Cut(string(guide), "```go\n")
	if !ok {
		t.Fatal("first-module guide has no Go example")
	}
	code, _, ok = strings.Cut(code, "```")
	if !ok || strings.TrimSpace(code) != strings.TrimSpace(string(source)) {
		t.Fatal("update docs/modules.md and examples/module/catalog/register.go together")
	}
}

// Examples teach the shipped construction and job contracts. Keep complete
// snippets tied to compiled source, rather than checking only Go syntax.
func TestWorkflowGuidesMatchCompiledSources(t *testing.T) {
	for _, example := range []struct{ guide, source string }{
		{"entry-points.md", "cmd/app/main.go"},
		{"queues.md", "internal/modules/identity/jobs/welcome.go"},
		{"queues.md", "internal/modules/identity/delivery/jobs/welcome.go"},
		{"mail.md", "examples/mail/delivery/delivery.go"},
		{"transactional-notifications.md", "examples/module/profile/update.go"},
		{"onboarding.md", "internal/modules/identity/delivery/events/user_registered.go"},
		{"onboarding.md", "internal/modules/identity/notifications/welcome.go"},
		{"persistent-workflow.md", "internal/modules/identity/delivery/cli/commands.go"},
	} {
		t.Run(example.source, func(t *testing.T) {
			guide, err := os.ReadFile("../../docs/" + example.guide)
			if err != nil {
				t.Fatal(err)
			}
			source, err := os.ReadFile("../../" + example.source)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(guide), "```go\n"+strings.TrimSpace(string(source))+"\n```") {
				t.Fatalf("update %s alongside %s", example.guide, example.source)
			}
		})
	}
}

func TestAssemblyGuideMatchesCompiledSource(t *testing.T) {
	guide, err := os.ReadFile("../../docs/module-registration.md")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../internal/app/modules.go")
	if err != nil {
		t.Fatal(err)
	}
	_, code, ok := strings.Cut(string(guide), "```go\n")
	if !ok {
		t.Fatal("missing assembly example")
	}
	code, _, ok = strings.Cut(code, "\n```")
	if !ok || !strings.Contains(string(source), code) {
		t.Fatal("update assembly guide with app/modules.go")
	}
}
