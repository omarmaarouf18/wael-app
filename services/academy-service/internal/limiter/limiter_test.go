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
		limited, retryAfter := tl.CheckAndRecord(TierRead, userID)
		if limited {
			t.Fatalf("read call %d unexpectedly rate limited (retryAfter=%v)", i, retryAfter)
		}
	}

	// 31st call must be rate limited
	limited, retryAfter := tl.CheckAndRecord(TierRead, userID)
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
		limited, retryAfter := tl.CheckAndRecord(TierDownload, userID)
		if limited {
			t.Fatalf("download call %d unexpectedly rate limited (retryAfter=%v)", i, retryAfter)
		}
	}

	// 11th call must be rate limited
	limited, retryAfter := tl.CheckAndRecord(TierDownload, userID)
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
		limited, retryAfter := tl.CheckAndRecord(TierWrite, userID)
		if limited {
			t.Fatalf("write call %d unexpectedly rate limited (retryAfter=%v)", i, retryAfter)
		}
	}

	// 6th call must be rate limited
	limited, retryAfter := tl.CheckAndRecord(TierWrite, userID)
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
		_, _ = tl.CheckAndRecord(TierWrite, userA)
	}
	limitedA, _ := tl.CheckAndRecord(TierWrite, userA)
	if !limitedA {
		t.Fatal("expected user A to be rate limited")
	}

	// User B is still unconstrained
	limitedB, _ := tl.CheckAndRecord(TierWrite, userB)
	if limitedB {
		t.Fatal("user B should not be affected by user A rate limit")
	}

	// User A reading is not affected by user A write limit
	limitedARead, _ := tl.CheckAndRecord(TierRead, userA)
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
	limited, _ := customTL.CheckAndRecord(TierWrite, "user-custom")
	if limited {
		t.Fatal("call 1 should succeed")
	}
	limited, retryAfter := customTL.CheckAndRecord(TierWrite, "user-custom")
	if !limited || retryAfter <= 0 {
		t.Fatalf("call 2 should be limited with positive retryAfter, got limited=%v, retryAfter=%v", limited, retryAfter)
	}
}

func TestRedisTierLimiter_FailClosed_NilRedis(t *testing.T) {
	tl := NewRedisTierLimiter(nil, 30, 10, 5)

	for _, tier := range []string{TierRead, TierDownload, TierWrite, "unknown"} {
		limited, retryAfter := tl.CheckAndRecord(tier, "user-fail-closed")
		if !limited {
			t.Errorf("tier %s: expected fail closed (limited=true)", tier)
		}
		if retryAfter <= 0 {
			t.Errorf("tier %s: expected positive retryAfter on fail closed, got %v", tier, retryAfter)
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

	limited, retryAfter := tl.CheckAndRecord(TierRead, "user-redis-down")
	if !limited {
		t.Fatal("expected fail closed (limited=true) when Redis is down")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retryAfter on fail closed, got %v", retryAfter)
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
			limited, _ := tl.CheckAndRecord(TierWrite, userID) // limit is 5
			if !limited {
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
