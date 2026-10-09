// Package handlers implements the academy-service admin API (Phase 4.1+).
//
// The admin listener (ADMIN_LISTEN_ADDR, internal network only, mTLS, never
// published) serves ONLY /internal/admin/* routes. Every admin route requires,
// in this order:
//
//  1. X-Internal-Token, constant-time compared against INTERNAL_SERVICE_TOKEN
//     (empty-secret guard, mirroring auth-service).
//  2. X-Admin-Token, verified on EVERY request via auth-service
//     POST {AUTH_ADMIN_URL}/internal/admin/verify over mTLS with a 3 s
//     timeout. No caching, so revocation takes effect immediately (ADR-0008
//     Section 6.4). Auth-service down (or any verify failure that is not a
//     clean 401/429) fails closed with 503.
//
// The verified admin_id and admin name are placed in the request context.
// X-Admin-Client-IP is read and forwarded to the verify call (so auth-service
// lockout keys on the real client, as set by admin-console which overwrites
// any client-supplied value) but is NEVER trusted locally for auth and NEVER
// persisted: admin_audit_log stores no IP addresses (SPEC Section 5,
// ADR-0008 Sections 7 and 9).
//
// Every mutating admin call writes one admin_audit_log entry in the same
// operation flow; like auth-service, an audit write failure is logged at
// error level and does not fail the call.
package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// adminVerifyTimeout bounds every admin token verification call.
const adminVerifyTimeout = 3 * time.Second

type adminContextKey string

const adminIdentityContextKey adminContextKey = "admin"

// AdminIdentity is the verified admin operator attached to admin requests.
type AdminIdentity struct {
	ID   string
	Name string
}

// AdminFromRequest returns the verified admin identity from the request context.
func AdminFromRequest(r *http.Request) (*AdminIdentity, bool) {
	if adm, ok := r.Context().Value(adminIdentityContextKey).(*AdminIdentity); ok && adm != nil {
		return adm, true
	}
	return nil, false
}

// verifyHTTPClient returns the configured mTLS verify client, or a plain
// client with the verify timeout when none is configured (tests, local dev).
func (s *Server) verifyHTTPClient() *http.Client {
	if s.VerifyClient != nil {
		return s.VerifyClient
	}
	return &http.Client{Timeout: adminVerifyTimeout}
}

