// Package handlers implements the auth-service HTTP API: signup, login,
// OTP verification, JWT (HS256) refresh, and two-phase password reset.
// All failures use safe error bodies; login/reset are enumeration-safe.
package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nyaruka/phonenumbers"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/mailer"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/notify"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"golang.org/x/crypto/bcrypt"
)

const dbTimeout = 5 * time.Second

// unverifiedExpiry is how long an unverified signup record survives before it
// no longer blocks a new signup. Physical deletion is via a Mongo TTL index;
// handlers also treat expired records as absent (lazy expiry for MemoryStore
// and for TTL lag).
const unverifiedExpiry = 24 * time.Hour

// isUnverifiedExpired reports whether u is an unverified signup older than 24h.
func isUnverifiedExpired(u *models.User, now time.Time) bool {
	if u == nil || u.EmailVerified {
		return false
	}
	if u.CreatedAt.IsZero() {
		return false
	}
	return now.Sub(u.CreatedAt) > unverifiedExpiry
}

// Server wires auth dependencies.
type Server struct {
	Store              store.Store
	Codes              otp.Store
	Lockout            Lockout
	LoginLockout       LoginLockout
	Sender             mailer.Sender
	AppEnv             string
	GatewaySecret      string
	InternalToken      string
	BlocklistHMACKey   string
	DefaultPhoneRegion string
	NotifyURL          string
	NotifyToken        string
}

// New creates a Server.
func New(st store.Store, codes otp.Store, lockout Lockout, sender mailer.Sender, appEnv, gatewaySecret string) *Server {
	return &Server{
		Store:              st,
		Codes:              codes,
		Lockout:            lockout,
		LoginLockout:       NewMemoryLoginLockout(),
		Sender:             sender,
		AppEnv:             appEnv,
		GatewaySecret:      gatewaySecret,
		DefaultPhoneRegion: "EG",
	}
}

func (s *Server) blocklistKey() string {
	if s.BlocklistHMACKey != "" {
		return s.BlocklistHMACKey
	}
	if s.AppEnv == "local" || s.AppEnv == "test" {
		return "local-dev-blocklist-hmac-key"
	}
	return ""
}

func (s *Server) defaultRegion() string {
	if s.DefaultPhoneRegion != "" {
		return s.DefaultPhoneRegion
	}
	return "EG"
}

func computeHMAC(key, data string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func writeStatusRefusal(w http.ResponseWriter, r *http.Request) {
	handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
}

func normalizePhone(raw, defaultRegion string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("phone number cannot be empty")
	}
	if defaultRegion == "" {
		defaultRegion = "EG"
	}
	num, err := phonenumbers.Parse(raw, defaultRegion)
	if err != nil {
		return "", fmt.Errorf("invalid phone number: %w", err)
	}
	if !phonenumbers.IsValidNumber(num) {
		return "", errors.New("invalid phone number")
	}
	return phonenumbers.Format(num, phonenumbers.E164), nil
}

func isValidUUIDv4(id string) bool {
	if len(id) != 36 {
		return false
	}
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	if id[14] != '4' {
		return false
	}
	switch id[19] {
	case '8', '9', 'a', 'b', 'A', 'B':
	default:
		return false
	}
	for i := 0; i < 36; i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		c := id[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

// GatewayAuth requires the gateway secret on every non-health route.
func (s *Server) GatewayAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-Gateway-Secret")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.GatewaySecret)) != 1 {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid request body", err)
		return false
	}
	return true
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validEmail(email string) bool {
	if email == "" || len(email) > 254 {
		return false
	}
	_, err := mail.ParseAddress(email)
	return err == nil
}

func (s *Server) devOTPField() bool {
	return s.AppEnv == "local" || s.AppEnv == "test"
}

func issuePair(userID string, role models.Role, email string) (access, refresh string, err error) {
	access, err = jwtutil.GenerateToken(userID, string(role), email)
	if err != nil {
		return "", "", err
	}
	raw, err := otp.GenerateOpaqueToken()
	if err != nil {
		return "", "", err
	}
	return access, raw, nil
}

