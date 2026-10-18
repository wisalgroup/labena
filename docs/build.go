//go:build tools

// The tools tag keeps the Markdown dependency visible to go mod tidy.
// Build the documentation from the repository root: go run docs/build.go.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/scanner"
	"go/token"
	"html"
	"html/template"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	htmlparser "golang.org/x/net/html"
)

type page struct {
	Title, Slug, Source string
	// Hidden pages preserve published URLs without competing with their replacement.
	Hidden bool
}
type section struct {
	Title string
	Pages []page
}
type view struct {
	Page           page
	Section        string
	Navigation     []section
	Content        template.HTML
	Headings       []heading
	Previous, Next *page
}
type heading struct {
	Title, ID string
	Level     int
}
type searchEntry struct{ Title, Section, Heading, Parent, URL, Text string }

func main() {
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build() error {
	raw, err := os.ReadFile("docs/navigation.json")
	if err != nil {
		return err
	}
	var navigation []section
	if err := json.Unmarshal(raw, &navigation); err != nil {
		return err
	}
	assets := map[string]string{}
	layout, err := template.New("layout.html").Funcs(template.FuncMap{
		"asset": func(name string) (string, error) {
			if versioned, ok := assets[name]; ok {
				return versioned, nil
			}
			data, err := os.ReadFile(filepath.Join("docs", name))
			if err != nil {
				return "", err
			}
			digest := sha256.Sum256(data)
			versioned := fmt.Sprintf("%s?v=%x", name, digest[:8])
			assets[name] = versioned
			return versioned, nil
		},
	}).ParseFiles("docs/layout.html")
	if err != nil {
		return err
	}
	md := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithAutoHeadingID()))
	slugs := map[string]bool{}
	destinations := map[string]string{}
	var pages []page
	for _, section := range navigation {
		for _, p := range section.Pages {
			if p.Slug == "" || filepath.Base(p.Slug) != p.Slug || slugs[p.Slug] {
				return fmt.Errorf("invalid or duplicate page slug: %q", p.Slug)
			}
			slugs[p.Slug] = true
			destinations[filepath.Clean(p.Source)] = p.Slug + ".html"
			pages = append(pages, p)
		}
	}
	visible := visibleNavigation(navigation)
	var readingOrder []page
	for _, section := range visible {
		readingOrder = append(readingOrder, section.Pages...)
	}
	if err := os.MkdirAll("docs/markdown", 0755); err != nil {
		return err
	}
	var search []searchEntry
	var views []view
	index := 0
	for _, section := range navigation {
		for _, p := range section.Pages {
			source, err := os.ReadFile(p.Source)
			if err != nil {
				return fmt.Errorf("%s: %w", p.Source, err)
			}
			var content bytes.Buffer
			if err := md.Convert(source, &content); err != nil {
				return err
			}
			// Only repository-authored Markdown is rendered as HTML.
			rendered, headings, entries, err := prepareArticle(renderWalkthroughs(highlightGoBlocks(resolvePageLinks(content.String(), p.Source, destinations))), p, section.Title)
			if err != nil {
				return fmt.Errorf("%s: %w", p.Source, err)
			}
			v := view{Page: p, Section: section.Title, Navigation: visible, Content: template.HTML(rendered), Headings: headings}
			if !p.Hidden {
				if index > 0 {
					v.Previous = &readingOrder[index-1]
				}
				if index+1 < len(readingOrder) {
					v.Next = &readingOrder[index+1]
				}
				search = append(search, entries...)
				index++
			}
			views = append(views, v)
			if err := os.WriteFile("docs/markdown/"+p.Slug+".md", source, 0644); err != nil {
				return err
			}
		}
	}
	data, err := json.Marshal(search)
	if err != nil {
		return err
	}
	if err := os.WriteFile("docs/search-index.js", append([]byte("window.docsSearch = "), append(data, ';')...), 0644); err != nil {
		return err
	}
	// Publish the index before hashing assets, so every page uses this build's
	// search data. Content hashes keep static browser caches fresh and reproducible.
	for _, v := range views {
		var output bytes.Buffer
		if err := layout.Execute(&output, v); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join("docs", v.Page.Slug+".html"), output.Bytes(), 0644); err != nil {
			return err
		}
	}
	if err := checkLinks(pages); err != nil {
		return err
	}
	fmt.Printf("Built %d documentation pages.\n", len(pages))
	return nil
}

func visibleNavigation(navigation []section) []section {
	var visible []section
	for _, group := range navigation {
		filtered := section{Title: group.Title}
		for _, p := range group.Pages {
			if !p.Hidden {
				filtered.Pages = append(filtered.Pages, p)
			}
		}
		if len(filtered.Pages) > 0 {
			visible = append(visible, filtered)
		}
	}
	return visible
}

