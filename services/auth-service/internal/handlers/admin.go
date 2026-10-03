package handlers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/notify"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
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

// authenticateAdmin verifies X-Internal-Token and X-Admin-Token headers.
// Applies constant-time compare on X-Internal-Token with empty-secret guard.
// Enforces lockout keyed on client IP and token hash.
// Returns resolved models.Admin, or HTTP status code, message, and error.
func (s *Server) authenticateAdmin(r *http.Request) (*models.Admin, int, string, error) {
	internalTok := r.Header.Get("X-Internal-Token")
	if s.InternalToken == "" || internalTok == "" || subtle.ConstantTimeCompare([]byte(internalTok), []byte(s.InternalToken)) != 1 {
		return nil, http.StatusUnauthorized, "unauthorized", nil
	}

	clientIP := adminClientIP(r)
	ipKey := "admin-verify-ip:" + clientIP

	adminTok := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
	if adminTok == "" {
		if locked, _ := s.Lockout.IsLocked(ipKey); locked {
			return nil, http.StatusTooManyRequests, "too many attempts, retry later", nil
		}
		s.Lockout.RecordFailure(ipKey)
		return nil, http.StatusUnauthorized, "unauthorized", nil
	}

	hashBytes := sha256.Sum256([]byte(adminTok))
	tokenHash := hex.EncodeToString(hashBytes[:])
	tokKey := "admin-verify-tok:" + tokenHash

	if locked, _ := s.Lockout.IsLocked(ipKey); locked {
		return nil, http.StatusTooManyRequests, "too many attempts, retry later", nil
	}
	if locked, _ := s.Lockout.IsLocked(tokKey); locked {
		return nil, http.StatusTooManyRequests, "too many attempts, retry later", nil
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	adm, err := s.Store.FindAdminByTokenHash(dbCtx, tokenHash)
	cancel()
	if err != nil {
		return nil, http.StatusServiceUnavailable, "service temporarily unavailable", err
	}

	now := time.Now().UTC()
	if adm == nil || !adm.IsActive(now) {
		s.Lockout.RecordFailure(ipKey)
		s.Lockout.RecordFailure(tokKey)
		return nil, http.StatusUnauthorized, "unauthorized", nil
	}

	s.Lockout.Reset(ipKey)
	s.Lockout.Reset(tokKey)
	return adm, http.StatusOK, "", nil
}

func writeAdminAuthError(w http.ResponseWriter, r *http.Request, status int, msg string, err error) {
	if status == http.StatusTooManyRequests {
		handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "too many attempts, retry later",
			"code":  "locked_out",
		})
		return
	}
	if status == http.StatusServiceUnavailable {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, msg, err)
		return
	}
	handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, msg, err)
}

