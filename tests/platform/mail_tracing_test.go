package platform_test

import (
	"context"

	"errors"
	"fmt"
	"github.com/wisalgroup/labena/internal/platform/mail"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type mailerFunc func(context.Context, mail.Message) error

func (f mailerFunc) Send(ctx context.Context, m mail.Message) error { return f(ctx, m) }
func TestTracedMailerPreservesMessageAndError(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	defer tp.Shutdown(context.Background())
	ctx, parent := tp.Tracer("test").Start(context.Background(), "parent")
	defer parent.End()
	failure := errors.New("secret SMTP response")
	mailer := mail.WithTracing(mailerFunc(func(ctx context.Context, msg mail.Message) error {
		if msg.Subject != "secret subject" || trace.SpanContextFromContext(ctx).TraceID() != parent.SpanContext().TraceID() {
			t.Fatal("message or context changed")
		}
		return failure
	}), tp.Tracer("mail"))
	if err := mailer.Send(ctx, mail.Message{Subject: "secret subject"}); !errors.Is(err, failure) {
		t.Fatal("error changed")
	}
	if len(spans.Ended()) != 1 {
		t.Fatal("span not ended")
	}
	span := spans.Ended()[0]
	if span.Status().Code != codes.Error || strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "secret") {
		t.Fatal("incorrect or unsafe span")
	}
}
