package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

func init() {
	jwtutil.Init("test-jwt-secret")
}

func newTestServer() (*Server, *store.MemoryStore) {
	st := store.NewMemoryStore()
	_ = st.SeedLevels(context.Background())
	srv := New(st, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002")
	return srv, st
}

func makeStudentToken(t *testing.T, userID string) string {
	t.Helper()
	tok, err := jwtutil.GenerateToken(userID, "user", userID+"@example.com")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	return tok
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
	s, _ := newTestServer()
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
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("wrong_gateway_secret_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "wrong-secret")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("valid_gateway_secret_allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}

func TestInternalTokenAuth(t *testing.T) {
	s, _ := newTestServer()
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	authed := s.InternalTokenAuth(nextHandler)

	t.Run("missing_token_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/admin/test", nil)
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("wrong_token_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/admin/test", nil)
		req.Header.Set("X-Internal-Token", "wrong-token")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("valid_token_allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/admin/test", nil)
		req.Header.Set("X-Internal-Token", "test-internal-token")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}

func TestAdminHandler_EmptyInPhase2(t *testing.T) {
	s, _ := newTestServer()
	handler := s.AdminHandler()

	req := httptest.NewRequest(http.MethodGet, "/internal/admin/anything", nil)
	req.Header.Set("X-Internal-Token", "test-internal-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for empty admin surface in Phase 2, got %d", rec.Code)
	}
}

func TestRouteIsolation(t *testing.T) {
	s, _ := newTestServer()
	adminHandler := s.AdminHandler()

	for _, path := range []string{"/health", "/academy/levels"} {
		t.Run("admin_rejects_"+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Internal-Token", "test-internal-token")
			rec := httptest.NewRecorder()
			adminHandler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected admin listener to return 404 for %s, got %d", path, rec.Code)
			}
		})
	}
}

func TestGetLevels(t *testing.T) {
	s, st := newTestServer()
	publicHandler := s.PublicHandler()
	tok := makeStudentToken(t, "student-123")

	t.Run("missing_gateway_secret_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("missing_bearer_token_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("invalid_bearer_token_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer invalid-jwt-token")
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("non_get_method_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("empty_levels_when_no_published_subjects", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var resp models.LevelsResponseDTO
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode JSON: %v", err)
		}
		if len(resp.Levels) != 0 {
			t.Fatalf("expected 0 levels when no published subjects, got %d", len(resp.Levels))
		}
	})

	t.Run("published_subject_reveals_level", func(t *testing.T) {
		// Mark bachelor-y1 as having published subjects
		st.SetLevelPublished("bachelor-y1", true)

		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var resp models.LevelsResponseDTO
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode JSON: %v", err)
		}
		if len(resp.Levels) != 1 {
			t.Fatalf("expected 1 level, got %d", len(resp.Levels))
		}
		if resp.Levels[0].Key != "bachelor-y1" {
			t.Errorf("level key = %q, want bachelor-y1", resp.Levels[0].Key)
		}
		if len(resp.StudyTypes) != 1 {
			t.Fatalf("expected 1 study type, got %d", len(resp.StudyTypes))
		}
		if resp.StudyTypes[0].Key != models.StudyTypeBachelor {
			t.Errorf("study type key = %q, want bachelor", resp.StudyTypes[0].Key)
		}
	})
}
