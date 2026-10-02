package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
)

func setupSessionTestServer(t *testing.T) (*Server, *miniredis.Miniredis, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("connect miniredis: %v", err)
	}
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	jwtutil.SetRedisClient(rdb)

	st := store.NewMemoryStore()
	codes := otp.NewRedisStore(rdb, "auth-test")
	lockout := NewRedisLockout(ratelimit.NewAuthRateLimiter(rdb, "auth-test"))
	s := New(st, codes, lockout, mailer.LogSender{}, "test", "gw-secret")
	s.BlocklistHMACKey = "test-blocklist-hmac-key"
	s.DefaultPhoneRegion = "EG"

	cleanup := func() {
		jwtutil.SetRedisClient(nil)
		_ = rdb.Close()
	}
	return s, mr, cleanup
}

func TestSession_DeviceValidation(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()

	// 1. Create a verified user
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Device Validation User",
		"email":     "devval@example.com",
		"phone":     "+201012345701",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d (%s)", rec.Code, rec.Body.String())
	}
	signupBody := decodeBody(t, rec)
	devOtp := signupBody["dev_otp"]

	// VerifyOTP with missing device_id -> 400
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":              "devval@example.com",
		"code":               devOtp,
		"__omit_device_id__": "true",
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for verify-otp missing device_id, got %d (%s)", rec.Code, rec.Body.String())
	}

	// VerifyOTP with invalid device_id (not UUIDv4) -> 400
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":     "devval@example.com",
		"code":      devOtp,
		"device_id": "not-a-uuid-at-all",
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for verify-otp invalid device_id, got %d (%s)", rec.Code, rec.Body.String())
	}

	// VerifyOTP with device_label > 64 runes -> 400
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":        "devval@example.com",
		"code":         devOtp,
		"device_id":    "11111111-1111-4111-8111-111111111111",
		"device_label": strings.Repeat("a", 65),
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for verify-otp device_label > 64 runes, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Valid VerifyOTP
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":        "devval@example.com",
		"code":         devOtp,
		"device_id":    "11111111-1111-4111-8111-111111111111",
		"device_label": "iPhone 15 Pro",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp failed: %d (%s)", rec.Code, rec.Body.String())
	}

	// Login with missing device_id -> 400
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":              "devval@example.com",
		"password":           "Password123!",
		"__omit_device_id__": "true",
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for login missing device_id, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Login with invalid device_id -> 400
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":     "devval@example.com",
		"password":  "Password123!",
		"device_id": "12345678-1234-1234-1234-123456789abc", // version 1, not 4
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for login non-v4 device_id, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Login with device_label > 64 runes -> 400
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":        "devval@example.com",
		"password":     "Password123!",
		"device_id":    "22222222-2222-4222-8222-222222222222",
		"device_label": strings.Repeat("ه", 65),
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for login device_label > 64 runes, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestSession_TwoDeviceCap_ThirdDeviceEndsLRU(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()

	// 1. User signs up and verifies on Device 1
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Two Device Cap User",
		"email":     "twodev@example.com",
		"phone":     "+201012345702",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d (%s)", rec.Code, rec.Body.String())
	}
	signupBody := decodeBody(t, rec)
	devOtp := signupBody["dev_otp"]

	dev1ID := "11111111-1111-4111-8111-111111111111"
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":        "twodev@example.com",
		"code":         devOtp,
		"device_id":    dev1ID,
		"device_label": "Device One",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp dev1 failed: %d (%s)", rec.Code, rec.Body.String())
	}
	dev1Tokens := decodeBody(t, rec)
	dev1Access := dev1Tokens["access_token"]
	dev1Refresh := dev1Tokens["refresh_token"]

	// Small pause so timestamps differ
	time.Sleep(20 * time.Millisecond)

	// 2. Login from Device 2
	dev2ID := "22222222-2222-4222-8222-222222222222"
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":        "twodev@example.com",
		"password":     "Password123!",
		"device_id":    dev2ID,
		"device_label": "Device Two",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login dev2 failed: %d (%s)", rec.Code, rec.Body.String())
	}
	dev2Tokens := decodeBody(t, rec)
	dev2Access := dev2Tokens["access_token"]
	dev2Refresh := dev2Tokens["refresh_token"]

	ctx := context.Background()
	u, err := s.Store.FindByEmail(ctx, "twodev@example.com")
	if err != nil || u == nil {
		t.Fatalf("FindByEmail failed: %v", err)
	}

	active, err := s.Store.ListActiveSessions(ctx, u.ID)
	if err != nil || len(active) != 2 {
		t.Fatalf("expected 2 active sessions, got %d (%v)", len(active), err)
	}

	time.Sleep(20 * time.Millisecond)

	// 3. Refresh on Device 1 to make Device 1 MORE recently used than Device 2
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{
		"refresh_token": dev1Refresh,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh dev1 failed: %d (%s)", rec.Code, rec.Body.String())
	}
	dev1NewTokens := decodeBody(t, rec)
	dev1Access = dev1NewTokens["access_token"]
	dev1Refresh = dev1NewTokens["refresh_token"]

	time.Sleep(20 * time.Millisecond)

	// 4. Now Device 2 is the least recently used. Login from Device 3.
	dev3ID := "33333333-3333-4333-8333-333333333333"
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":        "twodev@example.com",
		"password":     "Password123!",
		"device_id":    dev3ID,
		"device_label": "Device Three",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login dev3 failed: %d (%s)", rec.Code, rec.Body.String())
	}
	dev3Tokens := decodeBody(t, rec)
	dev3Access := dev3Tokens["access_token"]
	dev3Refresh := dev3Tokens["refresh_token"]

	// Active sessions should now be exactly 2: Device 3 and Device 1
	active, err = s.Store.ListActiveSessions(ctx, u.ID)
	if err != nil || len(active) != 2 {
		t.Fatalf("expected 2 active sessions after dev3, got %d", len(active))
	}
	activeDeviceIDs := map[string]bool{active[0].DeviceID: true, active[1].DeviceID: true}
	if !activeDeviceIDs[dev1ID] || !activeDeviceIDs[dev3ID] {
		t.Fatalf("expected active sessions for dev1 and dev3, got %+v", activeDeviceIDs)
	}
	if activeDeviceIDs[dev2ID] {
		t.Fatalf("device 2 should have been ended as least recently used")
	}

	// 5. Ended Device 2's access token is rejected immediately by ValidateToken
	dev2Claims, err := jwtutil.ValidateToken(dev2Access)
	if err == nil || dev2Claims != nil {
		t.Fatalf("expected ValidateToken to fail for ended device 2 access token, got nil error")
	}
	if !errors.Is(err, jwtutil.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked for ended device 2, got %v", err)
	}

	// 6. Ended Device 2's refresh token gets 401 code "session_replaced"
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{
		"refresh_token": dev2Refresh,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on refresh of ended device 2, got %d (%s)", rec.Code, rec.Body.String())
	}
	refBody := decodeBody(t, rec)
	if refBody["code"] != "session_replaced" {
		t.Fatalf("expected code session_replaced, got %q", refBody["code"])
	}
	if refBody["error"] != "Sorry, this account's usage limit has been exceeded" {
		t.Fatalf("expected English usage limit error message, got %q", refBody["error"])
	}

	// Test Arabic error message with Accept-Language: ar
	reqAr := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader(`{"refresh_token":"`+dev2Refresh+`"}`))
	reqAr.Header.Set("X-Gateway-Secret", "gw-secret")
	reqAr.Header.Set("Accept-Language", "ar,en;q=0.9")
	recAr := httptest.NewRecorder()
	s.GatewayAuth(http.HandlerFunc(s.Refresh)).ServeHTTP(recAr, reqAr)
	if recAr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on arabic refresh, got %d", recAr.Code)
	}
	var arBody map[string]string
	_ = json.Unmarshal(recAr.Body.Bytes(), &arBody)
	if arBody["code"] != "session_replaced" {
		t.Fatalf("expected code session_replaced, got %q", arBody["code"])
	}
	if arBody["error"] != "عفوًا، لقد تجاوزت الحد المسموح لاستخدام هذا الحساب" {
		t.Fatalf("expected Arabic usage limit error message, got %q", arBody["error"])
	}

	// 7. Device 1 and Device 3 tokens remain valid
	if _, err := jwtutil.ValidateToken(dev1Access); err != nil {
		t.Fatalf("dev1 access token should still be valid, got %v", err)
	}
	if _, err := jwtutil.ValidateToken(dev3Access); err != nil {
		t.Fatalf("dev3 access token should still be valid, got %v", err)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": dev3Refresh}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("dev3 refresh should succeed, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestSession_SameDeviceRelogin_KeepsSlots(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()

	// Signup + verify Device 1
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Same Device User",
		"email":     "samedev@example.com",
		"phone":     "+201012345703",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d", rec.Code)
	}
	signupBody := decodeBody(t, rec)
	devOtp := signupBody["dev_otp"]

	dev1ID := "11111111-1111-4111-8111-111111111111"
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":     "samedev@example.com",
		"code":      devOtp,
		"device_id": dev1ID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp failed: %d", rec.Code)
	}
	dev1Tokens := decodeBody(t, rec)
	dev1Access := dev1Tokens["access_token"]
	dev1Refresh := dev1Tokens["refresh_token"]

	// Login Device 2
	dev2ID := "22222222-2222-4222-8222-222222222222"
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":     "samedev@example.com",
		"password":  "Password123!",
		"device_id": dev2ID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login dev2 failed: %d", rec.Code)
	}
	dev2Tokens := decodeBody(t, rec)
	oldDev2Refresh := dev2Tokens["refresh_token"]

	// Re-login from same Device 2
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":     "samedev@example.com",
		"password":  "Password123!",
		"device_id": dev2ID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("re-login dev2 failed: %d", rec.Code)
	}
	newDev2Tokens := decodeBody(t, rec)
	newDev2Access := newDev2Tokens["access_token"]
	newDev2Refresh := newDev2Tokens["refresh_token"]

	ctx := context.Background()
	u, _ := s.Store.FindByEmail(ctx, "samedev@example.com")
	active, err := s.Store.ListActiveSessions(ctx, u.ID)
	if err != nil || len(active) != 2 {
		t.Fatalf("expected exactly 2 active sessions after same device re-login, got %d", len(active))
	}

	// Device 1 was not displaced!
	if _, err := jwtutil.ValidateToken(dev1Access); err != nil {
		t.Fatalf("dev1 access token should still be valid, got %v", err)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": dev1Refresh}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("dev1 refresh should still succeed, got %d (%s)", rec.Code, rec.Body.String())
	}

	// New device 2 tokens work
	if _, err := jwtutil.ValidateToken(newDev2Access); err != nil {
		t.Fatalf("new dev2 access token should be valid, got %v", err)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": newDev2Refresh}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("new dev2 refresh should succeed, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Old dev 2 refresh token was replaced and fails
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": oldDev2Refresh}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old dev2 refresh token should fail, got %d", rec.Code)
	}
}

