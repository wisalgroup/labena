package logviewer

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/wisalgroup/labena/pkg/logging"
)

const maxEntryBytes = 16 << 10

// Entry is a bounded copy of a log line. Raw is always displayed as text.
type Entry struct {
	ID        int64  `json:"id"`
	Time      string `json:"time,omitempty"`
	Level     string `json:"level,omitempty"`
	Message   string `json:"message"`
	Channel   string `json:"channel"`
	Raw       string `json:"raw"`
	Truncated bool   `json:"truncated,omitempty"`
}

type buffer struct {
	mu                            sync.Mutex
	entries                       []Entry
	start, count, bytes, maxBytes int
	sequence                      int64
	evicted                       uint64
}

func (b *buffer) add(entry Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sequence++
	entry.ID = b.sequence
	size := entrySize(entry)
	for b.count > 0 && (b.count == len(b.entries) || b.bytes+size > b.maxBytes) {
		b.bytes -= entrySize(b.entries[b.start])
		b.entries[b.start] = Entry{}
		b.start = (b.start + 1) % len(b.entries)
		b.count--
		b.evicted++
	}
	if size > b.maxBytes {
		b.evicted++
		return
	}
	b.entries[(b.start+b.count)%len(b.entries)] = entry
	b.count++
	b.bytes += size
}

func entrySize(e Entry) int {
	return len(e.Raw) + len(e.Message) + len(e.Channel) + len(e.Level) + len(e.Time)
}

func (b *buffer) query(q query) page {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := page{Entries: []Entry{}, Evicted: b.evicted}
	for i := b.count - 1; i >= 0; i-- {
		e := b.entries[(b.start+i)%len(b.entries)]
		if q.Before > 0 && e.ID >= q.Before {
			continue
		}
		if !q.matches(e) {
			continue
		}
		if len(result.Entries) == q.Limit {
			result.Next = result.Entries[len(result.Entries)-1].ID
			break
		}
		result.Entries = append(result.Entries, e)
	}
	return result
}

type captureWriter struct {
	viewer  *Viewer
	channel string
}

func (w captureWriter) Write(p []byte) (int, error) {
	entry := parseEntry(p, w.channel)
	// Successful viewer polling must not fill its own live buffer. Authentication
	// failures and server errors remain visible, and all outputs still receive them.
	var access struct {
		Message string `json:"msg"`
		Route   string `json:"route"`
		Status  int    `json:"status"`
	}
	if len(p) <= maxEntryBytes && json.Unmarshal(p, &access) == nil && access.Message == "HTTP request" && access.Route == RootPath+"/" && access.Status >= 200 && access.Status < 400 {
		return len(p), nil
	}
	w.viewer.buffer.add(entry)
	return len(p), nil
}

// WrapHandler copies records accepted by local stdout, stderr and file channels.
// It does not capture unrelated fmt.Print, log.Default, child processes or other
// application instances. No global output is redirected or logger replaced.
func (v *Viewer) WrapHandler(name string, handler slog.Handler) slog.Handler {
	ch, ok := v.channels[name]
	if !ok || (ch.Driver != "stdout" && ch.Driver != "stderr" && ch.Driver != "file") {
		return handler
	}
	mirror, _ := logging.New(captureWriter{v, name}, logging.Options{Level: slog.Level(-1 << 30)})
	return &captureHandler{output: handler, mirror: mirror.Handler()}
}

type captureHandler struct{ output, mirror slog.Handler }

func (h *captureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.output.Enabled(ctx, level)
}
func (h *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.output.Handle(ctx, r.Clone())
	_ = h.mirror.Handle(ctx, r.Clone())
	return err
}
func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &captureHandler{h.output.WithAttrs(append([]slog.Attr(nil), attrs...)), h.mirror.WithAttrs(append([]slog.Attr(nil), attrs...))}
}
func (h *captureHandler) WithGroup(name string) slog.Handler {
	return &captureHandler{h.output.WithGroup(name), h.mirror.WithGroup(name)}
}

func parseEntry(p []byte, channel string) Entry {
	e := Entry{Channel: channel}
	if len(p) > maxEntryBytes {
		p = p[:maxEntryBytes]
		e.Truncated = true
	}
	e.Raw = strings.TrimSpace(string(p))
	var fields struct {
		Time    string `json:"time"`
		Level   string `json:"level"`
		Message string `json:"msg"`
	}
	if json.Unmarshal(p, &fields) == nil {
		e.Time, e.Level, e.Message = fields.Time, fields.Level, fields.Message
	} else {
		e.Message = stripANSI(e.Raw)
		// A long slog JSON message can be truncated after its time and level.
		// Read complete prefix fields so severity filtering still finds it.
		if e.Truncated {
			decoder := json.NewDecoder(bytes.NewReader(p))
			if token, err := decoder.Token(); err == nil && token == json.Delim('{') {
				for decoder.More() {
					key, err := decoder.Token()
					if err != nil {
						break
					}
					var value any
					if err := decoder.Decode(&value); err != nil {
						break
					}
					text, _ := value.(string)
					switch key {
					case "time":
						e.Time = text
					case "level":
						e.Level = text
					}
				}
			}
		}
		// Tint's default prefix is time.StampMilli followed by severity. Do not
		// infer severity from words that happen to occur later in the message.
		words := strings.Fields(e.Message)
		if len(words) >= 4 {
			if _, err := time.Parse(time.StampMilli, strings.Join(words[:3], " ")); err == nil {
				words = words[3:]
			}
		}
		if e.Level == "" && len(words) > 0 {
			switch words[0] {
			case "DEBUG", "DBG":
				e.Level = "DEBUG"
			case "INFO", "INF":
				e.Level = "INFO"
			case "WARN", "WRN":
				e.Level = "WARN"
			case "ERROR", "ERR":
				e.Level = "ERROR"
			}
		}
	}
	return e
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 27 && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

type query struct {
	Source  string `json:"source"`
	Channel string `json:"channel"`
	Date    string `json:"date"`
	Search  string `json:"search"`
	Level   string `json:"level"`
	Before  int64  `json:"before"`
	Limit   int    `json:"limit"`
}

func (q query) matches(e Entry) bool {
	return (q.Channel == "" || e.Channel == q.Channel) && (q.Level == "" || e.Level == q.Level) && (q.Search == "" || strings.Contains(strings.ToLower(e.Raw), q.Search))
}

type page struct {
	Entries []Entry `json:"entries"`
	Next    int64   `json:"next"`
	Evicted uint64  `json:"evicted"`
	Limited bool    `json:"limited"`
}

func validDate(date string) bool {
	t, err := time.Parse(time.DateOnly, date)
	return err == nil && t.Format(time.DateOnly) == date
}
