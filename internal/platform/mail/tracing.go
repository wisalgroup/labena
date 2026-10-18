package mail

import (
	"context"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// WithTracing borrows a sender and tracer. It records one Send attempt without
// recipients, content, headers, attachments or raw errors. Success means the
// underlying sender accepted the send, not delivery to the recipient's inbox.
// Both arguments must be non-nil. The sender's ownership and concurrency rules
// still apply. Decorate the sender before New to trace immediate and queued sends.
func WithTracing(sender Sender, tracer trace.Tracer) Sender {
	return tracedSender{sender: sender, tracer: tracer}
}

type tracedSender struct {
	sender Sender
	tracer trace.Tracer
}

func (m tracedSender) Send(ctx context.Context, message Message) error {
	ctx, span := m.tracer.Start(ctx, "mail.send", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	err := m.sender.Send(ctx, message)
	if err != nil {
		span.SetStatus(codes.Error, "mail send failed")
	}
	return err
}
