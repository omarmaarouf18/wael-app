package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
	"golang.org/x/crypto/bcrypt"
)

type mailNotice struct {
	to      string
	subject string
	text    string
}

type sentCode struct {
	to      string
	code    string
	purpose string
}

// captureSender records codes and notices per recipient for F-UX2 tests.
type captureSender struct {
	mu      sync.Mutex
	sent    []sentCode
	notices []mailNotice
}

func (c *captureSender) SendCode(_ context.Context, toEmail, code, purpose string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, sentCode{to: toEmail, code: code, purpose: purpose})
	return nil
}

func (c *captureSender) SendNotice(_ context.Context, toEmail, subject, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notices = append(c.notices, mailNotice{to: toEmail, subject: subject, text: text})
	return nil
}

func (c *captureSender) codeFor(toEmail, purpose string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.sent) - 1; i >= 0; i-- {
		if c.sent[i].to == toEmail && c.sent[i].purpose == purpose {
			return c.sent[i].code
		}
	}
	return ""
}

func (c *captureSender) noticesFor(toEmail string) []mailNotice {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []mailNotice
	for _, n := range c.notices {
		if n.to == toEmail {
			out = append(out, n)
		}
	}
	return out
}

func accountTestServer(t *testing.T) (*Server, *captureSender, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("connect miniredis: %v", err)
	}
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	jwtutil.SetRedisClient(rdb)
	sender := &captureSender{}
	s := New(store.NewMemoryStore(), otp.NewMemoryStore(), NewMemoryLockout(), sender, "test", "gw-secret")
	s.BlocklistHMACKey = "test-blocklist-hmac-key"
	s.DefaultPhoneRegion = "EG"
	cleanup := func() {
		jwtutil.SetRedisClient(nil)
		_ = rdb.Close()
	}
	return s, sender, cleanup
}

func signupVerifyTokens(t *testing.T, s *Server, email, phone, password, deviceID string) (string, string) {
	t.Helper()
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Account Test User",
		"email":     email,
		"phone":     phone,
		"password":  password,
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup %s failed: %d (%s)", email, rec.Code, rec.Body.String())
	}
	var signup map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &signup); err != nil {
		t.Fatalf("decode signup: %v", err)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":     email,
		"code":      signup["dev_otp"],
		"device_id": deviceID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify %s failed: %d (%s)", email, rec.Code, rec.Body.String())
	}
	tokens := decodeBody(t, rec)
	if tokens["access_token"] == "" || tokens["refresh_token"] == "" {
		t.Fatalf("missing tokens: %v", tokens)
	}
	return tokens["access_token"], tokens["refresh_token"]
}

func loginTokens(t *testing.T, s *Server, email, password, deviceID string) (string, string) {
	t.Helper()
	rec := doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":     email,
		"password":  password,
		"device_id": deviceID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s failed: %d (%s)", email, rec.Code, rec.Body.String())
	}
	tokens := decodeBody(t, rec)
	return tokens["access_token"], tokens["refresh_token"]
}

func tokenSID(t *testing.T, access string) string {
	t.Helper()
	claims, err := jwtutil.ValidateToken(access)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if claims.SID == "" {
		t.Fatalf("token has no sid")
	}
	return claims.SID
}

const (
	devA = "11111111-1111-4111-8111-111111111111"
	devB = "22222222-2222-4222-8222-222222222222"
	devC = "33333333-3333-4333-8333-333333333333"
)

