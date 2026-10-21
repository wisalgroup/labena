package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/pkg/logging"
	"github.com/wisalgroup/labena/pkg/metrics"
	"github.com/wisalgroup/labena/pkg/tracing"
)

func TestHTTPRequestLogging(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		status int
		level  string
		route  string
	}{
		{"success", "/items/private-id?token=secret", 200, "INFO", "/items/{id}"},
		{"rejected", "/reject", 401, "WARN", "/reject"},
		{"failure", "/failure", 503, "ERROR", "/failure"},
		{"panic", "/panic", 500, "ERROR", "/panic"},
		{"missing", "/private-id?token=secret", 404, "WARN", "unmatched"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil)).With("correlation", "test")
			router := newAPI(t)
			router.Mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				r = r.WithContext(logging.WithLogger(ctx, logging.FromContext(ctx).With("operation", "read")))
				w.Write([]byte("ok"))
			})
			router.Mux.HandleFunc("GET /reject", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
			router.Mux.HandleFunc("GET /failure", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
			router.Mux.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) { panic("test panic") })
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer secret")
			req = req.WithContext(logging.WithLogger(req.Context(), logger))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status = %d", response.Code)
			}
			decoder := json.NewDecoder(&output)
			count := 0
			for {
				var record map[string]any
				if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if record["msg"] != "HTTP request" {
					continue
				}
				count++
				for key, want := range map[string]any{"server": "api", "method": "GET", "route": tc.route, "status": float64(tc.status), "level": tc.level, "correlation": "test"} {
					if record[key] != want {
						t.Errorf("%s = %v; want %v", key, record[key], want)
					}
				}
				if duration, ok := record["duration"].(float64); !ok || duration < 0 {
					t.Errorf("invalid duration: %v", record["duration"])
				}

				encoded, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "private-id") {
					t.Fatal("access log exposed raw request data")
				}
			}
			if count != 1 {
				t.Fatalf("access records = %d; want 1", count)
			}
		})
	}
}

