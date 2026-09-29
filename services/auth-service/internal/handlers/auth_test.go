package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/mailer"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

func testServer() *Server {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	return New(store.NewMemoryStore(), otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "test", "gw-secret")
}

func testServerProd() *Server {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	return New(store.NewMemoryStore(), otp.NewMemoryStore(), NewMemoryLockout(), mailer.LogSender{}, "production", "gw-secret")
}

func doRequest(t *testing.T, s *Server, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
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
	case "/auth/verify-otp":
		h = s.VerifyOTP
	case "/auth/login":
		h = s.Login
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
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{"email": "User@Example.com", "password": "password123"}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d (%s)", rec.Code, rec.Body.String())
	}
	signup := decodeBody(t, rec)
	code, ok := signup["dev_otp"]
	if !ok || len(code) != 6 {
		t.Fatalf("expected dev_otp in test env, got %v", signup)
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
	doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{"email": "reset@example.com", "password": "password123"}, "")
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

func TestLoginLockoutBackoff(t *testing.T) {
	s := testServer()
	doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{"email": "lock@example.com", "password": "password123"}, "")
	var last int
	for i := 0; i < 6; i++ {
		rec := doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{"email": "lock@example.com", "password": "wrongpass"}, "")
		last = rec.Code
	}
	if last != http.StatusTooManyRequests && last != http.StatusUnauthorized {
		t.Fatalf("expected 401/429 after failures, got %d", last)
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

func TestInvalidRoleRejected(t *testing.T) {
	s := testServer()
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{"email": "r@example.com", "password": "password123", "role": "owner"}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown role = %d, want 400", rec.Code)
	}
}

func TestProductionNeverReturnsDevOTP(t *testing.T) {
	s := testServerProd()
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{"email": "prod@example.com", "password": "password123"}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d (%s)", rec.Code, rec.Body.String())
	}
	var signup map[string]string
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
