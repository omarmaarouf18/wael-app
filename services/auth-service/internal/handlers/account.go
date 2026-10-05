// Account self-service handlers (F-UX2 Part A): device list (A1), password
// change (A2), name/phone change with 30-day limits (A3), two-step email
// change (A4), and account deletion with a 30-day grace period (A5).
//
// Every endpoint requires a valid student JWT plus the Phase 1.7 session
// check: the token must carry a sid whose session is still active. Each
// change writes one account_events entry (user_id, type, created_at; no PII
// values) and notifies the student best-effort. Refusals that could reveal
// whether another account holds an email or phone return the same generic
// 409 everywhere.
package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/notify"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"golang.org/x/crypto/bcrypt"
)

// ErrCodeChangeTooSoon is returned (429) when a 30-day-limited field is
// changed too soon. The Retry-After header carries seconds until allowed.
const ErrCodeChangeTooSoon = "change_too_soon"

// changeCooldown mirrors the store's owner-decided per-field edit limit
// (F-UX2) for the fast-path pre-checks; the atomic enforcement lives in the
// store update filter.
const changeCooldown = store.ProfileChangeCooldown

// deletionGrace is the owner-decided self-deletion grace period (F-UX2).
const deletionGrace = 30 * 24 * time.Hour

// sessionClaims holds the authenticated account plus its active session.
type sessionClaims struct {
	user    *models.User
	session *models.Session
	claims  *jwtutil.Claims
}

// requireActiveSession authenticates a self-service request: valid student
// JWT, token carries a sid (Phase 1.7), the session is still active, and the
// account itself is active. Store errors fail closed (503).
func (s *Server) requireActiveSession(w http.ResponseWriter, r *http.Request) (*sessionClaims, bool) {
	token := handlerutil.BearerToken(r)
	if token == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return nil, false
	}
	claims, err := jwtutil.ValidateToken(token)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return nil, false
	}
	if claims.SID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return nil, false
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	sess, err := s.Store.GetSession(dbCtx, claims.SID)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return nil, false
	}
	if sess == nil || sess.EndedAt != nil || sess.UserID != claims.UserID {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return nil, false
	}
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	u, err := s.Store.FindByID(dbCtx, claims.UserID)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return nil, false
	}
	if u == nil || u.EffectiveStatus() != models.StatusActive {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return nil, false
	}
	return &sessionClaims{user: u, session: sess, claims: claims}, true
}

// loginLocked checks the D26 lockout keys for email and writes a generic 429
// with Retry-After when locked. Unknown emails share the same counters.
func (s *Server) loginLocked(w http.ResponseWriter, r *http.Request, email string) bool {
	ll := s.LoginLockout
	if ll == nil {
		ll = NewMemoryLoginLockout()
	}
	ip := handlerutil.GetIP(r)
	if locked, retryAfter := ll.IsPairLocked(email, ip); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Round(time.Second).Seconds()+1)))
		handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, retry later", "code": "too_many_attempts"})
		return true
	}
	if locked, retryAfter := ll.IsEmailLocked(email); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Round(time.Second).Seconds()+1)))
		handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, retry later", "code": "too_many_attempts"})
		return true
	}
	return false
}

// verifyCurrentPassword checks the account password. A wrong password counts
// toward the D26 lockout keys (pair and email) and returns 401; it never
// reveals anything else.
func (s *Server) verifyCurrentPassword(w http.ResponseWriter, r *http.Request, u *models.User, password string) bool {
	if password == "" || s.bcryptCompare([]byte(u.PasswordHash), []byte(password)) != nil {
		ll := s.LoginLockout
		if ll == nil {
			ll = NewMemoryLoginLockout()
		}
		email := normalizeEmail(u.Email)
		ip := handlerutil.GetIP(r)
		ll.RecordPairFailure(email, ip)
		ll.RecordEmailFailure(email)
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "invalid credentials", nil)
		return false
	}
	return true
}

