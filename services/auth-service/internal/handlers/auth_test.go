package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/mailer"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

func testServer() *Server {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	s := New(store.NewMemoryStore(), otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")
	s.BlocklistHMACKey = "test-blocklist-hmac-key"
	s.DefaultPhoneRegion = "EG"
	return s
}

func testServerProd() *Server {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	s := New(store.NewMemoryStore(), otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "production", "gw-secret")
	s.BlocklistHMACKey = "test-blocklist-hmac-key"
	s.DefaultPhoneRegion = "EG"
	return s
}

var testPendingMu sync.Mutex
var testPendingIDs = map[string]string{}

// testPendingKey normalizes an email for the pending_id registry.
func testPendingKey(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// rememberPendingID records the pending_id issued by a successful signup,
// mimicking a real client that stores it for the OTP step.
func rememberPendingID(email, pendingID string) {
	if email == "" || pendingID == "" {
		return
	}
	testPendingMu.Lock()
	defer testPendingMu.Unlock()
	testPendingIDs[testPendingKey(email)] = pendingID
}

// lookupPendingID returns the stored pending_id for email, if any.
func lookupPendingID(email string) string {
	testPendingMu.Lock()
	defer testPendingMu.Unlock()
	return testPendingIDs[testPendingKey(email)]
}

func doRequest(t *testing.T, s *Server, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	if m, ok := body.(map[string]string); ok && (path == "/auth/login" || path == "/auth/verify-otp") {
		if _, hasDevice := m["device_id"]; !hasDevice {
			if _, omit := m["__omit_device_id__"]; !omit {
				cp := make(map[string]string, len(m)+1)
				for k, v := range m {
					cp[k] = v
				}
				cp["device_id"] = "11111111-1111-4111-8111-111111111111"
				body = cp
				m = cp
			} else {
				cp := make(map[string]string, len(m))
				for k, v := range m {
					if k != "__omit_device_id__" {
						cp[k] = v
					}
				}
				body = cp
				m = cp
			}
		}
		// Mimic a real client: attach the stored pending_id to verify-otp
		// unless the test passes one explicitly (e.g. a stale takeover id).
		if path == "/auth/verify-otp" {
			if _, hasPending := m["pending_id"]; !hasPending {
				if pid := lookupPendingID(m["email"]); pid != "" {
					cp := make(map[string]string, len(m)+1)
					for k, v := range m {
						cp[k] = v
					}
					cp["pending_id"] = pid
					body = cp
				}
			}
		}
	}
	if m, ok := body.(map[string]any); ok && (path == "/auth/login" || path == "/auth/verify-otp") {
		if _, hasDevice := m["device_id"]; !hasDevice {
			if _, omit := m["__omit_device_id__"]; !omit {
				cp := make(map[string]any, len(m)+1)
				for k, v := range m {
					cp[k] = v
				}
				cp["device_id"] = "11111111-1111-4111-8111-111111111111"
				body = cp
				m = cp
			} else {
				cp := make(map[string]any, len(m))
				for k, v := range m {
					if k != "__omit_device_id__" {
						cp[k] = v
					}
				}
				body = cp
				m = cp
			}
		}
		if path == "/auth/verify-otp" {
			if _, hasPending := m["pending_id"]; !hasPending {
				email, _ := m["email"].(string)
				if pid := lookupPendingID(email); pid != "" {
					cp := make(map[string]any, len(m)+1)
					for k, v := range m {
						cp[k] = v
					}
					cp["pending_id"] = pid
					body = cp
				}
			}
		}
	}
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("X-Gateway-Secret", "gw-secret")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	var h http.HandlerFunc
	switch path {
	case "/auth/signup":
		h = s.Signup
	case "/auth/signup/resend":
		h = s.ResendSignupOTP
	case "/auth/verify-otp":
		h = s.VerifyOTP
	case "/auth/login":
		h = s.Login
	case "/auth/logout":
		h = s.Logout
	case "/auth/refresh":
		h = s.Refresh
	case "/auth/reset/request":
		h = s.RequestReset
	case "/auth/reset/verify":
		h = s.VerifyResetCode
	case "/auth/reset/confirm":
		h = s.ConfirmReset
	case "/auth/me":
		h = s.Me
	default:
		t.Fatalf("unknown path %s", path)
	}
	s.GatewayAuth(h).ServeHTTP(rec, req)
	// Capture the pending_id issued by a successful signup, like a client.
	if path == "/auth/signup" && rec.Code == http.StatusCreated {
		raw := rec.Body.Bytes()
		var parsed map[string]string
		if err := json.Unmarshal(raw, &parsed); err == nil {
			rememberPendingID(parsed["email"], parsed["pending_id"])
		}
	}
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var out map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v (body %q)", err, rec.Body.String())
	}
	return out
}

func TestSignupVerifyLoginMeRefresh(t *testing.T) {
	s := testServer()
	signupPayload := map[string]string{
		"full_name": "Test User",
		"email":     "User@Example.com",
		"phone":     "+201012345678",
		"password":  "password123",
	}
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", signupPayload, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d (%s)", rec.Code, rec.Body.String())
	}
	signup := decodeBody(t, rec)
	code, ok := signup["dev_otp"]
	if !ok || len(code) != 6 {
		t.Fatalf("expected dev_otp in test env, got %v", signup)
	}
	if signup["full_name"] != "Test User" || signup["phone"] != "+201012345678" {
		t.Fatalf("unexpected signup fields: %v", signup)
	}

	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{"email": "user@example.com", "password": "password123"}, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("login before verify = %d, want 403", rec.Code)
	}

	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "user@example.com", "code": code}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d (%s)", rec.Code, rec.Body.String())
	}
	tokens := decodeBody(t, rec)
	if tokens["access_token"] == "" || tokens["refresh_token"] == "" {
		t.Fatalf("missing tokens: %v", tokens)
	}

	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, tokens["access_token"])
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d (%s)", rec.Code, rec.Body.String())
	}
	var meProfile map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &meProfile); err != nil {
		t.Fatalf("decode /auth/me: %v", err)
	}
	if meProfile["full_name"] != "Test User" || meProfile["phone"] != "+201012345678" {
		t.Fatalf("unexpected full_name or phone in /auth/me: %v", meProfile)
	}

	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": tokens["refresh_token"]}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d (%s)", rec.Code, rec.Body.String())
	}
	rotated := decodeBody(t, rec)
	if rotated["refresh_token"] == tokens["refresh_token"] {
		t.Fatal("expected rotated refresh token")
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": tokens["refresh_token"]}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused refresh token = %d, want 401", rec.Code)
	}
}

func TestTwoPhasePasswordReset(t *testing.T) {
	s := testServer()
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)
	doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Reset User",
		"email":     "reset@example.com",
		"phone":     "+201012345679",
		"password":  "password123",
	}, "")
	rec := doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": "reset@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request = %d", rec.Code)
	}
	devCode := decodeBody(t, rec)["dev_otp"]
	if len(devCode) != 6 {
		t.Fatalf("expected dev_otp, got %v", rec.Body.String())
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": "reset@example.com", "code": devCode}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset verify = %d (%s)", rec.Code, rec.Body.String())
	}
	resetToken := decodeBody(t, rec)["reset_token"]
	if resetToken == "" {
		t.Fatal("missing reset_token")
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/confirm", map[string]string{"reset_token": resetToken, "new_password": "newpassword456"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset confirm = %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/confirm", map[string]string{"reset_token": resetToken, "new_password": "another789"}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused reset token = %d, want 401", rec.Code)
	}
}

func TestConfirmReset_ParallelSingleUse(t *testing.T) {
	s := testServer()
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Parallel User",
		"email":     "parallel@example.com",
		"phone":     "+201012345704",
		"password":  "password123",
	}, "")
	rec := doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": "parallel@example.com"}, "")
	devCode := decodeBody(t, rec)["dev_otp"]
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": "parallel@example.com", "code": devCode}, "")
	resetToken := decodeBody(t, rec)["reset_token"]
	if resetToken == "" {
		t.Fatal("missing reset_token")
	}

	// 10 parallel confirms with the same token: exactly one 200 (atomic Take).
	var mu sync.Mutex
	results := map[int]int{}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{"reset_token": resetToken, "new_password": "newpassword456"})
			req := httptest.NewRequest(http.MethodPost, "/auth/reset/confirm", bytes.NewReader(body))
			req.Header.Set("X-Gateway-Secret", "gw-secret")
			rec := httptest.NewRecorder()
			s.GatewayAuth(http.HandlerFunc(s.ConfirmReset)).ServeHTTP(rec, req)
			mu.Lock()
			results[rec.Code]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if results[http.StatusOK] != 1 {
		t.Fatalf("parallel confirms: got %v, want exactly one 200", results)
	}
	if results[http.StatusUnauthorized] != 9 {
		t.Fatalf("parallel confirms: got %v, want nine 401", results)
	}
}

func TestResetRequestAntiEnumeration(t *testing.T) {
	s := testServer()
	rec := doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": "nobody@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown email request = %d, want 200", rec.Code)
	}
	if got := decodeBody(t, rec)["status"]; got != "ok" {
		t.Fatalf("status = %q", got)
	}
}

func TestPassword_72ByteLimit(t *testing.T) {
	s := testServer()
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	// 72 bytes accepted on signup.
	pw72 := strings.Repeat("a", 72)
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Longpw User",
		"email":     "longpw72@example.com",
		"phone":     "+201012345682",
		"password":  pw72,
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup 72-byte password = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	// 73 bytes rejected with 400 password_too_long.
	pw73 := strings.Repeat("b", 73)
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Longpw User",
		"email":     "longpw73@example.com",
		"phone":     "+201012345683",
		"password":  pw73,
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("signup 73-byte password = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	var errBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode 73-byte error: %v", err)
	}
	if errBody["code"] != "password_too_long" {
		t.Fatalf("73-byte code = %q, want password_too_long (%s)", errBody["code"], rec.Body.String())
	}

	// 40 Arabic characters (80 bytes) rejected with 400, not 500.
	ar40 := strings.Repeat("أ", 40)
	if len(ar40) != 80 {
		t.Fatalf("test setup: len(ar40) = %d, want 80", len(ar40))
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Arabic User",
		"email":     "arabic40@example.com",
		"phone":     "+201012345684",
		"password":  ar40,
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("signup 40-char Arabic password = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode arabic error: %v", err)
	}
	if errBody["code"] != "password_too_long" {
		t.Fatalf("arabic code = %q, want password_too_long", errBody["code"])
	}

	// Reset token stays usable after password_too_long.
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": "longpw72@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request = %d", rec.Code)
	}
	devCode := decodeBody(t, rec)["dev_otp"]
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": "longpw72@example.com", "code": devCode}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset verify = %d (%s)", rec.Code, rec.Body.String())
	}
	resetToken := decodeBody(t, rec)["reset_token"]
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/confirm", map[string]string{"reset_token": resetToken, "new_password": pw73}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reset confirm 73-byte = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	// Same token works with a valid password.
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/confirm", map[string]string{"reset_token": resetToken, "new_password": "newvalid123"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset confirm retry with valid password = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestConfirmReset_RevokesSessions(t *testing.T) {
	s := testServer()
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	// Signup and verify to get initial tokens (device 1).
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Revoke User",
		"email":     "revoke@example.com",
		"phone":     "+201012345681",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d (%s)", rec.Code, rec.Body.String())
	}
	signupOTP := decodeBody(t, rec)["dev_otp"]
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":     "revoke@example.com",
		"code":      signupOTP,
		"device_id": "11111111-1111-4111-8111-111111111111",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp = %d (%s)", rec.Code, rec.Body.String())
	}
	oldTokens := decodeBody(t, rec)
	oldAccess := oldTokens["access_token"]
	oldRefresh := oldTokens["refresh_token"]
	if oldAccess == "" || oldRefresh == "" {
		t.Fatalf("missing tokens: %v", oldTokens)
	}

	// Old access works before reset.
	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, oldAccess)
	if rec.Code != http.StatusOK {
		t.Fatalf("me before reset = %d, want 200", rec.Code)
	}

	// Reset password.
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": "revoke@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request = %d", rec.Code)
	}
	devCode := decodeBody(t, rec)["dev_otp"]
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": "revoke@example.com", "code": devCode}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset verify = %d (%s)", rec.Code, rec.Body.String())
	}
	resetToken := decodeBody(t, rec)["reset_token"]
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/confirm", map[string]string{"reset_token": resetToken, "new_password": "newpassword456"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset confirm = %d (%s)", rec.Code, rec.Body.String())
	}

	// Old access token gets 401.
	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, oldAccess)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me with old access after reset = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}

	// Old refresh token gets 401.
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": oldRefresh}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh with old token after reset = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}

	// New login works.
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":     "revoke@example.com",
		"password":  "newpassword456",
		"device_id": "22222222-2222-4222-8222-222222222222",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login with new password = %d (%s)", rec.Code, rec.Body.String())
	}
	newTokens := decodeBody(t, rec)
	if newTokens["access_token"] == "" || newTokens["refresh_token"] == "" {
		t.Fatalf("missing new tokens: %v", newTokens)
	}
	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, newTokens["access_token"])
	if rec.Code != http.StatusOK {
		t.Fatalf("me with new access = %d, want 200", rec.Code)
	}
}

