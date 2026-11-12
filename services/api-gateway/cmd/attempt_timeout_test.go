package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/api-gateway/internal/config"
)

// A slow auth answer (signup: bcrypt plus the synchronous OTP email) must
// reach the client; other upstreams keep the short fail-fast limit
// (2026-11-12: every production signup ended in 502 after 2 s).
func TestNewMux_AuthGetsLongerAttemptTimeout(t *testing.T) {
	prevDefault, prevAuth := defaultAttemptTimeout, authAttemptTimeout
	defaultAttemptTimeout, authAttemptTimeout = 100*time.Millisecond, 2*time.Second
	defer func() { defaultAttemptTimeout, authAttemptTimeout = prevDefault, prevAuth }()

	slow := func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	}
	auth := httptest.NewServer(http.HandlerFunc(slow))
	defer auth.Close()
	academy := httptest.NewServer(http.HandlerFunc(slow))
	defer academy.Close()

	cfg := &config.Config{
		GatewaySecret: "gw-secret",
		Routes: []config.ServiceRoute{
			{Name: "auth", Prefix: "/api/v1/auth/", Target: auth.URL, StripPrefix: "/api/v1"},
			{Name: "academy", Prefix: "/api/v1/academy/", Target: academy.URL, StripPrefix: "/api/v1"},
		},
	}
	mux, err := newMux(cfg, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", strings.NewReader("{}")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("slow auth signup = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/academy/requests", strings.NewReader("{}")))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("slow academy POST = %d, want 502 (short limit kept)", rec.Code)
	}
}

func TestAttemptTimeoutFor(t *testing.T) {
	if got := attemptTimeoutFor(config.ServiceRoute{Name: "auth"}); got != 15*time.Second {
		t.Fatalf("auth = %v, want 15s", got)
	}
	for _, n := range []string{"academy", "notification"} {
		if got := attemptTimeoutFor(config.ServiceRoute{Name: n}); got != 2*time.Second {
			t.Fatalf("%s = %v, want 2s", n, got)
		}
	}
}