func (s *Server) notifyStudent(ctx context.Context, userID, action string, pushFn func(context.Context, string, string, string) error) {
	if s.NotifyURL == "" {
		return
	}
	pushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	cleanID := strings.ReplaceAll(strings.ReplaceAll(userID, "\r", ""), "\n", "")
	if err := pushFn(pushCtx, s.NotifyURL, s.NotifyToken, userID); err != nil {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[AUTH] student notification failed (%s user %s): %v", action, cleanID, err)
	}
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

	adm, status, msg, err := s.authenticateAdmin(r)
	if status != http.StatusOK {
		writeAdminAuthError(w, r, status, msg, err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{
		"admin_id": adm.ID,
		"name":     adm.Name,
	})
}

// Accounts handles GET /internal/admin/accounts?search=&status=&page=&limit=.
// Search matches by name, email, phone (normalized like signup) or exact id.
// Escape user input in any regex (handled via regexp.QuoteMeta).
// Limit default 20, max 100. Status filter validated.
// Response is UserDTO: never exposes password_hash, OTP, or reset fields.
func (s *Server) Accounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	_, status, msg, err := s.authenticateAdmin(r)
	if status != http.StatusOK {
		writeAdminAuthError(w, r, status, msg, err)
		return
	}

	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("search"))
	if utf8.RuneCountInString(search) > 100 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "bad_request", "search too long", nil)
		return
	}
	statusFilter := strings.TrimSpace(q.Get("status"))

	if statusFilter != "" {
		switch models.UserStatus(statusFilter) {
		case models.StatusActive, models.StatusSuspended, models.StatusDeleted:
		default:
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "bad_request", "invalid status filter", nil)
			return
		}
	}

	page := 1
	if pStr := strings.TrimSpace(q.Get("page")); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p >= 1 {
			page = p
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

	var normPhone string
	if search != "" {
		if p, err := normalizePhone(search, s.defaultRegion()); err == nil {
			normPhone = p
		}
	}

	var idsList []string
	if rawIDs := strings.TrimSpace(q.Get("ids")); rawIDs != "" {
		parts := strings.Split(rawIDs, ",")
		if len(parts) > 100 {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "bad_request", "at most 100 ids allowed", nil)
			return
		}
		seen := make(map[string]bool, len(parts))
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" && !seen[trimmed] {
				seen[trimmed] = true
				idsList = append(idsList, trimmed)
			}
		}
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	users, total, err := s.Store.ListUsers(dbCtx, store.UserFilter{
		Search:          search,
		NormalizedPhone: normPhone,
		Status:          statusFilter,
		IDs:             idsList,
		Page:            page,
		Limit:           limit,
	})
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	dtos := make([]*models.UserDTO, len(users))
	for i, u := range users {
		dtos[i] = u.ToDTO()
	}
	if dtos == nil {
		dtos = []*models.UserDTO{}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"items": dtos,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// AccountSubroute dispatches requests under /internal/admin/accounts/:
// - POST /internal/admin/accounts/{id}/suspend
// - POST /internal/admin/accounts/{id}/reactivate
// - DELETE /internal/admin/accounts/{id}
func (s *Server) AccountSubroute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/internal/admin/accounts/")
	parts := strings.Split(path, "/")

	if len(parts) == 2 && parts[1] == "suspend" {
		id := strings.TrimSpace(parts[0])
		if id == "" {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
			return
		}
		s.SuspendAccount(w, r, id)
		return
	}

	if len(parts) == 2 && parts[1] == "reactivate" {
		id := strings.TrimSpace(parts[0])
		if id == "" {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
			return
		}
		s.ReactivateAccount(w, r, id)
		return
	}

	if len(parts) == 1 && parts[0] != "" {
		id := strings.TrimSpace(parts[0])
		s.DeleteAccount(w, r, id)
		return
	}

	handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
}

// SuspendAccount handles POST /internal/admin/accounts/{id}/suspend {reason 1-1000}.
// Order: (1) RevokeAllUserTokens (idempotent), (2) SetStatus active->suspended (CAS).
// Same state -> 409. Revoking first means a retry after a partial failure is safe.
func (s *Server) SuspendAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	adm, status, msg, err := s.authenticateAdmin(r)
	if status != http.StatusOK {
		writeAdminAuthError(w, r, status, msg, err)
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	runeLen := utf8.RuneCountInString(reason)
	if runeLen < 1 || runeLen > 1000 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "bad_request", "reason must be between 1 and 1000 characters", nil)
		return
	}
	cleanReason := strings.ReplaceAll(strings.ReplaceAll(reason, "\r", " "), "\n", " ")

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	user, err := s.Store.FindByID(dbCtx, id)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if user == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "account not found", nil)
		return
	}
	if user.EffectiveStatus() == models.StatusSuspended {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "account is already suspended", nil)
		return
	}
	if user.EffectiveStatus() == models.StatusDeleted {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "deleted accounts cannot be suspended", nil)
		return
	}

	// 1. RevokeAllUserTokens (idempotent, fails closed if Redis error)
	if err := jwtutil.RevokeAllUserTokens(id); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	now := time.Now().UTC()
	// End all user sessions (end_reason=admin)
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	endedSessions, endErr := s.Store.EndAllUserSessions(dbCtx, id, models.EndReasonAdmin, now)
	cancel()
	if endErr != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", endErr)
		return
	}
	for _, endedSess := range endedSessions {
		if endedSess.RefreshHash != "" {
			_ = s.Codes.Delete(r.Context(), "refresh:"+endedSess.RefreshHash)
		}
		// RevokeAllUserTokens (fail-closed) is the backstop; keep ignoring individual RevokeSession errors.
		_ = jwtutil.RevokeSession(endedSess.ID)
	}

	// 2. SetStatus active->suspended (CAS)
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	err = s.Store.SetStatus(dbCtx, id, string(models.StatusActive), string(models.StatusSuspended), cleanReason, now)
	cancel()
	if err != nil {
		if errors.Is(err, store.ErrStatusConflict) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "account status conflict", err)
			return
		}
		if errors.Is(err, store.ErrUserNotFound) {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "account not found", err)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	// Best-effort student notification
	s.notifyStudent(r.Context(), id, "suspend", notify.AccountSuspended)

	// Audit write
	auditID, _ := jwtutil.GenerateUUID()
	auditEntry := &models.AuditLog{
		ID:         auditID,
		ActorID:    adm.ID,
		ActorName:  adm.Name,
		Action:     "account_suspend",
		TargetType: "user",
		TargetID:   id,
		Detail:     cleanReason,
		CreatedAt:  now,
	}
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	auditErr := s.Store.CreateAuditLog(dbCtx, auditEntry)
	cancel()
	cleanID := strings.ReplaceAll(strings.ReplaceAll(id, "\r", ""), "\n", "")
	if auditErr != nil {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ERROR] admin audit log write failed for action %s actor_id=%s target_id=%s: %v", auditEntry.Action, adm.ID, cleanID, auditErr)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReactivateAccount handles POST /internal/admin/accounts/{id}/reactivate.
