package middleware

import (
	"log/slog"

	"github.com/ThreeDotsLabs/watermill/message"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/wisalgroup/labena/pkg/logging"
)

// TraceHandler starts an independent consumer trace for every handler
// invocation and links it to the producer trace propagated with the message.
//
// Using a link instead of a parent relationship is useful for asynchronous
// fanout: every consumer gets its own trace while remaining correlated with
// the producer.
func TraceHandler(
	tracer trace.Tracer,
	propagator propagation.TextMapPropagator,
	logger *slog.Logger,
	system string,
) message.HandlerMiddleware {
	if propagator == nil {
		propagator = propagation.TraceContext{}
	}

	return func(next message.HandlerFunc) message.HandlerFunc {
		return func(msg *message.Message) (
			out []*message.Message,
			err error,
		) {
			original := msg.Context()
			transport := system

			producerCtx := propagator.Extract(
				original,
				propagation.MapCarrier(msg.Metadata),
			)

			topic := message.SubscribeTopicFromCtx(original)
			handler := message.HandlerNameFromCtx(original)

			options := []trace.SpanStartOption{
				trace.WithNewRoot(),
				trace.WithSpanKind(trace.SpanKindConsumer),
				trace.WithAttributes(
					attribute.String(
						"messaging.system",
						transport,
					),
					attribute.String(
						"messaging.destination.name",
						topic,
					),
					attribute.String(
						"messaging.operation.name",
						"process",
					),
					attribute.String(
						"messaging.operation.type",
						"process",
					),
					attribute.String(
						"messaging.message.id",
						msg.UUID,
					),
					attribute.String(
						"watermill.handler.name",
						handler,
					),
				),
			}

			if sc := trace.SpanContextFromContext(producerCtx); sc.IsValid() {
				options = append(
					options,
					trace.WithLinks(trace.Link{
						SpanContext: sc,
					}),
				)
			}

			ctx, span := tracer.Start(
				original,
				"process "+topic,
				options...,
			)

			ctx = logging.WithLogger(
				ctx,
				logger.With(
					"message_id", msg.UUID,
					"handler", handler,
					"topic", topic,
				),
			)

			msg.SetContext(ctx)

			defer func() {
				msg.SetContext(original)

				if err != nil {
					span.SetStatus(
						codes.Error,
						"message processing failed",
					)

					span.SetAttributes(
						attribute.String(
							"error.type",
							"messaging.process_failed",
						),
					)

				}

				span.End()
			}()

			return next(msg)
		}
	}
}

// LogFailures logs messages that leave the middleware chain with an error.
//
// Place this middleware at the point where an error actually means processing
// has been exhausted. If retry middleware wraps handlers internally, ordering
// determines whether this sees every attempt or only the terminal failure.
func LogFailures(
	logger *slog.Logger,
) message.HandlerMiddleware {
	return func(next message.HandlerFunc) message.HandlerFunc {
		return func(
			msg *message.Message,
		) ([]*message.Message, error) {
			out, err := next(msg)
			if err == nil {
				return out, nil
			}

			ctx := msg.Context()

			logger.ErrorContext(
				ctx,
				"message processing failed",
				"error_category", logging.ErrorCategory(err),
				"message_id", msg.UUID,
				"handler",
				message.HandlerNameFromCtx(ctx),
				"topic",
				message.SubscribeTopicFromCtx(ctx),
			)

			return out, err
		}
	}
}
