package handlerutil

import (
	"net/http"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
)

type RateLimiter struct {
	rl *ratelimit.RateLimiter
}

func NewRateLimiter(rl *ratelimit.RateLimiter) *RateLimiter {
	return &RateLimiter{rl: rl}
}

func (rl *RateLimiter) CheckAndRecord(key string) (bool, time.Duration) {
	return rl.rl.CheckAndRecord(key)
}

// GetIP extracts the client IP, preferring X-Forwarded-For.
// Trusts X-Forwarded-For only because api-gateway overwrites it at the
// edge (see proxy.go) — do not expose these services directly to the
// internet without that guarantee holding.
func GetIP(r *http.Request) string {
	var ip string
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip = strings.TrimSpace(parts[0])
	} else if rip := r.Header.Get("X-Real-IP"); rip != "" {
		ip = rip
	} else {
		ip = r.RemoteAddr
	}

	if strings.Contains(ip, "]") {
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			ip = ip[:idx]
		}
		ip = strings.Trim(ip, "[]")
	} else {
		if count := strings.Count(ip, ":"); count == 1 {
			if idx := strings.LastIndex(ip, ":"); idx != -1 {
				ip = ip[:idx]
			}
		}
	}
	return ip
}
