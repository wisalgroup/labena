// Package logviewer serves a protected, bounded local view of application logs.
// It owns no listeners, global loggers, file writers or background goroutines.
package logviewer

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/wisalgroup/labena/internal/platform/adminhttp"
	"github.com/wisalgroup/labena/pkg/logging"
)

const RootPath = "/admin/logs"

// Config controls access and live retention. File retention belongs to logging.
type Config struct {
	AppName                    string
	Username                   string
	Password                   string `json:"-"`
	BufferEntries, BufferBytes int
}

func (c Config) Validate() error {
	if err := (adminhttp.Config{Username: c.Username, Password: c.Password}).Validate(); err != nil {
		return err
	}
	if c.BufferEntries < 1 || c.BufferEntries > 100000 || c.BufferBytes < 64<<10 || c.BufferBytes > 64<<20 {
		return errors.New("logviewer: buffer entries must be 1..100000 and bytes 65536..67108864")
	}
	return nil
}

// Viewer borrows channel definitions at construction and keeps bounded log copies.
// ServeHTTP and WrapHandler are safe for concurrent use.
type Viewer struct {
	index    []byte
	buffer   buffer
	channels map[string]logging.Channel
	handler  http.Handler
	requests chan struct{}
}

// New validates configuration and resolves file locations without opening them.
// It does not enable or select logging outputs.
func New(cfg Config, channels map[string]logging.Channel) (*Viewer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	v := &Viewer{channels: maps.Clone(channels), requests: make(chan struct{}, 4)}
	if strings.TrimSpace(cfg.AppName) == "" {
		cfg.AppName = "Application"
	}
	page, err := template.ParseFS(assets, "web/index.html")
	if err != nil {
		return nil, err
	}
	var index bytes.Buffer
	if err := page.Execute(&index, struct{ AppName string }{cfg.AppName}); err != nil {
		return nil, err
	}
	v.index = index.Bytes()
	v.buffer = buffer{entries: make([]Entry, cfg.BufferEntries), maxBytes: cfg.BufferBytes}
	for name, ch := range v.channels {
		if ch.Driver == "file" {
			if ch.Path == "" {
				return nil, errors.New("logviewer: file channel requires a path")
			}
			path, err := filepath.Abs(ch.Path)
			if err != nil {
				return nil, err
			}
			ch.Path = path
			v.channels[name] = ch
		}
	}
	handler, err := adminhttp.Protect(http.HandlerFunc(v.serveHTTP), adminhttp.Config{Realm: "Logs", Username: cfg.Username, Password: cfg.Password})
	if err != nil {
		return nil, err
	}
	v.handler = handler
	return v, nil
}

//go:embed web/*
var assets embed.FS

var errQuery = errors.New("invalid log query")
var errChanged = errors.New("log file changed; refresh the view")

func (v *Viewer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	v.handler.ServeHTTP(w, r)
}

func (v *Viewer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	path := strings.TrimPrefix(r.URL.Path, RootPath)
	if !strings.HasPrefix(r.URL.Path, RootPath+"/") && r.URL.Path != RootPath {
		http.NotFound(w, r)
		return
	}
	if path == "" {
		http.Redirect(w, r, RootPath+"/", http.StatusTemporaryRedirect)
		return
	}
	if path == "/query" {
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method not allowed", 405)
			return
		}
	} else if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", 405)
		return
	}
	select {
	case v.requests <- struct{}{}:
		defer func() { <-v.requests }()
	default:
		w.Header().Set("Retry-After", "2")
		http.Error(w, "Viewer busy; try again", 429)
		return
	}
	switch path {
	case "/", "/app.js", "/app.css":
		name := strings.TrimPrefix(path, "/")
		contentType := "text/html; charset=utf-8"
		if name == "" {
			name = "index.html"
		}
		if name == "app.js" {
			contentType = "text/javascript; charset=utf-8"
		}
		if name == "app.css" {
			contentType = "text/css; charset=utf-8"
		}
		data := v.index
		if name != "index.html" {
			data, _ = assets.ReadFile("web/" + name)
		}
		w.Header().Set("Content-Type", contentType)
		if r.Method != "HEAD" {
			_, _ = w.Write(data)
		}
	case "/sources":
		sources, err := v.sources(r.Context())
		if err != nil {
			http.Error(w, "Cannot read configured log files", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Sources  []source `json:"sources"`
			Capacity int      `json:"capacity"`
			Bytes    int      `json:"bytes"`
		}{sources, len(v.buffer.entries), v.buffer.maxBytes})
	case "/query":
		v.query(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (v *Viewer) query(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]), "application/json") {
		http.Error(w, "Use application/json", 415)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var q query
	if err := decoder.Decode(&q); err != nil {
		http.Error(w, "Invalid log query", 400)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "Invalid log query", 400)
		return
	}
	if q.Source == "" {
		q.Source = "live"
	}
	if q.Limit == 0 {
		q.Limit = 200
	}
	if q.Limit < 1 || q.Limit > 500 || q.Before < 0 || len(q.Search) > 512 || (q.Source != "live" && q.Source != "file") || (q.Level != "" && q.Level != "DEBUG" && q.Level != "INFO" && q.Level != "WARN" && q.Level != "ERROR") {
		http.Error(w, "Invalid log query", 400)
		return
	}
	if q.Channel != "" {
		if _, ok := v.channels[q.Channel]; !ok {
			http.Error(w, "Unknown log channel", 400)
			return
		}
	}
	q.Search = strings.ToLower(q.Search)
	var result page
	var err error
	if q.Source == "file" {
		result, err = v.readFile(r.Context(), q)
	} else {
		result = v.buffer.query(q)
	}
	if err != nil {
		switch {
		case errors.Is(err, errQuery):
			http.Error(w, "Invalid log query", 400)
		case errors.Is(err, errChanged):
			http.Error(w, errChanged.Error(), 409)
		case errors.Is(err, os.ErrNotExist):
			http.Error(w, "Log file is no longer available; refresh the sources", 404)
		case errors.Is(err, os.ErrPermission):
			http.Error(w, "Log file is not readable", 403)
		default:
			http.Error(w, "Cannot read this log file", 500)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
