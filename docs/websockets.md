# HTTP/2 and WebSockets

Labena serves HTTP/1.1 and TLS-negotiated HTTP/2 directly through Go's HTTP
server. WebSocket sessions use the framework's `internal/platform/websocket`
integration, backed by `github.com/coder/websocket`. No reverse proxy is required.
HTTP/3, cleartext HTTP/2 and HTTP/2 WebSocket extended CONNECT are not implemented.

## Enable HTTPS and HTTP/2

Plain HTTP/1.1 remains the default. To enable HTTPS with HTTP/2:

```dotenv
HTTP_ADDRESS=:8443
HTTP_TLS_CERT_FILE=/run/secrets/server.crt
HTTP_TLS_KEY_FILE=/run/secrets/server.key
HTTP_HTTP2_ENABLED=true
```

Supply both TLS files. The server loads them before binding or signaling readiness;
missing, unreadable or invalid credentials fail startup. TLS uses a minimum of
TLS 1.2. HTTP/2 requires TLS, and HTTP/1.1 remains available on the same listener.
With TLS configured but `HTTP_HTTP2_ENABLED=false`, the listener serves HTTPS
using HTTP/1.1 only. Certificate issuance and renewal are deployment responsibilities;
restart with the replacement files to rotate certificates. There is no automatic
certificate reload or separate HTTP-to-HTTPS redirect listener.

Clients negotiate HTTP/2 using ALPN. For example, an HTTP/2-capable curl can verify
the public endpoint with `curl --http2 -v https://api.example.com:8443/healthz`.
Use a certificate trusted by the client; changing only the URL to HTTPS does not
enable TLS on the server.

## Register a WebSocket handler

Construct a handler and mount it on the platform API server's native mux, in app
composition after constructing authentication:

```go
socket, err := websocket.New(websocket.Config{
    AllowedOrigins: []string{"https://app.example.com"},
}, func(ctx context.Context, conn *websocket.Conn) error {
    principal, err := auth.FromContext(ctx)
    if err != nil {
        return err
    }
    _ = principal // Pass trusted identity to the service handling each message.

    for {
        kind, message, err := conn.Read(ctx)
        if err != nil {
            return err
        }
        // Illustrative echo. Real handlers decode, authorize and call services.
        if err := conn.Write(ctx, kind, message); err != nil {
            return err
        }
    }
})
if err != nil {
    return err
}
server.Mux.Handle("GET /ws", middleware.BearerHTTP(authentication)(socket))
```

Import `context`, `internal/platform/auth`, `internal/platform/websocket`, and
`internal/platform/api/middleware` under your application module path.
Here `server` is the existing `*api.Server`, and `authentication` implements
`auth.Authenticator`. This is an integration pattern, not a route automatically
installed by the template. The framework does not ship an application message protocol.

`BearerHTTP` applies the same credential validation and error responses as Huma
bearer middleware. A nil authenticator fails closed. Native mux routes do not
inherit Huma group authentication or rate limiting. Apply relevant policies
explicitly. Authentication at connection time does not authorize every message
or automatically revalidate a session after logout; message handlers own those checks.

WebSockets use the HTTP/1.1 upgrade path, even when HTTP/2 is enabled on that
listener. Use `ws://` for local plaintext connections and `wss://` for TLS.
Normal HTTP request deadlines are cleared by Go when the connection is hijacked;
the socket's own limits take over.

### Browser authentication

Browser `WebSocket` APIs cannot set an Authorization header. `BearerHTTP` works
with clients that can set it; it does not accept credentials in query parameters.
For browsers, supply an explicit trusted session or short-lived ticket adapter
that authenticates before upgrade and stores the principal with `auth.WithPrincipal`.
Ticket issuance, expiry, replay prevention and per-message authorization belong to
the application. The framework does not silently enable cookie or query-token auth.

## Limits and origins

`websocket.Config` is supplied per endpoint; zero values select these defaults:

| Setting | Default | Behavior |
| --- | --- | --- |
| `AllowedOrigins` | none added | Permit same-origin and non-browser clients without Origin |
| `MaxMessageBytes` | 64 KiB | Bound each inbound and outbound message |
| `WriteTimeout` | 10s | Bound message writes |
| `PingInterval` | 30s | Send periodic ping frames |
| `PingTimeout` | 10s | Disconnect peers that do not answer |

Additional origins must be exact HTTP(S) origins including any port, without
paths or wildcards. Checks compare scheme and host; malformed, opaque and duplicate
Origin headers are rejected. Allowed origins are copied at construction.
Origin checking is independent of CORS and is not authentication. Behind a TLS
terminating proxy, configure the public browser origin explicitly; the socket
handler does not reinterpret forwarded protocol headers.

Read continuously so ping, pong and close frames can be processed. One goroutine
may read; concurrent writes are serialized and time-bounded. There is no unbounded
outbound queue. Oversized writes return `websocket.ErrMessageTooLarge`; oversized
incoming messages close with protocol code 1009. A session handler can explicitly
close with, for example, `conn.Close(websocket.StatusPolicyViolation, "Session ended.")`.
Use short, static public close reasons, never provider errors or secrets.

## Session lifetime and shutdown

The callback owns its session work and must honor its context. Stop and join
application goroutines before returning; do not retain the connection afterward.
The framework owns and joins its heartbeat goroutine. A missing heartbeat response
cancels the session and closes its socket.

Returning nil sends a normal close; unexpected callback errors are safely logged
and receive a generic internal-error close. The protocol library bounds close handshakes to five seconds for writing
and five seconds for receiving the peer close. HTTP shutdown closes tracked upgraded TCP connections, including
TLS connections, and joins handlers before dependencies are released. This forced
transport shutdown does not promise a WebSocket going-away frame. Clients must
handle abrupt disconnects and reconnect according to application policy.

There is no automatic room registry, cross-replica broadcast, replay, presence or
offline delivery. Expo push is a separate capability. Bound any application queues
and define delivery guarantees before implementing a broadcast layer.

## Run the local example

`examples/websocket` supplies an unauthenticated echo socket at `/ws` and a
`/protocol` endpoint reporting the negotiated HTTP protocol. It always binds to
loopback, using the configured HTTP port, and opens no database or Redis clients:

```sh
APP_ENV_FILE='' go run ./examples/websocket
```

For TLS, supply the certificate/key settings and enable HTTP/2 as above. The
example retains those settings. Connect a WebSocket client to
`ws://127.0.0.1:8080/ws`, or `wss://` with a certificate valid for the selected
address. The public application does not mount this demonstration route.

## Verification

Tests use real clients and listeners for HTTP/1.1/HTTP/2 TLS negotiation, graceful
HTTP/2 drain, ws/wss message exchange, principal propagation, origin/auth rejection,
oversized messages, heartbeat timeout, slow readers and shutdown with active
connections. Request-recorder-only tests cannot establish socket correctness.
Continue with [middleware](middleware.md), [HTTP handlers](handlers.md) and
[application lifecycle](application-lifecycle.md).
