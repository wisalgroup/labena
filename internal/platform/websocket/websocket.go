// Package websocket mounts bounded WebSocket sessions on the native HTTP mux.
// Authentication and message authorization remain explicit caller responsibilities.
package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	wire "github.com/coder/websocket"

	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/pkg/apperror"
)

const (
	defaultMaxMessageBytes = 64 << 10
	defaultWriteTimeout    = 10 * time.Second
	defaultPingInterval    = 30 * time.Second
	defaultPingTimeout     = 10 * time.Second
)

// MessageType identifies a text or binary WebSocket message.
type MessageType = wire.MessageType

// StatusCode identifies a WebSocket close reason.
type StatusCode = wire.StatusCode

// Standard message and close codes used by session handlers.
const (
	StatusNormalClosure   = wire.StatusNormalClosure
	StatusGoingAway       = wire.StatusGoingAway
	StatusPolicyViolation = wire.StatusPolicyViolation
	StatusInternalError   = wire.StatusInternalError
	MessageText           = wire.MessageText
	MessageBinary         = wire.MessageBinary
)

// ErrMessageTooLarge indicates an outbound message exceeds the configured bound.
var ErrMessageTooLarge = errors.New("websocket: message exceeds configured limit")

// Config bounds each session. Zero limits use safe defaults.
// AllowedOrigins contains additional exact HTTP(S) origins, with no wildcards.
// Same-origin and clients without an Origin header are allowed; neither authenticates
// the caller. Configuration is copied by New and must not be mutated during New.
type Config struct {
	AllowedOrigins  []string
	MaxMessageBytes int64
	WriteTimeout    time.Duration
	PingInterval    time.Duration
	PingTimeout     time.Duration
}

// Handler owns each connection for the duration of its callback. It is safe for
// concurrent requests. It does not register a route or start background work.
type Handler struct {
	cfg    Config
	handle func(context.Context, *Conn) error
}

// New creates a handler with defaults of 64 KiB per message, 10s writes,
// 30s heartbeat interval and 10s heartbeat timeout. Negative limits are invalid.
// The callback must continuously read to process control frames, honor cancellation,
// and join its goroutines before returning. Wrap private routes in authentication
// middleware before mounting; Huma group middleware does not cover native mux routes.
func New(cfg Config, handle func(context.Context, *Conn) error) (*Handler, error) {
	if handle == nil {
		return nil, errors.New("websocket: session handler is required")
	}
	if cfg.MaxMessageBytes == 0 {
		cfg.MaxMessageBytes = defaultMaxMessageBytes
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = defaultWriteTimeout
	}
	if cfg.PingInterval == 0 {
		cfg.PingInterval = defaultPingInterval
	}
	if cfg.PingTimeout == 0 {
		cfg.PingTimeout = defaultPingTimeout
	}
	if cfg.MaxMessageBytes < 0 || cfg.MaxMessageBytes == 1<<63-1 || cfg.WriteTimeout < 0 || cfg.PingInterval < 0 || cfg.PingTimeout < 0 {
		return nil, errors.New("websocket: limits and timeouts must be positive and message limit must leave room for an overflow byte")
	}
	cfg.AllowedOrigins = slices.Clone(cfg.AllowedOrigins)
	for _, origin := range cfg.AllowedOrigins {
		if !validOrigin(origin) {
			return nil, errors.New("websocket: allowed origins must be exact HTTP(S) origins")
		}
	}
	return &Handler{cfg: cfg, handle: handle}, nil
}

// Conn provides bounded message I/O. One goroutine may read; writes may be
// concurrent and are serialized by the protocol library. Do not retain Conn after
// the handler returns. No application message queue or broadcast hub is created.
type Conn struct {
	conn         *wire.Conn
	limit        int64
	writeTimeout time.Duration
}

// Read reads one complete message and processes control frames. Context deadlines
// or cancellation terminate the connection; inbound messages are size-limited.
func (c *Conn) Read(ctx context.Context) (MessageType, []byte, error) {
	return c.conn.Read(ctx)
}

// Write writes one message with the earlier of the caller's deadline and the
// configured write timeout. Oversized payloads are rejected before transmission.
func (c *Conn) Write(ctx context.Context, kind MessageType, data []byte) error {
	if int64(len(data)) > c.limit {
		return ErrMessageTooLarge
	}
	ctx, cancel := context.WithTimeout(ctx, c.writeTimeout)
	defer cancel()
	return c.conn.Write(ctx, kind, data)
}

// Close performs the protocol library's bounded close handshake: up to five
// seconds to write the close frame and five seconds to receive the peer's close.
// Use a valid protocol code and a short, static public reason. It may run
// concurrently with Read and Write. Repeated calls may return a closed error.
func (c *Conn) Close(code StatusCode, reason string) error {
	return c.conn.Close(code, reason)
}

// ServeHTTP accepts HTTP/1.1 WebSocket upgrades, including over TLS (wss).
// HTTP/2 extended CONNECT is not supported. The HTTP server may serve HTTP/2 on
// the same TLS listener; WebSocket clients use its HTTP/1.1 upgrade path.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.originAllowed(r) {
		response.WriteHTTPError(r.Context(), w, apperror.Forbidden("WebSocket origin is not allowed."))
		return
	}
	// Origin verification above compares both scheme and host, and rejects duplicate
	// headers. Disable the library's looser host-only check only after that check.
	conn, err := wire.Accept(w, r, &wire.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		// Accept already wrote the handshake failure.
		return
	}
	conn.SetReadLimit(h.cfg.MaxMessageBytes)
	ctx, cancel := context.WithCancel(r.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(h.cfg.PingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, stop := context.WithTimeout(ctx, h.cfg.PingTimeout)
				err := conn.Ping(pingCtx)
				stop()
				if err != nil {
					cancel()
					_ = conn.CloseNow()
					return
				}
			}
		}
	}()
	defer func() { cancel(); _ = conn.CloseNow(); <-done }()
	session := &Conn{conn: conn, limit: h.cfg.MaxMessageBytes, writeTimeout: h.cfg.WriteTimeout}
	err = h.handle(ctx, session)
	status := wire.StatusNormalClosure
	if err != nil && wire.CloseStatus(err) == -1 && !errors.Is(err, context.Canceled) {
		response.LogError(ctx, err)
		status = wire.StatusInternalError
	}
	_ = session.Close(status, "")
}

func validOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" &&
		origin == u.Scheme+"://"+u.Host && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" &&
		!strings.ContainsAny(origin, "*\\ \t\r\n")
}

func (h *Handler) originAllowed(r *http.Request) bool {
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return true
	}
	if len(values) != 1 || !validOrigin(values[0]) {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if strings.EqualFold(values[0], scheme+"://"+r.Host) {
		return true
	}
	for _, allowed := range h.cfg.AllowedOrigins {
		if strings.EqualFold(values[0], allowed) {
			return true
		}
	}
	return false
}
