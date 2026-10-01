package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
)

func newTestServer() *Server {
	st := store.NewMemoryStore()
	return New(st, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002")
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %q, want ok", body["status"])
	}
}

func TestGatewayAuth(t *testing.T) {
	s := newTestServer()
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	authed := s.GatewayAuth(nextHandler)

	t.Run("health_bypasses_gateway_secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("missing_gateway_secret_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("wrong_gateway_secret_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		req.Header.Set("X-Gateway-Secret", "wrong-secret")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("valid_gateway_secret_accepted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("empty_configured_secret_fails_closed", func(t *testing.T) {
		sEmpty := New(store.NewMemoryStore(), "test", "", "test-internal-token", "")
		authedEmpty := sEmpty.GatewayAuth(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		req.Header.Set("X-Gateway-Secret", "")
		rec := httptest.NewRecorder()
		authedEmpty.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}

func TestInternalTokenAuth(t *testing.T) {
	s := newTestServer()
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	authed := s.InternalTokenAuth(nextHandler)

	t.Run("missing_internal_token_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/admin/test", nil)
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("wrong_internal_token_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/admin/test", nil)
		req.Header.Set("X-Internal-Token", "wrong-token")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("valid_internal_token_accepted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/admin/test", nil)
		req.Header.Set("X-Internal-Token", "test-internal-token")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("empty_configured_token_fails_closed", func(t *testing.T) {
		sEmpty := New(store.NewMemoryStore(), "test", "test-gateway-secret", "", "")
		authedEmpty := sEmpty.InternalTokenAuth(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/internal/admin/test", nil)
		req.Header.Set("X-Internal-Token", "")
		rec := httptest.NewRecorder()
		authedEmpty.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}

func TestAdminHandler_RoutesAndIsolation(t *testing.T) {
	s := newTestServer()
	adminHandler := s.AdminHandler()

	// Public listener setup (matching cmd/main.go)
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/health", Health)
	publicHandler := s.GatewayAuth(publicMux)

	t.Run("admin_listener_requires_internal_token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/admin/", nil)
		rec := httptest.NewRecorder()
		adminHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 without X-Internal-Token, got %d", rec.Code)
		}
	})

	t.Run("admin_listener_returns_404_for_empty_admin_routes", func(t *testing.T) {
		for _, path := range []string{"/internal/admin/", "/internal/admin/subjects", "/internal/admin/verify"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Internal-Token", "test-internal-token")
			rec := httptest.NewRecorder()
			adminHandler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for %s on admin listener, got %d", path, rec.Code)
			}
		}
	})

	t.Run("admin_listener_404s_on_public_routes", func(t *testing.T) {
		for _, path := range []string{"/health", "/academy/levels", "/academy/subjects"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Internal-Token", "test-internal-token")
			rec := httptest.NewRecorder()
			adminHandler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for %s on admin listener, got %d", path, rec.Code)
			}
		}
	})

	t.Run("public_listener_404s_on_admin_routes", func(t *testing.T) {
		for _, path := range []string{"/internal/admin/", "/internal/admin/subjects", "/internal/admin/verify"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
			req.Header.Set("X-Internal-Token", "test-internal-token")
			rec := httptest.NewRecorder()
			publicHandler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for %s on public listener, got %d", path, rec.Code)
			}
		}
	})
}
