// Package limiter implements tiered rate limiting for academy-service per SPEC Section 2 (D13).
package limiter

import (
	"errors"
	"fmt"
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
	TierPlay     = "play"
	TierDownload = "download"
	TierWrite    = "write"
)

// Default limits per minute per SPEC Section 2 (D13).
const (
	DefaultLimitRead     = 120
	DefaultLimitPlay     = 60
	DefaultLimitDownload = 10
	DefaultLimitWrite    = 5
)

// TierLimiter manages rate limits across tiers (Read, Play, Download, Write).
// Returns (limited bool, retryAfter time.Duration, err error).
// On backend failure (Redis down, nil client, etc.), err != nil.
// When user exceeds limit, limited = true, retryAfter > 0, err = nil.
type TierLimiter interface {
	CheckAndRecord(tier, key string) (bool, time.Duration, error)
}

// RedisTierLimiter implements TierLimiter backed by Redis via shared/infra/ratelimit.
// Keyed on JWT user id; fails closed with error when Redis is unavailable or unconfigured.
type RedisTierLimiter struct {
	client   *redis.Client
	read     *ratelimit.RateLimiter
	play     *ratelimit.RateLimiter
	download *ratelimit.RateLimiter
	write    *ratelimit.RateLimiter
}

// NewRedisTierLimiter creates a Redis-backed TierLimiter with specified limits per minute.
// If client is nil, it fails closed with error on all checks.
func NewRedisTierLimiter(client *redis.Client, readLimit, playLimit, downloadLimit, writeLimit int) *RedisTierLimiter {
	if readLimit <= 0 {
		readLimit = DefaultLimitRead
	}
	if playLimit <= 0 {
		playLimit = DefaultLimitPlay
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
		play:     ratelimit.NewRateLimiter(client, playLimit, time.Minute, "academy:play"),
		download: ratelimit.NewRateLimiter(client, downloadLimit, time.Minute, "academy:download"),
		write:    ratelimit.NewRateLimiter(client, writeLimit, time.Minute, "academy:write"),
	}
}

// CheckAndRecord checks and records request for tier and key.
// Returns (limited, retryAfter, err). Fails closed with error if client, tier, or Redis fails.
func (r *RedisTierLimiter) CheckAndRecord(tier, key string) (bool, time.Duration, error) {
	if r == nil || r.client == nil {
		log.Printf("[SECURITY CRITICAL] Redis rate limiter client is nil (FAIL CLOSED) for tier=%s key=%s", tier, key)
		return false, 0, errors.New("rate limiter client is nil")
	}

	var l *ratelimit.RateLimiter
	switch tier {
	case TierRead:
		l = r.read
	case TierPlay:
		l = r.play
	case TierDownload:
		l = r.download
	case TierWrite:
		l = r.write
	default:
		log.Printf("[SECURITY CRITICAL] Unknown rate limit tier %q (FAIL CLOSED)", tier)
		return false, 0, fmt.Errorf("unknown rate limit tier: %q", tier)
	}

	if l == nil {
		log.Printf("[SECURITY CRITICAL] Limiter for tier %s is nil (FAIL CLOSED)", tier)
		return false, 0, fmt.Errorf("limiter for tier %s is nil", tier)
	}

	return l.CheckAndRecordWithError(key)
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
