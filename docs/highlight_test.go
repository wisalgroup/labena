//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
)

func TestArticleSectionsUseRenderedIDsAndPlainText(t *testing.T) {
	source := "# Queues\n\nFirst, create a task.\n\n## Retries & timeouts\n\nAn **attempt** can fail.\n\n### Use `asynq.MaxRetry`\n\n```go\nasynq.MaxRetry(3)\n```\n\n## Retries & timeouts\n\nAnother section.\n"
	var output bytes.Buffer
	md := goldmark.New(goldmark.WithParserOptions(parser.WithAutoHeadingID()))
	if err := md.Convert([]byte(source), &output); err != nil {
		t.Fatal(err)
	}
	rendered, headings, entries, err := prepareArticle(highlightGoBlocks(output.String()), page{Title: "Queues", Slug: "queues"}, "Background work")
	if err != nil {
		t.Fatal(err)
	}
	if len(headings) != 3 || len(entries) != 4 {
		t.Fatalf("headings=%+v entries=%+v", headings, entries)
	}
	if headings[0].ID == headings[2].ID || headings[1].Level != 3 {
		t.Fatalf("lost heading depth or duplicate IDs: %+v", headings)
	}
	if entries[0].URL != "queues.html" || entries[0].Text != "First, create a task." {
		t.Fatalf("incorrect page introduction: %+v", entries[0])
	}
	if entries[1].Text != "An attempt can fail." || entries[2].Text != "asynq.MaxRetry(3)" {
		t.Fatalf("search text contains markup or altered code: %+v", entries)
	}
	if entries[2].Parent != "Retries & timeouts" || entries[2].Heading != "Use asynq.MaxRetry" || entries[2].URL != "queues.html#"+headings[1].ID {
		t.Fatalf("missing section context or exact anchor: %+v", entries[2])
	}
	if strings.Count(rendered, `class="heading-link"`) != 3 || !strings.Contains(rendered, `aria-label="Link to Retries &amp; timeouts"`) {
		t.Fatalf("missing escaped, accessible heading links: %s", rendered)
	}
}

