package scaffold

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"golang.org/x/mod/modfile"
)

func personalize(directory string, options Options) error {
	edit := func(name string, transform func([]byte) ([]byte, error)) error {
		name = filepath.Join(directory, filepath.FromSlash(name))
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		data, err = transform(data)
		if err != nil {
			return err
		}
		return os.WriteFile(name, data, 0o644)
	}
	if err := edit("go.mod", func(data []byte) ([]byte, error) {
		file, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			return nil, err
		}
		if err := file.AddModuleStmt(options.Module); err != nil {
			return nil, err
		}
		return file.Format()
	}); err != nil {
		return err
	}
	if err := edit("internal/app/config/app.go", func(data []byte) ([]byte, error) {
		return appNameDefault(data, options.Name)
	}); err != nil {
		return err
	}
	// Marshal just this assignment to retain comments, ordering and other values.
	assignment, err := godotenv.Marshal(map[string]string{"APP_NAME": options.Name})
	if err != nil {
		return err
	}
	examples, err := filepath.Glob(filepath.Join(directory, ".env*.example"))
	if err != nil {
		return err
	}
	for _, name := range examples {
		if err := edit(filepath.Base(name), func(data []byte) ([]byte, error) {
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if strings.HasPrefix(line, "APP_NAME=") {
					lines[i] = assignment
				}
			}
			return []byte(strings.Join(lines, "\n")), nil
		}); err != nil {
			return err
		}
	}
	// The ignored local environment is always based on the published tutorial profile.
	quickstart, err := os.ReadFile(filepath.Join(directory, ".env.quickstart.example"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, ".env"), quickstart, 0o600); err != nil {
		return err
	}
	if err := edit("deployments/configuration.md", func(data []byte) ([]byte, error) {
		name := strings.NewReplacer("|", "&#124;", "`", "&#96;").Replace(options.Name)
		return bytes.ReplaceAll(data, []byte("| `APP_NAME` | `labena` |"), []byte("| `APP_NAME` | `"+name+"` |")), nil
	}); err != nil {
		return err
	}
	// YAML quoted strings use JSON-compatible escaping. These are initial defaults;
	// operators can configure them independently later without changing Go imports.
	for _, name := range []string{"deployments/compose.yaml", "deployments/development/kubernetes/app-config.yaml"} {
		if err := edit(name, func(data []byte) ([]byte, error) {
			return bytes.ReplaceAll(data, []byte(`APP_NAME: "labena"`), []byte("APP_NAME: "+strconv.Quote(options.Name))), nil
		}); err != nil {
			return err
		}
	}
	name := strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "#", "\\#").Replace(options.Name)
	readme := fmt.Sprintf("# %s\n\nCreated with [Labena](https://github.com/wisalgroup/labena).\n\nGo module: `%s`. The initial template source is recorded in `.labena.json`.\n\n## Run locally\n\nFollow [installation](docs/getting-started.md) to start PostgreSQL and Redis.\nThe generated `.env` uses the tutorial profile; then run:\n\n```sh\ngo run ./cmd/cli migrate up\ngo run ./cmd/app\n```\n\n`APP_NAME` controls API, mail and log-viewer branding and telemetry identity.\nConfigure operational identifiers, credentials and deployment resources separately.\n\n## Develop and maintain\n\n- [Your first module](docs/modules.md)\n- [Project conventions](docs/conventions.md)\n- [Configuration](docs/configuration.md)\n- [Testing](docs/testing.md)\n- [Deployment](deployments/README.md)\n- [Adoption and updates](docs/upgrading.md)\n\nTo browse the documentation, run `go run docs/build.go` and `go run docs/serve.go`.\nKeep the supplied license and record reviewed upstream fixes in your own repository.\n", name, options.Module)
	return os.WriteFile(filepath.Join(directory, "README.md"), []byte(readme), 0o644)
}

func appNameDefault(data []byte, name string) ([]byte, error) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "app.go", data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	changed := false
	ast.Inspect(file, func(node ast.Node) bool {
		field, ok := node.(*ast.Field)
		if ok && len(field.Names) == 1 && field.Names[0].Name == "AppName" && field.Tag != nil {
			field.Tag.Value = strconv.Quote(`env:"APP_NAME" envDefault:` + strconv.Quote(name))
			changed = true
		}
		return true
	})
	if !changed {
		return nil, fmt.Errorf("template lacks the AppName configuration field")
	}
	var output bytes.Buffer
	err = format.Node(&output, set, file)
	return output.Bytes(), err
}
