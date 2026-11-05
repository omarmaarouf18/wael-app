package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCORS_AllowlistAndCredentials(t *testing.T) {
	allowed := "https://elmetracademy.app, https://www.elmetracademy.app, http://localhost:3000"
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := Logging(allowed)(okHandler)

	// 1. Allowlisted origin gets CORS headers and credentials
	for _, origin := range []string{"https://elmetracademy.app", "https://www.elmetracademy.app", "http://localhost:3000"} {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Errorf("origin %q: expected Access-Control-Allow-Origin %q, got %q", origin, origin, rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("origin %q: expected Access-Control-Allow-Credentials true, got %q", origin, rec.Header().Get("Access-Control-Allow-Credentials"))
		}
	}

	// 2. Disallowed origin (evil site, vercel preview) gets NO allow origin and NO credentials
	for _, origin := range []string{"https://evil.com", "https://preview.vercel.app", "https://wael-app.vercel.app"} {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("disallowed origin %q: expected empty Access-Control-Allow-Origin, got %q", origin, rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Errorf("disallowed origin %q: expected empty Access-Control-Allow-Credentials, got %q", origin, rec.Header().Get("Access-Control-Allow-Credentials"))
		}
	}

	// 3. OPTIONS preflight
	optReq := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/refresh", nil)
	optReq.Header.Set("Origin", "https://elmetracademy.app")
	optRec := httptest.NewRecorder()
	h.ServeHTTP(optRec, optReq)
	if optRec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS preflight expected 204, got %d", optRec.Code)
	}
	if !strings.Contains(optRec.Header().Get("Access-Control-Allow-Headers"), "X-Client") {
		t.Errorf("expected Access-Control-Allow-Headers to contain X-Client, got %q", optRec.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestCSRF_Enforcement(t *testing.T) {
	allowed := "https://elmetracademy.app, http://localhost:3000"
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := CSRF(allowed)(okHandler)

	// 1. Web client on /api/v1/auth/refresh with valid Origin and X-Client: web -> passes
	validReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	validReq.Header.Set("Origin", "https://elmetracademy.app")
	validReq.Header.Set("X-Client", "web")
	validReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: "token-123"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, validReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid web refresh expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Cookie present with disallowed Origin -> refused (403)
	evilReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	evilReq.Header.Set("Origin", "https://evil.com")
	evilReq.Header.Set("X-Client", "web")
	evilReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: "token-123"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, evilReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("evil origin expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. Cookie present without X-Client: web -> refused (403)
	noHeaderReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	noHeaderReq.Header.Set("Origin", "https://elmetracademy.app")
	noHeaderReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: "token-123"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, noHeaderReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing X-Client expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	// 4. Mobile client (no cookie, no X-Client: web) on /api/v1/auth/refresh -> passes
	mobileReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, mobileReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("mobile client expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