// Derive section search and outlines from the same rendered headings as links.
// IDs remain Goldmark's IDs (including duplicate-heading suffixes). Extract text
// before adding permalinks so navigation labels never leak into search excerpts.
func prepareArticle(rendered string, p page, section string) (string, []heading, []searchEntry, error) {
	document, err := htmlparser.Parse(strings.NewReader(rendered))
	if err != nil {
		return "", nil, nil, err
	}
	var body *htmlparser.Node
	var findBody func(*htmlparser.Node)
	findBody = func(node *htmlparser.Node) {
		if node.Type == htmlparser.ElementNode && node.Data == "body" {
			body = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			findBody(child)
		}
	}
	findBody(document)
	if body == nil {
		return "", nil, nil, fmt.Errorf("rendered article has no body")
	}
	var headings []heading
	var entries []searchEntry
	entry := searchEntry{Title: p.Title, Section: section, URL: p.Slug + ".html"}
	var text strings.Builder
	parent := ""
	flush := func() {
		entry.Text = strings.Join(strings.Fields(text.String()), " ")
		entries = append(entries, entry)
		text.Reset()
	}
	var collect func(*htmlparser.Node)
	collect = func(node *htmlparser.Node) {
		if node.Type == htmlparser.TextNode {
			text.WriteString(node.Data)
			return
		}
		if node.Type != htmlparser.ElementNode {
			return
		}
		if node.Data == "h1" {
			return
		}
		if node.Data == "h2" || node.Data == "h3" {
			id := ""
			for _, attr := range node.Attr {
				if attr.Key == "id" {
					id = attr.Val
				}
			}
			if id != "" {
				var label strings.Builder
				var labelText func(*htmlparser.Node)
				labelText = func(n *htmlparser.Node) {
					if n.Type == htmlparser.TextNode {
						label.WriteString(n.Data)
					}
					for child := n.FirstChild; child != nil; child = child.NextSibling {
						labelText(child)
					}
				}
				labelText(node)
				title := strings.Join(strings.Fields(label.String()), " ")
				level := 2
				flush()
				entry = searchEntry{Title: p.Title, Section: section, Heading: title, URL: p.Slug + ".html#" + url.PathEscape(id)}
				if node.Data == "h3" {
					level = 3
					entry.Parent = parent
				} else {
					parent = title
				}
				headings = append(headings, heading{Title: title, ID: id, Level: level})
				link := &htmlparser.Node{Type: htmlparser.ElementNode, Data: "a", Attr: []htmlparser.Attribute{
					{Key: "class", Val: "heading-link"}, {Key: "href", Val: "#" + url.PathEscape(id)},
					{Key: "aria-label", Val: "Link to " + title},
				}}
				link.AppendChild(&htmlparser.Node{Type: htmlparser.TextNode, Data: "#"})
				node.AppendChild(link)
				return
			}
		}
		// Preserve token adjacency inside syntax spans, but separate block content.
		block := strings.Contains(" p div section pre blockquote li ul ol table tr td th hr br ", " "+node.Data+" ")
		if block {
			text.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
		if block {
			text.WriteByte(' ')
		}
	}
	for node := body.FirstChild; node != nil; node = node.NextSibling {
		collect(node)
	}
	flush()
	var output strings.Builder
	for node := body.FirstChild; node != nil; node = node.NextSibling {
		if err := htmlparser.Render(&output, node); err != nil {
			return "", nil, nil, err
		}
	}
	return output.String(), headings, entries, nil
}

// Check the published pages, not the layout template or historical Markdown.
// This runs after rendering so cross-page anchors and nested assets are checked
// exactly as a static host sees them, without contacting external sites.
func checkLinks(pages []page) error {
	ids := make(map[string]map[string]bool)
	type reference struct{ source, href string }
	var references []reference
	for _, p := range pages {
		filename := filepath.Join("docs", p.Slug+".html")
		content, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		ids[filename] = make(map[string]bool)
		tokens := htmlparser.NewTokenizer(bytes.NewReader(content))
		for {
			kind := tokens.Next()
			if kind == htmlparser.ErrorToken {
				if err := tokens.Err(); err != io.EOF {
					return fmt.Errorf("%s: %w", filename, err)
				}
				break
			}
			if kind != htmlparser.StartTagToken && kind != htmlparser.SelfClosingTagToken {
				continue
			}
			token := tokens.Token()
			for _, attr := range token.Attr {
				if attr.Key == "id" || (token.Data == "a" && attr.Key == "name") {
					ids[filename][attr.Val] = true
				}
				if attr.Key == "href" || attr.Key == "src" {
					references = append(references, reference{filename, attr.Val})
				}
			}
		}
	}
	for _, ref := range references {
		u, err := url.Parse(ref.href)
		if err != nil {
			return fmt.Errorf("%s: invalid link %q: %w", ref.source, ref.href, err)
		}
		if u.IsAbs() || u.Host != "" {
			continue
		}
		target := ref.source
		if u.Path != "" {
			target = filepath.Join(filepath.Dir(ref.source), filepath.FromSlash(u.Path))
		}
		relative, err := filepath.Rel("docs", target)
		if err != nil || strings.HasPrefix(u.Path, "/") || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s: link %q leaves the published site", ref.source, ref.href)
		}
		info, err := os.Stat(target)
		if err != nil || info.IsDir() {
			return fmt.Errorf("%s: missing link target %q", ref.source, ref.href)
		}
		if filepath.Ext(target) == ".html" && (ids[target] == nil || (u.Fragment != "" && !ids[target][u.Fragment])) {
			return fmt.Errorf("%s: unpublished page or missing anchor %q", ref.source, ref.href)
		}
	}
	return nil
}