func TestLoginLockoutBackoff(t *testing.T) {
	s := testServer()
	doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Lock User",
		"email":     "lock@example.com",
		"phone":     "+201012345680",
		"password":  "password123",
	}, "")
	var last *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		last = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{"email": "lock@example.com", "password": "wrongpass"}, "")
	}
	if last.Code != http.StatusTooManyRequests && last.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401/429 after failures, got %d", last.Code)
	}
	if last.Code == http.StatusTooManyRequests {
		var body map[string]string
		if err := json.Unmarshal(last.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode lockout: %v", err)
		}
		if body["code"] != "too_many_attempts" {
			t.Fatalf("lockout code = %q, want too_many_attempts", body["code"])
		}
		if last.Header().Get("Retry-After") == "" {
			t.Fatal("lockout response missing Retry-After header")
		}
	}
}

func doLoginWithIP(t *testing.T, s *Server, email, password, ip string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]string{
		"email":     email,
		"password":  password,
		"device_id": "11111111-1111-4111-8111-111111111111",
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/login", &buf)
	req.Header.Set("X-Gateway-Secret", "gw-secret")
	req.Header.Set("X-Forwarded-For", ip)
	rec := httptest.NewRecorder()
	s.GatewayAuth(http.HandlerFunc(s.Login)).ServeHTTP(rec, req)
	return rec
}

func TestLoginLockout_PairAndGlobal(t *testing.T) {
	s := testServer()
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	// Verified user for successful logins.
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Lockout User",
		"email":     "pairlock@example.com",
		"phone":     "+201012345691",
		"password":  "password123",
	}, "")
	otpCode := decodeBody(t, rec)["dev_otp"]
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "pairlock@example.com", "code": otpCode}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify = %d", rec.Code)
	}

	ipA := "10.0.0.1"
	// 5 failures from same (email, IP) -> 6th is 429 with Retry-After.
	for i := 0; i < 5; i++ {
		rec = doLoginWithIP(t, s, "pairlock@example.com", "wrongpass", ipA)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d = %d, want 401", i, rec.Code)
		}
	}
	rec = doLoginWithIP(t, s, "pairlock@example.com", "wrongpass", ipA)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th failure same pair = %d, want 429", rec.Code)
	}
	var locked map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &locked); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if locked["code"] != "too_many_attempts" {
		t.Fatalf("code = %q, want too_many_attempts", locked["code"])
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After on pair lockout")
	}

	// Other email from same IP is not affected (no IP-wide lock).
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Other User",
		"email":     "otherpair@example.com",
		"phone":     "+201012345692",
		"password":  "password123",
	}, "")
	otpOther := decodeBody(t, rec)["dev_otp"]
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "otherpair@example.com", "code": otpOther}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify other = %d", rec.Code)
	}
	rec = doLoginWithIP(t, s, "otherpair@example.com", "password123", ipA)
	if rec.Code != http.StatusOK {
		t.Fatalf("other email same IP login = %d, want 200 (no IP-wide lock)", rec.Code)
	}

	// Successful login clears the (email, IP) counter: after reset, wrong
	// password is 401 again, not 429. Use a fresh IP to avoid the existing lock.
	ipB := "10.0.0.2"
	rec = doLoginWithIP(t, s, "pairlock@example.com", "password123", ipB)
	if rec.Code != http.StatusOK {
		t.Fatalf("login from fresh IP = %d, want 200", rec.Code)
	}
	// One more failure from ipB is 401 (counter was cleared by success).
	rec = doLoginWithIP(t, s, "pairlock@example.com", "wrongpass", ipB)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("failure after success = %d, want 401", rec.Code)
	}
}

func TestLoginLockout_GlobalAcrossIPs(t *testing.T) {
	s := testServer()
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Global User",
		"email":     "globallock@example.com",
		"phone":     "+201012345693",
		"password":  "password123",
	}, "")
	otpCode := decodeBody(t, rec)["dev_otp"]
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "globallock@example.com", "code": otpCode}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify = %d", rec.Code)
	}

	// 20 failures spread across 20 IPs (1 each, so no pair locks).
	for i := 0; i < 20; i++ {
		ip := fmt.Sprintf("10.1.0.%d", i+1)
		rec = doLoginWithIP(t, s, "globallock@example.com", "wrongpass", ip)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d = %d, want 401", i, rec.Code)
		}
	}
	// 21st from a fresh IP hits the global lock.
	rec = doLoginWithIP(t, s, "globallock@example.com", "wrongpass", "10.1.0.99")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("21st failure = %d, want 429 global lock", rec.Code)
	}
	var locked map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &locked); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if locked["code"] != "too_many_attempts" {
		t.Fatalf("code = %q, want too_many_attempts", locked["code"])
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After on global lockout")
	}
	// Even the correct password is 429 while globally locked.
	rec = doLoginWithIP(t, s, "globallock@example.com", "password123", "10.1.0.100")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("correct password while globally locked = %d, want 429", rec.Code)
	}
}

func TestLoginLockout_ResetClearsLocks(t *testing.T) {
	s := testServer()
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Reset Unlock User",
		"email":     "resetunlock@example.com",
		"phone":     "+201012345694",
		"password":  "password123",
	}, "")
	otpCode := decodeBody(t, rec)["dev_otp"]
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "resetunlock@example.com", "code": otpCode}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify = %d", rec.Code)
	}

	// Lock the pair with 5 failures.
	ip := "10.2.0.1"
	for i := 0; i < 5; i++ {
		doLoginWithIP(t, s, "resetunlock@example.com", "wrongpass", ip)
	}
	rec = doLoginWithIP(t, s, "resetunlock@example.com", "wrongpass", ip)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected pair lock 429, got %d", rec.Code)
	}

	// Successful password reset clears both locks.
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": "resetunlock@example.com"}, "")
	devCode := decodeBody(t, rec)["dev_otp"]
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": "resetunlock@example.com", "code": devCode}, "")
	resetToken := decodeBody(t, rec)["reset_token"]
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/confirm", map[string]string{"reset_token": resetToken, "new_password": "newpassword456"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset confirm = %d (%s)", rec.Code, rec.Body.String())
	}

	// Login with new password from the same IP works (locks cleared).
	rec = doLoginWithIP(t, s, "resetunlock@example.com", "newpassword456", ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("login after reset = %d, want 200 (locks cleared)", rec.Code)
	}
}

func TestLogin_DummyBcryptCalledOnce(t *testing.T) {
	s := testServer()

	var mu sync.Mutex
	calls := 0
	real := s.BcryptCompare
	s.BcryptCompare = func(hashed, password []byte) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return real(hashed, password)
	}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}

	// Unknown email path calls the comparer exactly once.
	rec := doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "nobody-dummy@example.com", "password": "wrongpass",
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown email login = %d, want 401", rec.Code)
	}
	if got := count(); got != 1 {
		t.Fatalf("unknown-email bcrypt calls = %d, want exactly 1", got)
	}

	// Wrong-password path calls the comparer exactly once.
	doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Dummy User",
		"email":     "dummy@example.com",
		"phone":     "+201012345699",
		"password":  "password123",
	}, "")
	mu.Lock()
	calls = 0
	mu.Unlock()
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email": "dummy@example.com", "password": "wrongpass",
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password login = %d, want 401", rec.Code)
	}
	if got := count(); got != 1 {
		t.Fatalf("wrong-password bcrypt calls = %d, want exactly 1", got)
	}
}

func TestGatewaySecretRequired(t *testing.T) {
	s := testServer()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	s.GatewayAuth(http.HandlerFunc(s.Login)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing gateway secret = %d, want 401", rec.Code)
	}
}

func TestSignupRejectsClientRole(t *testing.T) {
	s := testServer()
	for i, role := range []string{"user", "admin"} {
		rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
			"full_name": "Role User",
			"phone":     "+20101234568" + string('1'+rune(i)),
			"email":     "r-" + role + "@example.com",
			"password":  "password123",
			"role":      role,
		}, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("role %q: status = %d, want 400", role, rec.Code)
		}
	}
}

func TestProductionNeverReturnsDevOTP(t *testing.T) {
	s := testServerProd()
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Prod User",
		"email":     "prod@example.com",
		"phone":     "+201012345685",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d (%s)", rec.Code, rec.Body.String())
	}
	var signup map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&signup); err != nil {
		t.Fatal(err)
	}
	if _, present := signup["dev_otp"]; present {
		t.Fatal("production signup must never return dev_otp")
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": "prod@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request = %d", rec.Code)
	}
	var reset map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&reset); err != nil {
		t.Fatal(err)
	}
	if _, present := reset["dev_otp"]; present {
		t.Fatal("production reset request must never return dev_otp")
	}
}

func TestNormalizePhoneTable(t *testing.T) {
	tests := []struct {
		input       string
		region      string
		expected    string
		expectError bool
	}{
		// Egyptian local prefix formats (010, 011, 012, 015)
		{"01012345678", "EG", "+201012345678", false},
		{"01112345678", "EG", "+201112345678", false},
		{"01212345678", "EG", "+201212345678", false},
		{"01512345678", "EG", "+201512345678", false},
		// International +20
		{"+201012345678", "EG", "+201012345678", false},
		{"+201112345678", "EG", "+201112345678", false},
		// International 0020
		{"00201012345678", "EG", "+201012345678", false},
		{"00201512345678", "EG", "+201512345678", false},
		// With whitespace/formatting
		{"  010 1234 5678  ", "EG", "+201012345678", false},
		// Default region fallback when empty
		{"01012345678", "", "+201012345678", false},
		// Invalid formats
		{"123", "EG", "", true},
		{"01312345678", "EG", "", true}, // invalid mobile prefix
		{"not-a-phone", "EG", "", true},
		{"", "EG", "", true},
		{"0101234", "EG", "", true},             // too short
		{"0101234567890123456", "EG", "", true}, // too long
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			res, err := normalizePhone(tc.input, tc.region)
			if tc.expectError {
				if err == nil {
					t.Fatalf("normalizePhone(%q, %q): expected error, got %q", tc.input, tc.region, res)
				}
			} else {
				if err != nil {
					t.Fatalf("normalizePhone(%q, %q): unexpected error: %v", tc.input, tc.region, err)
				}
				if res != tc.expected {
					t.Fatalf("normalizePhone(%q, %q) = %q, want %q", tc.input, tc.region, res, tc.expected)
				}
			}
		})
	}
}

