package api

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net"
	"net/netip"
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

// PeerIPKey always uses the direct connection. Use ClientIPKey for trusted ingress.
// Arbitrary forwarding headers cannot change the budget. NAT clients share a limit.
func PeerIPKey(c huma.Context) (string, error) {
	host, _, err := net.SplitHostPort(c.RemoteAddr())
	if err != nil {
		return "", apperror.Validation("Invalid client address.")
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "", apperror.Validation("Invalid client address.")
	}
	hash := sha256.Sum256([]byte(addr.Unmap().String()))
	return "api:peer:" + hex.EncodeToString(hash[:]), nil
}

// ClientIPKey uses the address accepted by the server's trusted-proxy policy.
// Keep the key namespace stable for direct deployments upgrading this policy.
func ClientIPKey(c huma.Context) (string, error) {
	address, err := ClientIP(c.Context())
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(address))
	return "api:peer:" + hex.EncodeToString(hash[:]), nil
}
