package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"golang.org/x/crypto/bcrypt"
)

func TestWebAuth_LoginAndCookie(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("StrongPassword123!"), bcrypt.MinCost)
	u := &models.User{
		ID:            "user-web-1",
		Email:         "webstudent@example.com",
		PasswordHash:  string(hash),
		Role:          models.RoleUser,
		EmailVerified: true,
		FullName:      "Web Student",
		Status:        models.StatusActive,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 1. Web client login
	body, _ := json.Marshal(map[string]string{
		"email":    "webstudent@example.com",
		"password": "StrongPassword123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client", "web")
	rec := httptest.NewRecorder()

	s.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify JSON body has ONLY access_token (no refresh_token)
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	accessTok, ok := resp["access_token"].(string)
	if !ok || accessTok == "" {
		t.Fatalf("expected access_token in response, got: %v", resp)
	}
	if _, hasRefresh := resp["refresh_token"]; hasRefresh {
		t.Fatalf("web client response MUST NOT contain refresh_token in JSON body: %v", resp)
	}

	// Verify claims in access token
	claims, err := jwtutil.ValidateToken(accessTok)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if claims.Client != "web" {
		t.Fatalf("expected claims.Client == 'web', got %q", claims.Client)
	}

	// Verify cookie: HttpOnly, Secure, SameSite=Strict, Path=/api/v1/auth
	cookies := rec.Result().Cookies()
	var refreshCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "refresh_token" {
			refreshCookie = c
			break
		}
	}
	if refreshCookie == nil {
		t.Fatalf("expected refresh_token cookie, found none in %v", cookies)
	}
	if refreshCookie.Value == "" {
		t.Fatalf("refresh_token cookie value is empty")
	}
	if !refreshCookie.HttpOnly {
		t.Errorf("cookie HttpOnly = false, want true")
	}
	if !refreshCookie.Secure {
		t.Errorf("cookie Secure = false, want true")
	}
	if refreshCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite = %v, want SameSiteStrictMode", refreshCookie.SameSite)
	}
	if refreshCookie.Path != "/api/v1/auth" {
		t.Errorf("cookie Path = %q, want '/api/v1/auth'", refreshCookie.Path)
	}

	// 2. Mobile client login (without X-Client: web)
	mobileBody, _ := json.Marshal(map[string]string{
		"email":     "webstudent@example.com",
		"password":  "StrongPassword123!",
		"device_id": "a0000000-0000-4000-8000-000000000001",
	})
	mReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(mobileBody))
	mReq.Header.Set("Content-Type", "application/json")
	mRec := httptest.NewRecorder()

	s.Login(mRec, mReq)

	if mRec.Code != http.StatusOK {
		t.Fatalf("mobile login expected 200, got %d: %s", mRec.Code, mRec.Body.String())
	}
	var mResp map[string]any
	_ = json.Unmarshal(mRec.Body.Bytes(), &mResp)
	if mResp["access_token"] == nil || mResp["refresh_token"] == nil {
		t.Fatalf("mobile response must have both access_token and refresh_token in JSON: %v", mResp)
	}
	mClaims, err := jwtutil.ValidateToken(mResp["access_token"].(string))
	if err != nil {
		t.Fatalf("validate mobile token: %v", err)
	}
	if mClaims.Client != "" {
		t.Fatalf("mobile token must have empty Client claim, got %q", mClaims.Client)
	}
	// Mobile must NOT get a refresh_token cookie
	for _, c := range mRec.Result().Cookies() {
		if c.Name == "refresh_token" {
			t.Fatalf("mobile client should not receive refresh_token cookie")
		}
	}
}