func (s *Server) createSessionAndTokens(ctx context.Context, u *models.User, deviceID, deviceLabel string) (access, refresh string, err error) {
	sid, err := jwtutil.GenerateUUID()
	if err != nil {
		return "", "", err
	}
	access, err = jwtutil.GenerateTokenWithSession(u.ID, string(u.Role), u.Email, sid)
	if err != nil {
		return "", "", err
	}
	refresh, err = otp.GenerateOpaqueToken()
	if err != nil {
		return "", "", err
	}
	refreshHash := otp.HashToken(refresh)
	now := time.Now().UTC()
	sess := &models.Session{
		ID:          sid,
		UserID:      u.ID,
		DeviceID:    deviceID,
		DeviceLabel: deviceLabel,
		RefreshHash: refreshHash,
		CreatedAt:   now,
		LastUsedAt:  now,
	}

	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	ended, err := s.Store.CreateOrReplaceSession(dbCtx, sess)
	cancel()
	if err != nil {
		return "", "", err
	}

	for _, endedSess := range ended {
		if endedSess.RefreshHash != "" {
			if err := s.Codes.Delete(ctx, "refresh:"+endedSess.RefreshHash); err != nil {
				return "", "", err
			}
		}
		if err := jwtutil.RevokeSession(endedSess.ID); err != nil {
			return "", "", err
		}
	}

	if err := s.Codes.Set(ctx, "refresh:"+refreshHash, u.ID+":"+sid, 7*24*time.Hour); err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

type signupRequest struct {
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

// Signup registers a new unverified account and sends an email OTP.
// Single role (ADR-0002): the request carries no role and new accounts are always models.RoleUser.
// Refusals (duplicate email, duplicate phone, blocked email, blocked phone) return a generic
// response (P-5) to avoid account or blocklist oracles.
func (s *Server) Signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	name := strings.TrimSpace(req.FullName)
	nameRunes := utf8.RuneCountInString(name)
	if nameRunes < 2 || nameRunes > 100 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid full name: must be 2-100 characters", nil)
		return
	}

	phone, err := normalizePhone(req.Phone, s.defaultRegion())
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid phone number", err)
		return
	}

	email := normalizeEmail(req.Email)
	if !validEmail(email) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid email or password", nil)
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 128 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid email or password", nil)
		return
	}
	// bcrypt (x/crypto) rejects passwords longer than 72 bytes with an error;
	// validate in bytes (an Arabic character is 2 bytes) and return 400, not 500.
	if len(req.Password) > 72 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "password_too_long", "password is too long", nil)
		return
	}

	bKey := s.blocklistKey()
	if bKey == "" {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("blocklist key not configured"))
		return
	}

	const refusalMsg = "unable to complete registration"

	// 1. Blocklist check on normalized email (R9, D15)
	emailHash := computeHMAC(bKey, email)
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	emailBlocked, err := s.Store.IsBlocked(dbCtx, "email", emailHash)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if emailBlocked {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
		return
	}

	// 2. Blocklist check on normalized phone (R9, D15)
	phoneHash := computeHMAC(bKey, phone)
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	phoneBlocked, err := s.Store.IsBlocked(dbCtx, "phone", phoneHash)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if phoneBlocked {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
		return
	}

	// 3. Existing account check by email (P-3, P-4; amended: only verified
	// accounts block signup. Unverified records are replaced, expired
	// unverified records are treated as absent.)
	now := time.Now().UTC()
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	existingEmail, err := s.Store.FindByEmail(dbCtx, email)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	replaceUser := (*models.User)(nil)
	if existingEmail != nil {
		if existingEmail.EmailVerified {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
			return
		}
		if !isUnverifiedExpired(existingEmail, now) {
			// Live unverified record: replace it below (new OTP, old invalid).
			replaceUser = existingEmail
		}
		// Expired unverified: fall through as if absent (TTL deletes physically).
	}

	// 4. Existing account check by phone (P-3, P-4, P-6; amended: only verified
	// active/suspended accounts block signup; unverified phones are free).
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	existingPhone, err := s.Store.FindByPhone(dbCtx, phone)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if existingPhone != nil && existingPhone.EmailVerified && existingPhone.EffectiveStatus() != models.StatusDeleted {
		// Same-record phone reuse on replacement is allowed.
		if replaceUser == nil || existingPhone.ID != replaceUser.ID {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
			return
		}
	}

	role := models.RoleUser
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}

	code, err := otp.GenerateNumericCode(6)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}

	if replaceUser != nil {
		// Replace the live unverified record in place (same ID): new name,
		// phone, password, fresh CreatedAt (restarts 24h expiry), new OTP
		// overwrites the old (old becomes invalid).
		replaceUser.FullName = name
		replaceUser.Phone = phone
		replaceUser.PasswordHash = string(hash)
		replaceUser.CreatedAt = now
		dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
		err = s.Store.Update(dbCtx, replaceUser)
		cancel()
		if err != nil {
			if errors.Is(err, store.ErrDuplicate) {
				handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, err)
				return
			}
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
		err = s.Codes.Set(dbCtx, "signup-otp:"+email, otp.HashToken(code), 10*time.Minute)
		cancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
			return
		}
		_ = s.Sender.SendCode(r.Context(), email, code, "signup")
		resp := map[string]any{
			"id":        replaceUser.ID,
			"email":     email,
			"role":      string(role),
			"full_name": name,
			"phone":     phone,
		}
		if s.devOTPField() {
			resp["dev_otp"] = code
		}
		handlerutil.WriteJSON(w, http.StatusCreated, resp)
		return
	}

	id, err := jwtutil.GenerateUUID()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	u := &models.User{
		ID:           id,
		Email:        email,
		PasswordHash: string(hash),
		Role:         role,
		FullName:     name,
		Phone:        phone,
		Status:       models.StatusActive,
		CreatedAt:    now,
	}

	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	err = s.Store.Create(dbCtx, u)
	cancel()
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			// Race: two signups at the same time, exactly one survives.
			// Re-fetch; if the winner is unverified, replace it.
			dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
			winner, findErr := s.Store.FindByEmail(dbCtx, email)
			cancel()
			if findErr == nil && winner != nil && !winner.EmailVerified && !isUnverifiedExpired(winner, time.Now().UTC()) {
				winner.FullName = name
				winner.Phone = phone
				winner.PasswordHash = string(hash)
				winner.CreatedAt = time.Now().UTC()
				dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
				updErr := s.Store.Update(dbCtx, winner)
				cancel()
				if updErr == nil {
					dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
					setErr := s.Codes.Set(dbCtx, "signup-otp:"+email, otp.HashToken(code), 10*time.Minute)
					cancel()
					if setErr == nil {
						_ = s.Sender.SendCode(r.Context(), email, code, "signup")
						resp := map[string]any{
							"id":        winner.ID,
							"email":     email,
							"role":      string(role),
							"full_name": name,
							"phone":     phone,
						}
						if s.devOTPField() {
							resp["dev_otp"] = code
						}
						handlerutil.WriteJSON(w, http.StatusCreated, resp)
						return
					}
				}
			}
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, err)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	err = s.Codes.Set(dbCtx, "signup-otp:"+email, otp.HashToken(code), 10*time.Minute)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	_ = s.Sender.SendCode(r.Context(), email, code, "signup")
	resp := map[string]any{
		"id":        id,
		"email":     email,
		"role":      string(role),
		"full_name": name,
		"phone":     phone,
	}
	if s.devOTPField() {
		resp["dev_otp"] = code
	}
	handlerutil.WriteJSON(w, http.StatusCreated, resp)
}

type verifyOTPRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	DeviceID    string `json:"device_id"`
	DeviceLabel string `json:"device_label,omitempty"`
}

// VerifyOTP consumes a signup OTP, marks the email verified, and issues tokens.
func (s *Server) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req verifyOTPRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	if !validEmail(email) || len(req.Code) != 6 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid email or code", nil)
		return
	}
	if !isValidUUIDv4(req.DeviceID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid device_id", nil)
		return
	}
	deviceLabel := strings.TrimSpace(req.DeviceLabel)
	if utf8.RuneCountInString(deviceLabel) > 64 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid device_label", nil)
		return
	}
	ctx := r.Context()

	exceeded, err := s.Codes.FailuresExceeded(ctx, "signup", email, otp.MaxFailuresPerHour)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if exceeded {
		handlerutil.WriteSafeError(w, r, http.StatusTooManyRequests, "too_many_attempts", "too many attempts, retry later", nil)
		return
	}

	ok, err := s.Codes.ConsumeWithAttempts(ctx, "signup-otp:"+email, otp.HashToken(strings.TrimSpace(req.Code)), 5, 10*time.Minute)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !ok {
		if _, recErr := s.Codes.RecordFailure(ctx, "signup", email, time.Hour); recErr != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", recErr)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired code", nil)
		return
	}

	if clearErr := s.Codes.ClearFailures(ctx, "signup", email); clearErr != nil {
		log.Printf("[AUTH] ClearFailures error: %v", clearErr)
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	u, err := s.Store.FindByEmail(dbCtx, email)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if u == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired code", nil)
		return
	}
	if u.EffectiveStatus() != models.StatusActive {
		writeStatusRefusal(w, r)
		return
	}
	if !u.EmailVerified && isUnverifiedExpired(u, time.Now().UTC()) {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired code", nil)
		return
	}
	u.EmailVerified = true
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	err = s.Store.Update(dbCtx, u)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	access, refresh, err := s.createSessionAndTokens(ctx, u, req.DeviceID, deviceLabel)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	go notify.Welcome(context.WithoutCancel(r.Context()), s.NotifyURL, s.NotifyToken, u.ID)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"access_token": access, "refresh_token": refresh})
}

