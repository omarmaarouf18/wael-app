// Lockout tracking with exponential backoff (5 failures → 30s, doubling to 300s cap).
package handlers

import (
	"sync"
	"time"

	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
)

// Lockout tracks authentication failures per key (email, IP).
type Lockout interface {
	IsLocked(key string) (bool, time.Duration)
	RecordFailure(key string) time.Duration
	Reset(key string)
}

// RedisLockout delegates to the shared Redis-backed auth limiter.
type RedisLockout struct {
	rl *ratelimit.AuthRateLimiter
}

// NewRedisLockout wraps rl.
func NewRedisLockout(rl *ratelimit.AuthRateLimiter) *RedisLockout {
	return &RedisLockout{rl: rl}
}

// IsLocked reports an active lockout.
func (l *RedisLockout) IsLocked(key string) (bool, time.Duration) {
	return l.rl.IsLocked(key)
}

// RecordFailure records a failure, returning the new backoff (0 when below threshold).
func (l *RedisLockout) RecordFailure(key string) time.Duration {
	return l.rl.RecordFailure(key)
}

// Reset clears failures and lockout for key.
func (l *RedisLockout) Reset(key string) {
	l.rl.Reset(key)
}

type memFailures struct {
	count     int
	lockedFor time.Duration
	lockedTil time.Time
}

// MemoryLockout is an in-process exponential-backoff tracker (tests, no-redis dev).
type MemoryLockout struct {
	mu   sync.Mutex
	data map[string]*memFailures
}

// NewMemoryLockout creates an empty MemoryLockout.
func NewMemoryLockout() *MemoryLockout {
	return &MemoryLockout{data: map[string]*memFailures{}}
}

// IsLocked reports an active lockout.
func (l *MemoryLockout) IsLocked(key string) (bool, time.Duration) {
	if key == "" {
		return false, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.data[key]
	if !ok {
		return false, 0
	}
	if time.Now().Before(f.lockedTil) {
		return true, time.Until(f.lockedTil)
	}
	return false, 0
}

// RecordFailure records a failure, engaging backoff from the 5th failure.
func (l *MemoryLockout) RecordFailure(key string) time.Duration {
	if key == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.data[key]
	if !ok {
		f = &memFailures{}
		l.data[key] = f
	}
	f.count++
	if f.count < 5 {
		return 0
	}
	backoff := 30 * time.Second
	for i := 5; i < f.count; i++ {
		backoff *= 2
		if backoff >= 300*time.Second {
			backoff = 300 * time.Second
			break
		}
	}
	f.lockedFor = backoff
	f.lockedTil = time.Now().Add(backoff)
	return backoff
}

// Reset clears failures and lockout for key.
func (l *MemoryLockout) Reset(key string) {
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.data, key)
}