// verifyAdminToken validates an admin token against auth-service
// POST {AUTH_ADMIN_URL}/internal/admin/verify. No caching: every admin request
// verifies, so revocation and expiry take effect immediately.
// It returns the identity on success, otherwise an HTTP status and safe code:
// 401 for unknown/expired/revoked/missing tokens (mirroring auth's uniform
// 401), 429 when auth-service reports lockout, 503 fail-closed otherwise.
func (s *Server) verifyAdminToken(r *http.Request, adminToken string) (*AdminIdentity, int, string) {
	base := strings.TrimRight(strings.TrimSpace(s.AuthAdminURL), "/")
	if base == "" || s.InternalToken == "" {
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}

	ctx, cancel := context.WithTimeout(r.Context(), adminVerifyTimeout)
	defer cancel()

	// #nosec G704 //nolint:gosec -- base URL comes from validated service config, path is fixed
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/internal/admin/verify", nil)
	if err != nil {
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}
	req.Header.Set("X-Internal-Token", s.InternalToken)
	req.Header.Set("X-Admin-Token", adminToken)
	// Forwarded so auth-service lockout keys on the real client address as set
	// by admin-console. Never trusted locally for auth decisions.
	if clientIP := strings.TrimSpace(r.Header.Get("X-Admin-Client-IP")); clientIP != "" {
		req.Header.Set("X-Admin-Client-IP", clientIP)
	}

	resp, err := s.verifyHTTPClient().Do(req)
	if err != nil {
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var v struct {
			AdminID string `json:"admin_id"`
			Name    string `json:"name"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil {
			return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
		}
		if strings.TrimSpace(v.AdminID) == "" || strings.TrimSpace(v.Name) == "" {
			return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
		}
		return &AdminIdentity{ID: v.AdminID, Name: v.Name}, http.StatusOK, ""
	case http.StatusUnauthorized:
		return nil, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized
	case http.StatusTooManyRequests:
		return nil, http.StatusTooManyRequests, "locked_out"
	default:
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}
}

// requireAdmin enforces the two-token admin auth chain, in order:
// X-Internal-Token (constant-time, empty-secret guard), then X-Admin-Token
// verified via auth-service. Missing/bad internal token and bad admin tokens
// return 401 like auth-service; auth-service lockout relays as 429; auth
// down fails closed as 503.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Internal-Token")
		if s.InternalToken == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.InternalToken)) != 1 {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}

		adminToken := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
		if adminToken == "" {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}

		adm, status, code := s.verifyAdminToken(r, adminToken)
		switch status {
		case http.StatusOK:
			ctx := context.WithValue(r.Context(), adminIdentityContextKey, adm)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		case http.StatusTooManyRequests:
			handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "too many attempts, retry later",
				"code":  code,
			})
			return
		case http.StatusUnauthorized:
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		default:
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", nil)
			return
		}
	})
}

// decodeAdminJSON decodes a strict JSON body (unknown fields refused).
func decodeAdminJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid request body", err)
		return false
	}
	return true
}

// sanitizeDetail strips CR/LF from audit detail text (ADR-0008 Section 7).
func sanitizeDetail(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
}

// writeAdminAudit writes one admin_audit_log entry for a mutating call.
// The detail carries no secrets; no IP address is persisted (SPEC Section 5,
// ADR-0008 Sections 7 and 9).
func (s *Server) writeAdminAudit(ctx context.Context, adm *AdminIdentity, action, targetType, targetID, detail string) error {
	id, _ := jwtutil.GenerateUUID()
	entry := &models.AuditLog{
		ID:         id,
		ActorID:    adm.ID,
		ActorName:  adm.Name,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     sanitizeDetail(detail),
		CreatedAt:  time.Now().UTC(),
	}
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	return s.Store.CreateAuditLog(dbCtx, entry)
}

// logAuditFailure records an audit write failure at error level with the
// action and target id only (no secrets). Like auth-service account actions,
// an audit failure never fails the admin call itself.
func logAuditFailure(adm *AdminIdentity, action, targetID string, err error) {
	cleanTarget := strings.ReplaceAll(strings.ReplaceAll(targetID, "\r", ""), "\n", "")
	// #nosec G706 -- cleanTarget sanitized of CR/LF
	log.Printf("[ERROR] admin audit log write failed for action %s actor_id=%s target_id=%s: %v", action, adm.ID, cleanTarget, err)
}

// AuditLogs handles GET /internal/admin/audit-log?page=&limit=.
// Returns paginated audit entries, newest first, in the same response shape
// as auth-service ({items, total, page, limit}) so the console can merge both
// logs ordered by created_at descending.
func (s *Server) AuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	q := r.URL.Query()
	const maxPage = 10000
	page := 1
	if pStr := strings.TrimSpace(q.Get("page")); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil {
			if p < 1 {
				page = 1
			} else if p > maxPage {
				page = maxPage
			} else {
				page = p
			}
		}
	}

	limit := 20
	if lStr := strings.TrimSpace(q.Get("limit")); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil {
			if l <= 0 {
				limit = 20
			} else if l > 100 {
				limit = 100
			} else {
				limit = l
			}
		}
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	logs, total, err := s.Store.ListAuditLogs(dbCtx, page, limit)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if logs == nil {
		logs = []*models.AuditLog{}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"items": logs,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// notifyStudent delivers an internal push to notification-service, best-effort.
// A failure is logged with clean ids only (no secrets) and never fails the admin call.
func (s *Server) notifyStudent(ctx context.Context, userID, action string, pushFn func(context.Context) error) {
	if s.NotifyURL == "" {
		return
	}
	pushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	cleanID := strings.ReplaceAll(strings.ReplaceAll(userID, "\r", ""), "\n", "")
	if err := pushFn(pushCtx); err != nil {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ACADEMY] student notification failed (%s user %s): %v", action, cleanID, err)
	}
}

// AdminHandler constructs the HTTP handler for the internal admin listener.
// It serves ONLY /internal/admin/* routes behind X-Internal-Token +
// X-Admin-Token (verified via auth-service, fail closed) and 404s on all
// public routes.
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/admin/audit-log", s.AuditLogs)
	mux.HandleFunc("/internal/admin/levels", s.AdminLevels)
	mux.HandleFunc("/internal/admin/levels/", s.AdminLevelSubroute)
	mux.HandleFunc("/internal/admin/subjects", s.AdminSubjects)
	mux.HandleFunc("/internal/admin/subjects/", s.AdminSubjectSubroute)
	mux.HandleFunc("/internal/admin/videos/", s.AdminVideoSubroute)
	mux.HandleFunc("/internal/admin/requests", s.AdminRequests)
	mux.HandleFunc("/internal/admin/requests/", s.AdminRequestSubroute)
	mux.HandleFunc("/internal/admin/entitlements", s.AdminEntitlements)
	mux.HandleFunc("/internal/admin/entitlements/", s.AdminEntitlementSubroute)
	mux.HandleFunc("/internal/admin/settings", s.AdminSettings)

	var h http.Handler = mux
	h = s.requireAdmin(h)
	// 1 MiB per body, except the PDF upload route (MAX_PDF_BYTES + form overhead).
	h = s.adminBodyLimit(h)
	return h
}