func TestHTTPRecovery(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			var logs bytes.Buffer
			r := newAPI(t)
			r.Mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
				if committed {
					w.WriteHeader(202)
					w.Write([]byte("accepted"))
				}
				panic("private panic")
			})
			request := httptest.NewRequest("GET", "/", nil)
			request = request.WithContext(logging.WithLogger(request.Context(), slog.New(slog.NewJSONHandler(&logs, nil))))
			out := httptest.NewRecorder()
			var recovered any
			func() { defer func() { recovered = recover() }(); r.ServeHTTP(out, request) }()
			if committed {
				if recovered != http.ErrAbortHandler || out.Code != 202 || out.Body.String() != "accepted" {
					t.Fatal(recovered, out)
				}
			} else {
				var p huma.ErrorModel
				if err := json.Unmarshal(out.Body.Bytes(), &p); err != nil {
					t.Fatal(err)
				}
				if recovered != nil || out.Code != 500 || p.Type != "urn:labena:error:internal_error" || out.Header().Get("X-Request-ID") == "" {
					t.Fatal(recovered, out, p)
				}
			}
			if strings.Contains(logs.String(), "private") || strings.Count(logs.String(), `"msg":"HTTP operation failed"`) != 1 {
				t.Fatal(logs.String())
			}
		})
	}
}
func TestHTTPTracing(t *testing.T) {
	for _, tc := range []struct {
		name, path      string
		status          int
		trust, canceled bool
	}{
		{"success", "/items/private-id?token=secret", 200, false, false},
		{"trusted", "/items/private-id", 200, true, false},
		{"rejected", "/reject", 401, false, false},
		{"missing", "/secret", 404, false, false},
		{"panic", "/panic", 500, false, false},
		{"canceled", "/items/private-id", 200, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			defer provider.Shutdown(context.Background())
			var output bytes.Buffer
			logger, err := logging.New(&output, logging.Options{})
			if err != nil {
				t.Fatal(err)
			}
			router := newAPI(t, api.Options{Tracer: provider.Tracer("test"), TrustTraceContext: tc.trust})
			router.Mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
				logging.FromContext(r.Context()).InfoContext(r.Context(), "inside handler")
				w.Write([]byte("ok"))
			})
			router.Mux.HandleFunc("GET /reject", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
			router.Mux.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) { panic("private panic") })
			req := httptest.NewRequest("GET", tc.path, nil)
			req.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
			req.Header.Set("baggage", "secret=value")
			ctx := logging.WithLogger(req.Context(), logger)
			if tc.canceled {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			req = req.WithContext(ctx)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("HTTP status %d", response.Code)
			}
			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("spans %d", len(spans))
			}
			span := spans[0]
			if span.SpanKind() != trace.SpanKindServer {
				t.Fatal("not a server span")
			}
			if tc.trust {
				if span.Parent().SpanID().String() != "2222222222222222" || !span.Parent().IsRemote() {
					t.Fatal("remote parent lost")
				}
			} else if span.Parent().IsValid() {
				t.Fatal("untrusted parent accepted")
			}
			if (tc.status >= 500 || tc.canceled) != (span.Status().Code == codes.Error) {
				t.Fatalf("span status: %v", span.Status())
			}
			if strings.Contains(fmt.Sprint(span.Attributes()), "secret") || strings.Contains(span.Name(), "private-id") {
				t.Fatal("raw request data in span")
			}
			if tc.name == "missing" && span.Name() != "GET unmatched" {
				t.Fatal("unmatched path used as span name")
			}
			if strings.HasPrefix(tc.path, "/items/") && span.Name() != "GET /items/{id}" {
				t.Fatal(span.Name())
			}
			if strings.Contains(output.String(), "private panic") {
				t.Fatal("panic log exposed raw value")
			}
			decoder := json.NewDecoder(&output)
			for {
				var record map[string]any
				if err := decoder.Decode(&record); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if record["trace_id"] != span.SpanContext().TraceID().String() || record["span_id"] != span.SpanContext().SpanID().String() {
					t.Fatalf("uncorrelated record: %v", record)
				}
			}
		})
	}
}

func TestTracingPreservesWebSocketHijackAndStreaming(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(context.Background())
	router := newAPI(t, api.Options{Tracer: provider.Tracer("test")})
	router.Mux.HandleFunc("GET /stream", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("part")); w.(http.Flusher).Flush() })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/stream", nil))
	if !response.Flushed || response.Body.String() != "part" {
		t.Fatal("streaming changed")
	}
	router.Mux.HandleFunc("GET /socket", func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, err = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		if err == nil {
			err = rw.Flush()
		}
		if err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(router)
	defer server.Close()
	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprint(conn, "GET /socket HTTP/1.1\r\nHost: localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, "101") {
		t.Fatalf("upgrade: %s %v", line, err)
	}
	if len(recorder.Ended()) != 1 {
		t.Fatal("upgrade should not create a connection-lifetime span")
	}
}

func TestHTTPTraceHeaderSamplingPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, header   string
		trust, sampled bool
	}{
		{"public ignores remote sampling", "00-11111111111111111111111111111111-2222222222222222-00", false, true},
		{"trusted honors remote sampling", "00-11111111111111111111111111111111-2222222222222222-00", true, false},
		{"invalid context starts new trace", "invalid", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())))
			defer provider.Shutdown(context.Background())
			router := newAPI(t, api.Options{Tracer: provider.Tracer("test"), TrustTraceContext: tc.trust})
			router.Mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
				sc := trace.SpanContextFromContext(r.Context())
				if !sc.IsValid() || sc.IsSampled() != tc.sampled {
					t.Fatalf("span context: %v", sc)
				}
			})
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("traceparent", tc.header)
			req = req.WithContext(logging.WithLogger(req.Context(), slog.New(slog.NewTextHandler(io.Discard, nil))))
			router.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}

