package logging

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// Correlation is evaluated per record, so nested spans and loggers selected from
// named channels use the current operation rather than a captured request span.
type traceHandler struct{ slog.Handler }

func withTracing(h slog.Handler) slog.Handler {
	if _, ok := h.(*traceHandler); ok {
		return h
	}
	return &traceHandler{Handler: h}
}

func (h *traceHandler) Handle(ctx context.Context, record slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		record = record.Clone()
		record.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()), slog.Bool("trace_sampled", sc.IsSampled()))
	}
	return h.Handler.Handle(ctx, record)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return withTracing(h.Handler.WithAttrs(attrs))
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return withTracing(h.Handler.WithGroup(name))
}