func TestSignup_FullNameValidation(t *testing.T) {
	s := testServer()

	cases := []struct {
		name       string
		fullName   string
		wantStatus int
	}{
		{"empty", "", http.StatusBadRequest},
		{"whitespace only", "   ", http.StatusBadRequest},
		{"single rune", "A", http.StatusBadRequest},
		{"two runes minimum", "Al", http.StatusCreated},
		{"arabic two runes", "علي", http.StatusCreated},
		{"100 runes boundary", strings.Repeat("A", 100), http.StatusCreated},
		{"101 runes exceeds", strings.Repeat("A", 101), http.StatusBadRequest},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			email := fmt.Sprintf("fn_val_%d@example.com", i)
			phone := fmt.Sprintf("+2010%08d", 10000000+i)
			rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
				"full_name": tc.fullName,
				"email":     email,
				"phone":     phone,
				"password":  "password123",
			}, "")
			if rec.Code != tc.wantStatus {
				t.Fatalf("name %q: got status %d, want %d (%s)", tc.fullName, rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestSignup_DuplicatePhone(t *testing.T) {
	s := testServer()

	// Register and verify user 1 (verified phones are reserved).
	rec1 := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "User One",
		"email":     "user1@example.com",
		"phone":     "+201012345678",
		"password":  "password123",
	}, "")
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first signup status = %d (%s)", rec1.Code, rec1.Body.String())
	}
	otp1 := decodeBody(t, rec1)["dev_otp"]
	rec := doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "user1@example.com",
		"code":  otp1,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify user1 = %d (%s)", rec.Code, rec.Body.String())
	}

	// Register user 2 with same phone in local format (01012345678)
	rec2 := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "User Two",
		"email":     "user2@example.com",
		"phone":     "01012345678", // normalizes to +201012345678
		"password":  "password123",
	}, "")
	if rec2.Code != http.StatusConflict {
		t.Fatalf("duplicate phone signup = %d, want 409 (%s)", rec2.Code, rec2.Body.String())
	}
	body := decodeBody(t, rec2)
	if body["error"] != "unable to complete registration" || body["code"] != "conflict" {
		t.Fatalf("expected generic refusal error, got %v", body)
	}
}

func TestSignup_UnverifiedPhoneIsFree(t *testing.T) {
	s := testServer()

	// Unverified user 1 does not reserve the phone.
	rec1 := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Unverified One",
		"email":     "unverified1@example.com",
		"phone":     "+201012345685",
		"password":  "password123",
	}, "")
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first signup = %d (%s)", rec1.Code, rec1.Body.String())
	}

	// User 2 with the same phone succeeds.
	rec2 := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Unverified Two",
		"email":     "unverified2@example.com",
		"phone":     "+201012345685",
		"password":  "password123",
	}, "")
	if rec2.Code != http.StatusCreated {
		t.Fatalf("unverified phone reuse = %d, want 201 (%s)", rec2.Code, rec2.Body.String())
	}
}

func TestSignup_BlockedEmail(t *testing.T) {
	s := testServer()

	blockedEmail := "banned-student@example.com"
	emailHash := computeHMAC(s.blocklistKey(), blockedEmail)
	if err := s.Store.AddToBlocklist(context.Background(), "email", emailHash, "cheating violation", time.Now()); err != nil {
		t.Fatalf("AddToBlocklist: %v", err)
	}

	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Banned Student",
		"email":     blockedEmail,
		"phone":     "+201099999991",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("blocked email signup = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	// P-5: same generic response as any refusal (no oracle)
	if body["error"] != "unable to complete registration" || body["code"] != "conflict" {
		t.Fatalf("expected generic refusal, got %v", body)
	}

	// Assert account was NOT created
	u, err := s.Store.FindByEmail(context.Background(), blockedEmail)
	if err != nil || u != nil {
		t.Fatalf("expected no user created for blocked email, got %+v (err: %v)", u, err)
	}
}

func TestSignup_ReplaceUnverified(t *testing.T) {
	s := testServer()

	// First unverified signup.
	rec1 := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "First Attempt",
		"email":     "replace@example.com",
		"phone":     "+201012345686",
		"password":  "password123",
	}, "")
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first signup = %d (%s)", rec1.Code, rec1.Body.String())
	}
	otp1 := decodeBody(t, rec1)["dev_otp"]

	// Second signup with the same unverified email works (replaces).
	rec2 := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Second Attempt",
		"email":     "replace@example.com",
		"phone":     "+201012345687",
		"password":  "newpassword456",
	}, "")
	if rec2.Code != http.StatusCreated {
		t.Fatalf("second signup same unverified email = %d, want 201 (%s)", rec2.Code, rec2.Body.String())
	}
	otp2 := decodeBody(t, rec2)["dev_otp"]
	if otp2 == "" || otp2 == otp1 {
		t.Fatalf("expected a new OTP after replacement, got %q (old %q)", otp2, otp1)
	}

	// Old OTP is invalid.
	rec := doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "replace@example.com",
		"code":  otp1,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("verify with old OTP = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}

	// New OTP verifies.
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "replace@example.com",
		"code":  otp2,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify with new OTP = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	// Verified duplicate is still refused with generic message.
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Third Attempt",
		"email":     "replace@example.com",
		"phone":     "+201012345688",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("verified duplicate signup = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["error"] != "unable to complete registration" || body["code"] != "conflict" {
		t.Fatalf("expected generic refusal, got %v", body)
	}
}

func TestSignup_TakeoverStalePendingFails(t *testing.T) {
	s := testServer()
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	// Victim signs up (unverified).
	recV := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Victim User",
		"email":     "takeover@example.com",
		"phone":     "+201012345695",
		"password":  "victimpassword1",
	}, "")
	if recV.Code != http.StatusCreated {
		t.Fatalf("victim signup = %d (%s)", recV.Code, recV.Body.String())
	}
	victimBody := decodeBody(t, recV)
	pidV := victimBody["pending_id"]
	if pidV == "" {
		t.Fatal("signup response missing pending_id")
	}

	// Attacker re-signs up with the victim's unverified email and their own
	// password (replacement rotates pending_id and OTP).
	recA := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Attacker User",
		"email":     "takeover@example.com",
		"phone":     "+201012345696",
		"password":  "attackerpassword1",
	}, "")
	if recA.Code != http.StatusCreated {
		t.Fatalf("attacker signup = %d (%s)", recA.Code, recA.Body.String())
	}
	attackerBody := decodeBody(t, recA)
	otpA := attackerBody["dev_otp"]
	pidA := attackerBody["pending_id"]
	if otpA == "" || pidA == "" {
		t.Fatalf("attacker signup missing otp/pending_id: %v", attackerBody)
	}
	if pidA == pidV {
		t.Fatal("replacement must rotate pending_id")
	}

	// Victim enters the new OTP with the OLD pending_id: generic 401.
	rec := doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":      "takeover@example.com",
		"code":       otpA,
		"pending_id": pidV,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stale pending_id verify = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	var staleBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &staleBody); err != nil {
		t.Fatalf("decode stale: %v", err)
	}
	if staleBody["code"] != "invalid_token" {
		t.Fatalf("stale pending code = %q, want invalid_token", staleBody["code"])
	}

	// Correct pending_id verifies (replacement flow is legitimate).
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":      "takeover@example.com",
		"code":       otpA,
		"pending_id": pidA,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("current pending_id verify = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	// The account now has the attacker's password, not the victim's.
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":    "takeover@example.com",
		"password": "attackerpassword1",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login with replacement password = %d, want 200", rec.Code)
	}
}

func TestSignupResend_KeepsPendingID(t *testing.T) {
	s := testServer()

	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Resend Pending User",
		"email":     "resendpid@example.com",
		"phone":     "+201012345697",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d (%s)", rec.Code, rec.Body.String())
	}
	signupBody := decodeBody(t, rec)
	pid := signupBody["pending_id"]
	if pid == "" {
		t.Fatal("signup missing pending_id")
	}

	_ = s.Codes.ClearCooldown(context.Background(), "signup", "resendpid@example.com")
	rec = doRequest(t, s, http.MethodPost, "/auth/signup/resend", map[string]string{"email": "resendpid@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("resend = %d (%s)", rec.Code, rec.Body.String())
	}
	var resendBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resendBody); err != nil {
		t.Fatalf("decode resend: %v", err)
	}
	if _, present := resendBody["pending_id"]; present {
		t.Fatalf("resend must not issue a pending_id (keeps current), got %v", resendBody)
	}

	// Verify with the SAME pending_id and the resent code works.
	newOTP := resendBody["dev_otp"]
	if newOTP == "" {
		t.Fatalf("expected dev_otp on resend, got %v", resendBody)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":      "resendpid@example.com",
		"code":       newOTP,
		"pending_id": pid,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify after resend with same pending_id = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestVerify_PhoneTakenReturns409(t *testing.T) {
	s := testServer()

	// A and B both sign up unverified with the same phone (free while unverified).
	recA := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Phone Winner",
		"email":     "phonewinner@example.com",
		"phone":     "+201012345698",
		"password":  "password123",
	}, "")
	if recA.Code != http.StatusCreated {
		t.Fatalf("A signup = %d (%s)", recA.Code, recA.Body.String())
	}
	otpA := decodeBody(t, recA)["dev_otp"]
	recB := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Phone Loser",
		"email":     "phoneloser@example.com",
		"phone":     "+201012345698",
		"password":  "password123",
	}, "")
	if recB.Code != http.StatusCreated {
		t.Fatalf("B signup = %d, want 201 (phone free while unverified) (%s)", recB.Code, recB.Body.String())
	}
	otpB := decodeBody(t, recB)["dev_otp"]

	// A verifies first and takes the phone.
	rec := doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "phonewinner@example.com",
		"code":  otpA,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("A verify = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	// B's verify: generic 409, no 500, B stays unverified.
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "phoneloser@example.com",
		"code":  otpB,
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("B verify = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	var conflictBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &conflictBody); err != nil {
		t.Fatalf("decode B verify: %v", err)
	}
	if conflictBody["code"] != "conflict" {
		t.Fatalf("B verify code = %q, want conflict", conflictBody["code"])
	}
	u, err := s.Store.FindByEmail(context.Background(), "phoneloser@example.com")
	if err != nil || u == nil {
		t.Fatalf("B lookup: %v %+v", err, u)
	}
	if u.EmailVerified {
		t.Fatal("B must stay unverified after phone-taken verify")
	}
}

