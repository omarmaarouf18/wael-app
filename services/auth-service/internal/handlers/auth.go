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
	"net/http"
	"net/mail"
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

// Server wires auth dependencies.
type Server struct {
	Store              store.Store
	Codes              otp.Store
	Lockout            Lockout
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

	// 3. Existing account check by email (P-3, P-4)
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	existingEmail, err := s.Store.FindByEmail(dbCtx, email)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if existingEmail != nil {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
		return
	}

	// 4. Existing account check by phone (P-3, P-4, P-6)
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	existingPhone, err := s.Store.FindByPhone(dbCtx, phone)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if existingPhone != nil && existingPhone.EffectiveStatus() != models.StatusDeleted {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, nil)
		return
	}

	role := models.RoleUser
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
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
	}

	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	err = s.Store.Create(dbCtx, u)
	cancel()
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, refusalMsg, err)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	code, err := otp.GenerateNumericCode(6)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
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
	Email string `json:"email"`
	Code  string `json:"code"`
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
	ctx := r.Context()
	ok, err := s.Codes.ConsumeWithAttempts(ctx, "signup-otp:"+email, otp.HashToken(strings.TrimSpace(req.Code)), 5, 10*time.Minute)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired code", nil)
		return
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
	u.EmailVerified = true
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	err = s.Store.Update(dbCtx, u)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	access, refresh, err := issuePair(u.ID, u.Role, u.Email)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	if err := s.Codes.Set(ctx, "refresh:"+otp.HashToken(refresh), u.ID, 7*24*time.Hour); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	go notify.Welcome(context.WithoutCancel(r.Context()), s.NotifyURL, s.NotifyToken, u.ID)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"access_token": access, "refresh_token": refresh})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login authenticates with email+password, enforcing lockout with backoff.
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
	ip := handlerutil.GetIP(r)
	emailKey := "login:email:" + email
	ipKey := "login:ip:" + ip
	if locked, _ := s.Lockout.IsLocked(emailKey); locked {
		handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, retry later", "code": "locked_out"})
		return
	}
	if locked, _ := s.Lockout.IsLocked(ipKey); locked {
		handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, retry later", "code": "locked_out"})
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
		s.Lockout.RecordFailure(emailKey)
		s.Lockout.RecordFailure(ipKey)
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
	s.Lockout.Reset(emailKey)
	s.Lockout.Reset(ipKey)
	access, refresh, err := issuePair(u.ID, u.Role, u.Email)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	if err := s.Codes.Set(r.Context(), "refresh:"+otp.HashToken(refresh), u.ID, 7*24*time.Hour); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
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
	key := "refresh:" + otp.HashToken(req.RefreshToken)
	// Resolve the refresh key's user. The refresh entry is not deleted here.
	userID, err := s.Codes.Get(ctx, key)
	if err != nil || userID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid refresh token", nil)
		return
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
	consumedID, err := s.Codes.Take(ctx, key)
	if err != nil || consumedID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid refresh token", nil)
		return
	}
	access, refresh, err := issuePair(u.ID, u.Role, u.Email)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	if err := s.Codes.Set(ctx, "refresh:"+otp.HashToken(refresh), u.ID, 7*24*time.Hour); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"access_token": access, "refresh_token": refresh})
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
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	u, _ := s.Store.FindByEmail(dbCtx, email)
	cancel()
	if u != nil {
		code, err := otp.GenerateNumericCode(6)
		if err == nil {
			_ = s.Codes.Set(ctx, "reset-code:"+email, otp.HashToken(code), 10*time.Minute)
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
	ok, err := s.Codes.ConsumeWithAttempts(ctx, "reset-code:"+email, otp.HashToken(strings.TrimSpace(req.Code)), 5, 10*time.Minute)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired code", nil)
		return
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

// ConfirmReset completes phase 2, setting the new password and revoking the token.
func (s *Server) ConfirmReset(w http.ResponseWriter, r *http.Request) {
	var req resetConfirmRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ResetToken == "" || len(req.NewPassword) < 8 || len(req.NewPassword) > 128 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid token or password", nil)
		return
	}
	ctx := r.Context()
	key := "reset-token:" + otp.HashToken(req.ResetToken)
	email, err := s.Codes.Get(ctx, key)
	if err != nil || email == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired reset token", nil)
		return
	}
	_ = s.Codes.Delete(ctx, key)

	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	u, err := s.Store.FindByEmail(dbCtx, email)
	cancel()
	if err != nil || u == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeInvalidToken, "invalid or expired reset token", nil)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	u.PasswordHash = string(hash)

	dbCtx, cancel = context.WithTimeout(ctx, dbTimeout)
	err = s.Store.Update(dbCtx, u)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	go notify.PasswordChanged(context.WithoutCancel(r.Context()), s.NotifyURL, s.NotifyToken, u.ID)
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
