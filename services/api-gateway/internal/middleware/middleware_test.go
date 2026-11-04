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
	if retryAfter := rec.Header().Get("Retry-After"); retryAfter != "30" {
		t.Fatalf("expected Retry-After header %q, got %q", "30", retryAfter)
	}
}

func TestRateLimit_AuthenticatedRoutesBypassGatewayIPLimiter(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rdb.Close() }()

	// Gateway limit is 1 request per minute
	rl := NewRateLimiter(ratelimit.NewRateLimiter(rdb, 1, time.Minute, "test-gw"), []string{"127.0.0.1"})
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RateLimit(rl)(okHandler)

	clientIP := "192.168.1.50:5678"

	// 1. Exhaust the IP limit via auth route
	reqAuth1 := httptest.NewRequest(http.MethodPost, "http://x/api/v1/auth/login", nil)
	reqAuth1.RemoteAddr = clientIP
	recAuth1 := httptest.NewRecorder()
	h.ServeHTTP(recAuth1, reqAuth1)
	if recAuth1.Code != http.StatusOK {
		t.Fatalf("auth call 1 status = %d, want 200", recAuth1.Code)
	}

	// 2. Second auth request from same IP is blocked (429)
	reqAuth2 := httptest.NewRequest(http.MethodPost, "http://x/api/v1/auth/login", nil)
	reqAuth2.RemoteAddr = clientIP
	recAuth2 := httptest.NewRecorder()
	h.ServeHTTP(recAuth2, reqAuth2)
	if recAuth2.Code != http.StatusTooManyRequests {
		t.Fatalf("auth call 2 status = %d, want 429", recAuth2.Code)
	}

	// 3. Authenticated routes from the EXACT SAME IP must bypass gateway per-IP limiter (CGNAT protection)
	bypassRoutes := []string{
		"http://x/api/v1/academy/videos/vid-1/play",
		"http://x/api/v1/academy/subjects",
		"http://x/api/v1/academy/levels",
		"http://x/api/v1/notifications/stream",
		"http://x/health",
	}

	for _, url := range bypassRoutes {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.RemoteAddr = clientIP
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("route %s unexpectedly rate limited: status = %d (want 200)", url, rec.Code)
		}
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

func TestRateLimit_RetryAfterHeaderExactValues(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rdb.Close() }()

	rl := NewRateLimiter(ratelimit.NewRateLimiter(rdb, 1, time.Minute, "test-retry-after"), []string{"127.0.0.1"})
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RateLimit(rl)(okHandler)

	req1 := httptest.NewRequest(http.MethodPost, "http://x/api/v1/auth/login", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("req 1 status = %d, want 200", rec1.Code)
	}

	// 2nd request triggers lockout (30s)
	req2 := httptest.NewRequest(http.MethodPost, "http://x/api/v1/auth/login", nil)
	req2.RemoteAddr = "10.0.0.1:1234"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("req 2 status = %d, want 429", rec2.Code)
	}
	if got := rec2.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("initial Retry-After = %q, want \"30\"", got)
	}

	// Advance 10s: remaining is ~20s
	mr.FastForward(10 * time.Second)
	req3 := httptest.NewRequest(http.MethodPost, "http://x/api/v1/auth/login", nil)
	req3.RemoteAddr = "10.0.0.1:1234"
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("req 3 status = %d, want 429", rec3.Code)
	}
	if got := rec3.Header().Get("Retry-After"); got != "20" {
		t.Fatalf("Retry-After after 10s = %q, want \"20\"", got)
	}
}

// The public catalog (ADR-0014 W2a) is limited per IP in its own bucket:
// exhausting it does not touch the auth bucket of the same IP, exhausting
// auth does not block the catalog, and another IP is unaffected.
func TestRateLimit_PublicCatalogPerIP(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rdb.Close() }()

	rl := NewRateLimiter(ratelimit.NewRateLimiter(rdb, 1, time.Minute, "test-gw-pub"), []string{"127.0.0.1"})
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RateLimit(rl)(okHandler)

	do := func(method, url, remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, nil)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	const ip = "192.0.2.10:4000"

	if rec := do(http.MethodGet, "http://x/api/v1/academy/public/catalog", ip); rec.Code != http.StatusOK {
		t.Fatalf("catalog 1 = %d, want 200", rec.Code)
	}
	rec := do(http.MethodGet, "http://x/api/v1/academy/public/subjects/s1", ip)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("catalog 2 = %d Retry-After %q, want 429", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := do(http.MethodPost, "http://x/api/v1/auth/login", ip); rec.Code != http.StatusOK {
		t.Fatalf("auth after catalog exhausted = %d, want 200 (separate bucket)", rec.Code)
	}
	if rec := do(http.MethodGet, "http://x/api/v1/academy/subjects", ip); rec.Code != http.StatusOK {
		t.Fatalf("authenticated academy route = %d, want 200 (no edge limit)", rec.Code)
	}
	if rec := do(http.MethodGet, "http://x/api/v1/academy/public/catalog", "198.51.100.20:4000"); rec.Code != http.StatusOK {
		t.Fatalf("other IP = %d, want 200", rec.Code)
	}
}
