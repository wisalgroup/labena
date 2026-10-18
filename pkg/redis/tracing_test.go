package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	redis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRedisHookMissAndPipelineFailure(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	defer tp.Shutdown(context.Background())
	hook := NewTracingHook(tp.Tracer("test"))
	cmd := redis.NewCmd(context.Background(), "GET", "secret-key")
	if err := hook.ProcessHook(func(context.Context, redis.Cmder) error { return redis.Nil })(context.Background(), cmd); !errors.Is(err, redis.Nil) {
		t.Fatal("miss changed")
	}
	cmds := []redis.Cmder{cmd, redis.NewCmd(context.Background(), "secret-custom-command")}
	failure := errors.New("secret failure")
	err := hook.ProcessPipelineHook(func(ctx context.Context, cmds []redis.Cmder) error {
		cmds[0].SetErr(redis.Nil)
		cmds[1].SetErr(failure)
		return redis.Nil
	})(context.Background(), cmds)
	if !errors.Is(err, redis.Nil) {
		t.Fatal("pipeline result changed")
	}
	ended := spans.Ended()
	if len(ended) != 2 || ended[0].Status().Code == codes.Error || ended[1].Status().Code != codes.Error {
		t.Fatal("incorrect pipeline/miss status")
	}
	for _, span := range ended {
		if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status(), span.Name()), "secret") {
			t.Fatal("data leaked")
		}
	}
	if redisOperation("secret-custom-command") != "OTHER" {
		t.Fatal("unbounded command name")
	}
}
