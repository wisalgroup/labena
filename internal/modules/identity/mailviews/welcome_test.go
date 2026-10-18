package mailviews

import (
	"strings"
	"testing"

	"github.com/wisalgroup/labena/pkg/mailview"
)

func TestWelcomeUsesDefaultLayoutAndEscapesContent(t *testing.T) {
	renderer, err := mailview.New(mailview.Brand{Name: "Example Brand", WebsiteURL: "https://example.com", AccentColor: "#1D4ED8", SupportEmail: "support@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	content, err := renderer.Render(NewWelcome("<script>alert(1)</script>"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Example Brand", "#1D4ED8", "&lt;script&gt;", "support@example.com", "<!--[if mso]>"} {
		if !strings.Contains(content.HTML, want) {
			t.Fatal("default layout/content missing", want)
		}
	}
	if strings.Contains(content.HTML, "<script>") {
		t.Fatal("name was not escaped")
	}
	if !strings.Contains(content.Text, "Your account is ready") || content.Subject != "Welcome to your account" {
		t.Fatal("plain-text/subject missing")
	}
	if NewWelcome("  ").Title != "Welcome aboard." {
		t.Fatal("empty name greeting")
	}
}
