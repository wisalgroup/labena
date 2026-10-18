package logviewer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lmittmann/tint"
	"github.com/wisalgroup/labena/internal/platform/logviewer"
	"github.com/wisalgroup/labena/pkg/logging"
)

func config() logviewer.Config {
	return logviewer.Config{Username: "operator", Password: "test-password", BufferEntries: 100, BufferBytes: 1 << 20}
}
func viewer(t *testing.T, cfg logviewer.Config, channels map[string]logging.Channel) *logviewer.Viewer {
	t.Helper()
	v, err := logviewer.New(cfg, channels)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func request(v http.Handler, method, path, body string, auth bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, logviewer.RootPath+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if auth {
		r.SetBasicAuth("operator", "test-password")
	}
	w := httptest.NewRecorder()
	v.ServeHTTP(w, r)
	return w
}

func TestApplicationBrandingIsEscapedAndProtected(t *testing.T) {
	cfg := config()
	cfg.AppName = `MyApp <script>alert("brand")</script>`
	v := viewer(t, cfg, nil)
	if w := request(v, "GET", "/", "", false); w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "MyApp") {
		t.Fatal("branding page bypassed authentication")
	}
	w := request(v, "GET", "/", "", true)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Logs · MyApp &lt;script&gt;") || strings.Contains(w.Body.String(), cfg.AppName) {
		t.Fatal("branding missing or not escaped", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Labena") || strings.Contains(w.Body.String(), "{{.AppName}}") {
		t.Fatal("template branding leaked")
	}
	if w := request(v, "HEAD", "/", "", true); w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatal("HEAD returned content")
	}
}

type result struct {
	Entries []logviewer.Entry `json:"entries"`
	Next    int64             `json:"next"`
	Evicted uint64            `json:"evicted"`
	Limited bool              `json:"limited"`
}