// recordAccountEvent writes one account_events entry. A write failure is
// logged with IDs only and never fails the call (same policy as the admin
// audit log).
func (s *Server) recordAccountEvent(ctx context.Context, userID, eventType string, at time.Time) {
	entry := &models.AccountEvent{UserID: userID, Type: eventType, CreatedAt: at}
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	err := s.Store.CreateAccountEvent(dbCtx, entry)
	cancel()
	if err != nil {
		cleanID := strings.ReplaceAll(strings.ReplaceAll(userID, "\r", ""), "\n", "")
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[AUTH] account event write failed type=%s user %s: %v", eventType, cleanID, err)
	}
}

// endOtherSessions terminates every session except exceptSID: end_reason,
// refresh-key deletion (fail-closed), then per-sid revocation (fail-closed:
// a revocation error aborts with an error and the caller answers 503, so a
// half-revoked change can never report success).
func (s *Server) endOtherSessions(ctx context.Context, userID, exceptSID string, reason models.SessionEndReason, at time.Time) error {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	ended, err := s.Store.EndAllUserSessionsExcept(dbCtx, userID, exceptSID, reason, at)
	cancel()
	if err != nil {
		return err
	}
	for _, sess := range ended {
		if sess.RefreshHash != "" {
			if delErr := s.Codes.Delete(ctx, "refresh:"+sess.RefreshHash); delErr != nil {
				return delErr
			}
		}
		if revErr := jwtutil.RevokeSession(sess.ID); revErr != nil {
			return revErr
		}
	}
	return nil
}

// revokeAllSessions terminates every session of the user (used when the
// current session ends too, e.g. email change and self-deletion).
func (s *Server) revokeAllSessions(ctx context.Context, userID string, reason models.SessionEndReason, at time.Time) error {
	if err := jwtutil.RevokeAllUserTokens(userID); err != nil {
		return err
	}
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	ended, err := s.Store.EndAllUserSessions(dbCtx, userID, reason, at)
	cancel()
	if err != nil {
		return err
	}
	for _, sess := range ended {
		if sess.RefreshHash != "" {
			if delErr := s.Codes.Delete(ctx, "refresh:"+sess.RefreshHash); delErr != nil {
				return delErr
			}
		}
		_ = jwtutil.RevokeSession(sess.ID)
	}
	return nil
}

// MeSubroute dispatches /auth/me: PATCH updates the profile (A3),
// anything else reads it.
func (s *Server) MeSubroute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPatch {
		s.UpdateProfile(w, r)
		return
	}
	s.Me(w, r)
}

// SessionSubroute dispatches /auth/sessions/* (A1):
// - GET /auth/sessions -> Sessions
// - DELETE /auth/sessions/{sid} -> DeleteSession
func (s *Server) SessionSubroute(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/auth/sessions" {
		if r.Method != http.MethodGet {
			handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		s.Sessions(w, r)
		return
	}
	sid := strings.TrimPrefix(r.URL.Path, "/auth/sessions/")
	if sid == "" || strings.Contains(sid, "/") {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "session not found", nil)
		return
	}
	if r.Method != http.MethodDelete {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	s.DeleteSession(w, r, sid)
}

type sessionDTO struct {
	SID         string `json:"sid"`
	DeviceLabel string `json:"device_label"`
	CreatedAt   string `json:"created_at"`
	LastUsedAt  string `json:"last_used_at"`
	Current     bool   `json:"current"`
}

