package tracing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPTransportPrivacyAndPropagation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		spans := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
		ctx, parent := tp.Tracer("test").Start(context.Background(), "parent")
		req, _ := http.NewRequestWithContext(ctx, "GET", "https://user:secret@example.test/private?token=secret", nil)
		req.Header.Set("baggage", "secret=value")
		req.Header.Set("traceparent", "stale")
		cause := errors.New("secret remote failure")
		body := io.NopCloser(strings.NewReader("secret response"))
		transport := NewHTTPTransport(roundTripFunc(func(got *http.Request) (*http.Response, error) {
			if got == req || got.Header.Get("baggage") != "" {
				t.Fatal("request not cloned/redacted")
			}
			remote := trace.SpanContextFromContext(propagation.TraceContext{}.Extract(context.Background(), propagation.HeaderCarrier(got.Header)))
			if remote.TraceID() != parent.SpanContext().TraceID() || remote.SpanID() == parent.SpanContext().SpanID() {
				t.Fatal("missing client propagation")
			}
			if fail {
				return nil, cause
			}
			return &http.Response{StatusCode: 503, Body: body}, nil
		}), tp.Tracer("client"))
		response, err := transport.RoundTrip(req)
		if fail && !errors.Is(err, cause) {
			t.Fatal("error identity changed")
		}
		if !fail && (err != nil || response.Body != body) {
			t.Fatal("response altered")
		}
		if req.Header.Get("traceparent") != "stale" || req.Header.Get("baggage") != "secret=value" {
			t.Fatal("caller headers mutated")
		}
		if len(spans.Ended()) != 1 {
			t.Fatal("span did not end at headers/error")
		}
		span := spans.Ended()[0]
		if span.Status().Code != codes.Error {
			t.Fatal("failure not recorded")
		}
		if strings.Contains(fmt.Sprint(span.Attributes(), span.Status(), span.Events(), span.Name()), "secret") {
			t.Fatal("private data leaked")
		}
		body.Close()
		parent.End()
		tp.Shutdown(context.Background())
	}
}