type resendSignupRequest struct {
	Email string `json:"email"`
}

// ResendSignupOTP sends a new signup OTP only for an unverified pending signup.
// The answer is always the same generic response, so it does not reveal whether
// the email exists. Cooldown 60s per email, at most 5 per hour per email (plus
// the gateway per-IP tier). OTP valid 10min; per-code attempt limits still apply.
func (s *Server) ResendSignupOTP(w http.ResponseWriter, r *http.Request) {
	var req resendSignupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	ctx := r.Context()

	const genericOK = true
	// Always generic, even for invalid format (anti-enumeration, like RequestReset).
	if !validEmail(email) {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	allowed, err := s.Codes.AllowIssue(ctx, "signup", email)
	if err != nil {
		log.Printf("[AUTH] AllowIssue error: %v", err)
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if !allowed {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	u, err := s.Store.FindByEmail(dbCtx, email)
	cancel()
	if err != nil || u == nil {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if u.EmailVerified || u.EffectiveStatus() != models.StatusActive {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if isUnverifiedExpired(u, time.Now().UTC()) {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	code, err := otp.GenerateNumericCode(6)
	if err != nil {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if setErr := s.Codes.Set(ctx, "signup-otp:"+email, otp.HashToken(code), 10*time.Minute); setErr != nil {
		log.Printf("[AUTH] Set signup-otp error: %v", setErr)
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	_ = s.Sender.SendCode(ctx, email, code, "signup")
	_ = genericOK
	if s.devOTPField() {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "dev_otp": code})
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type loginRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DeviceID    string `json:"device_id"`
	DeviceLabel string `json:"device_label,omitempty"`
}

// Login authenticates with email+password, enforcing the owner-decided lockout:
// (email, IP) 5 in a row -> 15min, (email) 20 in 1h -> 1h. No IP-wide lock;
// IP volume is handled only by the gateway rate limit.
// Locked responses are generic 429 too_many_attempts with Retry-After and do not
// reveal whether the email exists. Unknown emails go through the same counters.
// Never discards store read errors (P-3): store outage returns 503, not 401.
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	if !validEmail(email) || req.Password == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "invalid credentials", nil)
		return
	}
	if !isValidUUIDv4(req.DeviceID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid device_id", nil)
		return
	}
	deviceLabel := strings.TrimSpace(req.DeviceLabel)
	if utf8.RuneCountInString(deviceLabel) > 64 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid device_label", nil)
		return
	}

	ll := s.LoginLockout
	if ll == nil {
		ll = NewMemoryLoginLockout()
	}
	ip := handlerutil.GetIP(r)
	if locked, retryAfter := ll.IsPairLocked(email, ip); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Round(time.Second).Seconds()+1)))
		handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, retry later", "code": "too_many_attempts"})
		return
	}
	if locked, retryAfter := ll.IsEmailLocked(email); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Round(time.Second).Seconds()+1)))
		handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, retry later", "code": "too_many_attempts"})
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	u, err := s.Store.FindByEmail(dbCtx, email)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if u == nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		ll.RecordPairFailure(email, ip)
		ll.RecordEmailFailure(email)
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "invalid credentials", nil)
		return
	}
	if u.EffectiveStatus() != models.StatusActive {
		writeStatusRefusal(w, r)
		return
	}
	if !u.EmailVerified {
		handlerutil.WriteSafeError(w, r, http.StatusForbidden, handlerutil.ErrCodeUnauthorized, "email not verified", nil)
		return
	}
	ll.ResetPair(email, ip)
	access, refresh, err := s.createSessionAndTokens(r.Context(), u, req.DeviceID, deviceLabel)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"access_token": access, "refresh_token": refresh})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh rotates an opaque refresh token into a new pair.