// Sessions serves GET /auth/sessions (A1): the caller's active sessions.
// Fields are sid (opaque), device_label, created_at, last_used_at and current.
// Never the refresh hash, the IP or the raw device_id.
func (s *Server) Sessions(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.requireActiveSession(w, r)
	if !ok {
		return
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	sessions, err := s.Store.ListActiveSessions(dbCtx, sc.user.ID)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	out := make([]sessionDTO, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, sessionDTO{
			SID:         sess.ID,
			DeviceLabel: sess.DeviceLabel,
			CreatedAt:   sess.CreatedAt.UTC().Format(time.RFC3339),
			LastUsedAt:  sess.LastUsedAt.UTC().Format(time.RFC3339),
			Current:     sess.ID == sc.claims.SID,
		})
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// DeleteSession serves DELETE /auth/sessions/{sid} (A1): ends one of the
// caller's own sessions the same way as logout (end_reason=user, refresh key
// deleted, RevokeSession). Ending the current session is logout. Another
// user's sid, an unknown sid, or an already-ended session returns 404.
func (s *Server) DeleteSession(w http.ResponseWriter, r *http.Request, sid string) {
	sc, ok := s.requireActiveSession(w, r)
	if !ok {
		return
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	target, err := s.Store.GetSession(dbCtx, sid)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if target == nil || target.EndedAt != nil || target.UserID != sc.user.ID {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "session not found", nil)
		return
	}
	now := s.now()
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	err = s.Store.EndSession(dbCtx, sid, models.EndReasonUser, now)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if target.RefreshHash != "" {
		if delErr := s.Codes.Delete(r.Context(), "refresh:"+target.RefreshHash); delErr != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", delErr)
			return
		}
	}
	if revErr := jwtutil.RevokeSession(sid); revErr != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", revErr)
		return
	}
	s.recordAccountEvent(r.Context(), sc.user.ID, models.AccountEventSessionRevoked, now)
	go notify.DeviceSessionEnded(context.WithoutCancel(r.Context()), s.NotifyURL, s.NotifyToken, sc.user.ID)
	w.WriteHeader(http.StatusNoContent)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword serves POST /auth/password/change (A2). Same password rules
// as signup (8+ chars, at most 72 bytes). A wrong current password counts
// toward the D26 lockout keys. Ends every OTHER session; the current one
// stays.
func (s *Server) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	sc, ok := s.requireActiveSession(w, r)
	if !ok {
		return
	}
	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.NewPassword) < 8 || len(req.NewPassword) > 128 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid new password: must be 8 or more characters", nil)
		return
	}
	if len(req.NewPassword) > 72 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "password_too_long", "password is too long", nil)
		return
	}
	email := normalizeEmail(sc.user.Email)
	if s.loginLocked(w, r, email) {
		return
	}
	if !s.verifyCurrentPassword(w, r, sc.user, req.CurrentPassword) {
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	now := s.now()
	// End the other sessions before persisting the new hash, so a failure
	// below answers 503 with the password untouched and the retry uses the
	// same current password. Any revocation error fails the change: a
	// half-revoked change never reports success.
	if err := s.endOtherSessions(r.Context(), sc.user.ID, sc.claims.SID, models.EndReasonLogout, now); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	err = s.Store.UpdatePassword(dbCtx, sc.user.ID, string(hash), now)
	cancel()
	if err != nil {
		// The account stopped being active between the session check and
		// the write (suspend, deletion request, or purge).
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", err)
		return
	}
	if ll := s.LoginLockout; ll != nil {
		ll.ResetEmailAll(email)
	}
	s.recordAccountEvent(r.Context(), sc.user.ID, models.AccountEventPasswordChanged, now)
	go notify.PasswordChanged(context.WithoutCancel(r.Context()), s.NotifyURL, s.NotifyToken, sc.user.ID)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "password updated"})
}

type updateProfileRequest struct {
	FullName        string `json:"full_name,omitempty"`
	Phone           string `json:"phone,omitempty"`
	CurrentPassword string `json:"current_password"`
}

