package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/pkg/logging"
)

// observe surrounds every route, including infrastructure, CORS and unmatched
// requests. It preserves ResponseWriter capabilities and never records URLs,
// credentials, payloads, or arbitrary panic values in logs or telemetry.
func observe(mux *http.ServeMux, next http.Handler, options Options) (http.Handler, error) {
	var duration metric.Float64Histogram
	var active metric.Int64UpDownCounter
	if options.Meter != nil {
		var err error
		duration, err = options.Meter.Float64Histogram("http.server.request.duration", metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(.005, .01, .025, .05, .075, .1, .25, .5, .75, 1, 2.5, 5, 7.5, 10))
		if err != nil {
			return nil, err
		}
		active, err = options.Meter.Int64UpDownCounter("http.server.active_requests")
		if err != nil {
			return nil, err
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := uuid.NewString()
		ctx := response.WithRequestID(r.Context(), id)
		ctx = context.WithValue(ctx, peerKey{}, r.RemoteAddr)
		ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("request_id", id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-API-Version", options.APIVersion)
		_, pattern := mux.Handler(r)
		route := pattern
		if _, path, ok := strings.Cut(pattern, " "); ok {
			route = path
		}
		if route == "" {
			route = "unmatched"
		}
		method := httpMethod(r.Method)
		upgrade := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
		var span trace.Span
		if options.Tracer != nil && !upgrade {
			opts := []trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.request.method", method), attribute.String("http.route", route))}
			if options.TrustTraceContext {
				ctx = (propagation.TraceContext{}).Extract(ctx, propagation.HeaderCarrier(r.Header))
			} else {
				opts = append(opts, trace.WithNewRoot())
			}
			ctx, span = options.Tracer.Start(ctx, method+" "+route, opts...)
			defer span.End()
		}
		if active != nil && !upgrade {
			inFlight := metric.WithAttributes(attribute.String("server", "api"), attribute.String("http.request.method", method))
			active.Add(ctx, 1, inFlight)
			defer active.Add(ctx, -1, inFlight)
		}
		status, committed, hijacked := http.StatusOK, false, false
		commit := func(code int) {
			if !committed {
				status = code
				committed = true
			}
		}
		writer := httpsnoop.Wrap(w, httpsnoop.Hooks{
			WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
				return func(code int) {
					if code >= 200 || code == 101 {
						commit(code)
					}
					next(code)
				}
			},
			Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
				return func(p []byte) (int, error) { commit(200); return next(p) }
			},
			ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
				return func(r io.Reader) (int64, error) { commit(200); return next(r) }
			},
			Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc { return func() { commit(200); next() } },
			Hijack: func(next httpsnoop.HijackFunc) httpsnoop.HijackFunc {
				return func() (net.Conn, *bufio.ReadWriter, error) {
					c, rw, err := next()
					if err == nil {
						hijacked = true
					}
					return c, rw, err
				}
			},
		})
		defer func() {
			value := recover()
			abort := value != nil && (committed || hijacked || value == http.ErrAbortHandler)
			if value != nil {
				if value != http.ErrAbortHandler && (committed || hijacked) {
					response.LogError(ctx, errors.New("HTTP handler panic"))
				}
				if !committed && !hijacked && value != http.ErrAbortHandler {
					// Remove headers belonging to an incomplete response before writing a problem.
					writer.Header().Del("Content-Length")
					writer.Header().Del("Content-Encoding")
					response.WriteHTTPError(ctx, writer, errors.New("HTTP handler panic"))
				} else if !committed {
					status = 0
				}
			}
			elapsed := time.Since(started)
			if span != nil {
				span.SetAttributes(attribute.Int("http.response.status_code", status))
				if status >= 500 || abort {
					span.SetStatus(codes.Error, http.StatusText(status))
				}
				if ctx.Err() != nil {
					span.SetStatus(codes.Error, "request canceled")
					span.SetAttributes(attribute.String("error.type", "context_canceled"))
				}
			}
			if duration != nil && !upgrade {
				duration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attribute.String("server", "api"), attribute.String("http.request.method", method), attribute.String("http.route", route), attribute.Int("http.response.status_code", status)))
			}
			level := slog.LevelInfo
			if status >= 500 || abort {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			}
			logging.FromContext(ctx).LogAttrs(ctx, level, "HTTP request",
				slog.String("server", "api"), slog.String("method", method),
				slog.String("route", route), slog.Int("status", status),
				slog.Duration("duration", elapsed), slog.Bool("aborted", abort))
			if abort {
				panic(http.ErrAbortHandler)
			}
		}()
		client, err := resolveClientIP(r, options.TrustedProxies)
		if err != nil {
			response.WriteHTTPError(ctx, writer, err)
			return
		}
		ctx = context.WithValue(ctx, clientIPKey{}, client)
		next.ServeHTTP(writer, r.WithContext(ctx))
	}), nil
}
func httpMethod(method string) string {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
		return method
	default:
		return "_OTHER"
	}
}

// canonicalPath redirects an unregistered trailing-slash path to a registered
// resource. 308 preserves the HTTP method and body. Explicit subtree/slash routes
// keep ServeMux semantics, and escaped slashes inside parameters remain data.
func canonicalPath(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escaped := r.URL.EscapedPath()
		if len(escaped) > 1 && strings.HasSuffix(escaped, "/") {
			if _, pattern := mux.Handler(r); pattern == "" {
				candidate := new(http.Request)
				*candidate = *r
				url := *r.URL
				candidate.URL = &url
				url.Path = strings.TrimSuffix(url.Path, "/")
				url.RawPath = strings.TrimSuffix(escaped, "/")
				if _, pattern := mux.Handler(candidate); pattern != "" {
					location := url.EscapedPath()
					if url.ForceQuery || url.RawQuery != "" {
						location += "?" + url.RawQuery
					}
					http.Redirect(w, r, location, http.StatusPermanentRedirect)
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}
