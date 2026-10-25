// Package scaffold creates application source without starting application services.
package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

type Options struct {
	Directory, Module, Name string
	Source, Version         string
	SkipChecks              bool
}

// Create reserves a new directory, populates it and verifies it. Failure removes
// only the directory this call created. Existing destinations are never touched.
// It does not initialize Git or execute application commands.
func Create(ctx context.Context, options Options) (err error) {
	if err := options.validate(); err != nil {
		return err
	}
	destination, err := filepath.Abs(options.Directory)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("destination already exists or is inaccessible: %s", destination)
	}
	src, err := resolveSource(ctx, options)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(src.directory)
	if err != nil {
		return err
	}
	defer root.Close()
	moduleData, err := root.ReadFile("go.mod")
	if err != nil {
		return fmt.Errorf("read template module: %w", err)
	}
	if modfile.ModulePath(moduleData) != templateModule {
		return errors.New("source is not the Labena template module")
	}
	manifest, err := root.ReadFile("cmd/labena/template.json")
	var compatibility struct{ Schema int }
	if err != nil || json.Unmarshal(manifest, &compatibility) != nil || compatibility.Schema != 1 {
		return errors.New("this template is incompatible with the installer; install the CLI from the same release")
	}
	if err := os.Mkdir(destination, 0o755); err != nil {
		return fmt.Errorf("create destination (parent must exist): %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(destination))
		}
	}()
	if err := copySource(ctx, root, destination, src.files, options); err != nil {
		return err
	}
	if err := personalize(destination, options); err != nil {
		return err
	}
	metadata, err := json.MarshalIndent(src.origin, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(destination, ".labena.json"), append(metadata, '\n'), 0o644); err != nil {
		return err
	}
	if !options.SkipChecks {
		for _, args := range [][]string{{"mod", "tidy"}, {"run", "docs/build.go"}, {"build", "./cmd/...", "./examples/..."}} {
			if _, err := run(ctx, destination, "go", args...); err != nil {
				return fmt.Errorf("verify generated application (go %s): %w; use --skip-checks to keep source and diagnose locally", strings.Join(args, " "), err)
			}
		}
	}
	return ctx.Err()
}

func (o Options) validate() error {
	if strings.TrimSpace(o.Directory) == "" {
		return errors.New("destination directory is required")
	}
	if err := module.CheckPath(o.Module); err != nil {
		return fmt.Errorf("invalid module path: %w", err)
	}
	if o.Module == templateModule {
		return errors.New("choose your application's own module path")
	}
	if o.Name != strings.TrimSpace(o.Name) || o.Name == "" || len(o.Name) > 100 || !utf8.ValidString(o.Name) || strings.ContainsFunc(o.Name, unicode.IsControl) {
		return errors.New("name must be 1–100 UTF-8 bytes without control characters or surrounding whitespace")
	}
	if o.Source != "" && o.Version != "" {
		return errors.New("choose either --source or --template-version")
	}
	return nil
}

func copySource(ctx context.Context, root *os.Root, destination string, files []string, options Options) error {
	if files == nil {
		if err := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if name == "." {
				return nil
			}
			if !included(name) {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !entry.IsDir() {
				files = append(files, name)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	slices.Sort(files)
	var total int64
	for _, name := range slices.Compact(files) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !included(name) {
			continue
		}
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue // Locally deleted tracked files are omitted.
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("template contains a non-regular file: %s", name)
		}
		total += info.Size()
		if info.Size() > 8<<20 || total > 128<<20 {
			return errors.New("template exceeds the 8 MiB file or 128 MiB project limit")
		}
		data, err := root.ReadFile(name) // OpenRoot prevents escaping through ancestor symlinks.
		if err != nil {
			return fmt.Errorf("read template %s: %w", name, err)
		}
		if strings.HasSuffix(name, ".go") {
			data = bytes.ReplaceAll(data, []byte(templateModule+"/"), []byte(options.Module+"/"))
		} else if strings.HasSuffix(name, ".md") {
			data = bytes.ReplaceAll(data, []byte(`"`+templateModule+"/"), []byte(`"`+options.Module+"/"))
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return err
		}
	}
	return nil
}

// Copy source and checked-in examples, never local state, history or the installer.
func included(name string) bool {
	if !fs.ValidPath(name) || strings.Contains(name, "\\") {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == ".git" || segment == ".DS_Store" || segment == "node_modules" || strings.HasPrefix(segment, "client_secret_") || (strings.HasPrefix(segment, ".env") && !strings.HasSuffix(segment, ".example")) {
			return false
		}
	}
	if name == "cmd/labena" || strings.HasPrefix(name, "cmd/labena/") || name == "docs/review" || strings.HasPrefix(name, "docs/review/") {
		return false
	}
	first, _, nested := strings.Cut(name, "/")
	if slices.Contains([]string{"cmd", "internal", "pkg", "tests", "examples", "docs", "deployments", "scripts", ".github"}, first) {
		return true
	}
	return !nested && (slices.Contains([]string{"go.mod", "go.sum", "LICENSE", "README.md", "SUPPORT.md", "SECURITY.md", "CONTRIBUTING.md", "AGENTS.md", "CLAUDE.md", "Tiltfile", ".gitignore", ".dockerignore"}, name) || (strings.HasPrefix(path.Base(name), ".env") && strings.HasSuffix(name, ".example")))
}