// Registered Markdown sources can live outside docs. Resolve their links to
// published pages while leaving source READMEs and external URLs unchanged.
var pageLink = regexp.MustCompile(`<a href="([^"]*)"`)

func resolvePageLinks(rendered, source string, destinations map[string]string) string {
	return pageLink.ReplaceAllStringFunc(rendered, func(link string) string {
		href := html.UnescapeString(pageLink.FindStringSubmatch(link)[1])
		u, err := url.Parse(href)
		if err != nil || u.IsAbs() || u.Host != "" || u.Path == "" || strings.HasPrefix(u.Path, "/") {
			return link
		}
		target := filepath.Clean(filepath.Join(filepath.Dir(source), filepath.FromSlash(u.Path)))
		destination, ok := destinations[target]
		if !ok {
			// Nested guide sources can link downloadable assets inside docs (for
			// example api/openapi.json). Published HTML lives at the docs root.
			if !strings.HasPrefix(filepath.ToSlash(target), "docs/") || filepath.Ext(target) == ".md" || filepath.Ext(target) == ".html" {
				return link
			}
			destination = strings.TrimPrefix(filepath.ToSlash(target), "docs/")
		}
		u.Path, u.RawPath = destination, ""
		return `<a href="` + html.EscapeString(u.String()) + `"`
	})
}

// Walkthroughs are ordinary Markdown quotes and ordered lists, so downloads and
// browsers without JavaScript retain the complete explanation. Only transform
// Goldmark's escaped output; source HTML remains disabled.
var walkthroughBlock = regexp.MustCompile(`(?s)<blockquote>\n<p>\[!WALKTHROUGH\] ([^\n]+)</p>\n(.*?)</blockquote>`)

func renderWalkthroughs(rendered string) string {
	return walkthroughBlock.ReplaceAllString(rendered, `<section class="walkthrough" aria-label="Step-by-step explanation"><h3>$1</h3>$2</section>`)
}

var goBlock = regexp.MustCompile(`(?s)<pre><code class="language-go">(.*?)</code></pre>`)

func highlightGoBlocks(rendered string) string {
	return goBlock.ReplaceAllStringFunc(rendered, func(block string) string {
		source := html.UnescapeString(goBlock.FindStringSubmatch(block)[1])
		return `<pre><code class="language-go" data-highlighted="yes">` + highlightGo(source) + `</code></pre>`
	})
}

