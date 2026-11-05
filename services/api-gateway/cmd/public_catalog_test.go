package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/omarmaarouf18/wael-app/api-gateway/internal/config"
	"github.com/omarmaarouf18/wael-app/api-gateway/internal/middleware"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
)

// TestGateway_FullChain_PublicCatalog verifies that requests sent through the
// gateway's full middleware chain as wired in cmd/main.go (Logging -> RateLimit ->
// MaxBytesMiddleware -> ServeMux -> ReverseProxy with resilience transport) correctly
// proxy both public routes without student auth, while all other paths through the
// gateway still require authentication.
func TestGateway_FullChain_PublicCatalog(t *testing.T) {
	const gwSecret = "test-gateway-secret-at-least-32-chars-long"

	// Mock academy-service upstream enforcing the exact public-catalog bypass rule
	// alongside GatewayAuth and StudentAuth.
	academyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. GatewayAuth: every non-health route requires X-Gateway-Secret.
		if r.Header.Get("X-Gateway-Secret") != gwSecret {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized","code":"unauthorized"}`))
			return
		}

		// 2. Public routes: exact matches bypass StudentAuth.
		if r.URL.Path == "/academy/public/catalog" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "public, max-age=60")
			w.Header().Set("ETag", `"etag-catalog"`)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"study_types":[{"key":"bachelor","title":{"ar":"ليسانس الحقوق","en":"LL.B."}}]}`))
			return
		}
		if id, ok := strings.CutPrefix(r.URL.Path, "/academy/public/subjects/"); ok && id != "" && !strings.Contains(id, "/") {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "public, max-age=60")
			w.Header().Set("ETag", `"etag-subject"`)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"` + id + `","title":{"ar":"شريعة","en":"Sharia"}}`))
			return
		}

		// 3. Any other route requires student Bearer token.
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized","code":"unauthorized"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"authenticated":true}`))
	}))
	defer academyServer.Close()

	// Direct access to academy without gateway secret must be refused.
	directReq, _ := http.NewRequest(http.MethodGet, academyServer.URL+"/academy/public/catalog", nil)
	directRec := httptest.NewRecorder()
	academyServer.Config.Handler.ServeHTTP(directRec, directReq)
	if directRec.Code != http.StatusUnauthorized {
		t.Fatalf("direct access without gateway secret = %d, want 401", directRec.Code)
	}

	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rdb.Close() }()

	cfg := &config.Config{
		AppEnv:          "local",
		GatewaySecret:   gwSecret,
		AllowedOrigin:   "http://localhost:3000",
		TrustedProxyIPs: []string{"127.0.0.1"},
		Routes: []config.ServiceRoute{
			{
				Name:        "academy",
				Prefix:      "/api/v1/academy/",
				Target:      academyServer.URL,
				StripPrefix: "/api/v1",
			},
		},
	}

	mux, err := newMux(cfg, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}

	// Wire full middleware chain exactly as in cmd/main.go lines 134-137.
	rl := middleware.NewRateLimiter(ratelimit.NewRateLimiter(rdb, 100, time.Minute, "gateway"), cfg.TrustedProxyIPs)
	chain := handlerutil.MaxBytesMiddleware(1 << 20)(mux)
	chain = middleware.RateLimit(rl)(chain)
	chain = middleware.CSRF(cfg.AllowedOrigin)(chain)
	chain = middleware.Logging(cfg.AllowedOrigin)(chain)

	doRequest := func(method, path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://gateway"+path, nil)
		req.RemoteAddr = "192.0.2.1:1234"
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		return rec
	}

	// 1. GET /api/v1/academy/public/catalog without auth -> 200 OK with cache headers.
	recCatalog := doRequest(http.MethodGet, "/api/v1/academy/public/catalog", "")
	if recCatalog.Code != http.StatusOK {
		t.Fatalf("public catalog through full chain = %d, want 200; body: %s", recCatalog.Code, recCatalog.Body.String())
	}
	if got := recCatalog.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("Cache-Control = %q, want 'public, max-age=60'", got)
	}
	if got := recCatalog.Header().Get("ETag"); got != `"etag-catalog"` {
		t.Errorf("ETag = %q, want '\"etag-catalog\"'", got)
	}
	var catalogBody map[string]any
	if err := json.Unmarshal(recCatalog.Body.Bytes(), &catalogBody); err != nil {
		t.Fatalf("catalog JSON decode: %v", err)
	}
	if _, ok := catalogBody["study_types"]; !ok {
		t.Errorf("expected study_types in catalog response, got %+v", catalogBody)
	}

	// 2. GET /api/v1/academy/public/subjects/s1 without auth -> 200 OK.
	recSubject := doRequest(http.MethodGet, "/api/v1/academy/public/subjects/s1", "")
	if recSubject.Code != http.StatusOK {
		t.Fatalf("public subject through full chain = %d, want 200; body: %s", recSubject.Code, recSubject.Body.String())
	}
	if got := recSubject.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("subject Cache-Control = %q, want 'public, max-age=60'", got)
	}
	if got := recSubject.Header().Get("ETag"); got != `"etag-subject"` {
		t.Errorf("subject ETag = %q, want '\"etag-subject\"'", got)
	}

	// 3. Any other path through the gateway without auth -> 401 unauthorized.
	nonPublicPaths := []string{
		"/api/v1/academy/subjects",
		"/api/v1/academy/levels",
		"/api/v1/academy/subjects/s1",
		"/api/v1/academy/public/subjects/s1/extra",
		"/api/v1/academy/public/catalog/extra",
	}
	for _, p := range nonPublicPaths {
		rec := doRequest(http.MethodGet, p, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("path %s without auth = %d, want 401; body: %s", p, rec.Code, rec.Body.String())
		}
		var errBody map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err == nil {
			if errBody["code"] != "unauthorized" {
				t.Errorf("path %s err code = %v, want 'unauthorized'", p, errBody["code"])
			}
		}
	}

	// 4. Authenticated path with Bearer token -> 200 OK.
	recAuth := doRequest(http.MethodGet, "/api/v1/academy/subjects", "valid-token")
	if recAuth.Code != http.StatusOK {
		t.Fatalf("authenticated path with token = %d, want 200", recAuth.Code)
	}
}