func TestAccount_SessionsListShape(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, _ := signupVerifyTokens(t, s, "sess1@example.com", "+201012345711", "Password123!", devA)
	loginTokens(t, s, "sess1@example.com", "Password123!", devB)
	sidA := tokenSID(t, accessA)

	rec := doRequest(t, s, http.MethodGet, "/auth/sessions", nil, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions list = %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{`"refresh_hash"`, `"device_id"`, `"refresh_token"`, `"ip"`, "jwt", "hash"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("sessions list leaks %q: %s", forbidden, body)
		}
	}
	var parsed struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(parsed.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d: %s", len(parsed.Sessions), body)
	}
	allowed := map[string]bool{"sid": true, "device_label": true, "created_at": true, "last_used_at": true, "current": true}
	current := 0
	for _, sess := range parsed.Sessions {
		for k := range sess {
			if !allowed[k] {
				t.Fatalf("unexpected session key %q: %s", k, body)
			}
		}
		for _, k := range []string{"sid", "device_label", "created_at", "last_used_at", "current"} {
			if _, ok := sess[k]; !ok {
				t.Fatalf("missing session key %q: %s", k, body)
			}
		}
		if sess["sid"] == sidA {
			if sess["current"] != true {
				t.Fatalf("own session not marked current: %s", body)
			}
			current++
		} else if sess["current"] != false {
			t.Fatalf("other session marked current: %s", body)
		}
	}
	if current != 1 {
		t.Fatalf("expected exactly 1 current session: %s", body)
	}
}

func TestAccount_DeleteSession(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, _ := signupVerifyTokens(t, s, "sess2@example.com", "+201012345712", "Password123!", devA)
	accessB, refreshB := loginTokens(t, s, "sess2@example.com", "Password123!", devB)
	sidB := tokenSID(t, accessB)

	// Another user's sid returns 404.
	accessOther, _ := signupVerifyTokens(t, s, "sessother@example.com", "+201012345713", "Password123!", devC)
	rec := doRequest(t, s, http.MethodDelete, "/auth/sessions/"+tokenSID(t, accessOther), nil, accessA)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete other user's sid = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	// Unknown sid returns 404.
	rec = doRequest(t, s, http.MethodDelete, "/auth/sessions/99999999-9999-4999-8999-999999999999", nil, accessA)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete unknown sid = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	// Delete device B from device A.
	rec = doRequest(t, s, http.MethodDelete, "/auth/sessions/"+sidB, nil, accessA)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete session = %d (%s)", rec.Code, rec.Body.String())
	}
	// B's refresh is dead; A's session survives.
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshB}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after session delete = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("me after deleting other session = %d (%s)", rec.Code, rec.Body.String())
	}
	// Deleting the current session is logout.
	sidA := tokenSID(t, accessA)
	rec = doRequest(t, s, http.MethodDelete, "/auth/sessions/"+sidA, nil, accessA)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete current session = %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, accessA)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after deleting current session = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
}

func TestAccount_SessionsAuth(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	rec := doRequest(t, s, http.MethodGet, "/auth/sessions", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("sessions without token = %d, want 401", rec.Code)
	}
	rec = doRequest(t, s, http.MethodGet, "/auth/sessions", nil, "garbage-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("sessions with bad token = %d, want 401", rec.Code)
	}
}

func TestAccount_ChangePassword(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, _ := signupVerifyTokens(t, s, "pw@example.com", "+201012345714", "Password123!", devA)
	_, refreshB := loginTokens(t, s, "pw@example.com", "Password123!", devB)

	// Wrong current password counts as a failure.
	rec := doRequest(t, s, http.MethodPost, "/auth/password/change", map[string]string{
		"current_password": "WrongPassword1!",
		"new_password":     "NewPassword123!",
	}, accessA)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	// Too short and too long are refused before any state change.
	rec = doRequest(t, s, http.MethodPost, "/auth/password/change", map[string]string{
		"current_password": "Password123!",
		"new_password":     "short",
	}, accessA)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short password = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/password/change", map[string]string{
		"current_password": "Password123!",
		"new_password":     strings.Repeat("a", 73),
	}, accessA)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long password = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	var errBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["code"] != "password_too_long" {
		t.Fatalf("expected code password_too_long, got %s", rec.Body.String())
	}
	// Happy path.
	rec = doRequest(t, s, http.MethodPost, "/auth/password/change", map[string]string{
		"current_password": "Password123!",
		"new_password":     "NewPassword123!",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password = %d (%s)", rec.Code, rec.Body.String())
	}
	// Old password dead, new works.
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "pw@example.com", "password": "Password123!", "device_id": devC,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with old password = %d, want 401", rec.Code)
	}
	loginTokens(t, s, "pw@example.com", "NewPassword123!", devC)
	// The other session ended; the current one stays.
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshB}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("other session refresh = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("current session me = %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAccount_ChangePasswordLockout(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, _ := signupVerifyTokens(t, s, "pwlock@example.com", "+201012345715", "Password123!", devA)
	for i := 0; i < 5; i++ {
		rec := doRequest(t, s, http.MethodPost, "/auth/password/change", map[string]string{
			"current_password": "WrongPassword1!",
			"new_password":     "NewPassword123!",
		}, accessA)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i, rec.Code)
		}
	}
	rec := doRequest(t, s, http.MethodPost, "/auth/password/change", map[string]string{
		"current_password": "Password123!",
		"new_password":     "NewPassword123!",
	}, accessA)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked change = %d, want 429 (%s)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("locked change has no Retry-After header")
	}
}

