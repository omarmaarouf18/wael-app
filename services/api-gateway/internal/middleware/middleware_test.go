package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
)

func TestRateLimit_BlocksAfterExhaustion(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rdb.Close() }()

	rl := NewRateLimiter(ratelimit.NewRateLimiter(rdb, 2, time.Minute, "test-gw"), []string{"127.0.0.1"})
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RateLimit(rl)(okHandler)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://x/api/v1/auth/health", nil)
		req.RemoteAddr = "10.0.0.9:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "http://x/api/v1/auth/health", nil)
	req.RemoteAddr = "10.0.0.9:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exhaustion, got %d", rec.Code)
	}
}

func TestLogging_HealthPassthrough(t *testing.T) {
	h := Logging("http://localhost:3000")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://x/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