func TestSession_Logout(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()

	// Signup + verify
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Logout User",
		"email":     "logout@example.com",
		"phone":     "+201012345704",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d", rec.Code)
	}
	signupBody := decodeBody(t, rec)
	devOtp := signupBody["dev_otp"]

	devID := "11111111-1111-4111-8111-111111111111"
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":     "logout@example.com",
		"code":      devOtp,
		"device_id": devID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp failed: %d", rec.Code)
	}
	tokens := decodeBody(t, rec)
	accessToken := tokens["access_token"]
	refreshToken := tokens["refresh_token"]

	claims, err := jwtutil.ValidateToken(accessToken)
	if err != nil || claims.SID == "" {
		t.Fatalf("expected valid token with sid, got err=%v, sid=%q", err, claims.SID)
	}

	// 1. Call POST /auth/logout with Bearer token -> 204
	rec = doRequest(t, s, http.MethodPost, "/auth/logout", nil, accessToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on logout, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 2. Second call to POST /auth/logout with same token is idempotent -> 204
	rec = doRequest(t, s, http.MethodPost, "/auth/logout", nil, accessToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on repeated logout, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 3. Access token is revoked
	_, err = jwtutil.ValidateToken(accessToken)
	if err == nil || !errors.Is(err, jwtutil.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked after logout, got %v", err)
	}

	// 4. Refresh token is revoked
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshToken}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on refresh after logout, got %d", rec.Code)
	}

	// 5. Session in store marked ended with logout
	ctx := context.Background()
	sess, err := s.Store.GetSession(ctx, claims.SID)
	if err != nil || sess == nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if sess.EndedAt == nil || sess.EndReason != models.EndReasonLogout {
		t.Fatalf("expected session ended with logout, got %+v", sess)
	}
}

