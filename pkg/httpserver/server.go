// Package httpserver runs a standard HTTP handler with explicit startup and
// graceful shutdown. Routing and application middleware belong to the caller.
package httpserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Config selects listener protocols and bounds request/shutdown work.
// Empty TLS paths serve plaintext HTTP/1.1. TLS files are loaded once in Run;
// rotate certificates by restarting the server with the replacement files.
type Config struct {
	// TLSCertFile and TLSKeyFile must be supplied together to enable HTTPS.
	TLSCertFile string
	TLSKeyFile  string
	// HTTP2 enables TLS-negotiated HTTP/2 alongside HTTP/1.1. It requires TLS.
	// Cleartext HTTP/2 and HTTP/3 are not supported.
	HTTP2             bool
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

// Server is single-use. It owns its listener, but not the supplied handler.
// Hijacked connections (including WebSockets) are closed when shutdown begins.
// Protocol-level close frames and application session draining belong to handlers.
// Keep session work inside the handler so Run can join it before closing dependencies.
type Server struct {
	cfg     Config
	handler http.Handler
	ready   chan struct{}
	mu      sync.Mutex
	used    bool
	addr    net.Addr
}

// New validates settings without I/O. Run loads TLS credentials before binding.
func New(cfg Config, handler http.Handler) (*Server, error) {
	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return nil, errors.New("httpserver: TLS certificate and key must be configured together")
	}
	if cfg.HTTP2 && cfg.TLSCertFile == "" {
		return nil, errors.New("httpserver: HTTP/2 requires TLS")
	}
	if handler == nil {
		return nil, errors.New("httpserver: nil handler")
	}
	if cfg.Address == "" {
		cfg.Address = ":8080"
	}
	if cfg.ReadHeaderTimeout == 0 {
		cfg.ReadHeaderTimeout = 5 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 60 * time.Second
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}
	if cfg.MaxHeaderBytes == 0 {
		cfg.MaxHeaderBytes = 1 << 20
	}
	if cfg.ReadHeaderTimeout < 0 || cfg.ReadTimeout < 0 || cfg.WriteTimeout < 0 || cfg.IdleTimeout < 0 || cfg.ShutdownTimeout < 0 || cfg.MaxHeaderBytes < 0 {
		return nil, errors.New("httpserver: invalid limits or timeouts")
	}
	return &Server{cfg: cfg, handler: handler, ready: make(chan struct{})}, nil
}

// Ready closes once the listener is bound. It does not imply dependency health.
func (s *Server) Ready() <-chan struct{} { return s.ready }

// Addr returns the bound address, or nil before startup.
func (s *Server) Addr() net.Addr { s.mu.Lock(); defer s.mu.Unlock(); return s.addr }

// Run stops admission on cancellation and drains active handlers. Once the
// shutdown timeout expires, their contexts are canceled and connections closed.
// Handlers must honor cancellation; Run joins them before returning.
func (s *Server) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.used {
		s.mu.Unlock()
		return errors.New("httpserver: already started")
	}
	s.used = true
	s.mu.Unlock()
	var tlsConfig *tls.Config
	if s.cfg.TLSCertFile != "" {
		certificate, err := tls.LoadX509KeyPair(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("httpserver: load TLS credentials: %w", err)
		}
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.cfg.Address)
	if err != nil {
		return err
	}
	defer listener.Close()
	upgraded := &upgradedConnections{connections: make(map[*trackedConn]struct{})}
	listener = &trackedListener{Listener: listener, upgraded: upgraded}
	defer upgraded.close()
	work, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()
	var mu sync.Mutex
	var active sync.WaitGroup
	stopping := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if stopping {
			mu.Unlock()
			http.Error(w, "server stopping", http.StatusServiceUnavailable)
			return
		}
		active.Add(1)
		mu.Unlock()
		defer active.Done()
		s.handler.ServeHTTP(w, r)
	})
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(s.cfg.HTTP2)
	server := &http.Server{
		TLSConfig: tlsConfig, Protocols: protocols,
		Handler: handler, ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout: s.cfg.ReadTimeout, WriteTimeout: s.cfg.WriteTimeout,
		IdleTimeout: s.cfg.IdleTimeout, MaxHeaderBytes: s.cfg.MaxHeaderBytes,
		BaseContext: func(net.Listener) context.Context { return work },
		ConnState: func(conn net.Conn, state http.ConnState) {
			if state == http.StateHijacked {
				upgraded.hijack(conn)
			}
		},
	}
	s.mu.Lock()
	s.addr = listener.Addr()
	s.mu.Unlock()
	finished := make(chan error, 1)
	go func() {
		if tlsConfig != nil {
			finished <- server.ServeTLS(listener, "", "")
			return
		}
		finished <- server.Serve(listener)
	}()
	close(s.ready)
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-finished:
		finished = nil
	}
	mu.Lock()
	stopping = true
	mu.Unlock()
	drain, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancelDrain()
	upgraded.close()
	shutdownErr := server.Shutdown(drain)
	if shutdownErr != nil {
		cancelWork()
		_ = server.Close()
	}
	if finished != nil {
		serveErr = <-finished
	}
	cancelWork() // also releases upgraded handlers waiting on their request context
	active.Wait()
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, shutdownErr)
}
