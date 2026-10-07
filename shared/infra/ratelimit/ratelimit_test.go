package ratelimit

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRateLimiterSharedAndConcurrent(t *testing.T) {
	// 1. Start miniredis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	// 2. Create two client instances pointed at the same Redis (simulating scaling out)
	rdb1 := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb1.Close()

	rdb2 := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb2.Close()

	// Create RateLimiters on both clients
	rl1 := NewRateLimiter(rdb1, 10, 1*time.Minute, "test-service")
	rl2 := NewRateLimiter(rdb2, 10, 1*time.Minute, "test-service")

	key := "192.168.1.1"

	// 3. Increment requests using BOTH rate limiter client instances
	for i := 0; i < 5; i++ {
		limited, _ := rl1.CheckAndRecord(key)
		if limited {
			t.Errorf("expected not to be limited on iteration %d via client 1", i)
		}
	}
	for i := 0; i < 5; i++ {
		limited, _ := rl2.CheckAndRecord(key)
		if limited {
			t.Errorf("expected not to be limited on iteration %d via client 2", i)
		}
	}

	// 4. The 11th request (combined limit is 10) must be limited, regardless of which client calls it
	limited, backoff := rl1.CheckAndRecord(key)
	if !limited {
		t.Errorf("expected 11th request to be rate limited (sharing failed)")
	}
	if backoff < 29*time.Second || backoff > 31*time.Second {
		t.Errorf("expected backoff around 30s, got %v", backoff)
	}

	limited, _ = rl2.CheckAndRecord(key)
	if !limited {
		t.Errorf("expected client 2 to also observe the rate limit lockout (sharing failed)")
	}
}