func TestSignupResend_ExpiredOTPThenVerify(t *testing.T) {
	s := testServer()

	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Resend User",
		"email":     "resend@example.com",
		"phone":     "+201012345689",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d (%s)", rec.Code, rec.Body.String())
	}
	oldOTP := decodeBody(t, rec)["dev_otp"]

	// Expire the OTP by deleting it (simulates 10min expiry); resend issues a new one.
	ctx := context.Background()
	if err := s.Codes.Delete(ctx, "signup-otp:resend@example.com"); err != nil {
		t.Fatalf("delete otp: %v", err)
	}
	// Clear resend cooldown bucket so the test resend is allowed (signup does not
	// consume the resend bucket, but a prior resend in the same test would).
	_ = s.Codes.ClearCooldown(ctx, "signup", "resend@example.com")

	rec = doRequest(t, s, http.MethodPost, "/auth/signup/resend", map[string]string{"email": "resend@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("resend = %d (%s)", rec.Code, rec.Body.String())
	}
	newOTP := decodeBody(t, rec)["dev_otp"]
	if newOTP == "" || newOTP == oldOTP {
		t.Fatalf("expected new OTP after resend, got %q (old %q)", newOTP, oldOTP)
	}

	// Old OTP invalid, new OTP verifies.
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "resend@example.com", "code": oldOTP}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("verify old OTP = %d, want 401", rec.Code)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "resend@example.com", "code": newOTP}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify resent OTP = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestSignupResend_CooldownAndCap(t *testing.T) {
	s := testServer()

	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Cap User",
		"email":     "cap@example.com",
		"phone":     "+201012345690",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d (%s)", rec.Code, rec.Body.String())
	}
	firstOTP := decodeBody(t, rec)["dev_otp"]

	// First resend works.
	rec = doRequest(t, s, http.MethodPost, "/auth/signup/resend", map[string]string{"email": "cap@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("first resend = %d", rec.Code)
	}
	secondOTP := decodeBody(t, rec)["dev_otp"]
	if secondOTP == "" {
		t.Fatal("expected dev_otp on first resend")
	}

	// Immediate second resend is throttled (cooldown): generic ok, no new OTP.
	rec = doRequest(t, s, http.MethodPost, "/auth/signup/resend", map[string]string{"email": "cap@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("throttled resend = %d, want 200", rec.Code)
	}
	var throttled map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &throttled); err != nil {
		t.Fatalf("decode throttled: %v", err)
	}
	if _, hasOTP := throttled["dev_otp"]; hasOTP {
		t.Fatalf("throttled resend must not issue OTP, got %v", throttled)
	}

	// Unknown email gets the same generic response (no oracle).
	rec = doRequest(t, s, http.MethodPost, "/auth/signup/resend", map[string]string{"email": "nobody-resend@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("resend unknown = %d, want 200", rec.Code)
	}
	if got := decodeBody(t, rec)["status"]; got != "ok" {
		t.Fatalf("resend unknown status = %q, want ok", got)
	}

	// Hourly cap: clear cooldown and exhaust to 5/hour, 6th is throttled.
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		_ = s.Codes.ClearCooldown(ctx, "signup", "cap@example.com")
		rec = doRequest(t, s, http.MethodPost, "/auth/signup/resend", map[string]string{"email": "cap@example.com"}, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("resend %d = %d", i, rec.Code)
		}
	}
	_ = s.Codes.ClearCooldown(ctx, "signup", "cap@example.com")
	rec = doRequest(t, s, http.MethodPost, "/auth/signup/resend", map[string]string{"email": "cap@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("capped resend = %d, want 200", rec.Code)
	}
	var capped map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &capped); err != nil {
		t.Fatalf("decode capped: %v", err)
	}
	if _, hasOTP := capped["dev_otp"]; hasOTP {
		t.Fatalf("capped resend must not issue OTP (first %q)", firstOTP)
	}
}

func TestSignup_BlockedPhone(t *testing.T) {
	s := testServer()

	blockedPhone := "+201099999992"
	phoneHash := computeHMAC(s.blocklistKey(), blockedPhone)
	if err := s.Store.AddToBlocklist(context.Background(), "phone", phoneHash, "abusive phone", time.Now()); err != nil {
		t.Fatalf("AddToBlocklist: %v", err)
	}

	// Signup using local representation of the blocked phone (01099999992)
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Phone Abuser",
		"email":     "phoneabuser@example.com",
		"phone":     "01099999992",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("blocked phone signup = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	// P-5: same generic response as any refusal (no oracle)
	if body["error"] != "unable to complete registration" || body["code"] != "conflict" {
		t.Fatalf("expected generic refusal, got %v", body)
	}

	// Assert account was NOT created
	u, err := s.Store.FindByEmail(context.Background(), "phoneabuser@example.com")
	if err != nil || u != nil {
		t.Fatalf("expected no user created for blocked phone, got %+v (err: %v)", u, err)
	}
}

// failingStore implements store.Store and returns simulated DB outage errors.
type failingStore struct {
	err error
}

func (f *failingStore) Create(ctx context.Context, u *models.User) error { return f.err }
func (f *failingStore) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	return nil, f.err
}
func (f *failingStore) FindByID(ctx context.Context, id string) (*models.User, error) {
	return nil, f.err
}
func (f *failingStore) FindByPhone(ctx context.Context, phone string) (*models.User, error) {
	return nil, f.err
}
func (f *failingStore) Update(ctx context.Context, u *models.User) error { return f.err }
func (f *failingStore) SetStatus(ctx context.Context, userID, from, to, reason string, at time.Time) error {
	return f.err
}
func (f *failingStore) Count(ctx context.Context) (int, error) { return 0, f.err }
func (f *failingStore) IsBlocked(ctx context.Context, kind, hash string) (bool, error) {
	return false, f.err
}
func (f *failingStore) AddToBlocklist(ctx context.Context, kind, hash, reason string, at time.Time) error {
	return f.err
}
func (f *failingStore) CreateAdmin(ctx context.Context, a *models.Admin) error { return f.err }
func (f *failingStore) FindAdminByTokenHash(ctx context.Context, tokenHash string) (*models.Admin, error) {
	return nil, f.err
}
func (f *failingStore) FindAdminByID(ctx context.Context, id string) (*models.Admin, error) {
	return nil, f.err
}
func (f *failingStore) RevokeAdmin(ctx context.Context, id string, at time.Time) error { return f.err }
func (f *failingStore) ListUsers(ctx context.Context, filter store.UserFilter) ([]*models.User, int, error) {
	return nil, 0, f.err
}
func (f *failingStore) CreateAuditLog(ctx context.Context, entry *models.AuditLog) error {
	return f.err
}
func (f *failingStore) ListAuditLogs(ctx context.Context, page, limit int) ([]*models.AuditLog, int, error) {
	return nil, 0, f.err
}
func (f *failingStore) CreateOrReplaceSession(ctx context.Context, sess *models.Session) ([]*models.Session, error) {
	return nil, f.err
}
func (f *failingStore) GetSession(ctx context.Context, sid string) (*models.Session, error) {
	return nil, f.err
}
func (f *failingStore) FindSessionByRefreshHash(ctx context.Context, refreshHash string) (*models.Session, error) {
	return nil, f.err
}
func (f *failingStore) UpdateSessionActivity(ctx context.Context, sid string, refreshHash string, lastUsedAt time.Time) error {
	return f.err
}
func (f *failingStore) EndSession(ctx context.Context, sid string, reason models.SessionEndReason, at time.Time) error {
	return f.err
}
func (f *failingStore) EndAllUserSessions(ctx context.Context, userID string, reason models.SessionEndReason, at time.Time) ([]*models.Session, error) {
	return nil, f.err
}
func (f *failingStore) ListActiveSessions(ctx context.Context, userID string) ([]*models.Session, error) {
	return nil, f.err
}

func TestSignup_StoreDown_Returns503(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	fStore := &failingStore{err: errors.New("connection to mongodb refused")}
	s := New(fStore, otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")

	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Test User",
		"email":     "storedown@example.com",
		"phone":     "+201012345699",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("signup store down status = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "service_unavailable" {
		t.Fatalf("expected code service_unavailable, got %q", body["code"])
	}
}

func TestLogin_StoreDown_Returns503(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	fStore := &failingStore{err: errors.New("connection to mongodb timed out")}
	s := New(fStore, otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")

	rec := doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":    "storedown@example.com",
		"password": "password123",
	}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("login store down status = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "service_unavailable" {
		t.Fatalf("expected code service_unavailable, got %q", body["code"])
	}
}

func TestRefresh_StoreDown_Returns503(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	fStore := &failingStore{err: errors.New("connection to mongodb timed out")}
	codes := otp.NewMemoryStore()
	_ = codes.Set(context.Background(), "refresh:"+otp.HashToken("rt-storedown"), "u-1", time.Hour)
	s := New(fStore, codes, NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")

	rec := doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{
		"refresh_token": "rt-storedown",
	}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("refresh store down status = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "service_unavailable" {
		t.Fatalf("expected code service_unavailable, got %q", body["code"])
	}
}

func TestVerifyOTP_StoreDown_Returns503(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	fStore := &failingStore{err: errors.New("connection to mongodb timed out")}
	codes := otp.NewMemoryStore()
	_ = codes.Set(context.Background(), "signup-otp:storedown@example.com", otp.HashToken("123456"), time.Hour)
	s := New(fStore, codes, NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")

	rec := doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "storedown@example.com",
		"code":  "123456",
	}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("verify-otp store down status = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "service_unavailable" {
		t.Fatalf("expected code service_unavailable, got %q", body["code"])
	}
}

func TestMe_StoreDown_Returns503(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	fStore := &failingStore{err: errors.New("connection to mongodb timed out")}
	s := New(fStore, otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")

	tok, err := jwtutil.GenerateToken("u-1", "user", "storedown@example.com")
	if err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, s, http.MethodGet, "/auth/me", nil, tok)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("me store down status = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "service_unavailable" {
		t.Fatalf("expected code service_unavailable, got %q", body["code"])
	}
}

func TestMe_ProfileFields_NewAndLegacy(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	s := New(memStore, otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")
	ctx := context.Background()

	// 1. New user (with full_name and normalized E.164 phone)
	newUser := &models.User{
		ID:            "u-new-1",
		Email:         "new@example.com",
		Role:          models.RoleUser,
		EmailVerified: true,
		FullName:      "Ahmed Mahmud",
		Phone:         "+201098765432",
		Status:        models.StatusActive,
	}
	if err := memStore.Create(ctx, newUser); err != nil {
		t.Fatalf("create new user: %v", err)
	}

	tokNew, err := jwtutil.GenerateToken(newUser.ID, string(newUser.Role), newUser.Email)
	if err != nil {
		t.Fatal(err)
	}

	recNew := doRequest(t, s, http.MethodGet, "/auth/me", nil, tokNew)
	if recNew.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recNew.Code, recNew.Body.String())
	}
	var newProfile map[string]any
	if err := json.Unmarshal(recNew.Body.Bytes(), &newProfile); err != nil {
		t.Fatal(err)
	}

	if newProfile["full_name"] != "Ahmed Mahmud" {
		t.Errorf("full_name = %v, want 'Ahmed Mahmud'", newProfile["full_name"])
	}
	if newProfile["phone"] != "+201098765432" {
		t.Errorf("phone = %v, want '+201098765432'", newProfile["phone"])
	}
	if newProfile["email"] != "new@example.com" {
		t.Errorf("email = %v, want 'new@example.com'", newProfile["email"])
	}
	if newProfile["role"] != "user" {
		t.Errorf("role = %v, want 'user'", newProfile["role"])
	}
	if newProfile["email_verified"] != true {
		t.Errorf("email_verified = %v, want true", newProfile["email_verified"])
	}
	if newProfile["id"] != "u-new-1" {
		t.Errorf("id = %v, want 'u-new-1'", newProfile["id"])
	}

	// Assert NO status or admin fields are leaked
	for _, forbidden := range []string{
		"status", "status_reason", "suspended_at", "reactivated_at", "deleted_at",
		"password_hash", "otp_hash", "reset_token_hash", "admin", "is_admin",
	} {
		if _, exists := newProfile[forbidden]; exists {
			t.Errorf("LEAK: /auth/me leaked forbidden field %q: %v", forbidden, newProfile[forbidden])
		}
	}

	// 2. Legacy user without full_name or phone
	legacyUser := &models.User{
		ID:            "u-legacy-1",
		Email:         "legacy@example.com",
		Role:          models.RoleUser,
		EmailVerified: true,
		FullName:      "",
		Phone:         "",
		Status:        models.StatusActive,
	}
	if err := memStore.Create(ctx, legacyUser); err != nil {
		t.Fatalf("create legacy user: %v", err)
	}

	tokLegacy, err := jwtutil.GenerateToken(legacyUser.ID, string(legacyUser.Role), legacyUser.Email)
	if err != nil {
		t.Fatal(err)
	}

	recLegacy := doRequest(t, s, http.MethodGet, "/auth/me", nil, tokLegacy)
	if recLegacy.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recLegacy.Code, recLegacy.Body.String())
	}
	var legacyProfile map[string]any
	if err := json.Unmarshal(recLegacy.Body.Bytes(), &legacyProfile); err != nil {
		t.Fatal(err)
	}

	if legacyProfile["full_name"] != "" {
		t.Errorf("expected empty full_name for legacy user, got %v", legacyProfile["full_name"])
	}
	if legacyProfile["phone"] != "" {
		t.Errorf("expected empty phone for legacy user, got %v", legacyProfile["phone"])
	}
	if legacyProfile["id"] != "u-legacy-1" {
		t.Errorf("id = %v, want 'u-legacy-1'", legacyProfile["id"])
	}
	if legacyProfile["email"] != "legacy@example.com" {
		t.Errorf("email = %v, want 'legacy@example.com'", legacyProfile["email"])
	}
}