func TestWebAuth_VerifyOTPAndCookie(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("StrongPassword123!"), bcrypt.MinCost)
	pendingID := "pending-web-123456789012345678"
	u := &models.User{
		ID:            "user-web-otp",
		Email:         "webotp@example.com",
		PasswordHash:  string(hash),
		Role:          models.RoleUser,
		EmailVerified: false,
		FullName:      "Web OTP Student",
		PendingIDHash: otp.HashToken(pendingID),
		Status:        models.StatusActive,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	_ = s.Codes.Set(ctx, "signup-otp:webotp@example.com", otp.HashToken("123456"), 10*time.Minute)

	// Web client VerifyOTP
	body, _ := json.Marshal(map[string]string{
		"email":      "webotp@example.com",
		"code":       "123456",
		"pending_id": pendingID,
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/verify-otp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client", "web")
	rec := httptest.NewRecorder()

	s.VerifyOTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["access_token"] == nil {
		t.Fatalf("expected access_token in response: %v", resp)
	}
	if _, hasRefresh := resp["refresh_token"]; hasRefresh {
		t.Fatalf("web client response must not have refresh_token in JSON: %v", resp)
	}

	claims, err := jwtutil.ValidateToken(resp["access_token"].(string))
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if claims.Client != "web" {
		t.Fatalf("expected claims.Client == 'web', got %q", claims.Client)
	}

	cookies := rec.Result().Cookies()
	var foundCookie bool
	for _, c := range cookies {
		if c.Name == "refresh_token" && c.Value != "" && c.HttpOnly && c.Secure && c.SameSite == http.SameSiteStrictMode {
			foundCookie = true
			break
		}
	}
	if !foundCookie {
		t.Fatalf("expected HttpOnly; Secure; SameSite=Strict refresh_token cookie, got: %v", cookies)
	}
}

func TestWebAuth_RefreshAndRotateCookie(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("StrongPassword123!"), bcrypt.MinCost)
	u := &models.User{
		ID:            "user-web-refresh",
		Email:         "webrefresh@example.com",
		PasswordHash:  string(hash),
		Role:          models.RoleUser,
		EmailVerified: true,
		FullName:      "Web Refresh Student",
		Status:        models.StatusActive,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Login as web to obtain cookie
	body, _ := json.Marshal(map[string]string{
		"email":    "webrefresh@example.com",
		"password": "StrongPassword123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client", "web")
	rec := httptest.NewRecorder()
	s.Login(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d", rec.Code)
	}

	cookies := rec.Result().Cookies()
	var initialCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "refresh_token" {
			initialCookie = c
			break
		}
	}
	if initialCookie == nil {
		t.Fatalf("no refresh cookie after login")
	}

	// 1. Refresh using cookie without body
	refReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	refReq.Header.Set("X-Client", "web")
	refReq.AddCookie(initialCookie)
	refRec := httptest.NewRecorder()

	s.Refresh(refRec, refReq)

	if refRec.Code != http.StatusOK {
		t.Fatalf("refresh expected 200, got %d: %s", refRec.Code, refRec.Body.String())
	}
	var refResp map[string]any
	_ = json.Unmarshal(refRec.Body.Bytes(), &refResp)
	if refResp["access_token"] == nil {
		t.Fatalf("expected access_token in refresh response")
	}
	if _, hasRefresh := refResp["refresh_token"]; hasRefresh {
		t.Fatalf("web refresh response must not contain refresh_token in JSON: %v", refResp)
	}
	newClaims, err := jwtutil.ValidateToken(refResp["access_token"].(string))
	if err != nil {
		t.Fatalf("validate refreshed token: %v", err)
	}
	if newClaims.Client != "web" {
		t.Fatalf("refreshed token Client claim must be 'web', got %q", newClaims.Client)
	}

	// Verify rotated cookie
	refCookies := refRec.Result().Cookies()
	var rotatedCookie *http.Cookie
	for _, c := range refCookies {
		if c.Name == "refresh_token" {
			rotatedCookie = c
			break
		}
	}
	if rotatedCookie == nil || rotatedCookie.Value == "" {
		t.Fatalf("expected new rotated cookie")
	}
	if rotatedCookie.Value == initialCookie.Value {
		t.Fatalf("rotated cookie value must differ from initial cookie value")
	}
	if !rotatedCookie.HttpOnly || !rotatedCookie.Secure || rotatedCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("rotated cookie has invalid flags: %v", rotatedCookie)
	}
}

func TestWebAuth_LogoutClearsCookie(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("StrongPassword123!"), bcrypt.MinCost)
	u := &models.User{
		ID:            "user-web-logout",
		Email:         "weblogout@example.com",
		PasswordHash:  string(hash),
		Role:          models.RoleUser,
		EmailVerified: true,
		FullName:      "Web Logout Student",
		Status:        models.StatusActive,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Login as web
	body, _ := json.Marshal(map[string]string{
		"email":    "weblogout@example.com",
		"password": "StrongPassword123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client", "web")
	rec := httptest.NewRecorder()
	s.Login(rec, req)

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	accessTok := resp["access_token"].(string)

	// Logout with X-Client: web
	outReq := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	outReq.Header.Set("Authorization", "Bearer "+accessTok)
	outReq.Header.Set("X-Client", "web")
	outRec := httptest.NewRecorder()

	s.Logout(outRec, outReq)

	if outRec.Code != http.StatusNoContent {
		t.Fatalf("logout expected 204, got %d: %s", outRec.Code, outRec.Body.String())
	}

	// Check cleared cookie
	cookies := outRec.Result().Cookies()
	var clearedCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "refresh_token" {
			clearedCookie = c
			break
		}
	}
	if clearedCookie == nil {
		t.Fatalf("expected cleared refresh_token cookie")
	}
	if clearedCookie.MaxAge != -1 {
		t.Fatalf("expected MaxAge == -1 on cleared cookie, got %d", clearedCookie.MaxAge)
	}
}