func (s *Server) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid refresh token", nil)
		return
	}
	ctx := r.Context()
	refreshHash := otp.HashToken(req.RefreshToken)
	key := "refresh:" + refreshHash

	// 1. Resolve the refresh key's user and sid from Redis.
	val, err := s.Codes.Get(ctx, key)
	if err != nil || val == "" {
		// Key not in Redis. Check if session was ended with reason replaced.
		dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
		sess, findErr := s.Store.FindSessionByRefreshHash(dbCtx, refreshHash)
		cancel()
		if findErr == nil && sess != nil && sess.EndReason == models.EndReasonReplaced {
			msg := "Sorry, this account's usage limit has been exceeded"
			if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "ar") {
				msg = "عفوًا، لقد تجاوزت الحد المسموح لاستخدام هذا الحساب"
			}
			handlerutil.WriteJSON(w, http.StatusUnauthorized, map[string]string{
				"error": msg,
				"code":  "session_replaced",
			})
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid refresh token", nil)
		return
	}

	userID := val
	sid := ""
	if strings.Contains(val, ":") {
		parts := strings.SplitN(val, ":", 2)
		userID = parts[0]
		sid = parts[1]
	}

	// If session has sid, check if session was ended in DB.
	if sid != "" {
		dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
		sess, err := s.Store.GetSession(dbCtx, sid)
		cancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if sess != nil && sess.EndedAt != nil {
			if sess.EndReason == models.EndReasonReplaced {
				msg := "Sorry, this account's usage limit has been exceeded"
				if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "ar") {
					msg = "عفوًا، لقد تجاوزت الحد المسموح لاستخدام هذا الحساب"
				}
				handlerutil.WriteJSON(w, http.StatusUnauthorized, map[string]string{
					"error": msg,
					"code":  "session_replaced",
				})
				return
			}
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid refresh token", nil)
			return
		}
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	u, err := s.Store.FindByID(dbCtx, userID)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if u == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid refresh token", nil)
		return
	}
	if u.EffectiveStatus() != models.StatusActive {
		writeStatusRefusal(w, r)
		return
	}

	// Atomic take: exactly one concurrent redeemer wins; the rest get "".
	consumedVal, err := s.Codes.Take(ctx, key)
	if err != nil || consumedVal == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid refresh token", nil)
		return
	}

	newRefresh, err := otp.GenerateOpaqueToken()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	newRefreshHash := otp.HashToken(newRefresh)

	var access string
	if sid != "" {
		access, err = jwtutil.GenerateTokenWithSession(u.ID, string(u.Role), u.Email, sid)
	} else {
		access, err = jwtutil.GenerateToken(u.ID, string(u.Role), u.Email)
	}
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}

	now := time.Now().UTC()
	if sid != "" {
		dbCtx, cancel = context.WithTimeout(ctx, dbTimeout)
		err = s.Store.UpdateSessionActivity(dbCtx, sid, newRefreshHash, now)
		cancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
	}

	storeVal := u.ID
	if sid != "" {
		storeVal = u.ID + ":" + sid
	}
	if err := s.Codes.Set(ctx, "refresh:"+newRefreshHash, storeVal, 7*24*time.Hour); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"access_token": access, "refresh_token": newRefresh})
}

