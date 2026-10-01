package limiter

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
)

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *RedisTierLimiter) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("failed to connect to miniredis: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	tl := NewRedisTierLimiter(rdb, DefaultLimitRead, DefaultLimitDownload, DefaultLimitWrite)
	return mr, tl
}

func TestRedisTierLimiter_ReadTier(t *testing.T) {
	_, tl := setupTestRedis(t)
	const userID = "user-read-test"

	for i := 1; i <= DefaultLimitRead; i++ {
		limited, retryAfter, err := tl.CheckAndRecord(TierRead, userID)
		if err != nil {
			t.Fatalf("read call %d unexpected error: %v", i, err)
		}
		if limited {
			t.Fatalf("read call %d unexpectedly rate limited (retryAfter=%v)", i, retryAfter)
		}
	}

	// 31st call must be rate limited
	limited, retryAfter, err := tl.CheckAndRecord(TierRead, userID)
	if err != nil {
		t.Fatalf("read call 31 unexpected error: %v", err)
	}
	if !limited {
		t.Fatalf("expected call %d to be rate limited", DefaultLimitRead+1)
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retryAfter, got %v", retryAfter)
	}
}

func TestRedisTierLimiter_DownloadTier(t *testing.T) {
	_, tl := setupTestRedis(t)
	const userID = "user-download-test"

	for i := 1; i <= DefaultLimitDownload; i++ {
		limited, retryAfter, err := tl.CheckAndRecord(TierDownload, userID)
		if err != nil {
			t.Fatalf("download call %d unexpected error: %v", i, err)
		}
		if limited {
			t.Fatalf("download call %d unexpectedly rate limited (retryAfter=%v)", i, retryAfter)
		}
	}

	// 11th call must be rate limited
	limited, retryAfter, err := tl.CheckAndRecord(TierDownload, userID)
	if err != nil {
		t.Fatalf("download call 11 unexpected error: %v", err)
	}
	if !limited {
		t.Fatalf("expected call %d to be rate limited", DefaultLimitDownload+1)
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retryAfter, got %v", retryAfter)
	}
}

func TestRedisTierLimiter_WriteTier(t *testing.T) {
	_, tl := setupTestRedis(t)
	const userID = "user-write-test"

	for i := 1; i <= DefaultLimitWrite; i++ {
		limited, retryAfter, err := tl.CheckAndRecord(TierWrite, userID)
		if err != nil {
			t.Fatalf("write call %d unexpected error: %v", i, err)
		}
		if limited {
			t.Fatalf("write call %d unexpectedly rate limited (retryAfter=%v)", i, retryAfter)
		}
	}

	// 6th call must be rate limited
	limited, retryAfter, err := tl.CheckAndRecord(TierWrite, userID)
	if err != nil {
		t.Fatalf("write call 6 unexpected error: %v", err)
	}
	if !limited {
		t.Fatalf("expected call %d to be rate limited", DefaultLimitWrite+1)
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retryAfter, got %v", retryAfter)
	}
}

func TestRedisTierLimiter_PerUserIsolation(t *testing.T) {
	_, tl := setupTestRedis(t)
	const userA = "user-a"
	const userB = "user-b"

	// Exhaust user A write tier
	for i := 1; i <= DefaultLimitWrite; i++ {
		_, _, _ = tl.CheckAndRecord(TierWrite, userA)
	}
	limitedA, _, err := tl.CheckAndRecord(TierWrite, userA)
	if err != nil {
		t.Fatalf("user A write unexpected error: %v", err)
	}
	if !limitedA {
		t.Fatal("expected user A to be rate limited")
	}

	// User B is still unconstrained
	limitedB, _, err := tl.CheckAndRecord(TierWrite, userB)
	if err != nil {
		t.Fatalf("user B write unexpected error: %v", err)
	}
	if limitedB {
		t.Fatal("user B should not be affected by user A rate limit")
	}

	// User A reading is not affected by user A write limit
	limitedARead, _, err := tl.CheckAndRecord(TierRead, userA)
	if err != nil {
		t.Fatalf("user A read unexpected error: %v", err)
	}
	if limitedARead {
		t.Fatal("user A read tier should not be affected by user A write limit")
	}
}

func TestRedisTierLimiter_CustomLimits(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("failed to connect to miniredis: %v", err)
	}
	defer func() { _ = rdb.Close() }()

	customTL := NewRedisTierLimiter(rdb, 3, 2, 1)

	// Custom write: limit is 1
	limited, _, err := customTL.CheckAndRecord(TierWrite, "user-custom")
	if err != nil {
		t.Fatalf("call 1 unexpected error: %v", err)
	}
	if limited {
		t.Fatal("call 1 should succeed")
	}
	limited, retryAfter, err := customTL.CheckAndRecord(TierWrite, "user-custom")
	if err != nil {
		t.Fatalf("call 2 unexpected error: %v", err)
	}
	if !limited || retryAfter <= 0 {
		t.Fatalf("call 2 should be limited with positive retryAfter, got limited=%v, retryAfter=%v", limited, retryAfter)
	}
}

func TestRedisTierLimiter_FailClosed_NilRedis(t *testing.T) {
	tl := NewRedisTierLimiter(nil, 30, 10, 5)

	for _, tier := range []string{TierRead, TierDownload, TierWrite, "unknown"} {
		_, _, err := tl.CheckAndRecord(tier, "user-fail-closed")
		if err == nil {
			t.Errorf("tier %s: expected error on nil Redis", tier)
		}
	}
}

func TestRedisTierLimiter_FailClosed_RedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("failed to connect to miniredis: %v", err)
	}

	tl := NewRedisTierLimiter(rdb, 30, 10, 5)

	// Close redis to simulate outage
	_ = rdb.Close()
	mr.Close()

	_, _, err = tl.CheckAndRecord(TierRead, "user-redis-down")
	if err == nil {
		t.Fatal("expected error when Redis is down")
	}
}

func TestWriteRateLimitedResponse_HeadersAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteRateLimitedResponse(rec, 45*time.Second)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}

	retryAfterStr := rec.Header().Get("Retry-After")
	if retryAfterStr == "" {
		t.Fatal("expected Retry-After header")
	}
	retryAfter, err := strconv.Atoi(retryAfterStr)
	if err != nil || retryAfter != 45 {
		t.Fatalf("expected Retry-After = 45, got %q (err: %v)", retryAfterStr, err)
	}
}

func TestRedisTierLimiter_Concurrency(t *testing.T) {
	_, tl := setupTestRedis(t)
	const routines = 20
	const userID = "user-concurrent"

	var wg sync.WaitGroup
	var allowedCount int32
	var mu sync.Mutex

	for i := 0; i < routines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limited, _, err := tl.CheckAndRecord(TierWrite, userID) // limit is 5
			if err == nil && !limited {
				mu.Lock()
				allowedCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowedCount != DefaultLimitWrite {
		t.Fatalf("expected exactly %d allowed calls for write tier under concurrency, got %d", DefaultLimitWrite, allowedCount)
	}
}
