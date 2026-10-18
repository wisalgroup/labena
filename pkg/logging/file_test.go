package logging

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDailyRotationAndRetention(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"app-2026-10-09.log", "app-2026-10-10.log", "app-2026-10-11.log", "app-2026-10-20.log", "app.log", "other-2026-10-09.log", "app-invalid.log", "app-2026-10-09.log.bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("existing\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 12, 23, 59, 59, 0, time.UTC)
	if err := os.Mkdir(filepath.Join(dir, "app-2026-10-07.log"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "app.log"), filepath.Join(dir, "app-2026-10-08.log")); err != nil {
		t.Fatal(err)
	}
	w := &dailyFile{path: filepath.Join(dir, "app.log"), keepDays: 3, now: func() time.Time { return now }}
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.Write([]byte("before midnight\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "app-2026-10-09.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expired file retained", err)
	}
	now = now.Add(time.Second)
	if _, err := w.Write([]byte("after midnight\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "app-2026-10-10.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("retention did not advance", err)
	}
	for name, want := range map[string]string{
		"app-2026-10-12.log": "before midnight\n", "app-2026-10-13.log": "after midnight\n",
		"app-2026-10-11.log": "existing\n", "app-2026-10-20.log": "existing\n",
		"app.log": "existing\n", "other-2026-10-09.log": "existing\n",
		"app-invalid.log": "existing\n", "app-2026-10-09.log.bak": "existing\n",
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Errorf("%s: got %q, %v", name, got, err)
		}
	}
	for _, name := range []string{"app-2026-10-07.log", "app-2026-10-08.log"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			t.Errorf("removed directory or symlink: %s: %v", name, err)
		}
	}
}

func TestDailyUTCAndUnlimitedRetention(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "app-2000-01-01")
	if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	// The local calendar says October 13, but UTC is still October 12.
	now := time.Date(2026, 10, 13, 1, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	w := &dailyFile{path: filepath.Join(dir, "app"), now: func() time.Time { return now }}
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.Write([]byte("record")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{old, filepath.Join(dir, "app-2026-10-12")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDailyFailureRetriesAndClose(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	w := &dailyFile{path: filepath.Join(dir, "app.log"), now: func() time.Time { return now }}
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	now = now.AddDate(0, 0, 1)
	blocked := filepath.Join(dir, "app-2026-10-13.log")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Write([]byte("failed\n")); n != 0 || err == nil {
		t.Fatal("rotation failure was hidden")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("retried\n")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "app-2026-10-12.log"))
	if err != nil || string(got) != "first\n" {
		t.Fatal("wrote failed rotation into old file", err)
	}
	for range 2 {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Write([]byte("closed")); !errors.Is(err, os.ErrClosed) {
		t.Fatal("write after close accepted", err)
	}
}

func TestDailyChannelAppendsConcurrentlyAndPrunesOnOpen(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "app-2000-01-01.log")
	if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Default: []string{"file"}, Channels: map[string]Channel{
		"file": {Driver: "file", Path: filepath.Join(dir, "app.log"), FileMode: FileDaily, KeepDays: 7},
	}}
	for range 2 {
		m, err := Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() { m.Default().With("scoped", true).Info("record") })
		}
		wg.Wait()
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("opening did not prune old log", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "app-*.log"))
	if err != nil {
		t.Fatal(err)
	}
	records := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		records += strings.Count(string(data), "\n")
	}
	if records != 40 {
		t.Fatalf("got %d records, want 40", records)
	}
}

func TestFileModeValidation(t *testing.T) {
	for _, ch := range []Channel{
		{Driver: "file", Path: "app.log", FileMode: "weekly"},
		{Driver: "file", Path: "app.log", FileMode: FileDaily, KeepDays: -1},
		{Driver: "file", Path: "app.log", KeepDays: 7},
		{Driver: "stdout", FileMode: FileDaily},
	} {
		cfg := Config{Default: []string{"file"}, Channels: map[string]Channel{"file": ch}}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("accepted invalid options: %+v", ch)
		}
	}
}
