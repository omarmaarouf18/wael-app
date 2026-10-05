package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omarmaarouf18/wael-app/api-gateway/internal/config"
)

func TestNew_StripsPrefixAndSetsGatewaySecret(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/auth/health" {
			t.Errorf("backend path = %q, want /auth/health", got)
		}
		if got := r.Header.Get("X-Gateway-Secret"); got != "gw-secret" {
			t.Errorf("X-Gateway-Secret = %q", got)
		}
		if got := r.Header.Get("X-Forwarded-Prefix"); got != "/api/v1/auth/" {
			t.Errorf("X-Forwarded-Prefix = %q", got)
		}
		if got := r.Header.Get("X-Forwarded-For"); got == "" {
			t.Errorf("backend X-Forwarded-For empty")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	route := config.ServiceRoute{Prefix: "/api/v1/auth/", Target: backend.URL, StripPrefix: "/api/v1"}
	h, err := New(route, "gw-secret", []string{"127.0.0.1"}, backend.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://gateway/api/v1/auth/health", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestNew_RemovesClientInternalToken(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Internal-Token"); got != "" {
			t.Errorf("backend received client X-Internal-Token %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	route := config.ServiceRoute{Prefix: "/api/v1/auth/", Target: backend.URL, StripPrefix: "/api/v1"}
	h, err := New(route, "gw-secret", nil, backend.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://gateway/api/v1/auth/health", nil)
	req.Header.Set("X-Internal-Token", "attacker-supplied-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestNew_DoesNotInjectInternalToken(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Internal-Token"); got != "" {
			t.Errorf("backend received X-Internal-Token %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	route := config.ServiceRoute{Prefix: "/api/v1/auth/", Target: backend.URL, StripPrefix: "/api/v1"}
	h, err := New(route, "gw-secret", nil, backend.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://gateway/api/v1/auth/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestGateway_NoInternalRoute(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	mux := http.NewServeMux()
	routes := []config.ServiceRoute{
		{Prefix: "/api/v1/auth/", Target: backend.URL, StripPrefix: "/api/v1"},
		{Prefix: "/api/v1/notifications/", Target: backend.URL, StripPrefix: "/api/v1"},
		{Prefix: "/api/v1/academy/", Target: backend.URL, StripPrefix: "/api/v1"},
	}
	for _, route := range routes {
		h, err := New(route, "gw-secret", nil, backend.Client().Transport)
		if err != nil {
			t.Fatal(err)
		}
		mux.Handle(route.Prefix, h)
	}

	// Verify valid route succeeds
	validReq := httptest.NewRequest(http.MethodGet, "http://gateway/api/v1/auth/health", nil)
	validRec := httptest.NewRecorder()
	mux.ServeHTTP(validRec, validReq)
	if validRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for valid route, got %d", validRec.Code)
	}

	// Internal paths must never be routed by the gateway and must return 404
	internalPaths := []string{
		"/internal",
		"/internal/",
		"/internal/admin/verify",
		"/admin",
		"/admin/",
		"/admin/verify",
		"/api/v1/internal",
		"/api/v1/internal/admin/verify",
	}

	for _, path := range internalPaths {
		req := httptest.NewRequest(http.MethodPost, "http://gateway"+path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for unrouted internal path %s, got %d", path, rec.Code)
		}
	}
}

func TestGateway_AcademyRoute(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/academy/health" {
			t.Errorf("backend path = %q, want /academy/health", got)
		}
		if got := r.Header.Get("X-Gateway-Secret"); got != "gw-secret" {
			t.Errorf("X-Gateway-Secret = %q, want gw-secret", got)
		}
		if got := r.Header.Get("X-Forwarded-Prefix"); got != "/api/v1/academy/" {
			t.Errorf("X-Forwarded-Prefix = %q, want /api/v1/academy/", got)
		}
		if got := r.Header.Get("X-Internal-Token"); got != "" {
			t.Errorf("backend received client X-Internal-Token %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	route := config.ServiceRoute{Prefix: "/api/v1/academy/", Target: backend.URL, StripPrefix: "/api/v1"}
	h, err := New(route, "gw-secret", nil, backend.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://gateway/api/v1/academy/health", nil)
	req.Header.Set("X-Internal-Token", "attacker-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGateway_AuthLogoutRoute(t *testing.T) {
	var receivedMethod, receivedPath, receivedToken string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedToken = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	route := config.ServiceRoute{Prefix: "/api/v1/auth/", Target: backend.URL, StripPrefix: "/api/v1"}
	h, err := New(route, "gw-secret", nil, backend.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://gateway/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer sample-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if receivedMethod != http.MethodPost {
		t.Errorf("backend received method = %q, want POST", receivedMethod)
	}
	if receivedPath != "/auth/logout" {
		t.Errorf("backend received path = %q, want /auth/logout", receivedPath)
	}
	if receivedToken != "Bearer sample-token" {
		t.Errorf("backend received token = %q, want Bearer sample-token", receivedToken)
	}
}

// TestGateway_AccountSettingsRoutes verifies the F-UX2 Part A student routes
// reach their backends through the existing prefix routes (no gateway change
// was needed): /api/v1/auth/* -> auth-service, /api/v1/academy/* ->
// academy-service, with /api/v1 stripped and the gateway secret set.
func TestGateway_AccountSettingsRoutes(t *testing.T) {
	cases := []struct {
		name       string
		prefix     string
		method     string
		gatewayURL string
		wantPath   string
	}{
		{"sessions_list", "/api/v1/auth/", http.MethodGet, "http://gateway/api/v1/auth/sessions", "/auth/sessions"},
		{"session_delete", "/api/v1/auth/", http.MethodDelete, "http://gateway/api/v1/auth/sessions/abc-sid", "/auth/sessions/abc-sid"},
		{"password_change", "/api/v1/auth/", http.MethodPost, "http://gateway/api/v1/auth/password/change", "/auth/password/change"},
		{"profile_patch", "/api/v1/auth/", http.MethodPatch, "http://gateway/api/v1/auth/me", "/auth/me"},
		{"email_change", "/api/v1/auth/", http.MethodPost, "http://gateway/api/v1/auth/email/change", "/auth/email/change"},
		{"email_confirm", "/api/v1/auth/", http.MethodPost, "http://gateway/api/v1/auth/email/confirm", "/auth/email/confirm"},
		{"account_delete", "/api/v1/auth/", http.MethodPost, "http://gateway/api/v1/auth/account/delete", "/auth/account/delete"},
		{"app_config", "/api/v1/academy/", http.MethodGet, "http://gateway/api/v1/academy/app-config", "/academy/app-config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var receivedPath string
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedPath = r.URL.Path
				if got := r.Header.Get("X-Gateway-Secret"); got != "gw-secret" {
					t.Errorf("X-Gateway-Secret = %q, want gw-secret", got)
				}
				if got := r.Header.Get("X-Internal-Token"); got != "" {
					t.Errorf("backend received client X-Internal-Token %q", got)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			route := config.ServiceRoute{Prefix: tc.prefix, Target: backend.URL, StripPrefix: "/api/v1"}
			h, err := New(route, "gw-secret", nil, backend.Client().Transport)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(tc.method, tc.gatewayURL, nil)
			req.Header.Set("X-Internal-Token", "attacker-token")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if receivedPath != tc.wantPath {
				t.Errorf("backend received path = %q, want %q", receivedPath, tc.wantPath)
			}
		})
	}
}