func TestMe_ContractShape(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	s := New(memStore, otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")
	ctx := context.Background()

	u := &models.User{
		ID:            "u-contract-1",
		Email:         "contract@example.com",
		Role:          models.RoleUser,
		EmailVerified: true,
		FullName:      "Contract Student",
		Phone:         "+201055556666",
		Status:        models.StatusActive,
	}
	if err := memStore.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	tok, err := jwtutil.GenerateToken(u.ID, string(u.Role), u.Email)
	if err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, s, http.MethodGet, "/auth/me", nil, tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}

	expectedKeys := map[string]bool{
		"id":             true,
		"email":          true,
		"role":           true,
		"email_verified": true,
		"full_name":      true,
		"phone":          true,
	}
	for k := range raw {
		if !expectedKeys[k] {
			t.Errorf("unexpected key in /auth/me contract response: %q", k)
		}
	}
	for k := range expectedKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing expected key in /auth/me contract response: %q", k)
		}
	}
}

func TestSignup_EmptyBlocklistKey_OutsideLocal_Returns503(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	s := New(store.NewMemoryStore(), otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "production", "gw-secret")
	s.BlocklistHMACKey = "" // empty key outside local/test

	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Test User",
		"email":     "nokey@example.com",
		"phone":     "+201012345602",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on empty blocklist key in production, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "service_unavailable" {
		t.Fatalf("expected code service_unavailable, got %q", body["code"])
	}
}

type legacyStoreWrapper struct {
	store.Store
	legacyUsers map[string]*models.User
}

func (w *legacyStoreWrapper) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	if u, ok := w.legacyUsers[email]; ok {
		cp := *u
		return &cp, nil
	}
	return w.Store.FindByEmail(ctx, email)
}

func (w *legacyStoreWrapper) FindByID(ctx context.Context, id string) (*models.User, error) {
	for _, u := range w.legacyUsers {
		if u.ID == id {
			cp := *u
			return &cp, nil
		}
	}
	return w.Store.FindByID(ctx, id)
}

func (w *legacyStoreWrapper) Update(ctx context.Context, u *models.User) error {
	if existing, ok := w.legacyUsers[u.Email]; ok {
		existing.EmailVerified = u.EmailVerified
		existing.FullName = u.FullName
		existing.Phone = u.Phone
		existing.PasswordHash = u.PasswordHash
		return nil
	}
	return w.Store.Update(ctx, u)
}

func runStatusGateSuite(t *testing.T, baseStore store.Store) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)
	ctx := context.Background()

	pwHash, err := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}

	legacyUsers := make(map[string]*models.User)
	st := &legacyStoreWrapper{
		Store:       baseStore,
		legacyUsers: legacyUsers,
	}

	codes := otp.NewMemoryStore()
	lockout := NewMemoryLockout()
	s := New(st, codes, lockout, mailer.LogSender{}, "test", "gw-secret")
	s.BlocklistHMACKey = "test-blocklist-hmac-key"
	s.DefaultPhoneRegion = "EG"

	type statusCase struct {
		name          string
		email         string
		phone         string
		status        string
		expectSuccess bool
	}

	cases := []statusCase{
		{
			name:          "active",
			email:         "status_active@example.com",
			phone:         "+201099990001",
			status:        string(models.StatusActive),
			expectSuccess: true,
		},
		{
			name:          "legacy-empty-status",
			email:         "status_legacy@example.com",
			phone:         "+201099990002",
			status:        "",
			expectSuccess: true,
		},
		{
			name:          "suspended",
			email:         "status_suspended@example.com",
			phone:         "+201099990003",
			status:        string(models.StatusSuspended),
			expectSuccess: false,
		},
		{
			name:          "deleted",
			email:         "status_deleted@example.com",
			phone:         "+201099990004",
			status:        string(models.StatusDeleted),
			expectSuccess: false,
		},
	}

	// Pre-create the users
	for _, tc := range cases {
		userID := "uid-" + tc.name
		u := &models.User{
			ID:            userID,
			FullName:      "Test " + tc.name,
			Email:         tc.email,
			Phone:         tc.phone,
			PasswordHash:  string(pwHash),
			Role:          models.RoleUser,
			EmailVerified: true,
		}
		if tc.status == "" {
			u.Status = ""
			legacyUsers[tc.email] = u
		} else {
			if err := baseStore.Create(ctx, u); err != nil {
				t.Fatalf("Create user %s: %v", tc.email, err)
			}
			if tc.status != string(models.StatusActive) {
				if err := baseStore.SetStatus(ctx, u.ID, string(models.StatusActive), tc.status, "test status", time.Now()); err != nil {
					t.Fatalf("SetStatus %s: %v", tc.status, err)
				}
			}
		}
	}

	// 1. Table tests for Login path
	t.Run("Login path", func(t *testing.T) {
		var suspendedBody, deletedBody map[string]string
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				rec := doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
					"email":    tc.email,
					"password": "Password123!",
				}, "")
				if tc.expectSuccess {
					if rec.Code != http.StatusOK {
						t.Fatalf("expected 200 for %s, got %d (%s)", tc.name, rec.Code, rec.Body.String())
					}
					body := decodeBody(t, rec)
					if body["access_token"] == "" || body["refresh_token"] == "" {
						t.Fatalf("expected access and refresh tokens, got %+v", body)
					}
				} else {
					if rec.Code != http.StatusUnauthorized {
						t.Fatalf("expected 401 for %s, got %d (%s)", tc.name, rec.Code, rec.Body.String())
					}
					body := decodeBody(t, rec)
					if body["code"] != "unauthorized" || body["error"] != "unauthorized" {
						t.Fatalf("expected generic unauthorized refusal, got %+v", body)
					}
					if tc.status == string(models.StatusSuspended) {
						suspendedBody = body
					} else if tc.status == string(models.StatusDeleted) {
						deletedBody = body
					}
				}
			})
		}
		// Verify identical body for suspended and deleted (no oracle)
		if suspendedBody["code"] != deletedBody["code"] || suspendedBody["error"] != deletedBody["error"] {
			t.Fatalf("suspended and deleted bodies differ: suspended=%+v, deleted=%+v", suspendedBody, deletedBody)
		}

		// Verify: wrong password on suspended account returns normal 401 and counts toward lockout
		t.Run("wrong password on suspended account counts toward lockout", func(t *testing.T) {
			rec := doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
				"email":    "status_suspended@example.com",
				"password": "WrongPassword!",
			}, "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 on wrong password, got %d", rec.Code)
			}
			body := decodeBody(t, rec)
			if body["code"] != "unauthorized" || body["error"] != "invalid credentials" {
				t.Fatalf("expected invalid credentials on wrong password, got %+v", body)
			}
			// New scheme records in LoginLockout (pair + global), not the legacy Lockout.
			mll, ok := s.LoginLockout.(*MemoryLoginLockout)
			if !ok {
				t.Fatalf("LoginLockout is %T, want *MemoryLoginLockout", s.LoginLockout)
			}
			mll.mu.Lock()
			pair := mll.pairs[loginPairKey("status_suspended@example.com", "192.0.2.1")]
			global := mll.global["status_suspended@example.com"]
			var pairCount, globalCount int
			if pair != nil {
				pairCount = pair.count
			}
			if global != nil {
				globalCount = global.count
			}
			mll.mu.Unlock()
			if pairCount != 1 || globalCount != 1 {
				t.Fatalf("expected pair/global failure count 1/1, got %d/%d", pairCount, globalCount)
			}
		})
	})

	// 2. Table tests for Refresh path
	t.Run("Refresh path", func(t *testing.T) {
		var suspendedBody, deletedBody map[string]string
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				rfToken := "rf-" + tc.name
				userID := "uid-" + tc.name
				// Seed refresh token in Codes
				if err := codes.Set(ctx, "refresh:"+otp.HashToken(rfToken), userID, 7*24*time.Hour); err != nil {
					t.Fatalf("seed refresh token: %v", err)
				}
				rec := doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{
					"refresh_token": rfToken,
				}, "")
				if tc.expectSuccess {
					if rec.Code != http.StatusOK {
						t.Fatalf("expected 200 for %s, got %d (%s)", tc.name, rec.Code, rec.Body.String())
					}
					body := decodeBody(t, rec)
					if body["access_token"] == "" || body["refresh_token"] == "" {
						t.Fatalf("expected tokens, got %+v", body)
					}
				} else {
					if rec.Code != http.StatusUnauthorized {
						t.Fatalf("expected 401 for %s, got %d (%s)", tc.name, rec.Code, rec.Body.String())
					}
					body := decodeBody(t, rec)
					if body["code"] != "unauthorized" || body["error"] != "unauthorized" {
						t.Fatalf("expected generic unauthorized refusal, got %+v", body)
					}
					// Ensure key in Codes was NOT deleted
					val, err := codes.Get(ctx, "refresh:"+otp.HashToken(rfToken))
					if err != nil || val != userID {
						t.Fatalf("expected refresh key not deleted for %s, got val=%q err=%v", tc.name, val, err)
					}
					if tc.status == string(models.StatusSuspended) {
						suspendedBody = body
					} else if tc.status == string(models.StatusDeleted) {
						deletedBody = body
					}
				}
			})
		}
		// Verify identical body for suspended and deleted (no oracle)
		if suspendedBody["code"] != deletedBody["code"] || suspendedBody["error"] != deletedBody["error"] {
			t.Fatalf("suspended and deleted bodies differ: suspended=%+v, deleted=%+v", suspendedBody, deletedBody)
		}
	})

	// 3. Table tests for VerifyOTP path
	t.Run("VerifyOTP path", func(t *testing.T) {
		var suspendedBody, deletedBody map[string]string
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				// Seed OTP code in Codes
				if err := codes.Set(ctx, "signup-otp:"+tc.email, otp.HashToken("654321"), 10*time.Minute); err != nil {
					t.Fatalf("seed signup otp: %v", err)
				}
				rec := doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
					"email": tc.email,
					"code":  "654321",
				}, "")
				if tc.expectSuccess {
					if rec.Code != http.StatusOK {
						t.Fatalf("expected 200 for %s, got %d (%s)", tc.name, rec.Code, rec.Body.String())
					}
					body := decodeBody(t, rec)
					if body["access_token"] == "" || body["refresh_token"] == "" {
						t.Fatalf("expected tokens, got %+v", body)
					}
				} else {
					if rec.Code != http.StatusUnauthorized {
						t.Fatalf("expected 401 for %s, got %d (%s)", tc.name, rec.Code, rec.Body.String())
					}
					body := decodeBody(t, rec)
					if body["code"] != "unauthorized" || body["error"] != "unauthorized" {
						t.Fatalf("expected generic unauthorized refusal, got %+v", body)
					}
					if tc.status == string(models.StatusSuspended) {
						suspendedBody = body
					} else if tc.status == string(models.StatusDeleted) {
						deletedBody = body
					}
				}
			})
		}
		// Verify identical body for suspended and deleted (no oracle)
		if suspendedBody["code"] != deletedBody["code"] || suspendedBody["error"] != deletedBody["error"] {
			t.Fatalf("suspended and deleted bodies differ: suspended=%+v, deleted=%+v", suspendedBody, deletedBody)
		}
	})
}

func TestStatusGating_ThreePaths_MemoryStore(t *testing.T) {
	st := store.NewMemoryStore()
	runStatusGateSuite(t, st)
}

func TestStatusGating_ThreePaths_MongoStore(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_status_gate")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := store.NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore: %v", err)
	}

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = client.Database(dbName).Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
	})

	runStatusGateSuite(t, s)
}

