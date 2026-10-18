package migrate

import (
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/wisalgroup/labena/pkg/database"
)

type source struct {
	path  string
	files fs.FS
}

// Registry collects migration directories during application assembly. Its zero
// value is ready to use. Register sequentially, then call FS before starting work.
// Sources are borrowed and must remain unchanged while their history is in use.
type Registry struct{ sources []source }

// Register declares a source without reading files or accessing a database.
// directory identifies the source in errors; files is rooted at that directory.
// Prefer embed.FS so deployed binaries do not depend on their working directory.
func (r *Registry) Register(directory string, files fs.FS) error {
	if !fs.ValidPath(directory) || directory == "." || files == nil {
		return fmt.Errorf("migrations: a source directory and filesystem are required")
	}
	for _, s := range r.sources {
		if s.path == directory {
			return fmt.Errorf("migrations: directory %q already registered", directory)
		}
	}
	r.sources = append(r.sources, source{directory, files})
	return nil
}

// FS combines all registered directories into one flat Goose history. Only SQL
// files directly in each source are included. Versions must be unique across all
// sources; ordering is by timestamp, not module registration order. An empty
// history is valid for assembly but Goose cannot run it. An empty driver means
// PostgreSQL, matching database defaults. No SQL is executed here.
func (r *Registry) FS(driver database.Driver) (fs.FS, error) {
	if driver == "" {
		driver = database.DriverPostgres
	}
	if driver != database.DriverPostgres && driver != database.DriverMySQL {
		return nil, fmt.Errorf("migrations: PostgreSQL or MySQL driver required")
	}
	h := &history{files: make(map[string]fs.FS)}
	versions := make(map[string]string)
	for _, s := range r.sources {
		entries, err := fs.ReadDir(s.files, ".")
		if err != nil {
			return nil, fmt.Errorf("migrations: read %s: %w", s.path, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}
			name := entry.Name()
			parts := migrationName.FindStringSubmatch(name)
			if parts == nil {
				return nil, fmt.Errorf("migrations: %s/%s must use a UTC YYYYMMDDHHMMSS_name.sql filename", s.path, name)
			}
			if previous, exists := versions[parts[1]]; exists {
				return nil, fmt.Errorf("migrations: duplicate version %s in %s and %s/%s", parts[1], previous, s.path, name)
			}
			versions[parts[1]] = s.path + "/" + name
			h.files[name] = s.files
			h.entries = append(h.entries, entry)
		}
	}
	slices.SortFunc(h.entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	if _, _, err := readHistory(h, driver); err != nil {
		return nil, err
	}
	return h, nil
}

// history presents borrowed sources as one filesystem without copying files or
// changing migration names. Each opened directory has its own read position.
type history struct {
	files   map[string]fs.FS
	entries []fs.DirEntry
}

func (h *history) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return &directory{entries: h.entries}, nil
	}
	if source, ok := h.files[name]; ok {
		return source.Open(name)
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

type directory struct {
	entries []fs.DirEntry
	offset  int
	closed  bool
}

func (d *directory) Stat() (fs.FileInfo, error) {
	if d.closed {
		return nil, fs.ErrClosed
	}
	return directoryInfo{}, nil
}
func (d *directory) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid}
}
func (d *directory) Close() error { d.closed = true; return nil }
func (d *directory) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.closed {
		return nil, fs.ErrClosed
	}
	if n > 0 && d.offset == len(d.entries) {
		return nil, io.EOF
	}
	end := len(d.entries)
	if n > 0 && n < end-d.offset {
		end = d.offset + n
	}
	entries := append([]fs.DirEntry{}, d.entries[d.offset:end]...)
	d.offset = end
	return entries, nil
}

type directoryInfo struct{}

func (directoryInfo) Name() string       { return "." }
func (directoryInfo) Size() int64        { return 0 }
func (directoryInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (directoryInfo) ModTime() time.Time { return time.Time{} }
func (directoryInfo) IsDir() bool        { return true }
func (directoryInfo) Sys() any           { return nil }
