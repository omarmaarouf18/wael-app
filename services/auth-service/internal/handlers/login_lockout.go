// Login lockout with owner-decided scheme (no IP-wide lock):
//   - Key (email, IP): 5 failed logins in a row lock that pair for 15 min.
//   - Key (email) across all IPs: 20 failures in 1 h lock that account for 1 h.
//
// IP volume is handled only by the gateway rate limit, never by a hard lockout.
package handlers

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	loginPairThreshold   = 5
	loginPairLockTTL     = 15 * time.Minute
	loginPairCounterTTL  = 15 * time.Minute
	loginGlobalThreshold = 20
	loginGlobalWindow    = time.Hour
	loginGlobalLockTTL   = time.Hour
)

// LoginLockout tracks login failures per (email, IP) pair and per email.
type LoginLockout interface {
	IsPairLocked(email, ip string) (bool, time.Duration)
	RecordPairFailure(email, ip string) time.Duration
	ResetPair(email, ip string)
	IsEmailLocked(email string) (bool, time.Duration)
	RecordEmailFailure(email string) time.Duration
	ResetEmail(email string)
	// ResetEmailAll clears the global lock and every pair lock for email
	// (used on successful password reset).
	ResetEmailAll(email string)
}

func loginPairKey(email, ip string) string {
	return "login:pair:" + email + ":" + ip
}

func loginGlobalCountKey(email string) string {
	return "login:global:count:" + email
}

func loginGlobalLockKey(email string) string {
	return "login:global:lock:" + email
}

// RedisLoginLockout is Redis-backed with TTLs (fail-closed on Redis errors).
type RedisLoginLockout struct {
	client *redis.Client
	prefix string
}

// NewRedisLoginLockout wraps client.
func NewRedisLoginLockout(client *redis.Client, prefix string) *RedisLoginLockout {
	return &RedisLoginLockout{client: client, prefix: prefix}
}

func (l *RedisLoginLockout) fullKey(key string) string {
	if l.prefix == "" {
		return key
	}
	return l.prefix + ":" + key
}

const loginPairScript = `
local countKey = KEYS[1]
local lockKey = KEYS[2]
local threshold = tonumber(ARGV[1])
local counterTTL = tonumber(ARGV[2])
local lockTTL = tonumber(ARGV[3])

local count = redis.call('INCR', countKey)
if count == 1 then
    redis.call('EXPIRE', countKey, counterTTL)
end
if count >= threshold then
    redis.call('SET', lockKey, '1', 'EX', lockTTL)
    return lockTTL
end
return 0
`

const loginGlobalScript = `
local countKey = KEYS[1]
local lockKey = KEYS[2]
local threshold = tonumber(ARGV[1])
local windowSec = tonumber(ARGV[2])
local lockTTL = tonumber(ARGV[3])

local count = redis.call('INCR', countKey)
if count == 1 then
    redis.call('EXPIRE', countKey, windowSec)
end
if count >= threshold then
    redis.call('SET', lockKey, '1', 'EX', lockTTL)
    return lockTTL
end
return 0
`