func TestRefresh_TokenIssuedBeforeSuspension_Refused(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	st := store.NewMemoryStore()
	s := New(st, otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")
	s.BlocklistHMACKey = "test-blocklist-hmac-key"
	s.DefaultPhoneRegion = "EG"

	// 1. User signs up and verifies while active
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Presuspend User",
		"email":     "presuspend@example.com",
		"phone":     "+201012345601",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d (%s)", rec.Code, rec.Body.String())
	}
	signupBody := decodeBody(t, rec)
	devOtp := signupBody["dev_otp"]

	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "presuspend@example.com",
		"code":  devOtp,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp failed: %d (%s)", rec.Code, rec.Body.String())
	}
	tokens := decodeBody(t, rec)
	refreshToken := tokens["refresh_token"]
	if refreshToken == "" {
		t.Fatal("expected refresh token")
	}

	// 2. Resolve user ID from store
	ctx := context.Background()
	u, err := st.FindByEmail(ctx, "presuspend@example.com")
	if err != nil || u == nil {
		t.Fatalf("FindByEmail failed: %v", err)
	}

	// 3. Admin suspends the user via SetStatus (CAS active -> suspended)
	err = st.SetStatus(ctx, u.ID, string(models.StatusActive), string(models.StatusSuspended), "admin suspended account", time.Now())
	if err != nil {
		t.Fatalf("SetStatus to suspended failed: %v", err)
	}

	// 4. Calling /auth/refresh with the token issued before suspension is refused with 401
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on refresh of suspended user, got %d (%s)", rec.Code, rec.Body.String())
	}
	refusalBody := decodeBody(t, rec)
	if refusalBody["code"] != "unauthorized" || refusalBody["error"] != "unauthorized" {
		t.Fatalf("expected generic unauthorized refusal, got %+v", refusalBody)
	}

	// 5. The refresh entry in s.Codes is NOT deleted
	val, err := s.Codes.Get(ctx, "refresh:"+otp.HashToken(refreshToken))
	if err != nil || (!strings.HasPrefix(val, u.ID+":") && val != u.ID) {
		t.Fatalf("expected refresh key to remain in Codes, got val=%q, err=%v", val, err)
	}

	// 6. If user is reactivated (suspended -> active), refresh with this same token now succeeds
	err = st.SetStatus(ctx, u.ID, string(models.StatusSuspended), string(models.StatusActive), "admin reactivated", time.Now())
	if err != nil {
		t.Fatalf("SetStatus to active failed: %v", err)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on refresh after reactivation, got %d (%s)", rec.Code, rec.Body.String())
	}
	rotatedTokens := decodeBody(t, rec)
	if rotatedTokens["access_token"] == "" || rotatedTokens["refresh_token"] == "" {
		t.Fatalf("expected rotated tokens, got %+v", rotatedTokens)
	}

	// 7. A second refresh with the old token now fails (single redemption consumed it)
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on already-rotated token, got %d (%s)", rec.Code, rec.Body.String())
	}
}

type failingCodesStore struct {
	otp.Store
	consumeErr          error
	allowIssueErr       error
	failuresExceededErr error
	recordFailureErr    error
	clearFailuresErr    error
	setErr              error
}

func (f *failingCodesStore) ConsumeWithAttempts(ctx context.Context, key, hash string, maxAttempts int, ttl time.Duration) (bool, error) {
	if f.consumeErr != nil {
		return false, f.consumeErr
	}
	return f.Store.ConsumeWithAttempts(ctx, key, hash, maxAttempts, ttl)
}

func (f *failingCodesStore) AllowIssue(ctx context.Context, purpose, email string) (bool, error) {
	if f.allowIssueErr != nil {
		return false, f.allowIssueErr
	}
	return f.Store.AllowIssue(ctx, purpose, email)
}

func (f *failingCodesStore) FailuresExceeded(ctx context.Context, purpose, email string, maxFailures int) (bool, error) {
	if f.failuresExceededErr != nil {
		return false, f.failuresExceededErr
	}
	return f.Store.FailuresExceeded(ctx, purpose, email, maxFailures)
}

func (f *failingCodesStore) RecordFailure(ctx context.Context, purpose, email string, ttl time.Duration) (int, error) {
	if f.recordFailureErr != nil {
		return 0, f.recordFailureErr
	}
	return f.Store.RecordFailure(ctx, purpose, email, ttl)
}

func (f *failingCodesStore) ClearFailures(ctx context.Context, purpose, email string) error {
	if f.clearFailuresErr != nil {
		return f.clearFailuresErr
	}
	return f.Store.ClearFailures(ctx, purpose, email)
}

func (f *failingCodesStore) Set(ctx context.Context, key, hash string, ttl time.Duration) error {
	if f.setErr != nil {
		return f.setErr
	}
	return f.Store.Set(ctx, key, hash, ttl)
}

func (f *failingCodesStore) ClearCooldown(ctx context.Context, purpose, email string) error {
	return f.Store.ClearCooldown(ctx, purpose, email)
}

func TestVerifyOTP_AttemptLimit(t *testing.T) {
	s := testServer()

	// 1. Signup to receive initial code
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Attempt User",
		"email":     "attempt_otp@example.com",
		"phone":     "+201099998888",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d (%s)", rec.Code, rec.Body.String())
	}
	var signupBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &signupBody); err != nil {
		t.Fatal(err)
	}
	devOTP := signupBody["dev_otp"].(string)

	// 2. 5 wrong attempts -> all return 401 invalid_token
	for i := 1; i <= 5; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
			"email": "attempt_otp@example.com",
			"code":  "000000",
		}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong attempt %d status = %d, want 401 (%s)", i, rec.Code, rec.Body.String())
		}
		body := decodeBody(t, rec)
		if body["code"] != "invalid_token" {
			t.Fatalf("wrong attempt %d code = %q, want invalid_token", i, body["code"])
		}
	}

	// 3. 6th attempt with the RIGHT code is refused with 401
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "attempt_otp@example.com",
		"code":  devOTP,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("6th attempt with right code status = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "invalid_token" {
		t.Fatalf("6th attempt code = %q, want invalid_token", body["code"])
	}

	// 4. User requests a new code -> new code works
	newCode := "654321"
	if err := s.Codes.Set(context.Background(), "signup-otp:attempt_otp@example.com", otp.HashToken(newCode), 10*time.Minute); err != nil {
		t.Fatalf("set new code failed: %v", err)
	}

	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "attempt_otp@example.com",
		"code":  newCode,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify new code status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	tokens := decodeBody(t, rec)
	if tokens["access_token"] == "" || tokens["refresh_token"] == "" {
		t.Fatalf("expected valid tokens, got %+v", tokens)
	}

	// 5. Counter resets on success:
	// Register another user, do 2 wrong attempts, then 3rd is right code -> succeeds
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Counter Reset User",
		"email":     "reset_counter_otp@example.com",
		"phone":     "+201099998889",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d (%s)", rec.Code, rec.Body.String())
	}
	var signupBody3 map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &signupBody3); err != nil {
		t.Fatal(err)
	}
	devOTP3 := signupBody3["dev_otp"].(string)

	for i := 1; i <= 2; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
			"email": "reset_counter_otp@example.com",
			"code":  "000000",
		}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401 (%s)", i, rec.Code, rec.Body.String())
		}
	}

	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "reset_counter_otp@example.com",
		"code":  devOTP3,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("3rd attempt with right code status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestVerifyResetCode_AttemptLimit(t *testing.T) {
	s := testServer()

	// 1. Create active user
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Reset User",
		"email":     "reset_user@example.com",
		"phone":     "+201011112222",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d (%s)", rec.Code, rec.Body.String())
	}
	var signupBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &signupBody); err != nil {
		t.Fatal(err)
	}
	otpCode := signupBody["dev_otp"].(string)
	doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "reset_user@example.com",
		"code":  otpCode,
	}, "")

	// 2. Request password reset
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{
		"email": "reset_user@example.com",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request status = %d (%s)", rec.Code, rec.Body.String())
	}
	resetBody := decodeBody(t, rec)
	resetOTP := resetBody["dev_otp"]
	if resetOTP == "" {
		t.Fatal("expected dev_otp in reset request response")
	}

	// 3. 5 wrong attempts -> all return 401 invalid_token
	for i := 1; i <= 5; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{
			"email": "reset_user@example.com",
			"code":  "000000",
		}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong attempt %d status = %d, want 401 (%s)", i, rec.Code, rec.Body.String())
		}
		body := decodeBody(t, rec)
		if body["code"] != "invalid_token" {
			t.Fatalf("wrong attempt %d code = %q, want invalid_token", i, body["code"])
		}
	}

	// 4. 6th attempt with the RIGHT code is refused with 401
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{
		"email": "reset_user@example.com",
		"code":  resetOTP,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("6th attempt with right code status = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "invalid_token" {
		t.Fatalf("6th attempt code = %q, want invalid_token", body["code"])
	}

	// 5. Request new reset code -> new code works
	_ = s.Codes.ClearCooldown(context.Background(), "reset", "reset_user@example.com")
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{
		"email": "reset_user@example.com",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("new reset request status = %d (%s)", rec.Code, rec.Body.String())
	}
	resetBody2 := decodeBody(t, rec)
	resetOTP2 := resetBody2["dev_otp"]

	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{
		"email": "reset_user@example.com",
		"code":  resetOTP2,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify new reset code status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	resVerBody := decodeBody(t, rec)
	if resVerBody["reset_token"] == "" {
		t.Fatalf("expected reset_token, got %+v", resVerBody)
	}

	// 6. Counter resets on success:
	// Request reset code, do 2 wrong attempts, then 3rd is right code -> succeeds
	_ = s.Codes.ClearCooldown(context.Background(), "reset", "reset_user@example.com")
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{
		"email": "reset_user@example.com",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request status = %d (%s)", rec.Code, rec.Body.String())
	}
	resetBody3 := decodeBody(t, rec)
	resetOTP3 := resetBody3["dev_otp"]

	for i := 1; i <= 2; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{
			"email": "reset_user@example.com",
			"code":  "000000",
		}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401 (%s)", i, rec.Code, rec.Body.String())
		}
	}

	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{
		"email": "reset_user@example.com",
		"code":  resetOTP3,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("3rd attempt with right reset code status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	resVerBody3 := decodeBody(t, rec)
	if resVerBody3["reset_token"] == "" {
		t.Fatalf("expected reset_token, got %+v", resVerBody3)
	}
}

func TestVerifyOTP_RedisDown_Returns503(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	fCodes := &failingCodesStore{
		Store:      otp.NewMemoryStore(),
		consumeErr: errors.New("redis connection refused"),
	}
	s := New(memStore, fCodes, NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")

	pid, err := otp.GenerateOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	_ = memStore.Create(context.Background(), &models.User{
		ID:            "u-redisdown",
		Email:         "redisdown@example.com",
		Role:          models.RoleUser,
		Status:        models.StatusActive,
		PendingIDHash: otp.HashToken(pid),
	})
	rec := doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":      "redisdown@example.com",
		"code":       "123456",
		"pending_id": pid,
	}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("verify-otp redis down status = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "service_unavailable" {
		t.Fatalf("expected code service_unavailable, got %q", body["code"])
	}
}

func TestVerifyResetCode_RedisDown_Returns503(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	fCodes := &failingCodesStore{
		Store:      otp.NewMemoryStore(),
		consumeErr: errors.New("redis connection refused"),
	}
	s := New(memStore, fCodes, NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")

	rec := doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{
		"email": "redisdown@example.com",
		"code":  "123456",
	}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("reset/verify redis down status = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "service_unavailable" {
		t.Fatalf("expected code service_unavailable, got %q", body["code"])
	}
}

type countingSender struct {
	mu     sync.Mutex
	counts map[string]int
	total  int
}

func (c *countingSender) SendCode(_ context.Context, toEmail, code, purpose string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	c.counts[toEmail]++
	c.total++
	return nil
}

func (c *countingSender) countFor(toEmail string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[toEmail]
}

func (c *countingSender) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts = map[string]int{}
	c.total = 0
}

// blockingSender blocks inside SendCode until released, proving the handler
// does not wait for the mail provider.
type blockingSender struct {
	entered chan struct{}
	release chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	calls   int
}

func newBlockingSender() *blockingSender {
	return &blockingSender{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (b *blockingSender) SendCode(_ context.Context, _, _, _ string) error {
	close(b.entered)
	<-b.release
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	close(b.done)
	return nil
}

func TestRequestReset_ActiveOnlyAndBackground(t *testing.T) {
	newResetServer := func(sender mailer.Sender) *Server {
		jwtutil.Init("test-jwt-secret-0123456789abcdef")
		s := New(store.NewMemoryStore(), otp.NewMemoryStore(), NewMemoryLockout(), sender, "test", "gw-secret")
		s.BlocklistHMACKey = "test-blocklist-hmac-key"
		s.DefaultPhoneRegion = "EG"
		return s
	}
	mustSignupVerify := func(t *testing.T, s *Server, email, phone string) {
		t.Helper()
		rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
			"full_name": "Reset Target",
			"email":     email,
			"phone":     phone,
			"password":  "password123",
		}, "")
		if rec.Code != http.StatusCreated {
			t.Fatalf("signup %s = %d (%s)", email, rec.Code, rec.Body.String())
		}
		code := decodeBody(t, rec)["dev_otp"]
		rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
			"email": email, "code": code,
		}, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("verify %s = %d (%s)", email, rec.Code, rec.Body.String())
		}
	}

	// Suspended account: same 200, no email.
	sender := &countingSender{}
	s := newResetServer(sender)
	mustSignupVerify(t, s, "reset-suspended@example.com", "+201012345701")
	before := sender.countFor("reset-suspended@example.com")
	ctx := context.Background()
	u, err := s.Store.FindByEmail(ctx, "reset-suspended@example.com")
	if err != nil || u == nil {
		t.Fatalf("lookup: %v %+v", err, u)
	}
	if err := s.Store.SetStatus(ctx, u.ID, string(models.StatusActive), string(models.StatusSuspended), "test", time.Now()); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	rec := doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": "reset-suspended@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("suspended reset = %d, want 200", rec.Code)
	}
	// Background send never happens for suspended; poll briefly to be sure.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := sender.countFor("reset-suspended@example.com"); got != before {
		t.Fatalf("suspended account emails = %d, want %d (signup only)", got, before)
	}

	// Deleted account: same 200, no email.
	sender2 := &countingSender{}
	s2 := newResetServer(sender2)
	mustSignupVerify(t, s2, "reset-deleted@example.com", "+201012345702")
	before2 := sender2.countFor("reset-deleted@example.com")
	u2, err := s2.Store.FindByEmail(ctx, "reset-deleted@example.com")
	if err != nil || u2 == nil {
		t.Fatalf("lookup: %v %+v", err, u2)
	}
	if err := s2.Store.SetStatus(ctx, u2.ID, store.FromActiveOrSuspended, string(models.StatusDeleted), "test", time.Now()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rec = doRequest(t, s2, http.MethodPost, "/auth/reset/request", map[string]string{"email": "reset-deleted@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("deleted reset = %d, want 200", rec.Code)
	}
	deadline = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := sender2.countFor("reset-deleted@example.com"); got != before2 {
		t.Fatalf("deleted account emails = %d, want %d (signup only)", got, before2)
	}

	// Handler returns before the sender finishes.
	s3 := newResetServer(&countingSender{})
	mustSignupVerify(t, s3, "reset-blocking@example.com", "+201012345703")
	blocker := newBlockingSender()
	s3.Sender = blocker
	rec = doRequest(t, s3, http.MethodPost, "/auth/reset/request", map[string]string{"email": "reset-blocking@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("blocking reset = %d, want 200", rec.Code)
	}
	// The handler already returned; the sender must be in-flight (entered)
	// but not finished (done open).
	select {
	case <-blocker.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("sender goroutine did not start")
	}
	select {
	case <-blocker.done:
		t.Fatal("handler waited for the sender to finish")
	default:
	}
	close(blocker.release)
	select {
	case <-blocker.done:
	case <-time.After(2 * time.Second):
		t.Fatal("sender did not finish after release")
	}
}

func testResetCode_CooldownAndHourlyCap(t *testing.T, s *Server, sender *countingSender, clearCooldown func(purpose, email string)) {
	t.Helper()
	email := fmt.Sprintf("cooldown-%d@example.com", time.Now().UnixNano())

	// Create user
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Cooldown User",
		"email":     email,
		"phone":     "+201011114444",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d (%s)", rec.Code, rec.Body.String())
	}
	var signupBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &signupBody)
	otpCode := signupBody["dev_otp"].(string)
	doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": email,
		"code":  otpCode,
	}, "")

	sender.reset()

	// 1. First reset request: 200, sends email
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request 1 status = %d (%s)", rec.Code, rec.Body.String())
	}
	body1 := decodeBody(t, rec)
	code1 := body1["dev_otp"]
	if code1 == "" {
		t.Fatal("expected dev_otp on first reset request")
	}
	if sender.countFor(email) != 1 {
		t.Fatalf("expected sender count 1, got %d", sender.countFor(email))
	}

	// 2. Second reset request within 60s: 200, no email sent (fake sender count remains 1)
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("second reset request status = %d (%s)", rec.Code, rec.Body.String())
	}
	body2 := decodeBody(t, rec)
	if body2["dev_otp"] != "" {
		t.Fatalf("expected no dev_otp on cooldown rejection, got %q", body2["dev_otp"])
	}
	if sender.countFor(email) != 1 {
		t.Fatalf("expected sender count to stay 1, got %d", sender.countFor(email))
	}

	// 3. Old code still verifies
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": code1}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("old code verify status = %d (%s)", rec.Code, rec.Body.String())
	}
	if decodeBody(t, rec)["reset_token"] == "" {
		t.Fatal("expected reset_token from old code verify")
	}

	// 4. Issue codes 2..5 (clearing cooldown each time)
	for i := 2; i <= 5; i++ {
		clearCooldown("reset", email)
		rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("reset request %d status = %d (%s)", i, rec.Code, rec.Body.String())
		}
		if sender.countFor(email) != i {
			t.Fatalf("expected sender count %d, got %d", i, sender.countFor(email))
		}
	}

	// 5. 6th code in an hour: 200, nothing sent
	clearCooldown("reset", email)
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request 6 status = %d (%s)", rec.Code, rec.Body.String())
	}
	body6 := decodeBody(t, rec)
	if body6["dev_otp"] != "" {
		t.Fatalf("expected no dev_otp on hourly limit, got %q", body6["dev_otp"])
	}
	if sender.countFor(email) != 5 {
		t.Fatalf("expected sender count to stay 5, got %d", sender.countFor(email))
	}
}