func TestRateLimiterConcurrency(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	// Simulate 10 concurrent clients hitting the rate limiter at the same time
	const numClients = 10
	const requestsPerClient = 20
	const limit = 50

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	rl := NewRateLimiter(rdb, limit, 1*time.Minute, "concurrent-service")
	key := "shared-ip"

	var wg sync.WaitGroup
	var mu sync.Mutex
	limitedCount := 0
	successCount := 0

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerClient; j++ {
				limited, _ := rl.CheckAndRecord(key)
				mu.Lock()
				if limited {
					limitedCount++
				} else {
					successCount++
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// We expect exactly 'limit' requests to succeed, and the rest to be rate limited
	if successCount != limit {
		t.Errorf("expected exactly %d successful requests, got %d", limit, successCount)
	}
	expectedLimited := (numClients * requestsPerClient) - limit
	if limitedCount != expectedLimited {
		t.Errorf("expected exactly %d limited requests, got %d", expectedLimited, limitedCount)
	}
}

func TestAuthRateLimiterLockoutShared(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb1 := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb1.Close()

	rdb2 := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb2.Close()

	authRl1 := NewAuthRateLimiter(rdb1, "auth:email")
	authRl2 := NewAuthRateLimiter(rdb2, "auth:email")

	email := "user@example.com"

	// Record 4 failures on client 1
	for i := 0; i < 4; i++ {
		backoff := authRl1.RecordFailure(email)
		if backoff != 0 {
			t.Errorf("expected no lockout on failure %d", i+1)
		}
	}

	// 5th failure on client 2 must trigger lockout
	backoff := authRl2.RecordFailure(email)
	if backoff == 0 {
		t.Errorf("expected 5th failure (on client 2) to trigger lockout")
	}

	// Check lockout on both clients
	locked1, _ := authRl1.IsLocked(email)
	locked2, _ := authRl2.IsLocked(email)

	if !locked1 || !locked2 {
		t.Errorf("lockout was not correctly shared: client1=%v, client2=%v", locked1, locked2)
	}

	// Reset on client 1 should reset lockout on client 2 too
	authRl1.Reset(email)

	locked1, _ = authRl1.IsLocked(email)
	locked2, _ = authRl2.IsLocked(email)

	if locked1 || locked2 {
		t.Errorf("reset was not correctly shared: client1=%v, client2=%v", locked1, locked2)
	}
}

func TestRateLimiterFailClosed(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rl := NewRateLimiter(rdb, 10, 1*time.Minute, "fail-closed-service")
	authRl := NewAuthRateLimiter(rdb, "auth:ip")

	key := "test-key"

	// Stop miniredis to simulate runtime network failure
	rdb.Close()
	mr.Close()

	// 1. Check RateLimiter fail closed
	limited, retryAfter := rl.CheckAndRecord(key)
	if !limited {
		t.Errorf("expected rate limiter to fail closed on Redis unavailability")
	}
	if retryAfter != 30*time.Second {
		t.Errorf("expected 30s fallback retry time, got %v", retryAfter)
	}

	// 2. Check AuthRateLimiter fail closed (IsLocked)
	locked, lockoutDur := authRl.IsLocked(key)
	if !locked {
		t.Errorf("expected auth rate limiter (IsLocked) to fail closed on Redis unavailability")
	}
	if lockoutDur != 5*time.Minute {
		t.Errorf("expected 5m fallback lockout, got %v", lockoutDur)
	}

	// 3. Check AuthRateLimiter fail closed (RecordFailure)
	backoff := authRl.RecordFailure(key)
	if backoff != 5*time.Minute {
		t.Errorf("expected auth rate limiter (RecordFailure) to fail closed on Redis unavailability with 5m backoff, got %v", backoff)
	}
}

func TestRateLimiter_ExtraCoverage(t *testing.T) {
	// 1. Lockout expiry and reset
	t.Run("LockoutExpiryAndReset", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()

		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		key := "test-expiry-key"

		// RateLimiter expiry test
		rl := NewRateLimiter(rdb, 2, 10*time.Second, "rl-expiry") // use 10s window to keep it small
		// First 2 requests succeed
		l, _ := rl.CheckAndRecord(key)
		if l {
			t.Fatal("expected not limited")
		}
		l, _ = rl.CheckAndRecord(key)
		if l {
			t.Fatal("expected not limited")
		}
		// 3rd request limits and locks out
		l, backoff := rl.CheckAndRecord(key)
		if !l || backoff <= 0 {
			t.Fatalf("expected rate limited with backoff, got: limited=%v, backoff=%v", l, backoff)
		}

		// Fast-forward miniredis time past the backoff (which also covers the 10s window)
		mr.FastForward(backoff + 1*time.Second)

		// Request should succeed again
		l, _ = rl.CheckAndRecord(key)
		if l {
			t.Error("expected rate limit to expire after fast-forward, but it was still limited")
		}

		// AuthRateLimiter expiry and reset test
		authRl := NewAuthRateLimiter(rdb, "auth-expiry")
		// Call 4 times, no lockout
		for i := 0; i < 4; i++ {
			b := authRl.RecordFailure(key)
			if b != 0 {
				t.Fatalf("expected no lockout on iteration %d, got %v", i, b)
			}
		}

		// 5th failure triggers lockout
		b := authRl.RecordFailure(key)
		if b == 0 {
			t.Fatal("expected lockout on 5th failure")
		}

		locked, _ := authRl.IsLocked(key)
		if !locked {
			t.Error("expected IsLocked to be true")
		}

		// Reset lockout
		authRl.Reset(key)

		locked, _ = authRl.IsLocked(key)
		if locked {
			t.Error("expected IsLocked to be false after Reset")
		}

		// Re-trigger lockout by recording 5 more failures
		for i := 0; i < 4; i++ {
			authRl.RecordFailure(key)
		}
		b = authRl.RecordFailure(key)
		if b == 0 {
			t.Fatal("expected lockout after 5 failures post-reset")
		}

		locked, _ = authRl.IsLocked(key)
		if !locked {
			t.Error("expected IsLocked to be true")
		}

		// Fast-forward past lockout
		mr.FastForward(b + 1*time.Second)

		locked, _ = authRl.IsLocked(key)
		if locked {
			t.Error("expected IsLocked to be false after lockout expiry")
		}
	})

	// 2. Cap behavior on repeated failures (backoff never exceeds cap)
	t.Run("CapBehaviorOnRepeatedFailures", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()

		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		key := "test-cap-key"

		// RateLimiter cap check
		rl := NewRateLimiter(rdb, 1, 10*time.Minute, "rl-cap")
		rl.cap = 120 // set cap lower to test custom cap if we want, or use default 300
		// Exceed limit repeatedly
		var maxBackoff time.Duration
		for i := 0; i < 15; i++ {
			_, backoff := rl.CheckAndRecord(key)
			if backoff > maxBackoff {
				maxBackoff = backoff
			}
		}
		if maxBackoff > 120*time.Second {
			t.Errorf("expected max backoff to be capped at 120s, got %v", maxBackoff)
		}
		if maxBackoff == 0 {
			t.Error("expected non-zero backoff")
		}

		// AuthRateLimiter cap check
		authRl := NewAuthRateLimiter(rdb, "auth-cap")
		authRl.cap = 150
		var maxAuthBackoff time.Duration
		for i := 0; i < 20; i++ {
			backoff := authRl.RecordFailure(key)
			if backoff > maxAuthBackoff {
				maxAuthBackoff = backoff
			}
		}
		if maxAuthBackoff > 150*time.Second {
			t.Errorf("expected auth max backoff to be capped at 150s, got %v", maxAuthBackoff)
		}
	})

	// 3. Concurrent calls to RecordFailure on the same key
	t.Run("AuthRateLimiterConcurrency", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()

		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		authRl := NewAuthRateLimiter(rdb, "auth-concurrent")
		key := "shared-auth-ip"

		const numGoroutines = 10
		const callsPerGoroutine = 5
		var wg sync.WaitGroup

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < callsPerGoroutine; j++ {
					authRl.RecordFailure(key)
				}
			}()
		}

		wg.Wait()

		// 50 total failures recorded. Lockout should be active.
		locked, backoff := authRl.IsLocked(key)
		if !locked {
			t.Error("expected auth rate limiter to be locked after concurrent failures")
		}
		if backoff <= 0 {
			t.Errorf("expected positive lockout duration, got %v", backoff)
		}
	})
}