// Logout terminates the caller's session, revoking the session and refresh token.
// Authenticated via Bearer token. Idempotent: returns 204 No Content.
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	token := handlerutil.BearerToken(r)
	if token == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	claims, err := jwtutil.ValidateToken(token)
	if err != nil {
		// If the session was already revoked, logout is idempotent -> 204 No Content.
		if errors.Is(err, jwtutil.ErrSessionRevoked) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	if claims.SID != "" {
		now := time.Now().UTC()
		dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
		sess, getErr := s.Store.GetSession(dbCtx, claims.SID)
		cancel()
		if getErr != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", getErr)
			return
		}
		if sess != nil && sess.EndedAt == nil {
			dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
			endErr := s.Store.EndSession(dbCtx, claims.SID, models.EndReasonLogout, now)
			cancel()
			if endErr != nil {
				handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", endErr)
				return
			}
			if sess.RefreshHash != "" {
				if delErr := s.Codes.Delete(r.Context(), "refresh:"+sess.RefreshHash); delErr != nil {
					handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", delErr)
					return
				}
			}
		}

		if revErr := jwtutil.RevokeSession(claims.SID); revErr != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", revErr)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

type resetRequestRequest struct {
	Email string `json:"email"`
}

// RequestReset starts phase 1: always 200 (anti-enumeration), emailing a code only when the account exists.
func (s *Server) RequestReset(w http.ResponseWriter, r *http.Request) {
	var req resetRequestRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	ctx := r.Context()

	allowed, err := s.Codes.AllowIssue(ctx, "reset", email)
	if err != nil {
		log.Printf("[AUTH] AllowIssue error: %v", err)
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if !allowed {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	u, _ := s.Store.FindByEmail(dbCtx, email)
	cancel()
	if u != nil {
		code, err := otp.GenerateNumericCode(6)
		if err == nil {
			if setErr := s.Codes.Set(ctx, "reset-code:"+email, otp.HashToken(code), 10*time.Minute); setErr != nil {
				log.Printf("[AUTH] Set reset-code error: %v", setErr)
				handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
				return
			}
			_ = s.Sender.SendCode(ctx, email, code, "password-reset")
			if s.devOTPField() {
				handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "dev_otp": code})
				return
			}
		}
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type resetVerifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// VerifyResetCode completes phase 1, minting a single-use 10-minute reset token (phase 2 possession).
func (s *Server) VerifyResetCode(w http.ResponseWriter, r *http.Request) {
	var req resetVerifyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	if !validEmail(email) || len(strings.TrimSpace(req.Code)) != 6 {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired code", nil)
		return
	}
	ctx := r.Context()

	exceeded, err := s.Codes.FailuresExceeded(ctx, "reset", email, otp.MaxFailuresPerHour)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if exceeded {
		handlerutil.WriteSafeError(w, r, http.StatusTooManyRequests, "too_many_attempts", "too many attempts, retry later", nil)
		return
	}

	ok, err := s.Codes.ConsumeWithAttempts(ctx, "reset-code:"+email, otp.HashToken(strings.TrimSpace(req.Code)), 5, 10*time.Minute)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !ok {
		if _, recErr := s.Codes.RecordFailure(ctx, "reset", email, time.Hour); recErr != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", recErr)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired code", nil)
		return
	}

	if clearErr := s.Codes.ClearFailures(ctx, "reset", email); clearErr != nil {
		log.Printf("[AUTH] ClearFailures error: %v", clearErr)
	}
	raw, err := otp.GenerateOpaqueToken()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	if err := s.Codes.Set(ctx, "reset-token:"+otp.HashToken(raw), email, 10*time.Minute); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"reset_token": raw})
}

