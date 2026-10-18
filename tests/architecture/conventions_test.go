package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Inspect source imports, including files behind build tags. Tests are excluded
// from dependency rules because integration tests intentionally cross layers.
func TestDependencyDirection(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var module string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			module = strings.Trim(fields[1], `"`)
			break
		}
	}
	if module == "" {
		t.Fatal("go.mod has no module directive")
	}
	for _, tree := range []string{"pkg", "internal", "examples"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			source := filepath.ToSlash(filepath.Dir(relative))
			business := businessImport(source)
			businessLogic := business && (segment(source, "service") || segment(source, "repository"))
			if strings.HasSuffix(path, "_test.go") {
				if source == "internal/app" || businessLogic {
					t.Errorf("%s: keep app/service/repository tests under tests/<owner>", relative)
				}
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				dependency, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				local := strings.TrimPrefix(dependency, module+"/")
				var reason string
				switch {
				case within(dependency, "github.com/gin-gonic/gin") || within(dependency, "github.com/gin-contrib") || within(dependency, "github.com/danielgtaylor/huma/v2/adapters/humagin"):
					reason = "HTTP uses Huma and net/http; Gin dependencies are not permitted"
				case (within(source, "examples/storage") || within(source, "examples/mail")) && within(local, "internal/app") && !within(local, "internal/app/config"):
					reason = "independent capability examples must not import app assembly"
				case within(local, "internal/platform/testdb"):
					reason = "test database helpers cannot be imported by application code"
				case within(source, "pkg") && within(local, "internal"):
					reason = "reusable packages cannot depend on application code"
				case within(source, "internal/modules/shared") && within(local, "internal") && !within(local, "internal/modules/shared"):
					reason = "shared definitions cannot depend on platform or business implementations"
				case (business || within(source, "internal/platform")) && within(local, "internal/app"):
					reason = "business modules and platform packages cannot import application composition or environment configuration"
				case within(source, "internal/platform") && within(local, "internal/modules"):
					reason = "platform packages cannot depend on business modules or shared business definitions"
				case businessLogic && (within(local, "internal/app") || within(local, "internal/platform/dependencies") || within(local, "internal/platform/api") || segment(local, "delivery")):
					reason = "services and repositories receive collaborators, not application assembly or transports"
				case businessLogic && (within(dependency, "github.com/gin-gonic/gin") || within(dependency, "github.com/danielgtaylor/huma/v2") || within(dependency, "github.com/spf13/cobra")):
					reason = "HTTP and CLI frameworks belong in delivery packages"
				case business && segment(source, "repository") && segment(local, "service"):
					reason = "repositories cannot depend on business services"
				case capabilityCore(source) && within(local, "internal/platform/dependencies"):
					reason = "platform cores remain independently constructible; wiring belongs in integration"
				}
				if reason != "" {
					t.Errorf("%s imports %s: %s", relative, dependency, reason)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func within(path, prefix string) bool { return path == prefix || strings.HasPrefix(path, prefix+"/") }
func segment(path, part string) bool {
	for _, s := range strings.Split(path, "/") {
		if s == part {
			return true
		}
	}
	return false
}
func businessImport(path string) bool {
	return strings.HasPrefix(path, "internal/modules/") && !within(path, "internal/modules/shared")
}
func capabilityCore(path string) bool {
	for _, name := range []string{"mail", "media", "notification", "messaging"} {
		base := "internal/platform/" + name
		if within(path, base) && !within(path, base+"/integration") {
			return true
		}
	}
	return false
}