func TestNewRedisClient_SentinelAndStandaloneBranching(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	// 1. Standalone branch when REDIS_SENTINEL_ADDRS is empty
	t.Run("StandaloneFallback", func(t *testing.T) {
		env := map[string]string{
			"REDIS_SENTINEL_ADDRS": "",
		}
		getenv := func(key string) string {
			return env[key]
		}

		client, err := NewRedisClientFromEnv(mr.Addr(), getenv)
		if err != nil {
			t.Fatalf("expected NewRedisClientFromEnv to succeed with standalone miniredis, got: %v", err)
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := client.Ping(ctx).Err(); err != nil {
			t.Errorf("expected ping to succeed on standalone client, got: %v", err)
		}
	})

	// 2. Sentinel branch when REDIS_SENTINEL_ADDRS is configured
	t.Run("SentinelConfigurationBranching", func(t *testing.T) {
		env := map[string]string{
			"REDIS_SENTINEL_ADDRS":       " 127.0.0.1:26379 , 127.0.0.1:26380 ",
			"REDIS_SENTINEL_MASTER_NAME": "mymaster-prod",
			"REDIS_PASSWORD":             "secretpass",
			"REDIS_SENTINEL_PASSWORD":    "sentinelpass",
		}
		getenv := func(key string) string {
			return env[key]
		}

		// When sentinel addresses are provided but unreachable in unit test, it should branch to NewFailoverClient and fail ping with sentinel master error
		client, err := NewRedisClientFromEnv("redis://ignored:6379", getenv)
		if err == nil {
			if client != nil {
				client.Close()
			}
			t.Fatal("expected connection error for unreachable sentinel addresses")
		}

		expectedSubstr := "ratelimit: failed to connect to Redis Sentinel master (mymaster-prod)"
		if !strings.Contains(err.Error(), expectedSubstr) {
			t.Errorf("expected error containing %q, got %v", expectedSubstr, err)
		}
	})

	// 3. Password extracted from URI if REDIS_PASSWORD not in env
	t.Run("PasswordFromURIInSentinelMode", func(t *testing.T) {
		env := map[string]string{
			"REDIS_SENTINEL_ADDRS": "127.0.0.1:26379",
		}
		getenv := func(key string) string {
			return env[key]
		}

		client, err := NewRedisClientFromEnv("redis://:uri-secret@127.0.0.1:6379", getenv)
		if err == nil {
			if client != nil {
				client.Close()
			}
			t.Fatal("expected connection error for unreachable sentinel")
		}

		expectedSubstr := "ratelimit: failed to connect to Redis Sentinel master (mymaster)"
		if !strings.Contains(err.Error(), expectedSubstr) {
			t.Errorf("expected default master name 'mymaster' in error, got %v", err)
		}
	})
}

// TestRateLimiter_CounterResetsOnLockoutEngage verifies that crossing the
// limit clears the counter, so the client starts with a fresh budget when
// the lockout expires instead of resuming above the limit and being
// instantly re-locked with an exponentially longer penalty.
func TestRateLimiter_CounterResetsOnLockoutEngage(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	rl := NewRateLimiter(rdb, 2, 1*time.Minute, "reset-test")

	limited := false
	for i := 0; i < 3; i++ {
		if l, _ := rl.CheckAndRecord("client-a"); l {
			limited = true
		}
	}
	if !limited {
		t.Fatalf("expected lockout to engage after exceeding limit of 2")
	}

	countKey := "ratelimit:reset-test:count:client-a"
	if mr.Exists(countKey) {
		t.Errorf("counter key survived lockout engage; client would resume above the limit after expiry")
	}

	// Fast-forward past the (short) initial lockout: the next request must be
	// allowed with a fresh budget rather than immediately re-locked.
	mr.FastForward(31 * time.Second)
	if l, _ := rl.CheckAndRecord("client-a"); l {
		t.Errorf("request right after lockout expiry was limited; counter was not reset")
	}
}

func TestRateLimiter_FixedWindowAndLockout(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	// 1. TTL is not refreshed by later hits inside the window
	t.Run("TTLNotRefreshedByLaterHits", func(t *testing.T) {
		rl := NewRateLimiter(rdb, 10, 60*time.Second, "ttl-test")
		key := "ttl-user"
		countKey := "ratelimit:ttl-test:count:" + key

		if limited, _ := rl.CheckAndRecord(key); limited {
			t.Fatal("first request should not be limited")
		}
		initialTTL := mr.TTL(countKey)
		if initialTTL > 60*time.Second || initialTTL <= 0 {
			t.Fatalf("expected initial TTL <= 60s, got %v", initialTTL)
		}

		// Fast-forward 20s into the window
		mr.FastForward(20 * time.Second)

		// Second request arrives at t = 20s
		if limited, _ := rl.CheckAndRecord(key); limited {
			t.Fatal("second request should not be limited")
		}

		// TTL should now be ~40s, NOT renewed back to 60s
		currentTTL := mr.TTL(countKey)
		if currentTTL > 41*time.Second {
			t.Fatalf("TTL was refreshed by later hit: got %v, expected <= 40s", currentTTL)
		}
	})

	// 2. N requests spread across more than one window never lock
	t.Run("SteadyTrafficAcrossWindowsNeverLocks", func(t *testing.T) {
		limit := 3
		window := 10 * time.Second
		rl := NewRateLimiter(rdb, limit, window, "steady-traffic")
		key := "steady-user"

		// Send 6 requests, spaced 4 seconds apart (t=0, 4, 8, 12, 16, 20)
		// Across 20s with a 10s window:
		// Window 1 [0s, 10s): requests at 0s, 4s, 8s (3 requests <= limit 3)
		// Window 2 [10s, 20s): requests at 12s, 16s (2 requests <= limit 3)
		// Window 3 [20s, 30s): request at 20s (1 request <= limit 3)
		// None should ever be rate limited!
		for i := 0; i < 6; i++ {
			if i > 0 {
				mr.FastForward(4 * time.Second)
			}
			limited, backoff := rl.CheckAndRecord(key)
			if limited {
				t.Fatalf("request %d (at t=%ds) was unexpectedly limited with backoff %v", i+1, i*4, backoff)
			}
		}
	})

	// 3. N+1 requests inside one window locks
	t.Run("NPlusOneInsideOneWindowLocksAndExpires", func(t *testing.T) {
		limit := 3
		window := 10 * time.Second
		rl := NewRateLimiter(rdb, limit, window, "burst-test")
		key := "burst-user"

		// First N requests succeed
		for i := 0; i < limit; i++ {
			limited, _ := rl.CheckAndRecord(key)
			if limited {
				t.Fatalf("request %d inside window unexpectedly limited", i+1)
			}
		}

		// N+1 request inside the same window must lock with 30s lockout
		limited, backoff := rl.CheckAndRecord(key)
		if !limited {
			t.Fatal("N+1 request inside one window must be limited")
		}
		if backoff < 29*time.Second || backoff > 31*time.Second {
			t.Fatalf("expected 30s base lockout, got %v", backoff)
		}

		// Subsequent request while locked still limited
		limited2, _ := rl.CheckAndRecord(key)
		if !limited2 {
			t.Fatal("request while locked must be limited")
		}

		// 4. Lockout expires
		mr.FastForward(31 * time.Second)
		limitedAfterExpiry, _ := rl.CheckAndRecord(key)
		if limitedAfterExpiry {
			t.Fatal("request after lockout expiration should succeed")
		}
	})
}