// IsPairLocked reports an active (email, IP) lockout.
func (l *RedisLoginLockout) IsPairLocked(email, ip string) (bool, time.Duration) {
	if email == "" || ip == "" {
		return false, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ttl, err := l.client.TTL(ctx, l.fullKey(loginPairKey(email, ip)+":lock")).Result()
	if err != nil {
		log.Printf("[SECURITY CRITICAL] Redis login pair IsLocked error (FAIL CLOSED): %v", err)
		return true, 5 * time.Minute
	}
	if ttl > 0 {
		return true, ttl
	}
	return false, 0
}

// RecordPairFailure records an (email, IP) failure, locking at 5 in a row for 15 min.
func (l *RedisLoginLockout) RecordPairFailure(email, ip string) time.Duration {
	if email == "" || ip == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	countKey := l.fullKey(loginPairKey(email, ip) + ":count")
	lockKey := l.fullKey(loginPairKey(email, ip) + ":lock")
	res, err := l.client.Eval(ctx, loginPairScript, []string{countKey, lockKey}, loginPairThreshold, int(loginPairCounterTTL.Seconds()), int(loginPairLockTTL.Seconds())).Result()
	if err != nil {
		log.Printf("[SECURITY CRITICAL] Redis login pair RecordFailure error (FAIL CLOSED): %v", err)
		return 5 * time.Minute
	}
	secs, _ := res.(int64)
	return time.Duration(secs) * time.Second
}

// ResetPair clears the (email, IP) counter and lockout (on successful login).
func (l *RedisLoginLockout) ResetPair(email, ip string) {
	if email == "" || ip == "" || l.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	base := loginPairKey(email, ip)
	_, err := l.client.Del(ctx, l.fullKey(base+":count"), l.fullKey(base+":lock")).Result()
	if err != nil {
		log.Printf("[ERROR] Failed to reset login pair keys: %v", err)
	}
}

// IsEmailLocked reports an active per-email global lockout.
func (l *RedisLoginLockout) IsEmailLocked(email string) (bool, time.Duration) {
	if email == "" {
		return false, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ttl, err := l.client.TTL(ctx, l.fullKey(loginGlobalLockKey(email))).Result()
	if err != nil {
		log.Printf("[SECURITY CRITICAL] Redis login global IsLocked error (FAIL CLOSED): %v", err)
		return true, 5 * time.Minute
	}
	if ttl > 0 {
		return true, ttl
	}
	return false, 0
}

// RecordEmailFailure records a per-email failure, locking at 20 in 1h for 1h.
func (l *RedisLoginLockout) RecordEmailFailure(email string) time.Duration {
	if email == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := l.client.Eval(ctx, loginGlobalScript, []string{l.fullKey(loginGlobalCountKey(email)), l.fullKey(loginGlobalLockKey(email))}, loginGlobalThreshold, int(loginGlobalWindow.Seconds()), int(loginGlobalLockTTL.Seconds())).Result()
	if err != nil {
		log.Printf("[SECURITY CRITICAL] Redis login global RecordFailure error (FAIL CLOSED): %v", err)
		return 5 * time.Minute
	}
	secs, _ := res.(int64)
	return time.Duration(secs) * time.Second
}

// ResetEmail clears the per-email global counter and lockout.
func (l *RedisLoginLockout) ResetEmail(email string) {
	if email == "" || l.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := l.client.Del(ctx, l.fullKey(loginGlobalCountKey(email)), l.fullKey(loginGlobalLockKey(email))).Result()
	if err != nil {
		log.Printf("[ERROR] Failed to reset login global keys: %v", err)
	}
}

// ResetEmailAll clears the global lock and every pair lock for email
// (used on successful password reset).
func (l *RedisLoginLockout) ResetEmailAll(email string) {
	if email == "" || l.client == nil {
		return
	}
	l.ResetEmail(email)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// SCAN for pair keys of this email (bounded: few IPs per email in practice).
	var cursor uint64
	pattern := l.fullKey("login:pair:" + email + ":*")
	for {
		keys, next, err := l.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			log.Printf("[ERROR] Failed to scan login pair keys for reset: %v", err)
			return
		}
		if len(keys) > 0 {
			if _, err := l.client.Del(ctx, keys...).Result(); err != nil {
				log.Printf("[ERROR] Failed to delete login pair keys for reset: %v", err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
}

type loginPairState struct {
	count     int
	lockedTil time.Time
}

type loginGlobalState struct {
	count     int
	windowExp time.Time
	lockedTil time.Time
}

// MemoryLoginLockout is an in-process tracker (tests, no-redis dev).
type MemoryLoginLockout struct {
	mu     sync.Mutex
	pairs  map[string]*loginPairState
	global map[string]*loginGlobalState
}

// NewMemoryLoginLockout creates an empty MemoryLoginLockout.
func NewMemoryLoginLockout() *MemoryLoginLockout {
	return &MemoryLoginLockout{
		pairs:  map[string]*loginPairState{},
		global: map[string]*loginGlobalState{},
	}
}

// IsPairLocked reports an active (email, IP) lockout.
func (l *MemoryLoginLockout) IsPairLocked(email, ip string) (bool, time.Duration) {
	if email == "" || ip == "" {
		return false, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.pairs[loginPairKey(email, ip)]
	if !ok {
		return false, 0
	}
	if time.Now().Before(st.lockedTil) {
		return true, time.Until(st.lockedTil)
	}
	return false, 0
}

// RecordPairFailure records an (email, IP) failure, locking at 5 in a row for 15 min.
func (l *MemoryLoginLockout) RecordPairFailure(email, ip string) time.Duration {
	if email == "" || ip == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := loginPairKey(email, ip)
	st, ok := l.pairs[k]
	if !ok {
		st = &loginPairState{}
		l.pairs[k] = st
	}
	st.count++
	if st.count >= loginPairThreshold {
		st.lockedTil = time.Now().Add(loginPairLockTTL)
		return loginPairLockTTL
	}
	return 0
}

// ResetPair clears the (email, IP) counter and lockout.
func (l *MemoryLoginLockout) ResetPair(email, ip string) {
	if email == "" || ip == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.pairs, loginPairKey(email, ip))
}

// IsEmailLocked reports an active per-email global lockout.
func (l *MemoryLoginLockout) IsEmailLocked(email string) (bool, time.Duration) {
	if email == "" {
		return false, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.global[email]
	if !ok {
		return false, 0
	}
	if time.Now().Before(st.lockedTil) {
		return true, time.Until(st.lockedTil)
	}
	return false, 0
}

// RecordEmailFailure records a per-email failure, locking at 20 in 1h for 1h.
func (l *MemoryLoginLockout) RecordEmailFailure(email string) time.Duration {
	if email == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	st, ok := l.global[email]
	if !ok || now.After(st.windowExp) {
		st = &loginGlobalState{windowExp: now.Add(loginGlobalWindow)}
		l.global[email] = st
	}
	st.count++
	if st.count >= loginGlobalThreshold {
		st.lockedTil = now.Add(loginGlobalLockTTL)
		return loginGlobalLockTTL
	}
	return 0
}

// ResetEmail clears the per-email global counter and lockout.
func (l *MemoryLoginLockout) ResetEmail(email string) {
	if email == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.global, email)
}

// ResetEmailAll clears the global lock and every pair lock for email.
func (l *MemoryLoginLockout) ResetEmailAll(email string) {
	if email == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.global, email)
	prefix := "login:pair:" + email + ":"
	for k := range l.pairs {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(l.pairs, k)
		}
	}
}