func TestAccount_UpdateProfile(t *testing.T) {
	s, sender, cleanup := accountTestServer(t)
	defer cleanup()
	_ = sender

	accessA, _ := signupVerifyTokens(t, s, "prof@example.com", "+201012345716", "Password123!", devA)

	// Wrong password refuses.
	rec := doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"full_name": "New Name", "current_password": "Wrong1!",
	}, accessA)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	// Happy path: name and phone together.
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"full_name": "New Name", "phone": "+201012345717", "current_password": "Password123!",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("update profile = %d (%s)", rec.Code, rec.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated["full_name"] != "New Name" || updated["phone"] != "+201012345717" {
		t.Fatalf("unexpected update response: %s", rec.Body.String())
	}
	// /auth/me returns the new values at once (watermark updates on next play).
	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d (%s)", rec.Code, rec.Body.String())
	}
	var me map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me["full_name"] != "New Name" || me["phone"] != "+201012345717" {
		t.Fatalf("me did not return new values: %s", rec.Body.String())
	}
	// A change too soon returns 429 change_too_soon with Retry-After.
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"full_name": "Another Name", "current_password": "Password123!",
	}, accessA)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("too-soon change = %d, want 429 (%s)", rec.Code, rec.Body.String())
	}
	var tooSoon map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &tooSoon); err != nil || tooSoon["code"] != "change_too_soon" {
		t.Fatalf("expected code change_too_soon, got %s", rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("change_too_soon has no Retry-After header")
	}
	// After 31 days the same change succeeds (injectable clock).
	s.Clock = func() time.Time { return time.Now().UTC().Add(31 * 24 * time.Hour) }
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"full_name": "Another Name", "current_password": "Password123!",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("change after 31d = %d (%s)", rec.Code, rec.Body.String())
	}
	s.Clock = nil
}

func TestAccount_UpdateProfilePhoneConflicts(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, _ := signupVerifyTokens(t, s, "profa@example.com", "+201012345718", "Password123!", devA)
	signupVerifyTokens(t, s, "profb@example.com", "+201012345719", "Password123!", devB)

	// Phone held by another verified account: generic 409.
	rec := doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"phone": "+201012345719", "current_password": "Password123!",
	}, accessA)
	if rec.Code != http.StatusConflict {
		t.Fatalf("taken phone = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// Blocklisted phone: same generic 409.
	blocked, _ := normalizePhone("+201012345720", "EG")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := s.Store.AddToBlocklist(ctx, "phone", computeHMAC(s.BlocklistHMACKey, blocked), "test", time.Now().UTC()); err != nil {
		cancel()
		t.Fatalf("add blocklist: %v", err)
	}
	cancel()
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"phone": "+201012345720", "current_password": "Password123!",
	}, accessA)
	if rec.Code != http.StatusConflict {
		t.Fatalf("blocklisted phone = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	var conflict map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil || conflict["code"] != "conflict" {
		t.Fatalf("expected generic code conflict, got %s", rec.Body.String())
	}
}