// SetStatus suspended->active. Deleted accounts cannot be reactivated (409).
func (s *Server) ReactivateAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	adm, status, msg, err := s.authenticateAdmin(r)
	if status != http.StatusOK {
		writeAdminAuthError(w, r, status, msg, err)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	user, err := s.Store.FindByID(dbCtx, id)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if user == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "account not found", nil)
		return
	}
	if user.EffectiveStatus() == models.StatusDeleted {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "deleted accounts cannot be reactivated", nil)
		return
	}
	if user.EffectiveStatus() == models.StatusActive {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "account is already active", nil)
		return
	}

	now := time.Now().UTC()
	// SetStatus suspended->active (CAS)
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	err = s.Store.SetStatus(dbCtx, id, string(models.StatusSuspended), string(models.StatusActive), "", now)
	cancel()
	if err != nil {
		if errors.Is(err, store.ErrStatusConflict) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "account status conflict", err)
			return
		}
		if errors.Is(err, store.ErrDuplicate) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "phone number already taken by another active account", err)
			return
		}
		if errors.Is(err, store.ErrUserNotFound) {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "account not found", err)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	// Best-effort student notification
	s.notifyStudent(r.Context(), id, "reactivate", notify.AccountReactivated)

	// Audit write
	auditID, _ := jwtutil.GenerateUUID()
	auditEntry := &models.AuditLog{
		ID:         auditID,
		ActorID:    adm.ID,
		ActorName:  adm.Name,
		Action:     "account_reactivate",
		TargetType: "user",
		TargetID:   id,
		CreatedAt:  now,
	}
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	auditErr := s.Store.CreateAuditLog(dbCtx, auditEntry)
	cancel()
	cleanID := strings.ReplaceAll(strings.ReplaceAll(id, "\r", ""), "\n", "")
	if auditErr != nil {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ERROR] admin audit log write failed for action %s actor_id=%s target_id=%s: %v", auditEntry.Action, adm.ID, cleanID, auditErr)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteAccount handles DELETE /internal/admin/accounts/{id} {reason 1-1000}.
