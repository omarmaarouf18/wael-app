// Package middleware rate limiting for the API Gateway (Redis-backed, fail-closed).
package middleware

import (
	"net/http"
	"time"

	"github.com/omarmaarouf18/wael-app/api-gateway/internal/iputil"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
)

// RateLimiter wraps a Redis-backed limiter with trusted-proxy-aware IP resolution.
type RateLimiter struct {
	rl             *ratelimit.RateLimiter
	trustedProxies []string
}

// NewRateLimiter creates a gateway rate limiter.
func NewRateLimiter(rl *ratelimit.RateLimiter, trustedProxies ...[]string) *RateLimiter {
	var tp []string
	if len(trustedProxies) > 0 {
		tp = trustedProxies[0]
	}
	return &RateLimiter{rl: rl, trustedProxies: tp}
}

// CheckAndRecord records a hit for key.
func (rl *RateLimiter) CheckAndRecord(key string) (bool, time.Duration) {
	return rl.rl.CheckAndRecord(key)
}

func (rl *RateLimiter) getIP(r *http.Request) string {
	return iputil.ResolveClientIP(r, rl.trustedProxies)
}

// RateLimit enforces per-IP rate limits, responding 429 with Retry-After on excess.
func RateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			ip := limiter.getIP(r)
			if limited, retryAfter := limiter.CheckAndRecord("gw:" + ip); limited {
				w.Header().Set("Retry-After", time.Duration(retryAfter.Seconds()).String())
				handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
					"error": "rate limit exceeded, retry later",
					"code":  "rate_limited",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