// Classify syntax using Go's parser, not regular expressions over identifiers.
// Wrappers support the declarations and statements used in documentation snippets.
// This does not load packages or run gopls: unresolved external symbols remain
// syntactic guesses, rather than pretending to provide full semantic analysis.
func highlightGo(source string) string {
	classes := map[int]string{}
	imports := map[int]bool{}
	packages := map[string]bool{}
	parameters := map[*ast.Object]bool{}
	var file *ast.File
	var positions *token.FileSet
	offset := 0
	for _, wrapper := range []struct{ prefix, suffix string }{
		{"", ""}, {"package snippet\n", ""},
		{"package snippet\nfunc example() {\n", "\n}"},
	} {
		fs := token.NewFileSet()
		parsed, err := goparser.ParseFile(fs, "snippet.go", wrapper.prefix+source+wrapper.suffix, 0)
		if err == nil {
			file, positions, offset = parsed, fs, len(wrapper.prefix)
			break
		}
	}
	if file != nil {
		mark := func(id *ast.Ident, class string) {
			if id != nil {
				classes[positions.Position(id.Pos()).Offset-offset] = class
			}
		}
		markType := func(expr ast.Expr) {
			ast.Inspect(expr, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					mark(id, "type")
				}
				return true
			})
		}
		for _, spec := range file.Imports {
			imports[positions.Position(spec.Path.Pos()).Offset-offset] = true
			path, _ := strconv.Unquote(spec.Path.Value)
			name := filepath.Base(path)
			if spec.Name != nil {
				name = spec.Name.Name
				mark(spec.Name, "package")
			}
			packages[name] = true
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				mark(n.Name, "function")
			case *ast.FuncType:
				if n.Params != nil {
					for _, field := range n.Params.List {
						for _, id := range field.Names {
							if id.Obj != nil {
								parameters[id.Obj] = true
							}
							mark(id, "parameter")
						}
					}
				}
			case *ast.TypeSpec:
				mark(n.Name, "type")
			case *ast.Field:
				markType(n.Type)
			case *ast.ValueSpec:
				if n.Type != nil {
					markType(n.Type)
				}
			case *ast.CompositeLit:
				if n.Type != nil {
					markType(n.Type)
				}
			case *ast.TypeAssertExpr:
				if n.Type != nil {
					markType(n.Type)
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				if n.Obj != nil && parameters[n.Obj] {
					mark(n, "parameter")
				}
				if n.Obj != nil && n.Obj.Kind == ast.Con {
					mark(n, "number")
				}
			case *ast.SelectorExpr:
				if id, ok := n.X.(*ast.Ident); ok && (packages[id.Name] || (id.Obj == nil && id.Name != "c" && id.Name != "h" && id.Name != "m" && id.Name != "r" && id.Name != "deps")) {
					// A known or unresolved package qualifier. Bound locals retain their color.
					if classes[positions.Position(id.Pos()).Offset-offset] != "type" {
						mark(id, "package")
					}
					if strings.ToUpper(n.Sel.Name) == n.Sel.Name && len(n.Sel.Name) > 1 {
						mark(n.Sel, "number")
					}
				}
			case *ast.CallExpr:
				switch fn := n.Fun.(type) {
				case *ast.Ident:
					if fn.Obj == nil || fn.Obj.Kind == ast.Fun {
						mark(fn, "function")
					} else if fn.Obj.Kind == ast.Var && !parameters[fn.Obj] {
						mark(fn, "callable")
					}
				case *ast.SelectorExpr:
					mark(fn.Sel, "function")
				}
			}
			return true
		})
	}
	fs := token.NewFileSet()
	f := fs.AddFile("snippet.go", -1, len(source))
	var scan scanner.Scanner
	scan.Init(f, []byte(source), func(token.Position, string) {}, scanner.ScanComments)
	var output strings.Builder
	cursor, depth := 0, 0
	emit := func(class, value string) {
		escaped := html.EscapeString(value)
		if class == "" {
			output.WriteString(escaped)
		} else {
			output.WriteString(`<span class="go-` + class + `">` + escaped + `</span>`)
		}
	}
	for {
		pos, tok, literal := scan.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && literal == "\n" {
			continue
		}
		start := f.Offset(pos)
		length := len(literal)
		if length == 0 {
			length = len(tok.String())
		}
		end := min(start+length, len(source))
		if start < cursor {
			continue
		}
		emit("", source[cursor:start])
		class := classes[start]
		switch {
		case tok.IsKeyword():
			class = "keyword"
		case tok == token.COMMENT:
			class = "comment"
		case tok == token.STRING || tok == token.CHAR:
			class = "string"
		case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
			class = "number"
		case tok == token.IDENT:
			switch literal {
			case "nil", "true", "false":
				class = "keyword"
			case "error", "string", "bool", "byte", "rune", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128", "any", "comparable":
				class = "builtin-type"
			}
		case tok == token.LPAREN || tok == token.LBRACK || tok == token.LBRACE:
			class = fmt.Sprintf("bracket-%d", depth%6)
			depth++
		case tok == token.RPAREN || tok == token.RBRACK || tok == token.RBRACE:
			depth = max(0, depth-1)
			class = fmt.Sprintf("bracket-%d", depth%6)
		case tok == token.PERIOD || tok == token.COMMA || tok == token.SEMICOLON || tok == token.COLON:
			class = "punctuation"
		case tok.IsOperator():
			class = "operator"
		}
		value := source[start:end]
		if imports[start] && len(value) >= 2 {
			// Match the editor's navigable import path, keeping quote marks green.
			emit("string", value[:1])
			path := value[1 : len(value)-1]
			split := strings.LastIndex(path, "/") + 1
			emit("import-path", path[:split])
			emit("import-name", path[split:])
			emit("string", value[len(value)-1:])
		} else {
			emit(class, value)
		}
		cursor = end
	}
	emit("", source[cursor:])
	return output.String()
}
