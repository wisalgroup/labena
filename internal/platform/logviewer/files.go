package logviewer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/wisalgroup/labena/pkg/logging"
)

const maxScanBytes = 1 << 20

type source struct {
	Name   string    `json:"name"`
	Driver string    `json:"driver"`
	Files  []logFile `json:"files"`
}
type logFile struct {
	Date string `json:"date"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Only configured channel names and validated UTC dates reach filesystem lookup.
// HTTP clients never supply a path or glob. OpenRoot also contains symlink races.
func fileName(ch logging.Channel, date string) (string, error) {
	base := filepath.Base(ch.Path)
	if ch.FileMode != logging.FileDaily {
		if date != "" {
			return "", errQuery
		}
		return base, nil
	}
	if !validDate(date) {
		return "", errQuery
	}
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext) + "-" + date + ext, nil
}

func (v *Viewer) sources(ctx context.Context) ([]source, error) {
	result := []source{}
	for name, ch := range v.channels {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if ch.Driver != "stdout" && ch.Driver != "stderr" && ch.Driver != "file" {
			continue
		}
		s := source{Name: name, Driver: ch.Driver, Files: []logFile{}}
		if ch.Driver == "file" {
			files, err := listFiles(ch)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			s.Files = files
		}
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func listFiles(ch logging.Channel) ([]logFile, error) {
	files := []logFile{}
	root, err := os.OpenRoot(filepath.Dir(ch.Path))
	if err != nil {
		return files, err
	}
	defer root.Close()
	if ch.FileMode != logging.FileDaily {
		info, err := root.Lstat(filepath.Base(ch.Path))
		if err != nil {
			return files, err
		}
		if info.Mode().IsRegular() {
			files = append(files, logFile{Name: info.Name(), Size: info.Size()})
		}
		return files, nil
	}
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	// A dedicated log directory is expected. Bound directory enumeration too.
	entries, err := dir.ReadDir(4096)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) == 4096 {
		return nil, errors.New("logviewer: too many directory entries")
	}
	base := filepath.Base(ch.Path)
	ext := filepath.Ext(base)
	prefix := strings.TrimSuffix(base, ext) + "-"
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
			continue
		}
		date := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ext)
		if !validDate(date) {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		files = append(files, logFile{Date: date, Name: name, Size: info.Size()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Date > files[j].Date })
	return files, nil
}

func (v *Viewer) readFile(ctx context.Context, q query) (page, error) {
	result := page{Entries: []Entry{}}
	ch, ok := v.channels[q.Channel]
	if !ok || ch.Driver != "file" {
		return result, errQuery
	}
	name, err := fileName(ch, q.Date)
	if err != nil {
		return result, err
	}
	root, err := os.OpenRoot(filepath.Dir(ch.Path))
	if err != nil {
		return result, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, os.ErrPermission
	}
	// Nonblocking open prevents a raced FIFO replacement from stalling a request.
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return result, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, os.ErrPermission
	}
	end := info.Size()
	if q.Before > 0 {
		if q.Before > end {
			return result, errChanged
		}
		end = q.Before
	}
	start := max(int64(0), end-maxScanBytes)
	data := make([]byte, int(end-start))
	n, err := file.ReadAt(data, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return result, err
	}
	if n != len(data) {
		return result, errChanged
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// Only show complete lines; an active writer can still be appending the last.
	if q.Before == 0 && len(data) > 0 && data[len(data)-1] != '\n' {
		last := bytes.LastIndexByte(data, '\n')
		data = data[:last+1]
	}
	if start > 0 {
		var previous [1]byte
		if _, err := file.ReadAt(previous[:], start-1); err != nil {
			return result, err
		}
		if previous[0] != '\n' {
			first := bytes.IndexByte(data, '\n')
			if first < 0 {
				result.Next = start
				result.Limited = true
				return result, nil
			}
			start += int64(first + 1)
			data = data[first+1:]
		}
	}
	position := len(data)
	for position > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		lineEnd := position
		if data[lineEnd-1] == '\n' {
			lineEnd--
		}
		lineStart := bytes.LastIndexByte(data[:lineEnd], '\n') + 1
		e := parseEntry(data[lineStart:lineEnd], q.Channel)
		e.ID = start + int64(lineStart) + 1
		position = lineStart
		if e.Raw == "" || !q.matches(e) {
			continue
		}
		result.Entries = append(result.Entries, e)
		if len(result.Entries) == q.Limit {
			result.Next = start + int64(lineStart)
			return result, nil
		}
	}
	result.Next = start
	result.Limited = start > 0
	return result, nil
}