func TestHTTPMetricsUseBoundedLabelsAndFinalStatus(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p, err := metrics.New("test", logger)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(context.Background())
	router := newAPI(t, api.Options{Meter: p.Meter("test")})
	router.Mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	router.Mux.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) { panic("private") })
	for _, path := range []string{"/items/private-id?token=secret", "/unknown-private", "/panic"} {
		req := httptest.NewRequest("GET", path, nil)
		req = req.WithContext(logging.WithLogger(req.Context(), logger))
		router.ServeHTTP(httptest.NewRecorder(), req)
	}
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("CUSTOM-PRIVATE", "/unknown", nil))
	scrape := httptest.NewRecorder()
	p.Handler().ServeHTTP(scrape, httptest.NewRequest("GET", "/metrics", nil))
	text := scrape.Body.String()
	for _, want := range []string{"http_server_request_duration_seconds_count", `http_route="/items/{id}"`, `http_response_status_code="500"`, `http_route="unmatched"`, `http_request_method="_OTHER"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	for _, bad := range []string{"private-id", "token", "CUSTOM-PRIVATE", "unknown-private"} {
		if strings.Contains(text, bad) {
			t.Fatalf("unbounded data: %s", bad)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "http_server_active_requests{") && !strings.HasSuffix(line, " 0") {
			t.Fatalf("active request leaked: %s", line)
		}
	}
}

func TestHTTPContinuesWhileTraceCollectorIsBlocked(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		enteredOnce.Do(func() { close(entered) })
		<-release
		http.Error(w, "private collector response", http.StatusServiceUnavailable)
	}))
	defer collector.Close()
	defer releaseOnce.Do(func() { close(release) })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider, err := tracing.Open(t.Context(), tracing.Config{
		Enabled: true, ServiceName: "outage-test", Endpoint: collector.URL + "/v1/traces",
		SampleRatio: 1, ExportTimeout: 30 * time.Second, ShutdownTimeout: time.Second,
	}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		releaseOnce.Do(func() { close(release) })
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	router := newAPI(t, api.Options{Tracer: provider.Tracer("test")})
	completed := 0
	router.Mux.HandleFunc("POST /work", func(w http.ResponseWriter, r *http.Request) { completed++; w.WriteHeader(http.StatusCreated) })
	request := func() int {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/work", nil)
		req = req.WithContext(logging.WithLogger(req.Context(), logger))
		router.ServeHTTP(recorder, req)
		return recorder.Code
	}
	// The production exporter starts a batch after 512 completed spans.
	for range 512 {
		if request() != http.StatusCreated {
			t.Fatal("request failed")
		}
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("export did not start")
	}
	done := make(chan bool, 1)
	go func() {
		success := true
		// Exceed the production 2048-span buffer while the exporter is blocked.
		for range 4096 {
			if request() != http.StatusCreated {
				success = false
			}
		}
		done <- success
	}()
	select {
	case success := <-done:
		if !success || completed != 4608 {
			t.Fatal("business operations did not complete")
		}
	case <-time.After(5 * time.Second):
		releaseOnce.Do(func() { close(release) })
		<-done
		t.Fatal("requests blocked on collector")
	}
}

func newAPI(t testing.TB, options ...api.Options) *api.Server {
	t.Helper()
	var opts api.Options
	if len(options) > 0 {
		opts = options[0]
	}
	server, err := api.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestBodyDeadlineSurvivesHTTPMiddleware(t *testing.T) {
	server := newAPI(t)
	huma.Register(server, huma.Operation{Method: "POST", Path: "/", BodyReadTimeout: 50 * time.Millisecond}, func(context.Context, *struct{ Body typedBody }) (*struct{}, error) {
		t.Error("partial body reached handler")
		return nil, nil
	})
	listener := httptest.NewServer(server)
	defer listener.Close()
	conn, err := net.DialTimeout("tcp", listener.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"email\":"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 408 {
		data, _ := io.ReadAll(response.Body)
		t.Fatal(response.StatusCode, string(data))
	}
}
