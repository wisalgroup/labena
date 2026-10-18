package middleware

import (
	"github.com/ThreeDotsLabs/watermill/message"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracedPublisher decorates a Watermill publisher with OpenTelemetry tracing.
//
// Caller-owned messages are never modified. Trace context is injected into
// copies of the outgoing messages.
type TracedPublisher struct {
	message.Publisher

	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	system     string
}

func NewTracedPublisher(
	publisher message.Publisher,
	tracer trace.Tracer,
	propagator propagation.TextMapPropagator,
	system string,
) *TracedPublisher {
	if propagator == nil {
		propagator = propagation.TraceContext{}
	}

	return &TracedPublisher{
		Publisher:  publisher,
		tracer:     tracer,
		propagator: propagator,
		system:     system,
	}
}

func (p *TracedPublisher) Publish(
	topic string,
	msgs ...*message.Message,
) error {
	if len(msgs) == 0 {
		return nil
	}

	parent := msgs[0].Context()

	options := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", p.system),
			attribute.String("messaging.destination.name", topic),
			attribute.String("messaging.operation.name", "publish"),
			attribute.String("messaging.operation.type", "send"),
		),
	}

	if len(msgs) == 1 {
		options = append(
			options,
			trace.WithAttributes(
				attribute.String(
					"messaging.message.id",
					msgs[0].UUID,
				),
			),
		)
	} else {
		options = append(
			options,
			trace.WithAttributes(
				attribute.Int(
					"messaging.batch.message_count",
					len(msgs),
				),
			),
		)

		options = appendMessageContextLinks(options, msgs[1:])
	}

	ctx, span := p.tracer.Start(
		parent,
		"publish "+topic,
		options...,
	)
	defer span.End()

	copies := make([]*message.Message, len(msgs))

	for i, msg := range msgs {
		copied := msg.Copy()
		copied.SetContext(ctx)

		p.propagator.Inject(
			ctx,
			propagation.MapCarrier(copied.Metadata),
		)

		copies[i] = copied
	}

	if err := p.Publisher.Publish(topic, copies...); err != nil {
		span.SetStatus(codes.Error, "publication failed")
		span.SetAttributes(
			attribute.String(
				"error.type",
				"messaging.publish_failed",
			),
		)

		return err
	}

	return nil
}

func appendMessageContextLinks(
	options []trace.SpanStartOption,
	msgs []*message.Message,
) []trace.SpanStartOption {
	links := make([]trace.Link, 0, len(msgs))

	for _, msg := range msgs {
		sc := trace.SpanContextFromContext(msg.Context())
		if !sc.IsValid() {
			continue
		}

		links = append(links, trace.Link{
			SpanContext: sc,
		})
	}

	if len(links) == 0 {
		return options
	}

	return append(options, trace.WithLinks(links...))
}