type resetConfirmRequest struct {
	ResetToken  string `json:"reset_token"`
	NewPassword string `json:"new_password"`
}

// ConfirmReset completes phase 2, setting the new password and ending every session.
// Security: revokes ALL of the user's sessions (every Phase 1.7 sid via
// jwt:sid:<sid> keys in Redis, every refresh token) in the same flow as the
// password update. If revocation fails, the reset returns 503. No fresh tokens
// are issued; the user must log in again on every device, including the device
// that performed the reset.
func (s *Server) ConfirmReset(w http.ResponseWriter, r *http.Request) {
	var req resetConfirmRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ResetToken == "" || len(req.NewPassword) < 8 || len(req.NewPassword) > 128 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid token or password", nil)
		return
	}
	// Validate byte length before consuming the token, so a bad password never
	// burns the token. bcrypt rejects >72 bytes; return 400, not 500.
	if len(req.NewPassword) > 72 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "password_too_long", "password is too long", nil)
		return
	}
	ctx := r.Context()
	key := "reset-token:" + otp.HashToken(req.ResetToken)
	email, err := s.Codes.Take(ctx, key)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if email == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired reset token", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	u, err := s.Store.FindByEmail(dbCtx, email)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if u == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired reset token", nil)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}

	// 1. Revoke all access tokens first (fail-closed). This is the backstop for
	// individual sid revocations below.
	if err := jwtutil.RevokeAllUserTokens(u.ID); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	u.PasswordHash = string(hash)

	dbCtx, cancel = context.WithTimeout(ctx, dbTimeout)
	err = s.Store.Update(dbCtx, u)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	// 2. End all sessions and delete their refresh keys (fail-closed).
	now := time.Now().UTC()
	dbCtx, cancel = context.WithTimeout(ctx, dbTimeout)
	endedSessions, endErr := s.Store.EndAllUserSessions(dbCtx, u.ID, models.EndReasonLogout, now)
	cancel()
	if endErr != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", endErr)
		return
	}
	for _, endedSess := range endedSessions {
		if endedSess.RefreshHash != "" {
			if delErr := s.Codes.Delete(ctx, "refresh:"+endedSess.RefreshHash); delErr != nil {
				handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", delErr)
				return
			}
		}
		// Best-effort per-sid revocation; RevokeAllUserTokens above is the backstop.
		_ = jwtutil.RevokeSession(endedSess.ID)
	}

	go notify.PasswordChanged(context.WithoutCancel(r.Context()), s.NotifyURL, s.NotifyToken, u.ID)
	// Successful password reset clears both login locks for that email.
	if ll := s.LoginLockout; ll != nil {
		ll.ResetEmailAll(email)
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "password updated"})
}

// Me returns the JWT-authenticated account profile.
func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	token := handlerutil.BearerToken(r)
	if token == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	claims, err := jwtutil.ValidateToken(token)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	u, err := s.Store.FindByID(dbCtx, claims.UserID)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if u == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"id":             u.ID,
		"email":          u.Email,
		"role":           string(u.Role),
		"email_verified": u.EmailVerified,
		"full_name":      u.FullName,
		"phone":          u.Phone,
	})
}

// Health is the unauthenticated liveness probe.
func Health(w http.ResponseWriter, _ *http.Request) {
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
