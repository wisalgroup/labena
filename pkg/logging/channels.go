package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Channel configures one output. Open resolves relative file paths against the
// working directory once, including channels selected later.
type Channel struct {
	Driver  string
	Path    string
	Options Options
	// FileMode selects single-file (default) or daily output for the file driver.
	FileMode FileMode
	// KeepDays retains this many UTC calendar days, including today, in daily
	// mode. Zero keeps all files. Single-file mode requires zero.
	KeepDays int
	// OTLP configures the otlp driver. Other drivers do not use it.
	OTLP OTLPOptions
}

// Driver opens a concurrency-safe handler and optionally an owned resource.
// On failure it must clean up its partial initialization. Borrowed resources must
// return a nil closer. Handlers must obey the slog.Handler ownership contract.
// A closer implementing Shutdown(context.Context) error receives the manager's
// shutdown context; otherwise the manager calls Close.
type Driver func(Channel) (slog.Handler, io.Closer, error)

// Config defines named channels and the default fanout. Drivers adds custom
// drivers without global registration. Channels and Drivers are borrowed during Open.
type Config struct {
	Default  []string
	Channels map[string]Channel
	Drivers  map[string]Driver
	// WrapHandler optionally decorates each opened channel once, before deriving
	// scoped loggers. It must preserve Enabled, support concurrent use and return
	// a non-nil handler. The decorator borrows the output; Manager owns its lifetime.
	WrapHandler func(name string, handler slog.Handler) slog.Handler
	// OnError reports synchronous output failures, which slog.Logger otherwise discards.
	// Asynchronous OTLP export failures use independent stderr diagnostics.
	// It must be concurrency-safe and must not log back through these channels.
	// Nil reports failures directly to stderr.
	OnError func(context.Context, string, error)
}

func (c Config) Validate() error {
	for name, driver := range c.Drivers {
		if name == "" || driver == nil || name == "stdout" || name == "stderr" || name == "file" || name == "otlp" {
			return fmt.Errorf("logging: invalid or reserved driver %q", name)
		}
	}
	for name, ch := range c.Channels {
		if name == "" || strings.TrimSpace(name) != name {
			return errors.New("logging: invalid channel name")
		}
		if err := ch.Options.Format.Validate(); err != nil {
			return fmt.Errorf("logging: channel %q: %w", name, err)
		}
		if err := ch.validateFileOptions(); err != nil {
			return fmt.Errorf("logging: channel %q: %w", name, err)
		}
		switch ch.Driver {
		case "stdout", "stderr":
		case "file":
			if ch.Path == "" {
				return fmt.Errorf("logging: channel %q requires a file path", name)
			}
		case "otlp":
			if err := ch.OTLP.Validate(); err != nil {
				return fmt.Errorf("logging: channel %q: %w", name, err)
			}
			if ch.Options.Format == FormatTint {
				return fmt.Errorf("logging: channel %q: OTLP output cannot use tint", name)
			}
		default:
			if c.Drivers[ch.Driver] == nil {
				return fmt.Errorf("logging: channel %q has unknown driver %q", name, ch.Driver)
			}
		}
	}
	return validateSelection(c.Default, func(name string) bool { _, ok := c.Channels[name]; return ok })
}

func validateSelection(names []string, exists func(string) bool) error {
	if len(names) == 0 {
		return errors.New("logging: select at least one channel")
	}
	for i, name := range names {
		if !exists(name) {
			return fmt.Errorf("logging: unknown channel %q", name)
		}
		if slices.Contains(names[:i], name) {
			return fmt.Errorf("logging: duplicate channel %q", name)
		}
	}
	return nil
}

// Manager owns opened channel resources. Loggers support concurrent use; Close
// must run after their users finish. It never changes slog.Default.
type Manager struct {
	logger      *slog.Logger
	mu          sync.Mutex
	closed      bool
	channels    map[string]Channel
	drivers     map[string]Driver
	handlers    map[string]slog.Handler
	closers     []io.Closer
	once        sync.Once
	closeErr    error
	wrapHandler func(string, slog.Handler) slog.Handler
}

// Open validates all definitions and opens only default channels. Other channels
// open when explicitly selected. A failed selection can be retried. It closes
// acquired resources on failure. File output appends, creates directories with
// mode 0750 and new files with mode 0600. Daily files optionally expire by date;
// neither mode fsyncs each record.
func Open(cfg Config) (_ *Manager, err error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	m := &Manager{channels: maps.Clone(cfg.Channels), drivers: maps.Clone(cfg.Drivers), handlers: make(map[string]slog.Handler), wrapHandler: cfg.WrapHandler}
	for name, ch := range m.channels {
		if ch.Driver == "file" {
			path, err := filepath.Abs(ch.Path)
			if err != nil {
				return nil, fmt.Errorf("logging: resolve channel %q path: %w", name, err)
			}
			ch.Path = path
			m.channels[name] = ch
		}
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, m.Close())
		}
	}()
	h := &channelHandler{manager: m, names: slices.Clone(cfg.Default), onError: cfg.OnError}
	if h.onError == nil {
		h.onError = func(_ context.Context, name string, err error) {
			fmt.Fprintf(os.Stderr, "logging: channel %q: %v\n", name, err)
		}
	}
	h.handlers, err = m.open(cfg.Default)
	if err != nil {
		return nil, err
	}
	m.logger = slog.New(h)
	return m, nil
}

