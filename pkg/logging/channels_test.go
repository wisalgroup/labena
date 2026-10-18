package logging

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestChannelsSelectionAndScopedFields(t *testing.T) {
	var normal, audit bytes.Buffer
	outputs := map[string]*bytes.Buffer{"normal": &normal, "audit": &audit}
	m, err := Open(Config{Default: []string{"normal"}, Channels: map[string]Channel{
		"normal": {Driver: "capture", Path: "normal"},
		"audit":  {Driver: "capture", Path: "audit", Options: Options{Level: slog.LevelWarn}},
	}, Drivers: map[string]Driver{"capture": func(ch Channel) (slog.Handler, io.Closer, error) {
		l, err := New(outputs[ch.Path], ch.Options)
		return l.Handler(), nil, err
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx := WithLogger(context.Background(), m.Default().With("request_id", "r1").WithGroup("order").With("id", 7))
	FromContext(ctx).InfoContext(ctx, "default only")
	selected, err := Channels(ctx, "audit")
	if err != nil {
		t.Fatal(err)
	}
	selected.InfoContext(ctx, "filtered")
	selected.WarnContext(ctx, "audit only")
	both, err := Select(selected, "normal", "audit")
	if err != nil {
		t.Fatal(err)
	}
	both.WarnContext(ctx, "both")
	if strings.Contains(normal.String(), "audit only") || strings.Contains(audit.String(), "default only") || strings.Contains(audit.String(), "filtered") {
		t.Fatalf("incorrect routing: normal=%s audit=%s", normal.String(), audit.String())
	}
	for _, output := range outputs {
		if !strings.Contains(output.String(), `"request_id":"r1"`) || !strings.Contains(output.String(), `"order":{"id":7}`) || strings.Count(output.String(), `"msg":"both"`) != 1 {
			t.Fatal(output.String())
		}
	}
	for _, names := range [][]string{nil, {"missing"}, {"normal", "normal"}} {
		if _, err := m.Logger(names...); err == nil {
			t.Fatalf("accepted %v", names)
		}
	}
	if _, err := Channels(context.Background(), "audit"); err == nil {
		t.Fatal("accepted unrelated logger")
	}
}

func TestFileChannelsAppendAndClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "app.log")
	cfg := Config{Default: []string{"file"}, Channels: map[string]Channel{"file": {Driver: "file", Path: path}}}
	for range 2 {
		m, err := Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for range 10 {
			wg.Go(func() { m.Default().Info("record") })
		}
		wg.Wait()
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 20 {
		t.Fatalf("lost records: %s", data)
	}
}

type testCloser struct {
	calls int
	err   error
}

func (c *testCloser) Close() error { c.calls++; return c.err }

func TestInvalidDecoratorClosesNewOutput(t *testing.T) {
	closer := &testCloser{}
	_, err := Open(Config{Default: []string{"test"}, Channels: map[string]Channel{"test": {Driver: "custom"}}, Drivers: map[string]Driver{"custom": func(Channel) (slog.Handler, io.Closer, error) {
		return slog.NewJSONHandler(io.Discard, nil), closer, nil
	}}, WrapHandler: func(string, slog.Handler) slog.Handler { return nil }})
	if err == nil || closer.calls != 1 {
		t.Fatal(err, closer.calls)
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestChannelFailuresAndOwnership(t *testing.T) {
	openErr, closeErr := errors.New("open failed"), errors.New("close failed")
	closer := &testCloser{err: closeErr}
	_, err := Open(Config{Default: []string{"a", "b"}, Channels: map[string]Channel{
		"a": {Driver: "custom"}, "b": {Driver: "broken"},
	}, Drivers: map[string]Driver{
		"custom": func(Channel) (slog.Handler, io.Closer, error) {
			return slog.NewJSONHandler(io.Discard, nil), closer, nil
		},
		"broken": func(Channel) (slog.Handler, io.Closer, error) { return nil, nil, openErr },
	}})
	if !errors.Is(err, openErr) || !errors.Is(err, closeErr) || closer.calls != 1 {
		t.Fatalf("partial cleanup: %v, %d", err, closer.calls)
	}

	writeErr := errors.New("write failed")
	var output bytes.Buffer
	var reported error
	m, err := Open(Config{Default: []string{"bad", "good"}, Channels: map[string]Channel{"bad": {Driver: "bad"}, "good": {Driver: "good"}}, Drivers: map[string]Driver{
		"bad": func(Channel) (slog.Handler, io.Closer, error) {
			return slog.NewJSONHandler(failingWriter{writeErr}, nil), nil, nil
		},
		"good": func(Channel) (slog.Handler, io.Closer, error) { return slog.NewJSONHandler(&output, nil), nil, nil },
	}, OnError: func(_ context.Context, name string, err error) {
		if name != "bad" {
			t.Error(name)
		}
		reported = err
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Default().Info("survives")
	if !errors.Is(reported, writeErr) || !strings.Contains(output.String(), "survives") {
		t.Fatalf("failure prevented fanout: %v %s", reported, output.String())
	}
}

func TestUnselectedFileDoesNotRequireWritableStorage(t *testing.T) {
	// A regular file as the parent makes creation fail even when tests run as root.
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Default: []string{"stderr"}, Channels: map[string]Channel{
		"stderr": {Driver: "stderr"},
		"file":   {Driver: "file", Path: filepath.Join(blocked, "app.log")},
	}}
	m, err := Open(cfg)
	if err != nil {
		t.Fatalf("unused file prevented startup: %v", err)
	}
	defer m.Close()
	if _, err := m.Logger("file"); err == nil {
		t.Fatal("explicit selection hid the file error")
	}
	if _, err := m.Logger("stderr"); err != nil {
		t.Fatal(err)
	}
	cfg.Default = []string{"file"}
	if _, err := Open(cfg); err == nil {
		t.Fatal("default file did not fail startup")
	}
}

func TestConcurrentSelectionOpensOnceAndCannotReopenAfterClose(t *testing.T) {
	opened := 0
	closer := &testCloser{}
	m, err := Open(Config{Default: []string{"stderr"}, Channels: map[string]Channel{
		"stderr": {Driver: "stderr"}, "custom": {Driver: "custom"},
	}, Drivers: map[string]Driver{"custom": func(Channel) (slog.Handler, io.Closer, error) {
		opened++
		return slog.NewJSONHandler(io.Discard, nil), closer, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if opened != 0 {
		t.Fatal("opened unused channel")
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			l, err := m.Logger("custom")
			if err != nil {
				t.Error(err)
				return
			}
			l.Info("hello")
		})
	}
	wg.Wait()
	if opened != 1 {
		t.Fatalf("opened %d times", opened)
	}
	for range 2 {
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if closer.calls != 1 {
		t.Fatalf("closed %d times", closer.calls)
	}
	if _, err := m.Logger("custom"); err == nil {
		t.Fatal("selection after close succeeded")
	}
}

func TestRelativeFilePathIsFixedAtOpen(t *testing.T) {
	root, elsewhere := t.TempDir(), t.TempDir()
	t.Chdir(root)
	cfg := Config{Default: []string{"stderr"}, Channels: map[string]Channel{
		"stderr": {Driver: "stderr"},
		"file":   {Driver: "file", Path: "storage/logs/app.log"},
	}}
	m, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := os.Stat(filepath.Join(root, "storage")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unused file channel touched storage: %v", err)
	}
	t.Chdir(elsewhere)
	logger, err := m.Logger("file")
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("original directory")
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "storage", "logs", "app.log"))
	if err != nil || !strings.Contains(string(data), "original directory") {
		t.Fatalf("log missing: %s %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "storage")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("used later working directory: %v", err)
	}
	if cfg.Channels["file"].Path != "storage/logs/app.log" {
		t.Fatal("mutated caller configuration")
	}
}
