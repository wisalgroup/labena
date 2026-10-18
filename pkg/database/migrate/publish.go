package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/wisalgroup/labena/pkg/database"
)

// Table selects a package template and its configured SQL table name.
// Source contains <driver>/<template>.sql with a single {{.Table}} parameter.
type Table struct {
	Source   fs.FS
	Template string
	Name     string
}

// Publication reports either a newly published file or an existing application copy.
type Publication struct {
	Path    string
	Created bool
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
var migrationName = regexp.MustCompile(`^([0-9]{14})_([a-z0-9_]+)\.sql$`)

const marker = "-- platform: "

type publishedTable struct{ template, path string }

// Publish writes timestamped Goose migrations directly into directory. It opens
// no database connections, never overwrites files, and skips previously published
// tables even after their SQL is edited. Preserve each file's platform comment.
// A directory lock excludes concurrent publishers. Completed files remain on a
// later error; rerunning safely publishes the remaining files.
func Publish(ctx context.Context, directory string, driver database.Driver, tables ...Table) (results []Publication, err error) {
	if driver != database.DriverPostgres && driver != database.DriverMySQL {
		return nil, errors.New("migrations: DATABASE_DRIVER must be postgres or mysql")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if directory == "" {
		return nil, errors.New("migrations: output directory is required")
	}
	// Validate and render the entire selection before writing any migration.
	bodies := make(map[string][]byte, len(tables))
	for _, table := range tables {
		if table.Source == nil || !identifier.MatchString(table.Name) || !identifier.MatchString(table.Template) {
			return nil, errors.New("migrations: table and template names must match [a-z][a-z0-9_]{0,39}")
		}
		if _, duplicate := bodies[table.Name]; duplicate {
			return nil, fmt.Errorf("migrations: table %q selected more than once", table.Name)
		}
		data, err := fs.ReadFile(table.Source, path.Join(string(driver), table.Template+".sql"))
		if err != nil {
			return nil, fmt.Errorf("migrations: read %s template: %w", table.Template, err)
		}
		tmpl, err := template.New(table.Template).Option("missingkey=error").Parse(string(data))
		if err != nil {
			return nil, fmt.Errorf("migrations: parse %s template: %w", table.Template, err)
		}
		var body bytes.Buffer
		fmt.Fprintf(&body, "%s%s %s %s\n", marker, driver, table.Template, table.Name)
		if err := tmpl.Execute(&body, struct{ Table string }{table.Name}); err != nil {
			return nil, fmt.Errorf("migrations: render %s template: %w", table.Template, err)
		}
		bodies[table.Name] = body.Bytes()
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	lock := filepath.Join(directory, ".publish.lock")
	file, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("migrations: publisher lock exists at %s; if no publisher is running, remove the stale lock and retry", lock)
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.Remove(lock)) }()
	if err := file.Close(); err != nil {
		return nil, err
	}
	history := os.DirFS(directory)
	existing, latest, err := readHistory(history, driver)
	if err != nil {
		return nil, err
	}
	for _, table := range tables {
		if old, ok := existing[table.Name]; ok {
			if old.template != table.Template {
				return nil, fmt.Errorf("migrations: table %q already belongs to template %q", table.Name, old.template)
			}
			continue
		}
		matches, err := fs.Glob(history, "*_"+table.Name+".sql")
		if err != nil {
			return nil, err
		}
		if len(matches) != 0 {
			return nil, fmt.Errorf("migrations: existing migration for %q has no platform comment; review it before publishing", table.Name)
		}
	}
	next := time.Now().UTC().Truncate(time.Second)
	if !next.After(latest) {
		next = latest.Add(time.Second)
	}
	for _, table := range tables {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if old, ok := existing[table.Name]; ok {
			results = append(results, Publication{Path: filepath.Join(directory, old.path)})
			continue
		}
		if next.Year() > 9999 {
			return results, errors.New("migrations: migration timestamp range exhausted")
		}
		filename := filepath.Join(directory, next.Format("20060102150405")+"_"+table.Name+".sql")
		if err := publishFile(filename, bodies[table.Name]); err != nil {
			return results, err
		}
		results = append(results, Publication{Path: filename, Created: true})
		next = next.Add(time.Second)
	}
	return results, nil
}

// readHistory also prevents a rebuilt binary from applying generated SQL for the
// wrong database. Application-written migrations may omit the platform comment.
func readHistory(history fs.FS, driver database.Driver) (map[string]publishedTable, time.Time, error) {
	files, err := fs.Glob(history, "*.sql")
	if err != nil {
		return nil, time.Time{}, err
	}
	tables := make(map[string]publishedTable)
	versions := make(map[string]bool)
	var latest time.Time
	for _, file := range files {
		parts := migrationName.FindStringSubmatch(file)
		if parts == nil {
			return nil, latest, fmt.Errorf("migrations: %s must use a UTC YYYYMMDDHHMMSS_name.sql filename", file)
		}
		stamp, err := time.Parse("20060102150405", parts[1])
		if err != nil || versions[parts[1]] {
			return nil, latest, fmt.Errorf("migrations: invalid or duplicate timestamp in %s", file)
		}
		versions[parts[1]] = true
		if stamp.After(latest) {
			latest = stamp
		}
		body, err := fs.ReadFile(history, file)
		if err != nil {
			return nil, latest, err
		}
		line, _, _ := strings.Cut(string(body), "\n")
		if !strings.HasPrefix(line, marker) {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, marker))
		if len(fields) != 3 || !identifier.MatchString(fields[1]) || !identifier.MatchString(fields[2]) {
			return nil, latest, fmt.Errorf("migrations: invalid platform comment in %s", file)
		}
		if fields[0] != string(driver) {
			return nil, latest, fmt.Errorf("migrations: %s targets %s, but DATABASE_DRIVER is %s; use one database dialect per migration history", file, fields[0], driver)
		}
		if _, exists := tables[fields[2]]; exists {
			return nil, latest, fmt.Errorf("migrations: multiple published definitions for table %q", fields[2])
		}
		tables[fields[2]] = publishedTable{template: fields[1], path: file}
	}
	return tables, latest, nil
}

// Link a complete temporary file into place without replacing an existing path.
func publishFile(filename string, body []byte) (err error) {
	file, err := os.CreateTemp(filepath.Dir(filename), ".migration-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(body); err == nil {
		err = file.Chmod(0o644)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	return os.Link(file.Name(), filename)
}