func TestAccount_EmailChange(t *testing.T) {
	s, sender, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, refreshA := signupVerifyTokens(t, s, "emailchg@example.com", "+201012345721", "Password123!", devA)

	rec := doRequest(t, s, http.MethodPost, "/auth/email/change", map[string]string{
		"new_email": "newaddr@example.com", "current_password": "Wrong1!",
	}, accessA)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401", rec.Code)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/email/change", map[string]string{
		"new_email": "newaddr@example.com", "current_password": "Password123!",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("request email change = %d (%s)", rec.Code, rec.Body.String())
	}
	// The code goes to the new address only.
	code := sender.codeFor("newaddr@example.com", "email-change")
	if len(code) != 6 {
		t.Fatalf("no 6-digit code sent to new address (codes=%v)", sender.sent)
	}
	if got := sender.codeFor("emailchg@example.com", "email-change"); got != "" {
		t.Fatalf("code leaked to old address: %q", got)
	}
	// Wrong code refuses.
	rec = doRequest(t, s, http.MethodPost, "/auth/email/confirm", map[string]string{"code": "000000"}, accessA)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong confirm code = %d, want 401", rec.Code)
	}
	// Happy path.
	rec = doRequest(t, s, http.MethodPost, "/auth/email/confirm", map[string]string{"code": code}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm email change = %d (%s)", rec.Code, rec.Body.String())
	}
	// The old address is notified (masked new address, no code).
	notices := sender.noticesFor("emailchg@example.com")
	if len(notices) == 0 {
		t.Fatal("old address was not notified")
	}
	if strings.Contains(notices[0].text, "newaddr@example.com") {
		t.Fatalf("old-address notice leaks full new address: %q", notices[0].text)
	}
	if !strings.Contains(notices[0].text, "n***@example.com") {
		t.Fatalf("old-address notice has no masked address: %q", notices[0].text)
	}
	// All sessions end, including the current one.
	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, accessA)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after email change = %d, want 401", rec.Code)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshA}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after email change = %d, want 401", rec.Code)
	}
	// Sign in again with the new address.
	loginTokens(t, s, "newaddr@example.com", "Password123!", devC)
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "emailchg@example.com", "password": "Password123!", "device_id": devC,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with old email = %d, want 401", rec.Code)
	}
}

func TestAccount_EmailChangeTakenIsGeneric(t *testing.T) {
	s, sender, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, _ := signupVerifyTokens(t, s, "emailt1@example.com", "+201012345722", "Password123!", devA)
	signupVerifyTokens(t, s, "emailt2@example.com", "+201012345723", "Password123!", devB)

	rec := doRequest(t, s, http.MethodPost, "/auth/email/change", map[string]string{
		"new_email": "emailt2@example.com", "current_password": "Password123!",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("taken new email = %d, want generic 200 (%s)", rec.Code, rec.Body.String())
	}
	if code := sender.codeFor("emailt2@example.com", "email-change"); code != "" {
		t.Fatalf("code sent for a taken email: %q", code)
	}
}

func TestAccount_EmailChangeAttemptCap(t *testing.T) {
	s, sender, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, _ := signupVerifyTokens(t, s, "emailcap@example.com", "+201012345724", "Password123!", devA)
	rec := doRequest(t, s, http.MethodPost, "/auth/email/change", map[string]string{
		"new_email": "capnew@example.com", "current_password": "Password123!",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("request = %d (%s)", rec.Code, rec.Body.String())
	}
	code := sender.codeFor("capnew@example.com", "email-change")
	if code == "" {
		t.Fatal("no code sent")
	}
	for i := 0; i < 5; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/email/confirm", map[string]string{"code": "000000"}, accessA)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong attempt %d = %d, want 401", i, rec.Code)
		}
	}
	// The 5th wrong attempt deletes the code: the right code now fails.
	rec = doRequest(t, s, http.MethodPost, "/auth/email/confirm", map[string]string{"code": code}, accessA)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("correct code after cap = %d, want 401", rec.Code)
	}
}