func TestArticlePreparationPreservesCopiedCode(t *testing.T) {
	source := "// Keep escaping and indentation.\nfunc sample() {\n\tvalue := `</code><script>&`\n\tprintln(\"a < b && b > c\", value)\n}\n"
	input := `<h2 id="example">Example</h2><pre><code class="language-go">` + html.EscapeString(source) + `</code></pre>`
	rendered, _, entries, err := prepareArticle(highlightGoBlocks(input), page{Title: "Example", Slug: "example"}, "Guides")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)<pre><code[^>]*>(.*?)</code></pre>`).FindStringSubmatch(rendered)
	if len(block) != 2 {
		t.Fatalf("code block missing: %s", rendered)
	}
	got := html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(block[1], ""))
	if got != source {
		t.Fatalf("copied code changed:\ngot  %q\nwant %q", got, source)
	}
	if len(entries) != 2 || !strings.Contains(entries[1].Text, "</code><script>&") {
		t.Fatalf("code search text was lost: %+v", entries)
	}
}

func TestHiddenPagesRemainPublishedWithoutNavigationOrSearch(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("docs", 0755); err != nil {
		t.Fatal(err)
	}
	navigation := []section{{Title: "Guides", Pages: []page{
		{Title: "First", Slug: "first", Source: "docs/first.md"},
		{Title: "Legacy", Slug: "legacy", Source: "docs/legacy.md", Hidden: true},
		{Title: "Last", Slug: "last", Source: "docs/last.md"},
	}}, {Title: "Hidden group", Pages: []page{{Title: "Other legacy", Slug: "other", Source: "docs/other.md", Hidden: true}}}}
	visible := visibleNavigation(navigation)
	if len(visible) != 1 || len(visible[0].Pages) != 2 || len(navigation[0].Pages) != 3 {
		t.Fatalf("incorrect filtering or source mutated: %+v", visible)
	}
	raw, err := json.Marshal(navigation)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"navigation.json": string(raw),
		"layout.html":     `{{range .Navigation}}{{.Title}}{{range .Pages}}<a href="{{.Slug}}.html">{{.Title}}</a>{{end}}{{end}}<article>{{.Content}}</article>{{with .Previous}}<a data-previous href="{{.Slug}}.html">Previous</a>{{end}}{{with .Next}}<a data-next href="{{.Slug}}.html">Next</a>{{end}}`,
		"first.md":        "# First\n\n## Start\n\nRead [legacy](legacy.md#old-anchor).\n",
		"legacy.md":       "# Legacy\n\n## Old anchor\n\nRead [current](first.md#start).\n",
		"last.md":         "# Last\n\nFinal guide.\n",
		"other.md":        "# Other legacy\n",
	} {
		if err := os.WriteFile(filepath.Join("docs", name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := build(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"legacy.html", "other.html", "markdown/legacy.md", "markdown/other.md"} {
		if _, err := os.Stat(filepath.Join("docs", name)); err != nil {
			t.Fatalf("legacy page not published: %v", err)
		}
	}
	first, err := os.ReadFile("docs/first.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), `data-next href="last.html"`) || strings.Contains(string(first), ">Legacy</a>") || strings.Contains(string(first), "Hidden group") {
		t.Fatalf("hidden page in reading order: %s", first)
	}
	search, err := os.ReadFile("docs/search-index.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(search), "Legacy") || strings.Contains(string(search), "Other legacy") || !strings.Contains(string(search), "first.html#start") {
		t.Fatalf("unexpected search publication: %s", search)
	}
}

func TestAssetVersionsUseCurrentSearchAndStableContent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("docs", 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"navigation.json": `[{"title":"Guides","pages":[{"title":"Start","slug":"index","source":"docs/start.md"}]}]`,
		"layout.html":     `<script src="{{asset "search-index.js"}}"></script><link rel="stylesheet" href="{{asset "style.css"}}"><article>{{.Content}}</article>`,
		"style.css":       "body { color: black; }",
		"start.md":        "# Start\n\n## First step\n\nInitial content.\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join("docs", name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	render := func() string {
		t.Helper()
		if err := build(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile("docs/index.html")
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	first := render()
	if repeated := render(); first != repeated {
		t.Fatal("unchanged content produced different assets")
	}
	version := regexp.MustCompile(`search-index\.js\?v=[a-f0-9]+`)
	old := version.FindString(first)
	if old == "" {
		t.Fatal("missing search version")
	}
	if err := os.WriteFile("docs/start.md", []byte("# Start\n\n## New step\n\nNew content.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	updated := render()
	if current := version.FindString(updated); current == "" || current == old {
		t.Fatal("changed search reused stale asset URL")
	}
	if render() != updated {
		t.Fatal("asset version depends on the previous build")
	}
}

func TestPublishedLinks(t *testing.T) {
	for _, tc := range []struct {
		name, link string
		valid      bool
	}{
		{"cross page", `other.html#known`, true},
		{"same page", `#here`, true},
		{"encoded anchor", `other.html#hello%20world`, true},
		{"asset and query", `api/schema.json?download=1&amp;format=json`, true},
		{"external", `https://example.com/not-checked#unknown`, true},
		{"mail", `mailto:support@example.com`, true},
		{"missing file", `missing.html`, false},
		{"missing anchor", `other.html#missing`, false},
		{"unpublished page", `layout.html`, false},
		{"directory", `api/`, false},
		{"outside site", `../outside.txt`, false},
		{"encoded outside site", `%2e%2e/outside.txt`, false},
		{"root relative", `/other.html`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.MkdirAll("docs/api", 0755); err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{
				"index.html":      `<h1 id="here">Start</h1><a href="` + tc.link + `">Link</a><img src="image.svg">`,
				"other.html":      `<h1 id="known">Other</h1><h2 id="hello world">Encoded</h2>`,
				"api/schema.json": `{}`,
				"image.svg":       `<svg/>`,
				"layout.html":     `<a href="{{.Slug}}.html">Template</a>`,
			} {
				if err := os.WriteFile(filepath.Join("docs", name), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			pages := []page{{Slug: "index"}, {Slug: "other"}}
			if err := checkLinks(pages); (err == nil) != tc.valid {
				t.Fatal("link validation:", err)
			}
			if tc.valid {
				if err := os.Remove("docs/image.svg"); err != nil {
					t.Fatal(err)
				}
				if err := checkLinks(pages); err == nil {
					t.Fatal("missing image accepted")
				}
			}
		})
	}
}

func TestHighlightPreservesSource(t *testing.T) {
	tags := regexp.MustCompile(`<[^>]*>`)
	for _, source := range []string{
		"package main\n\nfunc main() { println(\"<script>&\\\"\") }\n",
		"// comment\nvalue := `raw <&> string`\nif value != \"\" { call(value) }\n",
		"type Thing struct { Name string `json:\"name\"` }\n",
		"incomplete(\n", "// Windows\r\nvar x = `a\r\nb`\r\n",
	} {
		got := html.UnescapeString(tags.ReplaceAllString(highlightGo(source), ""))
		if got != source {
			t.Fatalf("source changed:\ngot  %q\nwant %q", got, source)
		}
	}
}

