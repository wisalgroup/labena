package scaffold

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

func fixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	files := map[string]string{
		"go.mod":                       "module " + templateModule + "\n\ngo 1.27.3\n",
		"cmd/labena/template.json":     `{"schema":1}`,
		"cmd/app/main.go":              "package main\nimport _ \"" + templateModule + "/internal/app/config\"\nfunc main() {}\n",
		"internal/app/config/app.go":   "package config\ntype AppConfig struct { AppName string `env:\"APP_NAME\" envDefault:\"labena\"` }\n",
		".env.quickstart.example":      "# Tutorial\nAPP_NAME=labena\nAUTH_ENABLED=false\n",
		".env.example":                 "APP_NAME=labena\n",
		"AGENTS.md":                    "# Project guidance\nRead docs/conventions.md.\n",
		"CLAUDE.md":                    "# Project guidance\nRead docs/conventions.md.\n",
		".gitignore":                   ".env\nprivate.txt\nbuild/\n",
		"deployments/configuration.md": "| `APP_NAME` | `labena` |\n",
		"deployments/compose.yaml":     "environment:\n  APP_NAME: \"labena\"\n",
		"deployments/development/kubernetes/app-config.yaml": "data:\n  APP_NAME: \"labena\"\n",
		"scripts/example.sh": "#!/bin/sh\nexit 0\n",
	}
	for name, data := range files {
		write(t, directory, name, data)
	}
	if err := os.Chmod(filepath.Join(directory, "scripts/example.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "fixture"}} {
		if _, err := run(t.Context(), directory, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	write(t, directory, ".env", "SECRET=never-copy\n")
	write(t, directory, "private.txt", "never-copy")
	write(t, directory, "build/app", "never-copy")
	write(t, directory, "internal/app/config/uncommitted.go", "package config\n")
	return directory
}

func write(t *testing.T, directory, name, data string) {
	t.Helper()
	name = filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, directory, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCreatesIndependentApplication(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "myapp")
	options := Options{Directory: directory, Module: "github.com/example/myapp", Name: "MyApp", Source: fixture(t), SkipChecks: true}
	if err := Create(t.Context(), options); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, directory, "go.mod"), "module github.com/example/myapp\n") || !strings.Contains(read(t, directory, "cmd/app/main.go"), `"github.com/example/myapp/internal/app/config"`) {
		t.Fatal("module or imports retain the template identity")
	}
	for _, name := range []string{".env", ".env.example", ".env.quickstart.example"} {
		values, err := godotenv.Unmarshal(read(t, directory, name))
		if err != nil || values["APP_NAME"] != options.Name || values["SECRET"] != "" {
			t.Fatalf("unsafe environment %s: %v", name, err)
		}
	}
	for _, name := range []string{".git", "cmd/labena", "build", "private.txt"} {
		if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("copied excluded path %s", name)
		}
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if read(t, directory, name) != read(t, options.Source, name) {
			t.Fatalf("agent guidance changed during application creation: %s", name)
		}
	}
	read(t, directory, "internal/app/config/uncommitted.go")
	info, err := os.Stat(filepath.Join(directory, "scripts/example.sh"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatal("script executable mode lost", err)
	}
	var origin provenance
	if err := json.Unmarshal([]byte(read(t, directory, ".labena.json")), &origin); err != nil || !origin.Local || !origin.Modified || origin.Commit == "" {
		t.Fatal("source provenance missing", err)
	}
	if strings.Contains(read(t, directory, ".labena.json"), options.Source) {
		t.Fatal("local filesystem path leaked into provenance")
	}
}

func TestNeverOverwritesExistingDestination(t *testing.T) {
	directory := t.TempDir()
	write(t, directory, "keep.txt", "unchanged")
	err := Create(t.Context(), Options{Directory: directory, Module: "example.com/myapp", Name: "MyApp", Source: "not-even-resolved"})
	if err == nil || read(t, directory, "keep.txt") != "unchanged" {
		t.Fatal("existing destination was modified")
	}
}

func TestCopyFailureCleansOnlyNewDirectory(t *testing.T) {
	src := fixture(t)
	if err := os.Symlink("go.mod", filepath.Join(src, "internal/link.go")); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	write(t, parent, "keep.txt", "unchanged")
	destination := filepath.Join(parent, "myapp")
	err := Create(t.Context(), Options{Directory: destination, Module: "example.com/myapp", Name: "MyApp", Source: src, SkipChecks: true})
	if err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatal("symlink accepted", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) || read(t, parent, "keep.txt") != "unchanged" {
		t.Fatal("incorrect failure cleanup", err)
	}
}

func TestNameRoundTripsWithoutCodeOrEnvironmentExpansion(t *testing.T) {
	src := fixture(t)
	for _, name := range []string{"MyApp", "تطبيق", `MyApp's $HOME \ "quoted" <&>`} {
		t.Run(name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "app")
			if err := Create(t.Context(), Options{Directory: directory, Module: "example.com/myapp", Name: name, Source: src, SkipChecks: true}); err != nil {
				t.Fatal(err)
			}
			values, err := godotenv.Unmarshal(read(t, directory, ".env"))
			if err != nil || values["APP_NAME"] != name {
				t.Fatal("dotenv changed name", err, values["APP_NAME"])
			}
			file, err := parser.ParseFile(token.NewFileSet(), "app.go", read(t, directory, "internal/app/config/app.go"), 0)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			ast.Inspect(file, func(node ast.Node) bool {
				if field, ok := node.(*ast.Field); ok && field.Tag != nil {
					tag, _ := strconv.Unquote(field.Tag.Value)
					found = reflect.StructTag(tag).Get("envDefault") == name
				}
				return true
			})
			if !found {
				t.Fatal("Go default changed name")
			}
		})
	}
}

func TestInvalidInputsAndCancellationLeaveNoProject(t *testing.T) {
	for _, options := range []Options{
		{Module: "not a module", Name: "MyApp"},
		{Module: "example.com/myapp", Name: "name\nAPP_ENV=production"},
		{Module: templateModule, Name: "MyApp"},
		{Module: "example.com/myapp", Name: "MyApp", Version: "main"},
		{Module: "example.com/myapp", Name: "MyApp", Version: "latest"},
		{Module: "example.com/myapp", Name: "MyApp", Version: "v0.1.0", Source: "."},
	} {
		options.Directory = filepath.Join(t.TempDir(), "app")
		if err := Create(t.Context(), options); err == nil {
			t.Fatalf("accepted invalid options: %+v", options)
		}
		if _, err := os.Stat(options.Directory); !os.IsNotExist(err) {
			t.Fatal("created destination for invalid options")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Create(ctx, Options{Directory: filepath.Join(t.TempDir(), "app"), Module: "example.com/myapp", Name: "MyApp", Source: fixture(t)}); err == nil {
		t.Fatal("ignored cancellation")
	}
}
