// Package middleware rate limiting for the API Gateway (Redis-backed, fail-closed).
package middleware

import (
	"net/http"
	"strings"
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

// RateLimit enforces per-IP rate limits only for unauthenticated routes (/api/v1/auth/*),
// responding 429 with Retry-After on excess.
//
// Carrier-Grade NAT (CGNAT) rationale:
// In mobile telecom networks (predominant among Egyptian mobile carriers such as Vodafone,
// Orange, Etisalat, and WE), thousands of mobile client devices are multiplexed behind
// shared public IPv4 addresses using Carrier-Grade NAT (CGNAT). Applying edge per-IP rate
// limiting to authenticated routes (/api/v1/academy/*, /api/v1/notifications/*) creates a severe
// risk of "noisy neighbor" collateral denial-of-service, where several students concurrently
// browsing courses, watching video lectures, or listening to notification streams on mobile
// networks would inadvertently exhaust the shared IP's rate limit quota and lock each other out.
//
// Therefore, the gateway enforces per-IP rate limiting strictly on unauthenticated endpoints
// (/api/v1/auth/*) to protect against unauthenticated brute-force attacks, credential stuffing,
// and OTP flooding before user identity is established. Authenticated routes bypass the edge
// per-IP limiter and rely entirely on per-user/per-account rate limits enforced in downstream
// services (e.g. academy-service TierLimiter keyed by student JWT user ID, notification-service
// stream caps) where individual accounts are isolated regardless of shared public IP.
func RateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Per-IP rate limiting applies exclusively to unauthenticated routes (/api/v1/auth/*).
			// Health checks and authenticated service routes (/api/v1/academy/*, /api/v1/notifications/*)
			// bypass the edge per-IP limiter to avoid CGNAT collateral denial of service.
			if !strings.HasPrefix(r.URL.Path, "/api/v1/auth/") && r.URL.Path != "/api/v1/auth" {
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