// UpdateProfile serves PATCH /auth/me (A3): edit name and/or phone with the
// current password, each at most once every 30 days. A change too soon
// returns 429 change_too_soon with Retry-After (seconds). The phone must not
// be used by another verified account and must not be blocklisted (generic
// 409). Concurrent claims on one phone serialize on the unique partial index;
// a duplicate-key error maps to 409.
func (s *Server) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	sc, ok := s.requireActiveSession(w, r)
	if !ok {
		return
	}
	var req updateProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	wantName := req.FullName != ""
	wantPhone := req.Phone != ""
	if !wantName && !wantPhone {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "nothing to update", nil)
		return
	}
	var newName, newPhone string
	if wantName {
		newName = strings.TrimSpace(req.FullName)
		if n := utf8.RuneCountInString(newName); n < 2 || n > 100 {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid full name: must be 2-100 characters", nil)
			return
		}
	}
	if wantPhone {
		var err error
		newPhone, err = normalizePhone(req.Phone, s.defaultRegion())
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid phone number", err)
			return
		}
	}
	email := normalizeEmail(sc.user.Email)
	if s.loginLocked(w, r, email) {
		return
	}
	if !s.verifyCurrentPassword(w, r, sc.user, req.CurrentPassword) {
		return
	}
	now := s.now()
	if wantName {
		if !sc.user.NameChangedAt.IsZero() && now.Before(sc.user.NameChangedAt.Add(changeCooldown)) {
			writeChangeTooSoon(w, r, sc.user.NameChangedAt.Add(changeCooldown).Sub(now))
			return
		}
	}
	if wantPhone && newPhone != sc.user.Phone {
		if !sc.user.PhoneChangedAt.IsZero() && now.Before(sc.user.PhoneChangedAt.Add(changeCooldown)) {
			writeChangeTooSoon(w, r, sc.user.PhoneChangedAt.Add(changeCooldown).Sub(now))
			return
		}
	}
	const refusalMsg = "unable to complete the change"
	if wantPhone && newPhone != sc.user.Phone {
		bKey := s.blocklistKey()
		if bKey == "" {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("blocklist key not configured"))
			return
		}
		dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
		blocked, err := s.Store.IsBlocked(dbCtx, "phone", computeHMAC(bKey, newPhone))
		cancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if blocked {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
			return
		}
		dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
		holder, err := s.Store.FindByPhone(dbCtx, newPhone)
		cancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if holder != nil && holder.ID != sc.user.ID && holder.EmailVerified {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
			return
		}
	}
	// The atomic write below $set-touches only the supplied fields, guarded
	// by the active status and the 30-day limit in the store filter, so a
	// concurrent edit or deletion request cannot clobber these fields.
	var namePtr, phonePtr *string
	appliedName, appliedPhone := sc.user.FullName, sc.user.Phone
	if wantName {
		namePtr = &newName
		appliedName = newName
	}
	if wantPhone && newPhone != sc.user.Phone {
		phonePtr = &newPhone
		appliedPhone = newPhone
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	err := s.Store.UpdateProfileFields(dbCtx, sc.user.ID, store.ProfileFields{Name: namePtr, Phone: phonePtr}, now)
	cancel()
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, err)
			return
		}
		s.resolveProfileWriteError(w, r, sc.user.ID, store.ProfileFields{Name: namePtr, Phone: phonePtr}, now)
		return
	}
	s.recordAccountEvent(r.Context(), sc.user.ID, models.AccountEventProfileChanged, now)
	go notify.ProfileUpdated(context.WithoutCancel(r.Context()), s.NotifyURL, s.NotifyToken, sc.user.ID)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"id":        sc.user.ID,
		"email":     sc.user.Email,
		"full_name": appliedName,
		"phone":     appliedPhone,
	})
}