// open serializes acquisition with Close. Selection shares each opened output;
// deriving a logger never creates another file handle for the same channel.
func (m *Manager) open(names []string) (map[string]slog.Handler, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("logging: closed")
	}
	if err := validateSelection(names, func(name string) bool { _, ok := m.channels[name]; return ok }); err != nil {
		return nil, err
	}
	handlers := make(map[string]slog.Handler, len(names))
	for _, name := range names {
		handler := m.handlers[name]
		if handler == nil {
			ch := m.channels[name]
			driver := m.drivers[ch.Driver]
			if driver == nil {
				driver = openOutput
			}
			opened, closer, err := driver(ch)
			if err != nil {
				return nil, fmt.Errorf("logging: open channel %q: %w", name, err)
			}
			if opened == nil {
				err := fmt.Errorf("logging: driver for channel %q returned a nil handler", name)
				if closer != nil {
					err = errors.Join(err, closer.Close())
				}
				return nil, err
			}
			handler = withTracing(opened)
			if m.wrapHandler != nil {
				handler = m.wrapHandler(name, handler)
				if handler == nil {
					err := fmt.Errorf("logging: decorator for channel %q returned a nil handler", name)
					if closer != nil {
						err = errors.Join(err, closer.Close())
					}
					return nil, err
				}
			}
			if closer != nil {
				m.closers = append(m.closers, closer)
			}
			m.handlers[name] = handler
			handler = m.handlers[name]
		}
		handlers[name] = handler
	}
	return handlers, nil
}

func openOutput(ch Channel) (slog.Handler, io.Closer, error) {
	var output io.Writer
	var closer io.Closer
	switch ch.Driver {
	case "stdout":
		output = os.Stdout
	case "stderr":
		output = os.Stderr
	case "otlp":
		return openOTLP(ch)
	case "file":
		if ch.FileMode == FileDaily {
			file, err := openDailyFile(ch.Path, ch.KeepDays)
			if err != nil {
				return nil, nil, err
			}
			output, closer = file, file
			break
		}
		if err := os.MkdirAll(filepath.Dir(ch.Path), 0750); err != nil {
			return nil, nil, err
		}
		file, err := os.OpenFile(ch.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, nil, err
		}
		output, closer = file, file
	}
	logger, err := New(output, ch.Options)
	if err != nil {
		if closer != nil {
			err = errors.Join(err, closer.Close())
		}
		return nil, nil, err
	}
	return logger.Handler(), closer, nil
}

// Default returns the logger for the configured default channels.
func (m *Manager) Default() *slog.Logger { return m.logger }

// Logger selects only the named channels; it does not append default channels.
func (m *Manager) Logger(names ...string) (*slog.Logger, error) { return Select(m.logger, names...) }

func (m *Manager) Close() error { return m.Shutdown(context.Background()) }

// Shutdown closes outputs once, flushing OTLP within the caller's deadline and
// the channel's shutdown budget. Stop producers first. Close uses channel budgets.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.once.Do(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.closed = true
		for i := len(m.closers) - 1; i >= 0; i-- {
			if closer, ok := m.closers[i].(interface{ Shutdown(context.Context) error }); ok {
				m.closeErr = errors.Join(m.closeErr, closer.Shutdown(ctx))
			} else {
				m.closeErr = errors.Join(m.closeErr, m.closers[i].Close())
			}
		}
	})
	return m.closeErr
}

// Channels selects outputs from the context logger, preserving With/WithGroup
// fields. A logger created with New or an unrelated handler has no named channels.
func Channels(ctx context.Context, names ...string) (*slog.Logger, error) {
	return Select(FromContext(ctx), names...)
}

// Select derives a logger with different outputs and the same attached fields.
func Select(logger *slog.Logger, names ...string) (*slog.Logger, error) {
	if logger == nil {
		return nil, errors.New("logging: nil logger")
	}
	h, ok := logger.Handler().(*channelHandler)
	if !ok {
		return nil, errors.New("logging: logger has no named channels")
	}
	handlers, err := h.manager.open(names)
	if err != nil {
		return nil, err
	}
	for name, handler := range handlers {
		for _, scope := range h.scopes {
			handler = scope(handler)
		}
		handlers[name] = handler
	}
	return slog.New(&channelHandler{manager: h.manager, handlers: handlers, names: slices.Clone(names), onError: h.onError, scopes: h.scopes}), nil
}

type channelHandler struct {
	manager  *Manager
	scopes   []func(slog.Handler) slog.Handler
	handlers map[string]slog.Handler
	names    []string
	onError  func(context.Context, string, error)
}

func (h *channelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, name := range h.names {
		if h.handlers[name].Enabled(ctx, level) {
			return true
		}
	}
	return false
}
func (h *channelHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, name := range h.names {
		handler := h.handlers[name]
		if !handler.Enabled(ctx, record.Level) {
			continue
		}
		if err := handler.Handle(ctx, record.Clone()); err != nil {
			errs = append(errs, fmt.Errorf("channel %q: %w", name, err))
			h.onError(ctx, name, err)
		}
	}
	return errors.Join(errs...)
}
func (h *channelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	attrs = slices.Clone(attrs)
	return h.withScope(func(handler slog.Handler) slog.Handler { return handler.WithAttrs(slices.Clone(attrs)) })
}
func (h *channelHandler) WithGroup(name string) slog.Handler {
	return h.withScope(func(handler slog.Handler) slog.Handler { return handler.WithGroup(name) })
}

// Keep scope order so channels selected later receive the same fields and groups.
func (h *channelHandler) withScope(scope func(slog.Handler) slog.Handler) slog.Handler {
	next := &channelHandler{manager: h.manager, handlers: make(map[string]slog.Handler, len(h.handlers)), names: h.names, onError: h.onError, scopes: append(slices.Clone(h.scopes), scope)}
	for name, handler := range h.handlers {
		next.handlers[name] = scope(handler)
	}
	return next
}
