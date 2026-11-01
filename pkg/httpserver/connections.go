package httpserver

import (
	"crypto/tls"
	"net"
	"sync"
)

// upgradedConnections tracks hijacks without wrapping ResponseWriter, preserving
// the standard Hijacker and Flusher interfaces for WebSocket libraries.
type upgradedConnections struct {
	mu          sync.Mutex
	stopping    bool
	connections map[*trackedConn]struct{}
}

type trackedListener struct {
	net.Listener
	upgraded *upgradedConnections
}

func (l *trackedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &trackedConn{Conn: conn, upgraded: l.upgraded}, nil
}

type trackedConn struct {
	net.Conn
	upgraded *upgradedConnections
	closed   bool // protected by upgraded.mu
}

func (c *trackedConn) Close() error {
	c.upgraded.mu.Lock()
	c.closed = true
	delete(c.upgraded.connections, c)
	c.upgraded.mu.Unlock()
	return c.Conn.Close()
}

func (g *upgradedConnections) hijack(conn net.Conn) {
	// ServeTLS wraps our tracked connection. Close the underlying transport on
	// shutdown just as for plaintext upgrades; protocol close frames are handler-owned.
	if secure, ok := conn.(*tls.Conn); ok {
		conn = secure.NetConn()
	}
	c := conn.(*trackedConn) // every accepted underlying connection is tracked
	g.mu.Lock()
	stopping := g.stopping
	if !stopping && !c.closed {
		g.connections[c] = struct{}{}
	}
	g.mu.Unlock()
	if stopping {
		_ = c.Close()
	}
}

func (g *upgradedConnections) close() {
	g.mu.Lock()
	g.stopping = true
	connections := make([]*trackedConn, 0, len(g.connections))
	for c := range g.connections {
		connections = append(connections, c)
	}
	g.mu.Unlock()
	for _, c := range connections {
		_ = c.Close()
	}
}