// resolveProfileWriteError maps a failed atomic profile write by refetching
// the record: a missing or non-active account answers 401; a 30-day limit
// now in force answers 429 change_too_soon with Retry-After; anything else
// (e.g. a lost phone race that the refetch cannot explain) answers the
// generic 409. A store read failure answers 503.
func (s *Server) resolveProfileWriteError(w http.ResponseWriter, r *http.Request, userID string, f store.ProfileFields, now time.Time) {
	const refusalMsg = "unable to complete the change"
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	fresh, err := s.Store.FindByID(dbCtx, userID)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if fresh == nil || fresh.EffectiveStatus() != models.StatusActive {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	var wait time.Duration
	if f.Name != nil && *f.Name != fresh.FullName && !fresh.NameChangedAt.IsZero() {
		if d := fresh.NameChangedAt.Add(store.ProfileChangeCooldown).Sub(now); d > wait {
			wait = d
		}
	}
	if f.Phone != nil && *f.Phone != fresh.Phone && !fresh.PhoneChangedAt.IsZero() {
		if d := fresh.PhoneChangedAt.Add(store.ProfileChangeCooldown).Sub(now); d > wait {
			wait = d
		}
	}
	if wait > 0 {
		writeChangeTooSoon(w, r, wait)
		return
	}
	handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
}

// writeChangeTooSoon writes 429 change_too_soon with Retry-After (seconds,
// rounded up) until the field may be changed again.
func writeChangeTooSoon(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	secs := int(wait.Seconds()) + 1
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
		"error": "change too soon, retry later",
		"code":  ErrCodeChangeTooSoon,
	})
}

type requestEmailChangeRequest struct {
	NewEmail        string `json:"new_email"`
	CurrentPassword string `json:"current_password"`
}

