package middleware

import (
	"math"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/ratelimit"
)

// RateLimit applies one policy to a route group. Keys come from an explicit caller
// function; raw credentials and personal data should never be used as keys.
func RateLimit(limiter ratelimit.Limiter, limit ratelimit.Limit, key func(huma.Context) (string, error)) func(huma.Context, func(huma.Context)) {
	return func(c huma.Context, next func(huma.Context)) {
		if limiter == nil || key == nil {
			response.WriteError(c, apperror.Unavailable("Rate limiting is unavailable."))
			return
		}
		value, err := key(c)
		if err != nil {
			response.WriteError(c, err)
			return
		}
		decision, err := limiter.Allow(c.Context(), value, limit)
		if err != nil {
			response.WriteError(c, apperror.Unavailable("Rate limiting is temporarily unavailable."))
			return
		}
		if !decision.Allowed {
			seconds := int(math.Ceil(decision.RetryAfter.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			c.SetHeader("Retry-After", strconv.Itoa(seconds))
			response.WriteError(c, apperror.RateLimited("Too many requests. Try again later."))
			return
		}
		next(c)
	}
}