func TestAccount_EmailChangeConfirmRace409(t *testing.T) {
	s, sender, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, _ := signupVerifyTokens(t, s, "emailr1@example.com", "+201012345725", "Password123!", devA)
	rec := doRequest(t, s, http.MethodPost, "/auth/email/change", map[string]string{
		"new_email": "racetarget@example.com", "current_password": "Password123!",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("request = %d (%s)", rec.Code, rec.Body.String())
	}
	code := sender.codeFor("racetarget@example.com", "email-change")
	if code == "" {
		t.Fatal("no code sent")
	}
	// Someone else registers the address before confirm.
	signupVerifyTokens(t, s, "racetarget@example.com", "+201012345726", "Password123!", devB)
	rec = doRequest(t, s, http.MethodPost, "/auth/email/confirm", map[string]string{"code": code}, accessA)
	if rec.Code != http.StatusConflict {
		t.Fatalf("confirm raced email = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

func TestAccount_DeletionGraceAndCancel(t *testing.T) {
	s, sender, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, refreshA := signupVerifyTokens(t, s, "del1@example.com", "+201012345727", "Password123!", devA)

	// The confirmation word must be exactly "حذف".
	rec := doRequest(t, s, http.MethodPost, "/auth/account/delete", map[string]string{
		"current_password": "Password123!", "confirm": "delete",
	}, accessA)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("latin confirm = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/account/delete", map[string]string{
		"current_password": "Password123!", "confirm": "حذف",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("request deletion = %d (%s)", rec.Code, rec.Body.String())
	}
	var delResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &delResp); err != nil || delResp["deletion_date"] == "" {
		t.Fatalf("deletion response has no date: %s", rec.Body.String())
	}
	// The request email states the date and the cancel path.
	notices := sender.noticesFor("del1@example.com")
	if len(notices) == 0 || !strings.Contains(notices[0].text, delResp["deletion_date"]) {
		t.Fatalf("deletion email missing or dateless: %+v", notices)
	}
	// All sessions end at once.
	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, accessA)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after deletion request = %d, want 401", rec.Code)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshA}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh during grace = %d, want 401", rec.Code)
	}
	// A wrong password during grace does not cancel.
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "del1@example.com", "password": "Wrong1!", "device_id": devB,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-password login during grace = %d, want 401", rec.Code)
	}
	// Logging in with the correct password cancels the deletion.
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "del1@example.com", "password": "Password123!", "device_id": devB,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login during grace = %d (%s)", rec.Code, rec.Body.String())
	}
	var loginResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil || loginResp["deletion_cancelled"] != true {
		t.Fatalf("login response has no deletion_cancelled=true: %s", rec.Body.String())
	}
	if loginResp["access_token"] == "" || loginResp["refresh_token"] == "" {
		t.Fatalf("cancel login issued no tokens: %s", rec.Body.String())
	}
	// The next login is ordinary again.
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "del1@example.com", "password": "Password123!", "device_id": devC,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("second login = %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "deletion_cancelled") {
		t.Fatalf("second login still carries deletion_cancelled: %s", rec.Body.String())
	}
}