func testResetCode_FailureCapLoop(t *testing.T, s *Server, clearCooldown func(purpose, email string)) {
	t.Helper()
	email := fmt.Sprintf("failloop-%d@example.com", time.Now().UnixNano())

	// Create user
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Loop User",
		"email":     email,
		"phone":     "+201011115555",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d (%s)", rec.Code, rec.Body.String())
	}
	var signupBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &signupBody)
	otpCode := signupBody["dev_otp"].(string)
	doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": email,
		"code":  otpCode,
	}, "")

	// Round 1: request reset, 5 wrong verifies
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request round 1 status = %d", rec.Code)
	}
	for i := 1; i <= 5; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": "000000"}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("round 1 verify %d status = %d, want 401", i, rec.Code)
		}
	}

	// Round 2: request reset, 5 wrong verifies
	clearCooldown("reset", email)
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request round 2 status = %d", rec.Code)
	}
	for i := 1; i <= 5; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": "000000"}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("round 2 verify %d status = %d, want 401", i, rec.Code)
		}
	}

	// Round 3: request reset, 4 wrong verifies -> 14 failures; 5th wrong verify -> 15 failures
	clearCooldown("reset", email)
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request round 3 status = %d", rec.Code)
	}
	code3 := decodeBody(t, rec)["dev_otp"]
	if code3 == "" {
		t.Fatal("expected dev_otp on round 3 reset request")
	}

	for i := 1; i <= 4; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": "000000"}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("round 3 verify %d status = %d, want 401", i, rec.Code)
		}
	}
	// 5th wrong verify in round 3 (15th wrong verify overall)
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": "000000"}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("15th wrong verify status = %d, want 401", rec.Code)
	}

	// Now at the 15th wrong verify in the hour, a CORRECT code is refused with 429
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": code3}, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("correct code after 15 failures status = %d, want 429 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "too_many_attempts" {
		t.Fatalf("expected code too_many_attempts, got %q", body["code"])
	}
}

func testVerify_UnknownEmailMatchesKnownEmail(t *testing.T, s *Server) {
	t.Helper()
	email := fmt.Sprintf("unknown-%d@example.com", time.Now().UnixNano())

	// 1. Request reset: 200 {"status":"ok"}, same as known
	rec := doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown email reset status = %d, want 200", rec.Code)
	}
	if decodeBody(t, rec)["status"] != "ok" {
		t.Fatalf("unknown email reset body != ok")
	}

	// 2. Second request within 60s: 200 {"status":"ok"}
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown email second reset status = %d, want 200", rec.Code)
	}

	// 3. 15 wrong verifies -> all return 401
	for i := 1; i <= 15; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": "123456"}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unknown email verify %d status = %d, want 401", i, rec.Code)
		}
		if decodeBody(t, rec)["code"] != "invalid_token" {
			t.Fatalf("expected code invalid_token")
		}
	}

	// 4. 16th verify -> 429 too_many_attempts
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": "123456"}, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("unknown email 16th verify status = %d, want 429", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["code"] != "too_many_attempts" {
		t.Fatalf("unknown email 16th verify code = %q, want too_many_attempts", body["code"])
	}
}

func testVerify_ClearFailsOnSuccess_SetDoesNotClear(t *testing.T, s *Server, clearCooldown func(purpose, email string)) {
	t.Helper()
	email := fmt.Sprintf("clearfails-%d@example.com", time.Now().UnixNano())

	// Create user
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Clear Fails User",
		"email":     email,
		"phone":     "+201011116666",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d", rec.Code)
	}
	var signupBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &signupBody)
	otpCode := signupBody["dev_otp"].(string)
	doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": email, "code": otpCode}, "")

	// 1. Request reset code 1
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request status = %d", rec.Code)
	}

	// 2. 2 wrong verifies -> fails = 2
	for i := 1; i <= 2; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": "000000"}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong verify status = %d, want 401", rec.Code)
		}
	}

	// 3. Request reset code 2 (calls Set!) -> Set must NOT clear fails
	clearCooldown("reset", email)
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request 2 status = %d", rec.Code)
	}

	// 4. Do 12 more wrong verifies (total 2 + 12 = 14 failures)
	for i := 1; i <= 12; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": "000000"}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("verify status = %d, want 401", rec.Code)
		}
	}
	// 13th wrong verify (which is 2 + 13 = 15th wrong verify overall)
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": "000000"}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("15th wrong verify status = %d, want 401", rec.Code)
	}

	// Any subsequent verify is 429 (proving Set did not clear fails!)
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email, "code": "000000"}, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("verify after 15 fails status = %d, want 429", rec.Code)
	}

	// 5. Test that successful verify DOES clear fails:
	email2 := fmt.Sprintf("clearfails2-%d@example.com", time.Now().UnixNano())
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Clear Fails 2",
		"email":     email2,
		"phone":     "+201011117777",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &signupBody)
	doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": email2, "code": signupBody["dev_otp"].(string)}, "")

	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email2}, "")
	validCode := decodeBody(t, rec)["dev_otp"]

	// 2 wrong verifies -> fails = 2
	for i := 1; i <= 2; i++ {
		doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email2, "code": "000000"}, "")
	}

	// Successful verify with validCode -> clears fails!
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email2, "code": validCode}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("valid verify status = %d, want 200", rec.Code)
	}

	// Issue new code and do 14 wrong verifies: all 14 succeed in returning 401 (not 429!)
	clearCooldown("reset", email2)
	rec = doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": email2}, "")
	for i := 1; i <= 14; i++ {
		rec = doRequest(t, s, http.MethodPost, "/auth/reset/verify", map[string]string{"email": email2, "code": "000000"}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("verify %d after clear fails status = %d, want 401", i, rec.Code)
		}
	}
}

