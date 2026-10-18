package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileMode selects how the file driver names and rotates its output.
type FileMode string

const (
	FileSingle FileMode = "single"
	FileDaily  FileMode = "daily"
)

func (ch Channel) validateFileOptions() error {
	if ch.Driver != "file" {
		if ch.FileMode != "" || ch.KeepDays != 0 {
			return errors.New("file options require the file driver")
		}
		return nil
	}
	if ch.FileMode != "" && ch.FileMode != FileSingle && ch.FileMode != FileDaily {
		return fmt.Errorf("unknown file mode %q", ch.FileMode)
	}
	if ch.KeepDays < 0 || (ch.FileMode != FileDaily && ch.KeepDays != 0) {
		return errors.New("keep days must be nonnegative and requires daily mode")
	}
	return nil
}

// dailyFile serializes writes and rotation across derived slog handlers. It has
// no background worker: opening and the first write of a new UTC day prune files.
type dailyFile struct {
	mu       sync.Mutex
	path     string
	keepDays int
	now      func() time.Time
	file     *os.File
	date     string
	closed   bool
}

func openDailyFile(path string, keepDays int) (*dailyFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	w := &dailyFile{path: path, keepDays: keepDays, now: time.Now}
	if err := w.rotate(w.now().UTC()); err != nil {
		return nil, errors.Join(err, w.Close())
	}
	return w, nil
}

func (w *dailyFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	now := w.now().UTC()
	var rotateErr error
	if now.Format(time.DateOnly) != w.date {
		rotateErr = w.rotate(now)
		if now.Format(time.DateOnly) != w.date {
			return 0, rotateErr // Never write a new day's record into the old file.
		}
	}
	n, err := w.file.Write(p)
	return n, errors.Join(rotateErr, err)
}

func (w *dailyFile) rotate(now time.Time) error {
	date := now.Format(time.DateOnly)
	ext := filepath.Ext(w.path)
	path := strings.TrimSuffix(w.path, ext) + "-" + date + ext
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	var closeErr error
	if w.file != nil {
		closeErr = w.file.Close()
	}
	w.file, w.date = file, date
	return errors.Join(closeErr, w.prune(now))
}

func (w *dailyFile) prune(now time.Time) error {
	if w.keepDays == 0 {
		return nil
	}
	dir := filepath.Dir(w.path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	ext := filepath.Ext(w.path)
	prefix := strings.TrimSuffix(filepath.Base(w.path), ext) + "-"
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
			continue
		}
		date := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ext)
		day, err := time.Parse(time.DateOnly, date)
		if err != nil || day.Format(time.DateOnly) != date || int(now.Sub(day)/(24*time.Hour)) < w.keepDays || date == w.date {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (w *dailyFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}