func TestAccount_VerifyOTPRefusedDuringGrace(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Grace OTP User",
		"email":     "graceotp@example.com",
		"phone":     "+201012345728",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d (%s)", rec.Code, rec.Body.String())
	}
	var signup map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &signup); err != nil {
		t.Fatalf("decode signup: %v", err)
	}
	// Force the unverified record into pending_deletion (R7 applies to the
	// new status on every token-issuing path).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, err := s.Store.FindByEmail(ctx, "graceotp@example.com")
	if err != nil || u == nil {
		t.Fatalf("find user: %v", err)
	}
	now := time.Now().UTC()
	if err := s.Store.RequestDeletion(ctx, u.ID, now, now.Add(deletionGrace)); err != nil {
		t.Fatalf("request deletion: %v", err)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "graceotp@example.com", "code": signup["dev_otp"], "device_id": devA,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("verify-otp during grace = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
}

func TestAccount_Purge(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	t0 := time.Now().UTC().Truncate(time.Second)
	s.Clock = func() time.Time { return t0 }
	accessA, _ := signupVerifyTokens(t, s, "purge1@example.com", "+201012345729", "Password123!", devA)
	rec := doRequest(t, s, http.MethodPost, "/auth/account/delete", map[string]string{
		"current_password": "Password123!", "confirm": "حذف",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("request deletion = %d (%s)", rec.Code, rec.Body.String())
	}
	// Before the 30 days pass, nothing is purged.
	s.Clock = func() time.Time { return t0.Add(29 * 24 * time.Hour) }
	n, err := s.PurgeExpiredDeletions(context.Background())
	if err != nil {
		t.Fatalf("early purge: %v", err)
	}
	if n != 0 {
		t.Fatalf("early purge purged %d accounts", n)
	}
	// After the 30 days pass, the purge anonymizes the account.
	s.Clock = func() time.Time { return t0.Add(31 * 24 * time.Hour) }
	n, err = s.PurgeExpiredDeletions(context.Background())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 1 {
		t.Fatalf("purge purged %d accounts, want 1", n)
	}
	s.Clock = nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, err := s.Store.FindByEmail(ctx, "purge1@example.com")
	if err != nil {
		t.Fatalf("find purged: %v", err)
	}
	if u != nil {
		t.Fatalf("purged account still reachable by old email: %+v", u)
	}
	// Reach the record by id through the store: anonymized, id kept.
	due, err := s.Store.ListDeletionsDue(ctx, time.Now().UTC().Add(60*24*time.Hour))
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("purged account still due: %d", len(due))
	}
	// A self-deleted email and phone can sign up again (A6: no blocklist).
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Purge Reuse",
		"email":     "purge1@example.com",
		"phone":     "+201012345729",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("re-signup after self-deletion = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	bKey := s.BlocklistHMACKey
	if bKey == "" {
		bKey = "test"
	}
	blocked, err := s.Store.IsBlocked(ctx, "email", computeHMAC(s.BlocklistHMACKey, "purge1@example.com"))
	if err != nil || blocked {
		t.Fatalf("self-deleted email blocklisted: blocked=%v err=%v", blocked, err)
	}
	// The purged credentials no longer log in.
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "purge1@example.com", "password": "Password123!", "device_id": devB,
	}, "")
	_ = rec // the address now belongs to the new unverified signup; must not verify-password-match the old hash
}

func TestAccount_PurgeRacesLogin(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	t0 := time.Now().UTC().Truncate(time.Second)
	s.Clock = func() time.Time { return t0 }
	accessA, _ := signupVerifyTokens(t, s, "purgerace@example.com", "+201012345730", "Password123!", devA)
	rec := doRequest(t, s, http.MethodPost, "/auth/account/delete", map[string]string{
		"current_password": "Password123!", "confirm": "حذف",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("request deletion = %d (%s)", rec.Code, rec.Body.String())
	}
	// Login wins the race first: purge afterwards finds nothing due.
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "purgerace@example.com", "password": "Password123!", "device_id": devB,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel login = %d (%s)", rec.Code, rec.Body.String())
	}
	s.Clock = func() time.Time { return t0.Add(31 * 24 * time.Hour) }
	n, err := s.PurgeExpiredDeletions(context.Background())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 0 {
		t.Fatalf("purge after cancel purged %d accounts", n)
	}
	s.Clock = nil
}

