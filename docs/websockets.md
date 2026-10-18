# WebSocket integration

The HTTP server supports connection upgrades and tracks hijacked connections during shutdown. The application has no WebSocket endpoint, hub or room registry. This guide describes the transport lifecycle hooks available when adding your own protocol.

Choose a protocol implementation for your application and mount its handler on the server mux. This page describes the existing integration boundary without adding a dependency.

## Understand the available boundary

| Provided by the framework | Supplied by your application/integration |
| --- | --- |
| Standard HTTP handler mount and upgrade-compatible response handling | WebSocket protocol implementation and handshake |
| Request correlation and handler-lifetime logging | Authentication, origin checks, and message authorization |
| Tracking/closing hijacked connections during shutdown | Protocol close frames, heartbeats, backpressure, and reconnection |
| Application lifecycle ownership | Cross-replica broadcast, room state, history, and replay |

There is no built-in chat or realtime notification channel. Push notification
support is a separate [Expo capability](push.html); it does not provide an active
WebSocket connection or message-history protocol.

## Register a handler

Protocol upgrades are a platform transport integration, outside the typed business
API. Install an explicitly constructed `http.Handler` from platform HTTP setup,
where the native router is owned:

```go
server.Mux.Handle("GET /ws", socketHandler)
```

Here `server` is the platform's `*api.Server` and `socketHandler` is a standard
`http.Handler` implementing the handshake and session protocol. This illustrative
mount is not implemented by the boilerplate. It does not belong on
`dependencies.Targets.HTTP`, which is a Huma API. Keep native adapter details in
platform setup; modules and services retain standard contexts and business inputs.

## Keep the session inside the handler

The handler should remain active for the session lifetime. If it starts read/write goroutines, stop and join them before returning. Returning immediately while detached goroutines use app dependencies defeats the server's handler-drain ownership.

Pass `r.Context()` into application operations. A protocol read must also stop when its connection closes; context cancellation alone does not necessarily interrupt a library's blocking read.

Bound incoming message size, outbound queues, and write deadlines in the chosen implementation. Decide whether a slow consumer is disconnected or messages are dropped. Authenticate and check browser origins before upgrading, then authorize individual operations as needed.

## Configure the listener

WebSocket handlers share the application server's address and HTTP settings:

```dotenv
HTTP_ADDRESS=:8080
HTTP_READ_HEADER_TIMEOUT=5s
HTTP_SHUTDOWN_TIMEOUT=10s
```

HTTP limits govern the handshake and normal requests; protocol-level message limits and heartbeat behavior belong to the WebSocket implementation. Configure post-upgrade connection deadlines deliberately and verify how your library handles inherited deadlines.

Reverse proxies need upgrade forwarding and suitable connection timeouts.

## Shutdown behavior

When HTTP shutdown begins, the server stops admission and closes tracked upgraded connections. It then joins active handlers before their dependencies can be released.

This closes the transport connection; it does not automatically send a WebSocket close frame. If the product needs protocol-level farewell messages or session draining, implement and coordinate that behavior in the application. Keep it bounded within the app's shutdown budget.

Ordinary request contexts are preserved during HTTP drain, then canceled. Do not rely only on immediate context cancellation to notice WebSocket shutdown; socket closure is the direct signal at shutdown start.

## Logging and scaling

The built-in HTTP request log covers the handler lifetime, not individual frames. Add context logging for relevant session outcomes and errors without recording sensitive message bodies. Correlation and connection identifiers belong in logs rather than unbounded metric labels.

A process-local connection registry and GoChannel bus reach only that process. With multiple replicas, add an explicit cross-process broadcast design; the current bus cannot deliver to every replica. Match routing and retention to the product rather than treating queue workers as a broadcast hub.

Reconnect, replay, ordering, presence, and offline delivery remain application contracts. Define them before exposing those guarantees to clients.

## Verify a protocol integration

Use a real local HTTP server and a protocol client for tests that depend on
upgrades; a response recorder cannot establish socket behavior. Exercise:

- Authentication/origin rejection before upgrade.
- A permitted session exchanging messages without unbounded buffers.
- A slow or disconnected client and a blocked protocol read.
- Application shutdown with an active session and joined goroutines.
- Reconnect behavior and any promised replay/ordering rules.

Only advertise the semantics implemented by your chosen integration. The existing
HTTP shutdown support is a lifecycle building block, not proof of chat delivery
or multi-replica consistency. Continue with [HTTP handlers](handlers.md),
[application lifecycle](application-lifecycle.md), and [events](events.md).
