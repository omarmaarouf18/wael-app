package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omarmaarouf18/wael-app/api-gateway/internal/config"
)

func TestNew_StripsPrefixAndSetsSecrets(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/auth/health" {
			t.Errorf("backend path = %q, want /auth/health", got)
		}
		if got := r.Header.Get("X-Gateway-Secret"); got != "gw-secret" {
			t.Errorf("X-Gateway-Secret = %q", got)
		}
		if got := r.Header.Get("X-Internal-Token"); got != "internal-123" {
			t.Errorf("X-Internal-Token = %q", got)
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
	h, err := New(route, "gw-secret", "internal-123", []string{"127.0.0.1"}, backend.Client().Transport)
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