// Order: (1) blocklist entries for normalized email and phone (HMAC, idempotent),
// (2) RevokeAllUserTokens, (3) SetStatus active|suspended -> deleted (CAS). Already deleted -> 409.
func (s *Server) DeleteAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodDelete {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	adm, status, msg, err := s.authenticateAdmin(r)
	if status != http.StatusOK {
		writeAdminAuthError(w, r, status, msg, err)
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	runeLen := utf8.RuneCountInString(reason)
	if runeLen < 1 || runeLen > 1000 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "bad_request", "reason must be between 1 and 1000 characters", nil)
		return
	}
	cleanReason := strings.ReplaceAll(strings.ReplaceAll(reason, "\r", " "), "\n", " ")

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	user, err := s.Store.FindByID(dbCtx, id)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if user == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "account not found", nil)
		return
	}
	if user.EffectiveStatus() == models.StatusDeleted {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "account is already deleted", nil)
		return
	}

	bKey := s.blocklistKey()
	if bKey == "" {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("blocklist key not configured"))
		return
	}

	now := time.Now().UTC()

	// 1. Blocklist entries for normalized email and phone (HMAC, idempotent)
	email := normalizeEmail(user.Email)
	if email != "" {
		emailHash := computeHMAC(bKey, email)
		dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
		err = s.Store.AddToBlocklist(dbCtx, "email", emailHash, cleanReason, now)
		cancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
	}
	if user.Phone != "" {
		phoneHash := computeHMAC(bKey, user.Phone)
		dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
		err = s.Store.AddToBlocklist(dbCtx, "phone", phoneHash, cleanReason, now)
		cancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
	}

	// 2. RevokeAllUserTokens
	if err := jwtutil.RevokeAllUserTokens(id); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	// End all user sessions (end_reason=admin)
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	endedSessions, endErr := s.Store.EndAllUserSessions(dbCtx, id, models.EndReasonAdmin, now)
	cancel()
	if endErr != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", endErr)
		return
	}
	for _, endedSess := range endedSessions {
		if endedSess.RefreshHash != "" {
			_ = s.Codes.Delete(r.Context(), "refresh:"+endedSess.RefreshHash)
		}
		// RevokeAllUserTokens (fail-closed) is the backstop; keep ignoring individual RevokeSession errors.
		_ = jwtutil.RevokeSession(endedSess.ID)
	}

	// 3. SetStatus active|suspended -> deleted (CAS)
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	err = s.Store.SetStatus(dbCtx, id, store.FromActiveOrSuspended, string(models.StatusDeleted), cleanReason, now)
	cancel()
	if err != nil {
		if errors.Is(err, store.ErrStatusConflict) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "account status conflict", err)
			return
		}
		if errors.Is(err, store.ErrUserNotFound) {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "account not found", err)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	// Best-effort student notification
	s.notifyStudent(r.Context(), id, "delete", notify.AccountDeleted)

	// Audit write
	auditID, _ := jwtutil.GenerateUUID()
	auditEntry := &models.AuditLog{
		ID:         auditID,
		ActorID:    adm.ID,
		ActorName:  adm.Name,
		Action:     "account_delete",
		TargetType: "user",
		TargetID:   id,
		Detail:     cleanReason,
		CreatedAt:  now,
	}
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	auditErr := s.Store.CreateAuditLog(dbCtx, auditEntry)
	cancel()
	cleanID := strings.ReplaceAll(strings.ReplaceAll(id, "\r", ""), "\n", "")
	if auditErr != nil {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ERROR] admin audit log write failed for action %s actor_id=%s target_id=%s: %v", auditEntry.Action, adm.ID, cleanID, auditErr)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AuditLogs handles GET /internal/admin/audit-log?page=&limit=.
// Returns paginated audit log entries, newest first. Limit default 20, max 100.
func (s *Server) AuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	_, status, msg, err := s.authenticateAdmin(r)
	if status != http.StatusOK {
		writeAdminAuthError(w, r, status, msg, err)
		return
	}

	q := r.URL.Query()
	page := 1
	if pStr := strings.TrimSpace(q.Get("page")); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p >= 1 {
			page = p
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

// AdminHandler constructs the HTTP handler for the admin listener.
// It serves ONLY /internal/admin/* routes and 404s on all public routes.
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/admin/verify", s.VerifyAdmin)
	mux.HandleFunc("/internal/admin/accounts", s.Accounts)
	mux.HandleFunc("/internal/admin/accounts/", s.AccountSubroute)
	mux.HandleFunc("/internal/admin/audit-log", s.AuditLogs)

	var h http.Handler = mux
	h = s.InternalTokenAuth(h)
	h = handlerutil.MaxBytesMiddleware(1 << 20)(h)
	return h
}