func testVerify_20ParallelWrongVerifies(t *testing.T, s *Server) {
	t.Helper()
	email := fmt.Sprintf("parallelfail-%d@example.com", time.Now().UnixNano())

	const goroutines = 20
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = s.Codes.RecordFailure(context.Background(), "reset", email, time.Hour)
		}()
	}

	close(start)
	wg.Wait()

	// Verify fails counter is exactly 20
	cnt, err := s.Codes.RecordFailure(context.Background(), "reset", email, time.Hour)
	if err != nil {
		t.Fatalf("RecordFailure check error: %v", err)
	}
	if cnt != 21 {
		t.Fatalf("expected fails counter to be 20 (next=21), got %d (next=%d)", cnt-1, cnt)
	}
}

func TestResetCode_CooldownAndHourlyCap_MemoryStore(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	otpStore := otp.NewMemoryStore()
	sender := &countingSender{}
	s := New(memStore, otpStore, NewMemoryLockout(), sender, "test", "gw-secret")
	clearCooldown := func(purpose, email string) {
		_ = otpStore.ClearCooldown(context.Background(), purpose, email)
	}
	testResetCode_CooldownAndHourlyCap(t, s, sender, clearCooldown)
}

func TestResetCode_FailureCapLoop_MemoryStore(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	otpStore := otp.NewMemoryStore()
	sender := &countingSender{}
	s := New(memStore, otpStore, NewMemoryLockout(), sender, "test", "gw-secret")
	clearCooldown := func(purpose, email string) {
		_ = otpStore.ClearCooldown(context.Background(), purpose, email)
	}
	testResetCode_FailureCapLoop(t, s, clearCooldown)
}

func TestVerify_UnknownEmailMatchesKnownEmail_MemoryStore(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	otpStore := otp.NewMemoryStore()
	sender := &countingSender{}
	s := New(memStore, otpStore, NewMemoryLockout(), sender, "test", "gw-secret")
	testVerify_UnknownEmailMatchesKnownEmail(t, s)
}

func TestVerify_ClearFailsOnSuccess_SetDoesNotClear_MemoryStore(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	otpStore := otp.NewMemoryStore()
	sender := &countingSender{}
	s := New(memStore, otpStore, NewMemoryLockout(), sender, "test", "gw-secret")
	clearCooldown := func(purpose, email string) {
		_ = otpStore.ClearCooldown(context.Background(), purpose, email)
	}
	testVerify_ClearFailsOnSuccess_SetDoesNotClear(t, s, clearCooldown)
}

func TestVerify_20ParallelWrongVerifies_MemoryStore(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	otpStore := otp.NewMemoryStore()
	sender := &countingSender{}
	s := New(memStore, otpStore, NewMemoryLockout(), sender, "test", "gw-secret")
	testVerify_20ParallelWrongVerifies(t, s)
}

func TestResetCode_CooldownAndHourlyCap_RedisStore(t *testing.T) {
	redisURI := requireRedis(t)
	opts, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("invalid REDIS_URI %q: %v", redisURI, err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("redis ping failed: %v", err)
		}
		t.Skipf("skipping test: redis unreachable: %v", err)
	}

	prefix := fmt.Sprintf("test_hdl_cd_%d", time.Now().UnixNano())
	otpStore := otp.NewRedisStore(client, prefix)
	memStore := store.NewMemoryStore()
	sender := &countingSender{}
	s := New(memStore, otpStore, NewMemoryLockout(), sender, "test", "gw-secret")
	clearCooldown := func(purpose, email string) {
		_ = otpStore.ClearCooldown(context.Background(), purpose, email)
	}
	testResetCode_CooldownAndHourlyCap(t, s, sender, clearCooldown)
}

func TestResetCode_FailureCapLoop_RedisStore(t *testing.T) {
	redisURI := requireRedis(t)
	opts, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("invalid REDIS_URI %q: %v", redisURI, err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("redis ping failed: %v", err)
		}
		t.Skipf("skipping test: redis unreachable: %v", err)
	}

	prefix := fmt.Sprintf("test_hdl_fail_%d", time.Now().UnixNano())
	otpStore := otp.NewRedisStore(client, prefix)
	memStore := store.NewMemoryStore()
	sender := &countingSender{}
	s := New(memStore, otpStore, NewMemoryLockout(), sender, "test", "gw-secret")
	clearCooldown := func(purpose, email string) {
		_ = otpStore.ClearCooldown(context.Background(), purpose, email)
	}
	testResetCode_FailureCapLoop(t, s, clearCooldown)
}

func TestVerify_UnknownEmailMatchesKnownEmail_RedisStore(t *testing.T) {
	redisURI := requireRedis(t)
	opts, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("invalid REDIS_URI %q: %v", redisURI, err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("redis ping failed: %v", err)
		}
		t.Skipf("skipping test: redis unreachable: %v", err)
	}

	prefix := fmt.Sprintf("test_hdl_unknown_%d", time.Now().UnixNano())
	otpStore := otp.NewRedisStore(client, prefix)
	memStore := store.NewMemoryStore()
	sender := &countingSender{}
	s := New(memStore, otpStore, NewMemoryLockout(), sender, "test", "gw-secret")
	testVerify_UnknownEmailMatchesKnownEmail(t, s)
}

func TestVerify_ClearFailsOnSuccess_SetDoesNotClear_RedisStore(t *testing.T) {
	redisURI := requireRedis(t)
	opts, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("invalid REDIS_URI %q: %v", redisURI, err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("redis ping failed: %v", err)
		}
		t.Skipf("skipping test: redis unreachable: %v", err)
	}

	prefix := fmt.Sprintf("test_hdl_clr_%d", time.Now().UnixNano())
	otpStore := otp.NewRedisStore(client, prefix)
	memStore := store.NewMemoryStore()
	sender := &countingSender{}
	s := New(memStore, otpStore, NewMemoryLockout(), sender, "test", "gw-secret")
	clearCooldown := func(purpose, email string) {
		_ = otpStore.ClearCooldown(context.Background(), purpose, email)
	}
	testVerify_ClearFailsOnSuccess_SetDoesNotClear(t, s, clearCooldown)
}

func TestVerify_20ParallelWrongVerifies_RedisStore(t *testing.T) {
	redisURI := requireRedis(t)
	opts, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("invalid REDIS_URI %q: %v", redisURI, err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("redis ping failed: %v", err)
		}
		t.Skipf("skipping test: redis unreachable: %v", err)
	}

	prefix := fmt.Sprintf("test_hdl_par_%d", time.Now().UnixNano())
	otpStore := otp.NewRedisStore(client, prefix)
	memStore := store.NewMemoryStore()
	sender := &countingSender{}
	s := New(memStore, otpStore, NewMemoryLockout(), sender, "test", "gw-secret")
	testVerify_20ParallelWrongVerifies(t, s)
}

func TestResetAndVerify_StoreErrors(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	errOutage := errors.New("redis connection refused")

	// 1. AllowIssue error -> 200 on RequestReset (anti-enumeration)
	fCodes := &failingCodesStore{
		Store:         otp.NewMemoryStore(),
		allowIssueErr: errOutage,
	}
	s := New(memStore, fCodes, NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")
	rec := doRequest(t, s, http.MethodPost, "/auth/reset/request", map[string]string{"email": "err@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("allowIssue error status = %d, want 200", rec.Code)
	}
	if decodeBody(t, rec)["status"] != "ok" {
		t.Fatalf("expected status: ok")
	}

	// 2. Set error in RequestReset -> 200 on RequestReset, does not send email
	sender := &countingSender{}
	fCodesSet := &failingCodesStore{
		Store:  otp.NewMemoryStore(),
		setErr: errOutage,
	}
	sSet := New(memStore, fCodesSet, NewMemoryLockout(), sender, "test", "gw-secret")
	u := &models.User{
		ID:            "u-err-set",
		Email:         "err_set@example.com",
		Role:          models.RoleUser,
		EmailVerified: true,
		Status:        models.StatusActive,
	}
	_ = memStore.Create(context.Background(), u)
	rec = doRequest(t, sSet, http.MethodPost, "/auth/reset/request", map[string]string{"email": "err_set@example.com"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("set error status = %d, want 200", rec.Code)
	}
	if sender.countFor("err_set@example.com") != 0 {
		t.Fatalf("expected 0 emails sent on Set error, got %d", sender.countFor("err_set@example.com"))
	}

	// 3. FailuresExceeded error -> 503 on VerifyResetCode and VerifyOTP
	fCodesFailures := &failingCodesStore{
		Store:               otp.NewMemoryStore(),
		failuresExceededErr: errOutage,
	}
	sFail := New(memStore, fCodesFailures, NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")
	rec = doRequest(t, sFail, http.MethodPost, "/auth/reset/verify", map[string]string{"email": "err@example.com", "code": "123456"}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("FailuresExceeded err on reset/verify status = %d, want 503", rec.Code)
	}
	rec = doRequest(t, sFail, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "err@example.com", "code": "123456"}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("FailuresExceeded err on verify-otp status = %d, want 503", rec.Code)
	}

	// 4. RecordFailure error -> 503 on VerifyResetCode and VerifyOTP
	fCodesRec := &failingCodesStore{
		Store:            otp.NewMemoryStore(),
		recordFailureErr: errOutage,
	}
	sRec := New(memStore, fCodesRec, NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")
	rec = doRequest(t, sRec, http.MethodPost, "/auth/reset/verify", map[string]string{"email": "err@example.com", "code": "123456"}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("RecordFailure err on reset/verify status = %d, want 503", rec.Code)
	}
	rec = doRequest(t, sRec, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "err@example.com", "code": "123456"}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("RecordFailure err on verify-otp status = %d, want 503", rec.Code)
	}

	// 5. ClearFailures error -> does not fail consumed code; logs error and continues with 200
	baseStore := otp.NewMemoryStore()
	_ = baseStore.Set(context.Background(), "reset-code:err_clear@example.com", otp.HashToken("123456"), time.Hour)
	_ = baseStore.Set(context.Background(), "signup-otp:err_clear@example.com", otp.HashToken("123456"), time.Hour)
	pidClear, err := otp.GenerateOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	uClear := &models.User{
		ID:            "u-err-clear",
		Email:         "err_clear@example.com",
		Role:          models.RoleUser,
		Status:        models.StatusActive,
		PendingIDHash: otp.HashToken(pidClear),
	}
	_ = memStore.Create(context.Background(), uClear)

	fCodesClear := &failingCodesStore{
		Store:            baseStore,
		clearFailuresErr: errOutage,
	}
	sClear := New(memStore, fCodesClear, NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")
	rec = doRequest(t, sClear, http.MethodPost, "/auth/reset/verify", map[string]string{"email": "err_clear@example.com", "code": "123456"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("ClearFailures err on reset/verify status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if decodeBody(t, rec)["reset_token"] == "" {
		t.Fatalf("expected reset_token on successful verify despite ClearFailures error")
	}

	rec = doRequest(t, sClear, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":      "err_clear@example.com",
		"code":       "123456",
		"device_id":  "11111111-1111-4111-8111-111111111111",
		"pending_id": pidClear,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("ClearFailures err on verify-otp status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	otpTokens := decodeBody(t, rec)
	if otpTokens["access_token"] == "" || otpTokens["refresh_token"] == "" {
		t.Fatalf("expected tokens on successful verify-otp despite ClearFailures error")
	}
}
