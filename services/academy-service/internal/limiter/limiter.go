// Package limiter implements tiered rate limiting for academy-service per SPEC Section 2 (D13).
package limiter

import (
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
	"github.com/redis/go-redis/v9"
)

// Tier constants per SPEC Section 2 (D13).
const (
	TierRead     = "read"
	TierDownload = "download"
	TierWrite    = "write"
)

// Default limits per minute per SPEC Section 2 (D13).
const (
	DefaultLimitRead     = 30
	DefaultLimitDownload = 10
	DefaultLimitWrite    = 5
)

// RateLimiter checks rate limits for a given key.
type RateLimiter interface {
	CheckAndRecord(key string) (bool, time.Duration)
}

// TierLimiter manages rate limits across tiers (Read, Download, Write).
type TierLimiter interface {
	CheckAndRecord(tier, key string) (bool, time.Duration)
}

// RedisTierLimiter implements TierLimiter backed by Redis via shared/infra/ratelimit.
// Keyed on JWT user id; fails closed when Redis is unavailable or unconfigured.
type RedisTierLimiter struct {
	client   *redis.Client
	read     RateLimiter
	download RateLimiter
	write    RateLimiter
}

// NewRedisTierLimiter creates a Redis-backed TierLimiter with specified limits per minute.
// If client is nil, it fails closed on all checks.
func NewRedisTierLimiter(client *redis.Client, readLimit, downloadLimit, writeLimit int) *RedisTierLimiter {
	if readLimit <= 0 {
		readLimit = DefaultLimitRead
	}
	if downloadLimit <= 0 {
		downloadLimit = DefaultLimitDownload
	}
	if writeLimit <= 0 {
		writeLimit = DefaultLimitWrite
	}

	if client == nil {
		return &RedisTierLimiter{client: nil}
	}

	return &RedisTierLimiter{
		client:   client,
		read:     ratelimit.NewRateLimiter(client, readLimit, time.Minute, "academy:read"),
		download: ratelimit.NewRateLimiter(client, downloadLimit, time.Minute, "academy:download"),
		write:    ratelimit.NewRateLimiter(client, writeLimit, time.Minute, "academy:write"),
	}
}

// CheckAndRecord checks and records request for tier and key.
// Returns (limited, retryAfter). Fails closed if client or limiter is nil.
func (r *RedisTierLimiter) CheckAndRecord(tier, key string) (bool, time.Duration) {
	if r == nil || r.client == nil {
		log.Printf("[SECURITY CRITICAL] Redis rate limiter client is nil (FAIL CLOSED) for tier=%s key=%s", tier, key)
		return true, 30 * time.Second
	}

	var l RateLimiter
	switch tier {
	case TierRead:
		l = r.read
	case TierDownload:
		l = r.download
	case TierWrite:
		l = r.write
	default:
		log.Printf("[SECURITY CRITICAL] Unknown rate limit tier %q (FAIL CLOSED)", tier)
		return true, 30 * time.Second
	}

	if l == nil {
		log.Printf("[SECURITY CRITICAL] Limiter for tier %s is nil (FAIL CLOSED)", tier)
		return true, 30 * time.Second
	}

	return l.CheckAndRecord(key)
}

// WriteRateLimitedResponse writes a standard 429 response with Retry-After header.
func WriteRateLimitedResponse(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds <= 0 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
		"error": "rate limit exceeded, retry later",
		"code":  "rate_limited",
	})
}
