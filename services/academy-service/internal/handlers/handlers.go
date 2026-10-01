// Package handlers implements the academy-service HTTP API handlers and middleware.
package handlers

import (
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
)

const dbTimeout = 5 * time.Second

// Server wires academy-service HTTP dependencies.
type Server struct {
	Store         store.Store
	AppEnv        string
	GatewaySecret string
	InternalToken string
	AuthURL       string
}

// New creates a Server with dependencies.
func New(st store.Store, appEnv, gatewaySecret, internalToken, authURL string) *Server {
	return &Server{
		Store:         st,
		AppEnv:        appEnv,
		GatewaySecret: gatewaySecret,
		InternalToken: internalToken,
		AuthURL:       authURL,
	}
}

// Health is the unauthenticated liveness probe on the public listener.
func Health(w http.ResponseWriter, _ *http.Request) {
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GatewayAuth requires X-Gateway-Secret on every non-health route on the public listener.
func (s *Server) GatewayAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-Gateway-Secret")
		if s.GatewaySecret == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.GatewaySecret)) != 1 {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// InternalTokenAuth requires X-Internal-Token on every admin route (constant-time compare).
// Empty-secret guard ensures an empty configured token never authenticates.
func (s *Server) InternalTokenAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Internal-Token")
		if s.InternalToken == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.InternalToken)) != 1 {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AdminHandler constructs the HTTP handler for the internal admin listener.
// It serves ONLY /internal/admin/* routes (empty for Phase 2.1, 404) behind
// X-Internal-Token and 404s on all public routes.
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	// Phase 2.1: Admin routes will be mounted under /internal/admin/ in Phase 4.
	// Registering /internal/admin/ with 404 ensures matching requests return 404.
	mux.HandleFunc("/internal/admin/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	var h http.Handler = mux
	h = s.InternalTokenAuth(h)
	h = handlerutil.MaxBytesMiddleware(1 << 20)(h)
	return h
}
