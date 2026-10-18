package middleware_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	native "github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/wisalgroup/labena/internal/platform/messaging/middleware"
)

func TestRetryReportsOnlyFinalFailure(t *testing.T) {
	for _, failures := range []int{1, 2} {
		t.Run(map[int]string{1: "recovers", 2: "exhausted"}[failures], func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			attempts := 0
			failure := errors.New("private-provider-marker")
			handler := middleware.TraceHandler(noop.NewTracerProvider().Tracer("test"), nil, logger, "gochannel")(func(*message.Message) ([]*message.Message, error) {
				attempts++
				if attempts <= failures {
					return nil, failure
				}
				return nil, nil
			})
			retry := native.Retry{MaxRetries: 1, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond, Multiplier: 1, ResetContextOnRetry: true}
			wrapped := middleware.LogFailures(logger)(retry.Middleware(handler))
			msg := message.NewMessageWithContext(t.Context(), "id", nil)
			before := msg.Context()
			_, err := wrapped(msg)
			if (err != nil) != (failures == 2) || attempts != 2 || msg.Context() != before {
				t.Fatalf("result %v, attempts %d, lost context %t", err, attempts, msg.Context() != before)
			}
			want := 0
			if failures == 2 {
				want = 1
			}
			if strings.Count(output.String(), `"msg":"message processing failed"`) != want {
				t.Fatal("wrong terminal log count", output.String())
			}
			if strings.Contains(output.String(), "retry_budget_exhausted") || strings.Contains(output.String(), "private-provider-marker") {
				t.Fatal("misleading or unsafe diagnostics", output.String())
			}
		})
	}
}