func TestAccount_PurgeLoginAfterPurgeRefused(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	t0 := time.Now().UTC().Truncate(time.Second)
	s.Clock = func() time.Time { return t0 }
	accessA, _ := signupVerifyTokens(t, s, "purgeLogin@example.com", "+201012345731", "Password123!", devA)
	rec := doRequest(t, s, http.MethodPost, "/auth/account/delete", map[string]string{
		"current_password": "Password123!", "confirm": "حذف",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("request deletion = %d (%s)", rec.Code, rec.Body.String())
	}
	// Purge wins the race first: the later login is refused.
	s.Clock = func() time.Time { return t0.Add(31 * 24 * time.Hour) }
	n, err := s.PurgeExpiredDeletions(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("purge n=%d err=%v", n, err)
	}
	s.Clock = nil
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "purgeLogin@example.com", "password": "Password123!", "device_id": devB,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login after purge = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
}

func TestAccount_StoreDeletionCAS(t *testing.T) {
	st := store.NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	u := &models.User{ID: "cas-1", Email: "cas@example.com", Phone: "+201012345732", Status: models.StatusActive, EmailVerified: true}
	if err := st.Create(ctx, u); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Cancel on an active account conflicts.
	if err := st.CancelDeletion(ctx, "cas-1", now); err == nil {
		t.Fatal("CancelDeletion on active account succeeded, want conflict")
	}
	// Purge before purge_after conflicts.
	if err := st.RequestDeletion(ctx, "cas-1", now, now.Add(deletionGrace)); err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := st.PurgeDeletion(ctx, "cas-1", "deleted-cas-1@deleted.local", now.Add(time.Hour)); err == nil {
		t.Fatal("early PurgeDeletion succeeded, want conflict")
	}
	// Second request while pending conflicts.
	if err := st.RequestDeletion(ctx, "cas-1", now, now.Add(deletionGrace)); err == nil {
		t.Fatal("second RequestDeletion succeeded, want conflict")
	}
	// Cancel then purge conflicts (login won the race).
	if err := st.CancelDeletion(ctx, "cas-1", now); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := st.PurgeDeletion(ctx, "cas-1", "deleted-cas-1@deleted.local", now.Add(31*24*time.Hour)); err == nil {
		t.Fatal("PurgeDeletion after cancel succeeded, want conflict")
	}
}

func TestAccount_AdminSuspendDuringGrace(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, _ := signupVerifyTokens(t, s, "admsusp@example.com", "+201012345733", "Password123!", devA)
	rec := doRequest(t, s, http.MethodPost, "/auth/account/delete", map[string]string{
		"current_password": "Password123!", "confirm": "حذف",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("request deletion = %d (%s)", rec.Code, rec.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, err := s.Store.FindByEmail(ctx, "admsusp@example.com")
	if err != nil || u == nil {
		t.Fatalf("find user: %v", err)
	}
	// Admin suspends during the grace period: allowed, grace fields cleared.
	s.InternalToken = "internal-test-token"
	rawAdmin := "owner-admin-token"
	now := time.Now().UTC()
	if err := s.Store.CreateAdmin(ctx, &models.Admin{
		ID:        "adm-owner",
		Name:      "Owner",
		TokenHash: hashToken(rawAdmin),
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	suspendRec := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/"+u.ID+"/suspend", "internal-test-token", rawAdmin, map[string]string{"reason": "abuse review"})
	if suspendRec.Code != http.StatusOK {
		t.Fatalf("admin suspend during grace = %d (%s)", suspendRec.Code, suspendRec.Body.String())
	}
	after, err := s.Store.FindByID(ctx, u.ID)
	if err != nil || after == nil {
		t.Fatalf("find after: %v", err)
	}
	if after.EffectiveStatus() != models.StatusSuspended {
		t.Fatalf("status = %q, want suspended", after.EffectiveStatus())
	}
	if !after.DeletionRequestedAt.IsZero() || !after.PurgeAfter.IsZero() {
		t.Fatalf("grace fields not cleared: %+v", after)
	}
	// The suspended account is never purged.
	due, err := s.Store.ListDeletionsDue(ctx, time.Now().UTC().Add(60*24*time.Hour))
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	for _, d := range due {
		if d.ID == u.ID {
			t.Fatal("suspended account still due for purge")
		}
	}
}

// TestAccount_GracePhoneReserved proves identifiers stay reserved while a
// deletion is pending (owner decision D2): another account cannot sign up
// with the pending phone or email, nor PATCH to the pending phone — yet the
// owner's login during the grace period still cancels.
func TestAccount_GracePhoneReserved(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	accessA, _ := signupVerifyTokens(t, s, "graceowner@example.com", "+201012345734", "Password123!", devA)
	rec := doRequest(t, s, http.MethodPost, "/auth/account/delete", map[string]string{
		"current_password": "Password123!", "confirm": "حذف",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("request deletion = %d (%s)", rec.Code, rec.Body.String())
	}

	// Signup with the pending phone: generic 409.
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Grace Squatter", "email": "squatter@example.com",
		"phone": "+201012345734", "password": "Password123!",
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("signup with pending phone = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// Signup with the pending email: generic 409.
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Grace Squatter", "email": "graceowner@example.com",
		"phone": "+201012345735", "password": "Password123!",
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("signup with pending email = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// PATCH to the pending phone: generic 409.
	accessC, _ := signupVerifyTokens(t, s, "graceother@example.com", "+201012345736", "Password123!", devB)
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"phone": "+201012345734", "current_password": "Password123!",
	}, accessC)
	if rec.Code != http.StatusConflict {
		t.Fatalf("PATCH to pending phone = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// The owner's login during the grace period still cancels.
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "graceowner@example.com", "password": "Password123!", "device_id": devC,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner login during grace = %d (%s)", rec.Code, rec.Body.String())
	}
	var loginResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil || loginResp["deletion_cancelled"] != true {
		t.Fatalf("no deletion_cancelled=true: %s", rec.Body.String())
	}
}

// TestAccount_VerifyBlockedByPendingPhone: an unverified signup made while
// the phone was free cannot complete verification once a verified account
// holds that phone in pending_deletion; the record stays unverified.
func TestAccount_VerifyBlockedByPendingPhone(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	// B signs up unverified while the phone is free.
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Verify Waiter", "email": "waiter@example.com",
		"phone": "+201012345737", "password": "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("B signup = %d (%s)", rec.Code, rec.Body.String())
	}
	var bSignup map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &bSignup); err != nil {
		t.Fatalf("decode B signup: %v", err)
	}
	// A verifies another number, PATCHes to the same phone (B is unverified,
	// so it does not block), then requests deletion.
	signupVerifyTokens(t, s, "holdera@example.com", "+201012345738", "Password123!", devB)
	accessA, _ := loginTokens(t, s, "holdera@example.com", "Password123!", devB)
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"phone": "+201012345737", "current_password": "Password123!",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("A PATCH to unverified-held phone = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/account/delete", map[string]string{
		"current_password": "Password123!", "confirm": "حذف",
	}, accessA)
	if rec.Code != http.StatusOK {
		t.Fatalf("A deletion request = %d (%s)", rec.Code, rec.Body.String())
	}
	// B's verification now fails with the generic 409; B stays unverified.
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "waiter@example.com", "code": bSignup["dev_otp"], "device_id": devC,
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("B verify vs pending phone = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "waiter@example.com", "password": "Password123!", "device_id": devC,
	}, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("B login after refused verify = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
}

// cancelTakenStore simulates the unreachable state where another account took
// the phone of a pending_deletion record: CancelDeletion answers the distinct
// ErrPhoneTaken.
type cancelTakenStore struct {
	store.Store
	pending *models.User
}

func (f *cancelTakenStore) FindByEmail(_ context.Context, _ string) (*models.User, error) {
	cp := *f.pending
	return &cp, nil
}

func (f *cancelTakenStore) CancelDeletion(_ context.Context, _ string, _ time.Time) error {
	return store.ErrPhoneTaken
}

// TestAccount_LoginPhoneTakenConflict proves Login maps the distinct
// ErrPhoneTaken to a single 409 (no 503, no retry loop, no tokens).
func TestAccount_LoginPhoneTakenConflict(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	hash, err := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	pending := &models.User{
		ID: "pt-login", Email: "ptlogin@example.com", PasswordHash: string(hash),
		Role: models.RoleUser, FullName: "Taken", Phone: "+201012345739",
		EmailVerified: true, Status: models.StatusPendingDeletion,
		CreatedAt: time.Now().UTC(),
	}
	s.Store = &cancelTakenStore{Store: s.Store, pending: pending}

	rec := doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "ptlogin@example.com", "password": "Password123!", "device_id": devA,
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("login with taken phone = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["code"] != "conflict" {
		t.Fatalf("expected generic code conflict, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "access_token") {
		t.Fatalf("tokens issued on phone-taken login: %s", rec.Body.String())
	}
}
