package redis

import (
	"context"
	"errors"
	"strings"

	redis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// NewTracingHook returns a native hook. Attach it once before concurrent client use.
// It records bounded command names, never arguments, keys, addresses or error text.
// The tracer must be non-nil. The caller owns the client and provider. Dialing and pubsub receives are not traced.
func NewTracingHook(tracer trace.Tracer) redis.Hook { return tracingHook{tracer: tracer} }

type tracingHook struct{ tracer trace.Tracer }

func (h tracingHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h tracingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		op := redisOperation(cmd.Name())
		ctx, span := h.tracer.Start(ctx, op, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("db.system.name", "redis"), attribute.String("db.operation.name", op)))
		defer span.End()
		err := next(ctx, cmd)
		if err != nil && !errors.Is(err, redis.Nil) {
			span.SetStatus(codes.Error, "redis operation failed")
		}
		return err
	}
}
func (h tracingHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		ctx, span := h.tracer.Start(ctx, "redis.pipeline", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("db.system.name", "redis"), attribute.Int("db.operation.batch.size", len(cmds))))
		defer span.End()
		err := next(ctx, cmds)
		failed := err != nil && !errors.Is(err, redis.Nil)
		for _, cmd := range cmds {
			if cmd.Err() != nil && !errors.Is(cmd.Err(), redis.Nil) {
				failed = true
			}
		}
		if failed {
			span.SetStatus(codes.Error, "redis pipeline failed")
		}
		return err
	}
}
func redisOperation(name string) string {
	switch strings.ToUpper(name) {
	case "GET", "SET", "DEL", "MGET", "MSET", "EXISTS", "EXPIRE", "PEXPIRE", "TTL", "PTTL", "INCR", "DECR", "HGET", "HSET", "HGETALL", "LPUSH", "RPUSH", "LPOP", "RPOP", "BLPOP", "BRPOP", "SADD", "SREM", "ZADD", "ZREM", "ZRANGE", "EVAL", "EVALSHA", "SCRIPT", "XADD", "XREAD", "XREADGROUP", "XACK", "XGROUP", "XAUTOCLAIM", "XPENDING", "XDEL", "XTRIM", "PING", "MULTI", "EXEC":
		return strings.ToUpper(name)
	default:
		return "OTHER"
	}
}