func TestSession_LegacyTokenWithoutSID(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()

	ctx := context.Background()
	u := &models.User{
		ID:            "u-legacy-1",
		Email:         "legacy@example.com",
		PasswordHash:  "hash",
		Role:          models.RoleUser,
		EmailVerified: true,
		Status:        models.StatusActive,
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	// Generate legacy token without SID
	legacyAccess, err := jwtutil.GenerateToken(u.ID, string(u.Role), u.Email)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	claims, err := jwtutil.ValidateToken(legacyAccess)
	if err != nil {
		t.Fatalf("ValidateToken on legacy token failed: %v", err)
	}
	if claims.SID != "" {
		t.Fatalf("expected empty sid on legacy token, got %q", claims.SID)
	}

	// Set legacy refresh token (stores only u.ID in Redis, no sid)
	legacyRefreshRaw, _ := otp.GenerateOpaqueToken()
	legacyRefreshHash := otp.HashToken(legacyRefreshRaw)
	if err := s.Codes.Set(ctx, "refresh:"+legacyRefreshHash, u.ID, 7*24*time.Hour); err != nil {
		t.Fatalf("Set legacy refresh: %v", err)
	}

	// Refresh with legacy token succeeds
	rec := doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{
		"refresh_token": legacyRefreshRaw,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy refresh failed: %d (%s)", rec.Code, rec.Body.String())
	}
	newTokens := decodeBody(t, rec)
	if newTokens["access_token"] == "" || newTokens["refresh_token"] == "" {
		t.Fatalf("expected tokens in refresh response, got %+v", newTokens)
	}
}

func TestSession_TenParallelLoginsConvergeToTwo(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()

	// Signup + verify
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Parallel Login User",
		"email":     "parlogin@example.com",
		"phone":     "+201012345705",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d", rec.Code)
	}
	signupBody := decodeBody(t, rec)
	devOtp := signupBody["dev_otp"]

	initDevID := "00000000-0000-4000-8000-000000000000"
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":     "parlogin@example.com",
		"code":      devOtp,
		"device_id": initDevID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp failed: %d", rec.Code)
	}

	ctx := context.Background()
	u, _ := s.Store.FindByEmail(ctx, "parlogin@example.com")

	const n = 10
	deviceIDs := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555",
		"66666666-6666-4666-8666-666666666666",
		"77777777-7777-4777-8777-777777777777",
		"88888888-8888-4888-8888-888888888888",
		"99999999-9999-4999-8999-999999999999",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(devID string) {
			defer wg.Done()
			r := doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
				"email":     "parlogin@example.com",
				"password":  "Password123!",
				"device_id": devID,
			}, "")
			if r.Code != http.StatusOK {
				errs <- errors.New("login returned non-200")
			}
		}(deviceIDs[i])
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	active, err := s.Store.ListActiveSessions(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListActiveSessions failed: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected exactly 2 active sessions to remain after 10 parallel logins, got %d", len(active))
	}
}

