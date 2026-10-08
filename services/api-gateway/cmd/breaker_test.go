package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/omarmaarouf18/wael-app/api-gateway/internal/config"
	"github.com/omarmaarouf18/wael-app/shared/infra/resilience"
)

func breakerState(t *testing.T, name string) string {
	t.Helper()
	for _, s := range resilience.GetBreakerStats() {
		if s.Name == name {
			return s.State
		}
	}
	t.Fatalf("breaker %q not registered", name)
	return ""
}

// A failing academy upstream opens only the academy breaker; login keeps
// proxying to auth-service, and POST is never retried.
func TestNewMux_PerUpstreamBreakers(t *testing.T) {
	var academyHits, authHits atomic.Int32
	academy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		academyHits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer academy.Close()
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHits.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/auth/login" {
			t.Errorf("auth got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer auth.Close()
	notif := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer notif.Close()

	cfg := &config.Config{
		GatewaySecret: "gw-secret",
		Routes: []config.ServiceRoute{
			{Name: "auth", Prefix: "/api/v1/auth/", Target: auth.URL, StripPrefix: "/api/v1"},
			{Name: "notification", Prefix: "/api/v1/notifications/", Target: notif.URL, StripPrefix: "/api/v1"},
			{Name: "academy", Prefix: "/api/v1/academy/", Target: academy.URL, StripPrefix: "/api/v1"},
		},
	}
	mux, err := newMux(cfg, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}

	// POST to the failing upstream: exactly one attempt, no retry.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/academy/requests", strings.NewReader("{}")))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("academy POST status = %d, want 502", rec.Code)
	}
	if got := academyHits.Load(); got != 1 {
		t.Fatalf("academy POST attempts = %d, want 1 (POST must not be retried)", got)
	}

	// Repeated 503s trip the academy breaker (5 consecutive failures).
	for i := 0; i < 5 && breakerState(t, "api-gateway-academy") != "open"; i++ {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/academy/catalog", nil))
	}
	if got := breakerState(t, "api-gateway-academy"); got != "open" {
		t.Fatalf("academy breaker = %s, want open", got)
	}
	before := academyHits.Load()
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/academy/catalog", nil))
	if rec.Code != http.StatusBadGateway || academyHits.Load() != before {
		t.Fatalf("open academy breaker still reached upstream (status %d)", rec.Code)
	}

	// Login still proxies; the other breakers stay closed.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{}")))
	if rec.Code != http.StatusOK || authHits.Load() != 1 {
		t.Fatalf("login status = %d, auth hits = %d; want 200, 1", rec.Code, authHits.Load())
	}
	for _, name := range []string{"api-gateway-auth", "api-gateway-notification"} {
		if got := breakerState(t, name); got != "closed" {
			t.Fatalf("%s = %s, want closed", name, got)
		}
	}
}