// RequestEmailChange serves POST /auth/email/change (A4, step 1): with the
// current password, send a 6-digit code to the NEW email (10 min TTL, same
// attempt cap and resend cooldown as signup). A taken or blocklisted email
// gets the same generic 200 response and no code is sent.
func (s *Server) RequestEmailChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	sc, ok := s.requireActiveSession(w, r)
	if !ok {
		return
	}
	var req requestEmailChangeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	newEmail := normalizeEmail(req.NewEmail)
	if !validEmail(newEmail) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid email", nil)
		return
	}
	email := normalizeEmail(sc.user.Email)
	if s.loginLocked(w, r, email) {
		return
	}
	if !s.verifyCurrentPassword(w, r, sc.user, req.CurrentPassword) {
		return
	}
	// Generic response from here on: no oracle on whether the new address is
	// taken or blocklisted.
	if newEmail == email {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	bKey := s.blocklistKey()
	if bKey == "" {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("blocklist key not configured"))
		return
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	blocked, err := s.Store.IsBlocked(dbCtx, "email", computeHMAC(bKey, newEmail))
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !blocked {
		dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
		holder, err := s.Store.FindByEmail(dbCtx, newEmail)
		cancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if holder != nil && holder.ID != sc.user.ID {
			// Taken by another account: same generic response, no code.
			handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
	} else {
		// Blocklisted: same generic response, no code.
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	allowed, err := s.Codes.AllowIssue(r.Context(), "email-change", newEmail)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !allowed {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	code, err := otp.GenerateNumericCode(6)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	ctx := r.Context()
	if err := s.Codes.Set(ctx, "email-change-code:"+sc.user.ID, otp.HashToken(code), 10*time.Minute); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if err := s.Codes.Set(ctx, "email-change-pending:"+sc.user.ID, newEmail, 10*time.Minute); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	_ = s.Sender.SendCode(ctx, newEmail, code, "email-change")
	s.recordAccountEvent(ctx, sc.user.ID, models.AccountEventEmailChangeSent, s.now())
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type confirmEmailChangeRequest struct {
	Code string `json:"code"`
}

// ConfirmEmailChange serves POST /auth/email/confirm (A4, step 2):
// compare-and-set the email, notify the OLD email (masked new address, no
// code), and end ALL sessions including the current one. The app then shows
// login. A duplicate key at confirm time returns a generic 409.
func (s *Server) ConfirmEmailChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	sc, ok := s.requireActiveSession(w, r)
	if !ok {
		return
	}
	var req confirmEmailChangeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(strings.TrimSpace(req.Code)) != 6 {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired code", nil)
		return
	}
	ctx := r.Context()
	email := normalizeEmail(sc.user.Email)
	exceeded, err := s.Codes.FailuresExceeded(ctx, "email-change", email, otp.MaxFailuresPerHour)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if exceeded {
		handlerutil.WriteSafeError(w, r, http.StatusTooManyRequests, "too_many_attempts", "too many attempts, retry later", nil)
		return
	}
	codeOK, err := s.Codes.ConsumeWithAttempts(ctx, "email-change-code:"+sc.user.ID, otp.HashToken(strings.TrimSpace(req.Code)), 5, 10*time.Minute)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !codeOK {
		if _, recErr := s.Codes.RecordFailure(ctx, "email-change", email, time.Hour); recErr != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", recErr)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired code", nil)
		return
	}
	if clearErr := s.Codes.ClearFailures(ctx, "email-change", email); clearErr != nil {
		log.Printf("[AUTH] ClearFailures error: %v", clearErr)
	}
	pending, err := s.Codes.Take(ctx, "email-change-pending:"+sc.user.ID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	newEmail := normalizeEmail(pending)
	if !validEmail(newEmail) || newEmail == email {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired code", nil)
		return
	}
	bKey := s.blocklistKey()
	if bKey == "" {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("blocklist key not configured"))
		return
	}
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	blocked, err := s.Store.IsBlocked(dbCtx, "email", computeHMAC(bKey, newEmail))
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	const refusalMsg = "unable to complete the change"
	if blocked {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
		return
	}
	dbCtx, cancel = context.WithTimeout(ctx, dbTimeout)
	holder, err := s.Store.FindByEmail(dbCtx, newEmail)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if holder != nil && holder.ID != sc.user.ID {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
		return
	}
	oldEmail := sc.user.Email
	now := s.now()
	// End all sessions first (fail-closed backstop), then compare-and-set the
	// email, then end sessions and refresh keys. No fresh tokens: the app
	// shows login.
	if err := jwtutil.RevokeAllUserTokens(sc.user.ID); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	dbCtx, cancel = context.WithTimeout(ctx, dbTimeout)
	err = s.Store.SetEmail(dbCtx, sc.user.ID, oldEmail, newEmail, now)
	cancel()
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, err)
			return
		}
		// Status or email mismatch: refetch to answer precisely. A missing
		// or non-active account answers 401; an already-applied change
		// (idempotent double-submit) continues to success; anything else
		// is the generic 409.
		dbCtx, cancel = context.WithTimeout(ctx, dbTimeout)
		fresh, findErr := s.Store.FindByID(dbCtx, sc.user.ID)
		cancel()
		if findErr != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", findErr)
			return
		}
		if fresh == nil || fresh.EffectiveStatus() != models.StatusActive {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}
		if fresh.Email != newEmail {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, err)
			return
		}
	}
	dbCtx, cancel = context.WithTimeout(ctx, dbTimeout)
	endedSessions, endErr := s.Store.EndAllUserSessions(dbCtx, sc.user.ID, models.EndReasonLogout, now)
	cancel()
	if endErr != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", endErr)
		return
	}
	for _, sess := range endedSessions {
		if sess.RefreshHash != "" {
			if delErr := s.Codes.Delete(ctx, "refresh:"+sess.RefreshHash); delErr != nil {
				handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", delErr)
				return
			}
		}
		_ = jwtutil.RevokeSession(sess.ID)
	}
	masked := maskEmail(newEmail)
	_ = s.Sender.SendNotice(context.WithoutCancel(ctx), oldEmail,
		"Your account email was changed",
		"The email on your account was just changed to "+masked+". If this was not you, contact support at once.")
	s.recordAccountEvent(ctx, sc.user.ID, models.AccountEventEmailChanged, now)
	go notify.EmailChanged(context.WithoutCancel(ctx), s.NotifyURL, s.NotifyToken, sc.user.ID)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// maskEmail hides all but the first character of the local part:
// "wagdy@example.com" -> "w***@example.com".
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	local, domain := email[:at], email[at+1:]
	if local == "" || domain == "" {
		return "***"
	}
	return string([]rune(local)[:1]) + "***@" + domain
}

