package handlers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
)

// Trust chain: Caddy -> admin-console sets X-Admin-Client-IP -> auth-service admin listener, internal network only.
// Set ONLY by admin-console (Phase 6), which overwrites any client-supplied value.
// auth-service reads X-Admin-Client-IP ONLY on the admin listener and ONLY after X-Internal-Token is valid (constant-time).
// If absent, falls back to the connection RemoteAddr host. Never reads X-Forwarded-For on the admin listener.
// No IP allowlist for now.
func adminClientIP(r *http.Request) string {
	if clientIP := strings.TrimSpace(r.Header.Get("X-Admin-Client-IP")); clientIP != "" {
		return clientIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
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

// VerifyAdmin verifies an admin operator token (POST /internal/admin/verify).
// Reads X-Admin-Token, hashes with SHA-256 hex, looks up in admins collection.
// Valid iff revoked_at is empty and expires_at is in the future.
// Returns {admin_id, name}. No caching.
// Uniform 401 for unknown/expired/revoked/missing admin token.
// Lockout protection after repeated failures keyed on client IP and token hash.
// Store error -> 503.
func (s *Server) VerifyAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	internalTok := r.Header.Get("X-Internal-Token")
	if s.InternalToken == "" || internalTok == "" || subtle.ConstantTimeCompare([]byte(internalTok), []byte(s.InternalToken)) != 1 {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	clientIP := adminClientIP(r)
	ipKey := "admin-verify-ip:" + clientIP

	adminTok := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
	if adminTok == "" {
		if locked, _ := s.Lockout.IsLocked(ipKey); locked {
			handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "too many attempts, retry later",
				"code":  "locked_out",
			})
			return
		}
		s.Lockout.RecordFailure(ipKey)
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	hashBytes := sha256.Sum256([]byte(adminTok))
	tokenHash := hex.EncodeToString(hashBytes[:])
	tokKey := "admin-verify-tok:" + tokenHash

	if locked, _ := s.Lockout.IsLocked(ipKey); locked {
		handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "too many attempts, retry later",
			"code":  "locked_out",
		})
		return
	}
	if locked, _ := s.Lockout.IsLocked(tokKey); locked {
		handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "too many attempts, retry later",
			"code":  "locked_out",
		})
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	adm, err := s.Store.FindAdminByTokenHash(dbCtx, tokenHash)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	now := time.Now().UTC()
	if adm == nil || !adm.IsActive(now) {
		s.Lockout.RecordFailure(ipKey)
		s.Lockout.RecordFailure(tokKey)
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	s.Lockout.Reset(ipKey)
	s.Lockout.Reset(tokKey)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{
		"admin_id": adm.ID,
		"name":     adm.Name,
	})
}

// AdminHandler constructs the HTTP handler for the admin listener.
// It serves ONLY /internal/admin/* routes and 404s on all public routes.
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/admin/verify", s.VerifyAdmin)

	var h http.Handler = mux
	h = s.InternalTokenAuth(h)
	h = handlerutil.MaxBytesMiddleware(1 << 20)(h)
	return h
}