func TestSession_AdminSuspendAndRevokeSessions(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()

	// 1. Create and verify user on Device 1
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Admin Suspend User",
		"email":     "adminsusp@example.com",
		"phone":     "+201012345706",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d", rec.Code)
	}
	signupBody := decodeBody(t, rec)
	devOtp := signupBody["dev_otp"]

	dev1ID := "11111111-1111-4111-8111-111111111111"
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":     "adminsusp@example.com",
		"code":      devOtp,
		"device_id": dev1ID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp dev1 failed: %d", rec.Code)
	}
	dev1Tokens := decodeBody(t, rec)
	dev1Access := dev1Tokens["access_token"]
	dev1Refresh := dev1Tokens["refresh_token"]

	// 2. Login on Device 2
	dev2ID := "22222222-2222-4222-8222-222222222222"
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":     "adminsusp@example.com",
		"password":  "Password123!",
		"device_id": dev2ID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login dev2 failed: %d", rec.Code)
	}
	dev2Tokens := decodeBody(t, rec)
	dev2Access := dev2Tokens["access_token"]
	dev2Refresh := dev2Tokens["refresh_token"]

	ctx := context.Background()
	u, _ := s.Store.FindByEmail(ctx, "adminsusp@example.com")
	active, err := s.Store.ListActiveSessions(ctx, u.ID)
	if err != nil || len(active) != 2 {
		t.Fatalf("expected 2 active sessions, got %d", len(active))
	}

	// 3. Create Admin account and authenticate
	adm := &models.Admin{
		ID:        "adm-session-test",
		Name:      "Test Admin",
		TokenHash: hashToken("admin-session-token"),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	_ = s.Store.CreateAdmin(ctx, adm)
	s.InternalToken = "internal-test-token"

	// Admin suspends account
	suspendRec := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/"+u.ID+"/suspend", "internal-test-token", "admin-session-token", map[string]string{
		"reason": "suspicious device activity",
	})
	if suspendRec.Code != http.StatusOK {
		t.Fatalf("suspend user failed: %d (%s)", suspendRec.Code, suspendRec.Body.String())
	}

	// 4. All sessions are ended with reason admin
	active, err = s.Store.ListActiveSessions(ctx, u.ID)
	if err != nil || len(active) != 0 {
		t.Fatalf("expected 0 active sessions after admin suspend, got %d", len(active))
	}

	// 5. Access tokens for both devices rejected by ValidateToken
	_, err = jwtutil.ValidateToken(dev1Access)
	if err == nil {
		t.Fatalf("expected dev1 access token to be revoked")
	}
	_, err = jwtutil.ValidateToken(dev2Access)
	if err == nil {
		t.Fatalf("expected dev2 access token to be revoked")
	}

	// 6. Refresh tokens for both devices are refused (401)
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": dev1Refresh}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on dev1 refresh after suspend, got %d", rec.Code)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": dev2Refresh}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on dev2 refresh after suspend, got %d", rec.Code)
	}
}

