package tracing

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// NewHTTPTransport wraps a borrowed transport for a deliberately chosen integration.
// It injects W3C trace context, never baggage. Use only for destinations allowed to
// receive trace metadata. Spans end on response headers, not body consumption.
// URLs, headers, bodies and raw errors are never recorded. Nil base uses DefaultTransport.
// The tracer must be non-nil. The caller owns connection cleanup and client timeouts/redirect policy.
func NewHTTPTransport(base http.RoundTripper, tracer trace.Tracer) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &httpTransport{base: base, tracer: tracer}
}

type httpTransport struct {
	base   http.RoundTripper
	tracer trace.Tracer
}

func (t *httpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
	default:
		method = "_OTHER"
	}
	ctx, span := t.tracer.Start(req.Context(), "HTTP "+method, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("http.request.method", method)))
	defer span.End()
	cloned := req.Clone(ctx)
	if cloned.Header == nil {
		cloned.Header = make(http.Header)
	}
	// Never forward stale context or baggage supplied by a previous request.
	cloned.Header.Del("traceparent")
	cloned.Header.Del("tracestate")
	cloned.Header.Del("baggage")
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(cloned.Header))
	response, err := t.base.RoundTrip(cloned)
	if err != nil {
		span.SetStatus(codes.Error, "HTTP request failed")
	} else if response != nil {
		span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
		if response.StatusCode >= 400 {
			span.SetStatus(codes.Error, "HTTP response error")
		}
	}
	return response, err
}
func (t *httpTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
