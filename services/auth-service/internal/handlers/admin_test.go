package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/mailer"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

func newAdminTestServer(st store.Store, lockout Lockout) *Server {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	if st == nil {
		st = store.NewMemoryStore()
	}
	if lockout == nil {
		lockout = NewMemoryLockout()
	}
	s := New(st, otp.NewMemoryStore(), lockout, mailer.LogSender{}, "test", "gw-secret")
	s.InternalToken = "internal-test-token"
	return s
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func doAdminVerify(s *Server, internalTok, adminTok, clientIP, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/internal/admin/verify", nil)
	if internalTok != "" {
		req.Header.Set("X-Internal-Token", internalTok)
	}
	if adminTok != "" {
		req.Header.Set("X-Admin-Token", adminTok)
	}
	if clientIP != "" {
		req.Header.Set("X-Admin-Client-IP", clientIP)
	}
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	} else {
		req.RemoteAddr = "10.0.0.1:12345"
	}
	rec := httptest.NewRecorder()
	s.VerifyAdmin(rec, req)
	return rec
}

func TestVerifyAdmin_ValidToken(t *testing.T) {
	st := store.NewMemoryStore()
	s := newAdminTestServer(st, nil)

	rawToken := "valid-admin-secret-token"
	tokHash := hashToken(rawToken)
	adm := &models.Admin{
		ID:        "adm-001",
		Name:      "Alice Admin",
		TokenHash: tokHash,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	if err := st.CreateAdmin(context.Background(), adm); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	rec := doAdminVerify(s, "internal-test-token", rawToken, "192.168.1.10", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if res["admin_id"] != "adm-001" || res["name"] != "Alice Admin" {
		t.Fatalf("unexpected body: %+v", res)
	}
}

func TestVerifyAdmin_UnknownToken(t *testing.T) {
	s := newAdminTestServer(nil, nil)

	rec := doAdminVerify(s, "internal-test-token", "unknown-token", "192.168.1.10", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "unauthorized" {
		t.Fatalf("expected code unauthorized, got %v", body["code"])
	}
}

func TestVerifyAdmin_ExpiredToken(t *testing.T) {
	st := store.NewMemoryStore()
	s := newAdminTestServer(st, nil)

	rawToken := "expired-admin-token"
	tokHash := hashToken(rawToken)
	adm := &models.Admin{
		ID:        "adm-002",
		Name:      "Bob Expired",
		TokenHash: tokHash,
		CreatedAt: time.Now().UTC().Add(-48 * time.Hour),
		ExpiresAt: time.Now().UTC().Add(-24 * time.Hour), // Expired
	}
	if err := st.CreateAdmin(context.Background(), adm); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	rec := doAdminVerify(s, "internal-test-token", rawToken, "192.168.1.10", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "unauthorized" {
		t.Fatalf("expected code unauthorized, got %v", body["code"])
	}
}

func TestVerifyAdmin_RevokedToken(t *testing.T) {
	st := store.NewMemoryStore()
	s := newAdminTestServer(st, nil)

	rawToken := "revoked-admin-token"
	tokHash := hashToken(rawToken)
	now := time.Now().UTC()
	adm := &models.Admin{
		ID:        "adm-003",
		Name:      "Charlie Revoked",
		TokenHash: tokHash,
		CreatedAt: now.Add(-24 * time.Hour),
		ExpiresAt: now.Add(24 * time.Hour),
		RevokedAt: now.Add(-1 * time.Hour), // Revoked
	}
	if err := st.CreateAdmin(context.Background(), adm); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	rec := doAdminVerify(s, "internal-test-token", rawToken, "192.168.1.10", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "unauthorized" {
		t.Fatalf("expected code unauthorized, got %v", body["code"])
	}
}

func TestVerifyAdmin_MissingAdminTokenHeader(t *testing.T) {
	s := newAdminTestServer(nil, nil)

	rec := doAdminVerify(s, "internal-test-token", "", "192.168.1.10", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "unauthorized" {
		t.Fatalf("expected code unauthorized, got %v", body["code"])
	}
}

func TestVerifyAdmin_MissingOrInvalidInternalToken(t *testing.T) {
	s := newAdminTestServer(nil, nil)

	// Missing internal token
	rec := doAdminVerify(s, "", "some-admin-token", "192.168.1.10", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing internal token: expected status 401, got %d", rec.Code)
	}

	// Wrong internal token
	recWrong := doAdminVerify(s, "wrong-token", "some-admin-token", "192.168.1.10", "")
	if recWrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong internal token: expected status 401, got %d", recWrong.Code)
	}
}

func TestVerifyAdmin_LockoutPerIP(t *testing.T) {
	lockout := NewMemoryLockout()
	s := newAdminTestServer(nil, lockout)
	targetIP := "203.0.113.50"

	// 5 failed attempts from targetIP with different unknown tokens
	for i := 0; i < 5; i++ {
		rec := doAdminVerify(s, "internal-test-token", "bad-token-"+string(rune('A'+i)), targetIP, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, rec.Code)
		}
	}

	// 6th attempt from targetIP must be locked out (429)
	rec6 := doAdminVerify(s, "internal-test-token", "another-token", targetIP, "")
	if rec6.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 6 from targetIP: expected 429, got %d (%s)", rec6.Code, rec6.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rec6.Body.Bytes(), &body)
	if body["code"] != "locked_out" {
		t.Fatalf("expected code locked_out, got %v", body["code"])
	}

	// Attempt from a different IP is NOT locked out
	recOtherIP := doAdminVerify(s, "internal-test-token", "another-token", "203.0.113.99", "")
	if recOtherIP.Code != http.StatusUnauthorized {
		t.Fatalf("attempt from different IP: expected 401, got %d", recOtherIP.Code)
	}
}

func TestVerifyAdmin_LockoutPerTokenHash(t *testing.T) {
	lockout := NewMemoryLockout()
	s := newAdminTestServer(nil, lockout)
	targetedToken := "bad-shared-token"

	// 5 failed attempts with targetedToken from different client IPs
	for i := 0; i < 5; i++ {
		ip := "10.0.1." + string(rune('1'+i))
		rec := doAdminVerify(s, "internal-test-token", targetedToken, ip, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, rec.Code)
		}
	}

	// 6th attempt with targetedToken from a fresh new IP must be locked out by token hash (429)
	rec6 := doAdminVerify(s, "internal-test-token", targetedToken, "10.0.99.99", "")
	if rec6.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 6 for token: expected 429, got %d (%s)", rec6.Code, rec6.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rec6.Body.Bytes(), &body)
	if body["code"] != "locked_out" {
		t.Fatalf("expected code locked_out, got %v", body["code"])
	}

	// Attempt with a different token from the fresh IP is NOT locked out
	recDifferentToken := doAdminVerify(s, "internal-test-token", "fresh-token-xyz", "10.0.99.99", "")
	if recDifferentToken.Code != http.StatusUnauthorized {
		t.Fatalf("attempt with different token: expected 401, got %d", recDifferentToken.Code)
	}
}

func TestVerifyAdmin_StoreDown_Returns503(t *testing.T) {
	fStore := &failingStore{err: errors.New("mongodb cluster down")}
	s := newAdminTestServer(fStore, nil)

	rec := doAdminVerify(s, "internal-test-token", "some-admin-token", "192.168.1.10", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "service_unavailable" {
		t.Fatalf("expected code service_unavailable, got %v", body["code"])
	}
}

func TestVerifyAdmin_ClientIPFallbackToRemoteAddr(t *testing.T) {
	lockout := NewMemoryLockout()
	s := newAdminTestServer(nil, lockout)

	// Missing X-Admin-Client-IP header, RemoteAddr is "198.51.100.22:54321"
	// Perform 5 failures
	for i := 0; i < 5; i++ {
		rec := doAdminVerify(s, "internal-test-token", "tok-"+string(rune('0'+i)), "", "198.51.100.22:54321")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, rec.Code)
		}
	}

	// 6th attempt with same RemoteAddr without X-Admin-Client-IP must be locked out
	rec6 := doAdminVerify(s, "internal-test-token", "another-tok", "", "198.51.100.22:54321")
	if rec6.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 lockout on RemoteAddr host fallback, got %d", rec6.Code)
	}
}

func TestListenerSeparation_BothDirections(t *testing.T) {
	st := store.NewMemoryStore()
	s := newAdminTestServer(st, nil)

	// Public mux setup (as in cmd/main.go)
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/health", Health)
	publicMux.HandleFunc("/auth/signup", s.Signup)
	publicMux.HandleFunc("/auth/login", s.Login)
	publicMux.HandleFunc("/auth/me", s.Me)
	var publicHandler http.Handler = publicMux
	publicHandler = s.GatewayAuth(publicHandler)

	// Admin listener handler
	adminHandler := s.AdminHandler()

	// 1. Public listener must 404 on /internal/admin/* routes
	reqAdminOnPublic := httptest.NewRequest(http.MethodPost, "/internal/admin/verify", nil)
	reqAdminOnPublic.Header.Set("X-Gateway-Secret", "gw-secret")
	reqAdminOnPublic.Header.Set("X-Internal-Token", "internal-test-token")
	recAdminOnPublic := httptest.NewRecorder()
	publicHandler.ServeHTTP(recAdminOnPublic, reqAdminOnPublic)
	if recAdminOnPublic.Code != http.StatusNotFound {
		t.Fatalf("expected public listener to 404 on /internal/admin/verify, got %d", recAdminOnPublic.Code)
	}

	// 2. Admin listener must 404 on public routes (/health, /auth/login, /auth/me)
	for _, pubPath := range []string{"/health", "/auth/login", "/auth/me", "/auth/signup"} {
		reqPubOnAdmin := httptest.NewRequest(http.MethodGet, pubPath, nil)
		reqPubOnAdmin.Header.Set("X-Internal-Token", "internal-test-token")
		recPubOnAdmin := httptest.NewRecorder()
		adminHandler.ServeHTTP(recPubOnAdmin, reqPubOnAdmin)
		if recPubOnAdmin.Code != http.StatusNotFound {
			t.Fatalf("expected admin listener to 404 on %s, got %d", pubPath, recPubOnAdmin.Code)
		}
	}
}