type failingDeleteCodesStore struct {
	otp.Store
	failDelete bool
}

func (f *failingDeleteCodesStore) Delete(ctx context.Context, key string) error {
	if f.failDelete {
		return errors.New("simulated redis delete error")
	}
	return f.Store.Delete(ctx, key)
}

func TestSession_Replace_RedisFailureReturns503_OldSessionEnded(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()

	failingCodes := &failingDeleteCodesStore{Store: s.Codes}
	s.Codes = failingCodes

	// 1. Signup and verify on Device 1
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Replace Failure User",
		"email":     "replacefail@example.com",
		"phone":     "+201012345710",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d", rec.Code)
	}
	devOtp := decodeBody(t, rec)["dev_otp"]

	dev1ID := "11111111-1111-4111-8111-111111111111"
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":     "replacefail@example.com",
		"code":      devOtp,
		"device_id": dev1ID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp failed: %d", rec.Code)
	}

	ctx := context.Background()
	u, _ := s.Store.FindByEmail(ctx, "replacefail@example.com")
	sessions, _ := s.Store.ListActiveSessions(ctx, u.ID)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 active session, got %d", len(sessions))
	}
	sess1ID := sessions[0].ID

	// 2. Login on Device 2
	dev2ID := "22222222-2222-4222-8222-222222222222"
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":     "replacefail@example.com",
		"password":  "Password123!",
		"device_id": dev2ID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login dev2 failed: %d", rec.Code)
	}

	// Now 2 active sessions. Arm Codes.Delete to fail on replacement.
	failingCodes.failDelete = true

	// 3. Login on Device 3 -> attempts to replace Device 1, but Codes.Delete fails -> 503
	dev3ID := "33333333-3333-4333-8333-333333333333"
	rec = doRequest(t, s, http.MethodPost, "/auth/login", map[string]string{
		"email":     "replacefail@example.com",
		"password":  "Password123!",
		"device_id": dev3ID,
	}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on replace Codes failure, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 4. Verify old session (Device 1) is still ended in the store with reason replaced
	sess1, err := s.Store.GetSession(ctx, sess1ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if sess1 == nil || sess1.EndedAt == nil || sess1.EndReason != models.EndReasonReplaced {
		t.Fatalf("expected old session to still be ended with replaced in store, got: %+v", sess1)
	}
}

func TestSession_Logout_FailingRevokeSessionReturns503(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()

	// 1. Signup and verify
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Logout Revoke Fail User",
		"email":     "logoutrevfail@example.com",
		"phone":     "+201012345711",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d", rec.Code)
	}
	devOtp := decodeBody(t, rec)["dev_otp"]

	devID := "11111111-1111-4111-8111-111111111111"
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":     "logoutrevfail@example.com",
		"code":      devOtp,
		"device_id": devID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp failed: %d", rec.Code)
	}
	accessToken := decodeBody(t, rec)["access_token"]

	// Set Redis client to nil in jwtutil so RevokeSession fails
	jwtutil.SetRedisClient(nil)

	// Logout should return 503 because RevokeSession fails
	rec = doRequest(t, s, http.MethodPost, "/auth/logout", nil, accessToken)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on logout when RevokeSession fails, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestSession_Logout_AlreadyEndedSessionReturns204(t *testing.T) {
	s, mr, cleanup := setupSessionTestServer(t)
	defer cleanup()

	// 1. Signup and verify
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Logout Ended User",
		"email":     "logoutended@example.com",
		"phone":     "+201012345712",
		"password":  "Password123!",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d", rec.Code)
	}
	devOtp := decodeBody(t, rec)["dev_otp"]

	devID := "11111111-1111-4111-8111-111111111111"
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email":     "logoutended@example.com",
		"code":      devOtp,
		"device_id": devID,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp failed: %d", rec.Code)
	}
	accessToken := decodeBody(t, rec)["access_token"]
	claims, _ := jwtutil.ValidateToken(accessToken)

	// Mark session ended in store (e.g. simulated retry after store ended session but before RevokeSession)
	ctx := context.Background()
	now := time.Now().UTC()
	_ = s.Store.EndSession(ctx, claims.SID, models.EndReasonReplaced, now)

	// Verify sid is not yet revoked in Redis
	if mr.Exists("jwt:sid:" + claims.SID) {
		t.Fatalf("expected sid not yet revoked in redis")
	}

	// 1. Logout on already-ended session with Redis failing -> returns 503
	jwtutil.SetRedisClient(nil)
	rec = doRequest(t, s, http.MethodPost, "/auth/logout", nil, accessToken)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on already-ended session logout when RevokeSession fails, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 2. Restore Redis client -> Logout returns 204 and sid is now revoked in Redis
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("connect miniredis: %v", err)
	}
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)

	rec = doRequest(t, s, http.MethodPost, "/auth/logout", nil, accessToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on already-ended session logout, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !mr.Exists("jwt:sid:" + claims.SID) {
		t.Fatalf("expected sid to be revoked in Redis after logout")
	}

	// 3. Subsequent ValidateToken fails with ErrSessionRevoked
	_, err = jwtutil.ValidateToken(accessToken)
	if !errors.Is(err, jwtutil.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked after logout, got %v", err)
	}
}
