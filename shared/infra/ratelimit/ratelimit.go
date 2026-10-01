package ratelimit

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewRedisClient creates and pings a Redis client to ensure reachability.
// It is Sentinel-aware: if REDIS_SENTINEL_ADDRS is set, it constructs a failover client
// (redis.NewFailoverClient) monitoring REDIS_SENTINEL_MASTER_NAME (defaulting to "mymaster");
// otherwise it connects to redisURI directly.
func NewRedisClient(redisURI string) (*redis.Client, error) {
	return NewRedisClientFromEnv(redisURI, os.Getenv)
}

// NewRedisClientFromEnv is a Sentinel-aware factory function that resolves configuration via a custom getenv function.
func NewRedisClientFromEnv(redisURI string, getenv func(string) string) (*redis.Client, error) {
	sentinelAddrsEnv := strings.TrimSpace(getenv("REDIS_SENTINEL_ADDRS"))
	if sentinelAddrsEnv != "" {
		var sentinelAddrs []string
		for _, addr := range strings.Split(sentinelAddrsEnv, ",") {
			trimmed := strings.TrimSpace(addr)
			if trimmed != "" {
				sentinelAddrs = append(sentinelAddrs, trimmed)
			}
		}

		if len(sentinelAddrs) > 0 {
			masterName := strings.TrimSpace(getenv("REDIS_SENTINEL_MASTER_NAME"))
			if masterName == "" {
				masterName = "mymaster"
			}
			password := getenv("REDIS_PASSWORD")
			if password == "" && redisURI != "" {
				if uOpts, err := redis.ParseURL(redisURI); err == nil && uOpts.Password != "" {
					password = uOpts.Password
				}
			}

			sentinelPassword := getenv("REDIS_SENTINEL_PASSWORD")

			rdb := redis.NewFailoverClient(&redis.FailoverOptions{
				MasterName:       masterName,
				SentinelAddrs:    sentinelAddrs,
				Password:         password,
				SentinelPassword: sentinelPassword,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := rdb.Ping(ctx).Err(); err != nil {
				_ = rdb.Close()
				return nil, fmt.Errorf("ratelimit: failed to connect to Redis Sentinel master (%s): %w", masterName, err)
			}
			return rdb, nil
		}
	}

	opts, err := redis.ParseURL(redisURI)
	if err != nil {
		opts = &redis.Options{
			Addr: redisURI,
		}
	}
	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return rdb, nil
}

// RateLimiter is a Redis-backed sliding window rate limiter with exponential backoff lockout.
type RateLimiter struct {
	client *redis.Client
	limit  int
	window time.Duration
	cap    int
	prefix string
}

func NewRateLimiter(client *redis.Client, limit int, window time.Duration, prefix string) *RateLimiter {
	return &RateLimiter{
		client: client,
		limit:  limit,
		window: window,
		cap:    300,
		prefix: prefix,
	}
}

const checkAndRecordScript = `
local countKey = KEYS[1]
local lockoutKey = KEYS[2]

local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local cap = tonumber(ARGV[3])

-- 1. Check if locked out
local lockoutTTL = redis.call('TTL', lockoutKey)
if lockoutTTL > 0 then
    return {1, lockoutTTL}
end

-- 2. Increment count
local count = redis.call('INCR', countKey)
redis.call('EXPIRE', countKey, window)

-- 3. Check if limit exceeded
if count > limit then
    -- Reset the counter so the client starts with a clean budget when the
    -- lockout expires. Without this, a client that crosses the threshold
    -- resumes ABOVE the limit after a short (sub-window) lockout and is
    -- instantly re-locked with an exponentially longer penalty — an
    -- escalation spiral that persists for as long as it keeps using the app.
    redis.call('DEL', countKey)
    local diff = count - limit - 1
    local backoff = 30 * (2 ^ diff)
    if backoff > cap then
        backoff = cap
    end
    redis.call('SET', lockoutKey, '1', 'EX', math.floor(backoff))
    return {1, math.floor(backoff)}
end

return {0, 0}
`

// CheckAndRecordWithError checks and records a request for the given key, returning
// any Redis or communication error encountered.
func (rl *RateLimiter) CheckAndRecordWithError(key string) (bool, time.Duration, error) {
	if key == "" {
		return false, 0, nil
	}
	if rl == nil || rl.client == nil {
		return false, 0, fmt.Errorf("ratelimit: redis client is nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	countKey := fmt.Sprintf("ratelimit:%s:count:%s", rl.prefix, key)
	lockoutKey := fmt.Sprintf("ratelimit:%s:lockout:%s", rl.prefix, key)

	res, err := rl.client.Eval(ctx, checkAndRecordScript, []string{countKey, lockoutKey}, rl.limit, int(rl.window.Seconds()), rl.cap).Result()
	if err != nil {
		log.Printf("[SECURITY CRITICAL] Redis rate limiter error (FAIL CLOSED): %v. Restricting traffic for key: %s", err, key)
		return false, 0, err
	}

	results, ok := res.([]interface{})
	if !ok || len(results) < 2 {
		err := fmt.Errorf("unexpected rate limiter response: %v", res)
		log.Printf("[SECURITY CRITICAL] %v. Fail closed for key: %s", err, key)
		return false, 0, err
	}

	limited := results[0].(int64) == 1
	ttl := results[1].(int64)

	return limited, time.Duration(ttl) * time.Second, nil
}

func (rl *RateLimiter) CheckAndRecord(key string) (bool, time.Duration) {
	limited, ttl, err := rl.CheckAndRecordWithError(key)
	if err != nil {
		return true, 30 * time.Second
	}
	return limited, ttl
}

// AuthRateLimiter is a Redis-backed dual-key lockout tracker (IP + email).
type AuthRateLimiter struct {
	client *redis.Client
	cap    int
	prefix string
}

func NewAuthRateLimiter(client *redis.Client, prefix string) *AuthRateLimiter {
	return &AuthRateLimiter{
		client: client,
		cap:    300,
		prefix: prefix,
	}
}

const recordFailureScript = `
local countKey = KEYS[1]
local lockoutKey = KEYS[2]
local cap = tonumber(ARGV[1])

local count = redis.call('INCR', countKey)
redis.call('EXPIRE', countKey, 86400) -- 24h TTL for count

if count >= 5 then
    local diff = count - 5
    local backoff = 30 * (2 ^ diff)
    if backoff > cap then
        backoff = cap
    end
    redis.call('SET', lockoutKey, '1', 'EX', math.floor(backoff))
    return math.floor(backoff)
end
return 0
`

func (rl *AuthRateLimiter) IsLocked(key string) (bool, time.Duration) {
	if key == "" {
		return false, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	lockoutKey := fmt.Sprintf("ratelimit:%s:lockout:%s", rl.prefix, key)
	ttl, err := rl.client.TTL(ctx, lockoutKey).Result()
	if err != nil {
		log.Printf("[SECURITY CRITICAL] Redis IsLocked check error (FAIL CLOSED): %v. Locking key: %s", err, key)
		return true, 5 * time.Minute
	}

	if ttl > 0 {
		return true, ttl
	}
	return false, 0
}

func (rl *AuthRateLimiter) RecordFailure(key string) time.Duration {
	if key == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	countKey := fmt.Sprintf("ratelimit:%s:count:%s", rl.prefix, key)
	lockoutKey := fmt.Sprintf("ratelimit:%s:lockout:%s", rl.prefix, key)

	res, err := rl.client.Eval(ctx, recordFailureScript, []string{countKey, lockoutKey}, rl.cap).Result()
	if err != nil {
		log.Printf("[SECURITY CRITICAL] Redis RecordFailure error (FAIL CLOSED): %v. Enforcing lockout for key: %s", err, key)
		return 5 * time.Minute
	}

	backoffSec, ok := res.(int64)
	if !ok {
		log.Printf("[SECURITY CRITICAL] Unexpected RecordFailure response: %v. Fail closed for key: %s", res, key)
		return 5 * time.Minute
	}

	return time.Duration(backoffSec) * time.Second
}

func (rl *AuthRateLimiter) Reset(key string) {
	if key == "" || rl == nil || rl.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	countKey := fmt.Sprintf("ratelimit:%s:count:%s", rl.prefix, key)
	lockoutKey := fmt.Sprintf("ratelimit:%s:lockout:%s", rl.prefix, key)

	_, err := rl.client.Del(ctx, countKey, lockoutKey).Result()
	if err != nil {
		log.Printf("[ERROR] Failed to reset rate limiter keys in Redis: %v", err)
	}
}