type requestDeletionRequest struct {
	CurrentPassword string `json:"current_password"`
	Confirm         string `json:"confirm"`
}

// RequestDeletion serves POST /auth/account/delete (A5): with the current
// password and the typed word "حذف", the status becomes pending_deletion with
// purge_after = now + 30 days; all sessions end; an email states the deletion
// date and that logging in before then cancels it.
func (s *Server) RequestDeletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	sc, ok := s.requireActiveSession(w, r)
	if !ok {
		return
	}
	var req requestDeletionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Confirm) != "حذف" {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "confirmation word is required", nil)
		return
	}
	email := normalizeEmail(sc.user.Email)
	if s.loginLocked(w, r, email) {
		return
	}
	if !s.verifyCurrentPassword(w, r, sc.user, req.CurrentPassword) {
		return
	}
	now := s.now()
	purgeAfter := now.Add(deletionGrace)
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	err := s.Store.RequestDeletion(dbCtx, sc.user.ID, now, purgeAfter)
	cancel()
	if err != nil {
		if errors.Is(err, store.ErrStatusConflict) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "unable to complete the request", err)
			return
		}
		if errors.Is(err, store.ErrUserNotFound) {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", err)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if err := s.revokeAllSessions(r.Context(), sc.user.ID, models.EndReasonLogout, now); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	dateStr := purgeAfter.UTC().Format("2006-01-02")
	_ = s.Sender.SendNotice(context.WithoutCancel(r.Context()), email,
		"Your account will be deleted",
		"Your account will be deleted on "+dateStr+". Signing in before then cancels the deletion.")
	s.recordAccountEvent(r.Context(), sc.user.ID, models.AccountEventDeletionRequested, now)
	go notify.DeletionRequested(context.WithoutCancel(r.Context()), s.NotifyURL, s.NotifyToken, sc.user.ID)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "deletion_date": dateStr})
}

// PurgeExpiredDeletions anonymizes every pending_deletion account past
// purge_after (A5 purge job, run hourly by the service ticker). Order per
// account: RevokeAllUserTokens (fail-closed backstop; a failure skips the
// account for this round), then the atomic compare-and-set purge (a login
// that won the race makes it a no-op conflict), then session and refresh-key
// cleanup. A self-purged identity is NOT blocklisted, so it may sign up again
// (SPEC D2 amendment, F-UX2 A6); admin bans keep the blocklist (R9).
func (s *Server) PurgeExpiredDeletions(ctx context.Context) (int, error) {
	now := s.now()
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	due, err := s.Store.ListDeletionsDue(dbCtx, now)
	cancel()
	if err != nil {
		return 0, err
	}
	purged := 0
	var firstErr error
	for _, u := range due {
		if err := jwtutil.RevokeAllUserTokens(u.ID); err != nil {
			log.Printf("[AUTH] purge revoke failed user %s: %v", u.ID, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		anon := fmt.Sprintf("deleted-%s@deleted.local", u.ID)
		dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
		err = s.Store.PurgeDeletion(dbCtx, u.ID, anon, now)
		cancel()
		if err != nil {
			if errors.Is(err, store.ErrStatusConflict) {
				continue // login won the race; account is active again
			}
			log.Printf("[AUTH] purge failed user %s: %v", u.ID, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		dbCtx, cancel = context.WithTimeout(ctx, dbTimeout)
		ended, endErr := s.Store.EndAllUserSessions(dbCtx, u.ID, models.EndReasonLogout, now)
		cancel()
		if endErr != nil {
			log.Printf("[AUTH] purge session cleanup failed user %s: %v", u.ID, endErr)
		} else {
			for _, sess := range ended {
				if sess.RefreshHash != "" {
					_ = s.Codes.Delete(ctx, "refresh:"+sess.RefreshHash)
				}
				_ = jwtutil.RevokeSession(sess.ID)
			}
		}
		s.recordAccountEvent(ctx, u.ID, models.AccountEventAccountPurged, now)
		purged++
	}
	return purged, firstErr
}