func query(t *testing.T, v http.Handler, q map[string]any) result {
	t.Helper()
	body, _ := json.Marshal(q)
	w := request(v, "POST", "/query", string(body), true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p result
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCapturePreservesChannelsLevelsScopesAndOutputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	channels := map[string]logging.Channel{
		"stdout": {Driver: "stdout", Options: logging.Options{Level: slog.LevelInfo}},
		"file":   {Driver: "file", Path: path, Options: logging.Options{Level: slog.LevelWarn}},
	}
	v := viewer(t, config(), channels)
	m, err := logging.Open(logging.Config{Default: []string{"stdout"}, Channels: channels, WrapHandler: v.WrapHandler})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	logger := m.Default().With("request_id", "req-123").WithGroup("operation").With("kind", "sample")
	logger.Debug("hidden")
	logger.Info("visible", "count", 2)
	file, err := logging.Select(logger, "file")
	if err != nil {
		t.Fatal(err)
	}
	file.Info("below file threshold")
	file.Warn("file-only", "key", "value")
	p := query(t, v, map[string]any{})
	if len(p.Entries) != 2 || p.Entries[0].Channel != "file" || p.Entries[1].Channel != "stdout" {
		t.Fatal(p)
	}
	for _, e := range p.Entries {
		var raw map[string]any
		if err := json.Unmarshal([]byte(e.Raw), &raw); err != nil {
			t.Fatal(err)
		}
		if raw["request_id"] != "req-123" || raw["operation"].(map[string]any)["kind"] != "sample" {
			t.Fatal(raw)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "file-only") || strings.Contains(string(data), "threshold") {
		t.Fatal(err, string(data))
	}
	if got := query(t, v, map[string]any{"channel": "stdout", "search": "REQ-123", "level": "INFO"}); len(got.Entries) != 1 {
		t.Fatal(got)
	}
	if got := query(t, v, map[string]any{"level": "ERROR"}); len(got.Entries) != 0 {
		t.Fatal(got)
	}
}

func TestAuthenticationAndReadOnlyBoundary(t *testing.T) {
	v := viewer(t, config(), nil)
	for _, path := range []string{"", "/", "/sources", "/query", "/app.js", "/app.css", "/unknown"} {
		w := request(v, "GET", path, "", false)
		if w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(path, w.Code, w.Header())
		}
	}
	for _, path := range []string{"/", "/app.js", "/app.css", "/sources"} {
		w := request(v, "GET", path, "", true)
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || strings.Contains(w.Body.String(), "test-password") {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		origin, site string
		code         int
	}{{"http://example.com", "same-origin", 200}, {"https://example.com", "same-origin", 200}, {"https://evil.example", "cross-site", 403}, {"", "same-site", 403}, {"null", "", 403}} {
		r := httptest.NewRequest("GET", "http://example.com/admin/logs/sources", nil)
		r.SetBasicAuth("operator", "test-password")
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		v.ServeHTTP(w, r)
		if w.Code != tc.code || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatal(tc, w.Code, w.Header())
		}
	}
	if w := request(v, "DELETE", "/sources", "", true); w.Code != 405 {
		t.Fatal(w.Code)
	}
	for _, body := range []string{`{"limit":501}`, `{"before":-1}`, `{"source":"remote"}`, `{"channel":"unknown"}`, `{"level":"fatal"}`, `{"extra":true}`, `{} {}`, strings.Repeat("x", 5000)} {
		if w := request(v, "POST", "/query", body, true); w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/admin/logs/", nil)
	r.SetBasicAuth("operator", "wrong")
	w := httptest.NewRecorder()
	v.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestBoundedConcurrentCaptureAndPagination(t *testing.T) {
	cfg := config()
	cfg.BufferEntries = 3
	cfg.BufferBytes = 64 << 10
	v := viewer(t, cfg, map[string]logging.Channel{"stdout": {Driver: "stdout"}})
	logger := slog.New(v.WrapHandler("stdout", slog.NewJSONHandler(&safeDiscard{}, nil)))
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { logger.Info("sample", "data", strings.Repeat("a", 20000)); query(t, v, map[string]any{}) })
	}
	wg.Wait()
	p := query(t, v, map[string]any{"limit": 1})
	if len(p.Entries) != 1 || !p.Entries[0].Truncated || p.Entries[0].Level != "INFO" || len(p.Entries[0].Raw) > 16<<10 || p.Evicted == 0 {
		t.Fatal(p)
	}
	logger.Info("newest")
	p = query(t, v, map[string]any{"limit": 1})
	if p.Entries[0].Message != "newest" || p.Next == 0 {
		t.Fatal(p)
	}
	older := query(t, v, map[string]any{"before": p.Next})
	if len(older.Entries) == 0 || older.Entries[0].ID >= p.Entries[0].ID {
		t.Fatal(older)
	}
}

type safeDiscard struct{}

func (*safeDiscard) Write(p []byte) (int, error) { return len(p), nil }

func TestFileHistoryIsConfinedAndPaged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	data := strings.Join([]string{`{"time":"2026-10-18T10:00:00Z","level":"INFO","msg":"first"}`, `{"level":"ERROR","msg":"second","request_id":"abc"}`, "Oct 18 10:01:00.123 WARN third", `{"level":"INFO","msg":"fourth"}`}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "app-2026-10-18.log"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unrelated.log"), []byte("secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v := viewer(t, config(), map[string]logging.Channel{"file": {Driver: "file", Path: path, FileMode: logging.FileDaily}})
	w := request(v, "GET", "/sources", "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "2026-10-18") || strings.Contains(w.Body.String(), dir) || strings.Contains(w.Body.String(), "unrelated") {
		t.Fatal(w.Code, w.Body.String())
	}
	p := query(t, v, map[string]any{"source": "file", "channel": "file", "date": "2026-10-18", "limit": 2})
	if len(p.Entries) != 2 || p.Entries[0].Message != "fourth" || p.Entries[1].Level != "WARN" || p.Next == 0 {
		t.Fatal(p)
	}
	previous := query(t, v, map[string]any{"source": "file", "channel": "file", "date": "2026-10-18", "before": p.Next})
	if len(previous.Entries) != 2 || previous.Entries[0].Message != "second" || previous.Entries[1].Message != "first" || previous.Next != 0 {
		t.Fatal(previous)
	}
	filtered := query(t, v, map[string]any{"source": "file", "channel": "file", "date": "2026-10-18", "level": "ERROR", "search": "ABC"})
	if len(filtered.Entries) != 1 || filtered.Entries[0].Message != "second" {
		t.Fatal(filtered)
	}
	for _, date := range []string{"../unrelated", "../../secret", "2026-02-30", "/etc/passwd"} {
		body, _ := json.Marshal(map[string]any{"source": "file", "channel": "file", "date": date})
		if w := request(v, "POST", "/query", string(body), true); w.Code != 400 {
			t.Fatal(date, w.Code)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("do not expose\n"), 0600)
	if err := os.Symlink(outside, filepath.Join(dir, "app-2026-10-17.log")); err != nil {
		t.Fatal(err)
	}
	w = request(v, "POST", "/query", `{"source":"file","channel":"file","date":"2026-10-17"}`, true)
	if w.Code != 403 || strings.Contains(w.Body.String(), "do not expose") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(v, "GET", "/sources", "", true)
	if strings.Contains(w.Body.String(), "2026-10-17") {
		t.Fatal("symlink listed", w.Body.String())
	}
	if w = request(v, "POST", "/query", `{"source":"file","channel":"file","date":"2026-10-16"}`, true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w = request(v, "POST", "/query", `{"source":"file","channel":"file","date":"2026-10-18","before":999999}`, true); w.Code != 409 {
		t.Fatal(w.Code)
	}
}

func TestSingleFileScanBoundsAndPartialWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "single.log")
	line := []byte("INFO " + strings.Repeat("x", 1024) + "\n")
	if err := os.WriteFile(path, bytes.Repeat(line, 2200), 0600); err != nil {
		t.Fatal(err)
	}
	v := viewer(t, config(), map[string]logging.Channel{"file": {Driver: "file", Path: path}})
	p := query(t, v, map[string]any{"source": "file", "channel": "file", "search": "not-present"})
	if len(p.Entries) != 0 || !p.Limited || p.Next == 0 || p.Next >= int64(len(line)*2200) {
		t.Fatal(p)
	}
	if err := os.WriteFile(path, []byte("INFO complete\nINFO partial"), 0600); err != nil {
		t.Fatal(err)
	}
	p = query(t, v, map[string]any{"source": "file", "channel": "file"})
	if len(p.Entries) != 1 || !strings.Contains(p.Entries[0].Message, "complete") {
		t.Fatal(p)
	}
}

func TestViewerPollingDoesNotFloodCapture(t *testing.T) {
	v := viewer(t, config(), map[string]logging.Channel{"stdout": {Driver: "stdout"}})
	logger := slog.New(v.WrapHandler("stdout", slog.NewJSONHandler(&safeDiscard{}, nil)))
	logger.Info("HTTP request", "route", logviewer.RootPath+"/", "status", 200)
	logger.Warn("HTTP request", "route", logviewer.RootPath+"/", "status", 401)
	logger.Info("HTTP request", "route", "/api/v1/users", "status", 200)
	p := query(t, v, map[string]any{})
	if len(p.Entries) != 2 {
		t.Fatal(p)
	}
}

func TestInvalidViewerConfiguration(t *testing.T) {
	for _, change := range []func(*logviewer.Config){func(c *logviewer.Config) { c.Password = "" }, func(c *logviewer.Config) { c.Username = "a:b" }, func(c *logviewer.Config) { c.BufferBytes = 1 }, func(c *logviewer.Config) { c.BufferEntries = 100001 }} {
		cfg := config()
		change(&cfg)
		if _, err := logviewer.New(cfg, nil); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

// Benchmark records the cost of an enabled local capture without file or network I/O.
func BenchmarkCapture(b *testing.B) {
	v, err := logviewer.New(config(), map[string]logging.Channel{"stdout": {Driver: "stdout"}})
	if err != nil {
		b.Fatal(err)
	}
	h := v.WrapHandler("stdout", slog.NewJSONHandler(&safeDiscard{}, nil))
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "request completed", 0)
	r.AddAttrs(slog.String("request_id", fmt.Sprint(123)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = h.Handle(context.Background(), r)
	}
}

func TestFileHistoryRecognizesTintLevels(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		t.Run(level.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.log")
			var out bytes.Buffer
			// Use the original Tint handler to cover files written before full level names.
			logger := slog.New(tint.NewTextHandler(&out, &tint.Options{Level: slog.LevelDebug, NoColor: true}))
			logger.Log(context.Background(), level, "severity probe")
			if err := os.WriteFile(path, out.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			v := viewer(t, config(), map[string]logging.Channel{"file": {Driver: "file", Path: path}})
			p := query(t, v, map[string]any{"source": "file", "channel": "file", "level": level.String()})
			if len(p.Entries) != 1 || p.Entries[0].Level != level.String() {
				t.Fatalf("level %s not recognized in %q: %+v", level, out.String(), p)
			}
		})
	}
}