func TestWalkthroughKeepsReadableSteps(t *testing.T) {
	source := "> [!WALKTHROUGH] Commit & deliver\n>\n> Keep the boundary explicit.\n>\n> 1. **Save intent.** Persist the record.\n>\n>    No provider call yet.\n>\n> 2. **Deliver later.** A worker runs.\n"
	var output bytes.Buffer
	if err := goldmark.Convert([]byte(source), &output); err != nil {
		t.Fatal(err)
	}
	got := renderWalkthroughs(output.String())
	for _, want := range []string{`class="walkthrough"`, "<h3>Commit &amp; deliver</h3>", "<ol>", "<strong>Save intent.</strong>", "No provider call yet.", "<strong>Deliver later.</strong>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "[!WALKTHROUGH]") || strings.Contains(got, " hidden") {
		t.Fatalf("walkthrough is not readable without JavaScript: %s", got)
	}
}

func TestWalkthroughDoesNotChangeQuotesOrCode(t *testing.T) {
	for _, source := range []string{
		"> A normal quote.\n>\n> 1. A normal list.\n",
		"```markdown\n> [!WALKTHROUGH] Example\n>\n> 1. One\n> 2. Two\n```\n",
		"```mermaid\nflowchart LR\n A[Save & commit] --> B[Deliver]\n```\n",
	} {
		var output bytes.Buffer
		if err := goldmark.Convert([]byte(source), &output); err != nil {
			t.Fatal(err)
		}
		if got := renderWalkthroughs(output.String()); got != output.String() {
			t.Fatalf("unrelated Markdown changed: %s", got)
		}
	}
}

func TestGoTokenRoles(t *testing.T) {
	source := `package sample
import "fmt"
type Module struct { Name string }
func New(input *Module) error {
    fmt.Println(input.Name)
    if input == nil { return fmt.Errorf("missing") }
    return nil
}`
	got := highlightGo(source)
	for _, expected := range []string{
		`<span class="go-import-name">fmt</span>`,
		`<span class="go-type">Module</span>`,
		`<span class="go-function">New</span>`,
		`<span class="go-package">fmt</span>`,
		`<span class="go-function">Println</span>`,
		`<span class="go-parameter">input</span>`,
		`<span class="go-builtin-type">error</span>`,
		`<span class="go-operator">==</span>`,
		`<span class="go-bracket-1">{</span>`,
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("missing %s", expected)
		}
	}
	if strings.Contains(got, `<span class="go-type">Name</span>`) {
		t.Error("field name treated as a type")
	}
}

func TestGoHighlightScopesParameters(t *testing.T) {
	got := highlightGo("package sample\nfunc first(value string) { println(value) }\nfunc second() { value := 1; println(value) }")
	if count := strings.Count(got, `<span class="go-parameter">value</span>`); count != 2 {
		t.Fatalf("parameter coloring escaped its scope: %d occurrences", count)
	}
}

func TestBlocksDoNotEscapeTwice(t *testing.T) {
	got := highlightGoBlocks(`<pre><code class="language-go">s := &#34;&lt;&amp;&gt;&#34;
</code></pre>`)
	if !strings.Contains(got, `data-highlighted="yes"`) || strings.Contains(got, "&amp;lt;") {
		t.Fatalf("unexpected rendering: %s", got)
	}
}

func TestResolvePageLinks(t *testing.T) {
	destinations := map[string]string{"internal/platform/media/README.md": "media.html", "docs/modules.md": "modules.html"}
	for _, tc := range []struct{ input, want string }{
		{`<a href="../media/README.md#credentials">`, `<a href="media.html#credentials">`},
		{`<a href="../../../docs/modules.md?q=a&amp;b=c#example">`, `<a href="modules.html?q=a&amp;b=c#example">`},
		{`<a href="https://example.com/README.md">`, `<a href="https://example.com/README.md">`},
		{`<a href="#local">`, `<a href="#local">`},
		{`<a href="unknown.md">`, `<a href="unknown.md">`},
		{`<a href="/root.md">`, `<a href="/root.md">`},
		{`&lt;a href="../media/README.md"&gt;`, `&lt;a href="../media/README.md"&gt;`},
	} {
		if got := resolvePageLinks(tc.input, "internal/platform/notification/README.md", destinations); got != tc.want {
			t.Errorf("got %s, want %s", got, tc.want)
		}
	}
}

func TestResolveNestedGuideAssets(t *testing.T) {
	got := resolvePageLinks(`<a href="openapi.json?download=1#components">`, "docs/api/README.md", nil)
	if got != `<a href="api/openapi.json?download=1#components">` {
		t.Fatal(got)
	}
}
