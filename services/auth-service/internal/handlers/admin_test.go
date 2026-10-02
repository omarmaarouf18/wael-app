package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/mailer"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
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
	s.BlocklistHMACKey = "test-blocklist-hmac-key"
	s.DefaultPhoneRegion = "EG"
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

func setupAdminTestEnv(t *testing.T, st store.Store) (*Server, string, string) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	jwtutil.SetRedisClient(rdb)
	t.Cleanup(func() {
		_ = rdb.Close()
		mr.Close()
		jwtutil.SetRedisClient(nil)
	})

	if st == nil {
		st = store.NewMemoryStore()
	}
	lockout := NewMemoryLockout()
	s := New(st, otp.NewMemoryStore(), lockout, mailer.LogSender{}, "test", "gw-secret")
	s.InternalToken = "internal-test-token"
	s.BlocklistHMACKey = "test-blocklist-hmac-key"
	s.DefaultPhoneRegion = "EG"

	now := time.Now().UTC()

	// Provision active admin
	validRawToken := "admin-valid-secret-token"
	adm := &models.Admin{
		ID:        "adm-001",
		Name:      "Alice Operator",
		TokenHash: hashToken(validRawToken),
		CreatedAt: now,
		ExpiresAt: now.Add(48 * time.Hour),
	}
	if err := st.CreateAdmin(context.Background(), adm); err != nil {
		t.Fatalf("CreateAdmin valid: %v", err)
	}

	// Provision revoked admin
	revokedRawToken := "admin-revoked-secret-token"
	revAdm := &models.Admin{
		ID:        "adm-002",
		Name:      "Bob Revoked",
		TokenHash: hashToken(revokedRawToken),
		CreatedAt: now.Add(-24 * time.Hour),
		ExpiresAt: now.Add(24 * time.Hour),
		RevokedAt: now.Add(-1 * time.Hour),
	}
	if err := st.CreateAdmin(context.Background(), revAdm); err != nil {
		t.Fatalf("CreateAdmin revoked: %v", err)
	}

	return s, validRawToken, revokedRawToken
}

func doAdminRequest(s *Server, method, path, internalTok, adminTok string, body any) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if internalTok != "" {
		req.Header.Set("X-Internal-Token", internalTok)
	}
	if adminTok != "" {
		req.Header.Set("X-Admin-Token", adminTok)
	}
	req.Header.Set("X-Admin-Client-IP", "10.0.0.99")
	rec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(rec, req)
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

// -----------------------------------------------------------------------
// Phase 1.5 Admin Route Tests
// -----------------------------------------------------------------------

func TestAdmin_AuthMatrix(t *testing.T) {
	s, validTok, revokedTok := setupAdminTestEnv(t, nil)

	// Pre-create user for suspend/reactivate/delete targets
	now := time.Now().UTC()
	u := &models.User{
		ID:        "target-user-1",
		Email:     "target1@example.com",
		Status:    models.StatusActive,
		CreatedAt: now,
	}
	if err := s.Store.Create(context.Background(), u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	routes := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{
			name:   "GET /accounts",
			method: http.MethodGet,
			path:   "/internal/admin/accounts",
			body:   nil,
		},
		{
			name:   "POST /accounts/{id}/suspend",
			method: http.MethodPost,
			path:   "/internal/admin/accounts/target-user-1/suspend",
			body:   map[string]string{"reason": "test suspension reason"},
		},
		{
			name:   "POST /accounts/{id}/reactivate",
			method: http.MethodPost,
			path:   "/internal/admin/accounts/target-user-1/reactivate",
			body:   nil,
		},
		{
			name:   "DELETE /accounts/{id}",
			method: http.MethodDelete,
			path:   "/internal/admin/accounts/target-user-1",
			body:   map[string]string{"reason": "test deletion reason"},
		},
		{
			name:   "GET /audit-log",
			method: http.MethodGet,
			path:   "/internal/admin/audit-log",
			body:   nil,
		},
	}

	for _, rt := range routes {
		t.Run(rt.name+"/MissingInternalToken", func(t *testing.T) {
			rec := doAdminRequest(s, rt.method, rt.path, "", validTok, rt.body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
		t.Run(rt.name+"/MissingAdminToken", func(t *testing.T) {
			rec := doAdminRequest(s, rt.method, rt.path, "internal-test-token", "", rt.body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
		t.Run(rt.name+"/InvalidAdminToken", func(t *testing.T) {
			rec := doAdminRequest(s, rt.method, rt.path, "internal-test-token", "bogus-admin-token", rt.body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
		t.Run(rt.name+"/RevokedAdminToken", func(t *testing.T) {
			rec := doAdminRequest(s, rt.method, rt.path, "internal-test-token", revokedTok, rt.body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
		t.Run(rt.name+"/ValidAdminToken", func(t *testing.T) {
			rec := doAdminRequest(s, rt.method, rt.path, "internal-test-token", validTok, rt.body)
			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("expected non-401 for valid tokens, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdmin_Accounts_ListAndSearch(t *testing.T) {
	s, validTok, _ := setupAdminTestEnv(t, nil)
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed 5 users with various names, emails, phones, statuses
	users := []*models.User{
		{
			ID:        "usr-1",
			FullName:  "Mohamed Ali",
			Email:     "m.ali@example.com",
			Phone:     "+201011111111",
			Status:    models.StatusActive,
			CreatedAt: now.Add(-5 * time.Minute),
		},
		{
			ID:        "usr-2",
			FullName:  "Sara Ahmed",
			Email:     "sara@example.com",
			Phone:     "+201022222222",
			Status:    models.StatusActive,
			CreatedAt: now.Add(-4 * time.Minute),
		},
		{
			ID:           "usr-3",
			FullName:     "Kareem Hassan",
			Email:        "kareem@example.com",
			Phone:        "+201033333333",
			Status:       models.StatusSuspended,
			StatusReason: "past suspension",
			CreatedAt:    now.Add(-3 * time.Minute),
		},
		{
			ID:        "usr-4",
			FullName:  "Omar Tarek",
			Email:     "omar@example.com",
			Phone:     "+201044444444",
			Status:    models.StatusDeleted,
			CreatedAt: now.Add(-2 * time.Minute),
		},
		{
			ID:        "usr-5",
			FullName:  "Special.*User",
			Email:     "special.*@example.com",
			Phone:     "+201055555555",
			Status:    models.StatusActive,
			CreatedAt: now.Add(-1 * time.Minute),
		},
	}
	for _, u := range users {
		if err := s.Store.Create(ctx, u); err != nil {
			t.Fatalf("Create user %s: %v", u.ID, err)
		}
	}

	// 1. List all accounts (default limit=20, page=1)
	rec := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts", "internal-test-token", validTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var res struct {
		Items []*models.UserDTO `json:"items"`
		Total int               `json:"total"`
		Page  int               `json:"page"`
		Limit int               `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Total != 5 || len(res.Items) != 5 {
		t.Fatalf("expected total 5, got total=%d items=%d", res.Total, len(res.Items))
	}

	// 2. Pagination (limit=2, page=1 and page=2)
	recPage1 := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?limit=2&page=1", "internal-test-token", validTok, nil)
	var resPage1 struct {
		Items []*models.UserDTO `json:"items"`
		Total int               `json:"total"`
	}
	_ = json.Unmarshal(recPage1.Body.Bytes(), &resPage1)
	if len(resPage1.Items) != 2 || resPage1.Total != 5 {
		t.Fatalf("page 1: expected 2 items of 5 total, got %d of %d", len(resPage1.Items), resPage1.Total)
	}

	// 3. Limit cap: limit=500 capped to 100
	recCap := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?limit=500", "internal-test-token", validTok, nil)
	var resCap struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(recCap.Body.Bytes(), &resCap)
	if resCap.Limit != 100 {
		t.Fatalf("expected limit capped to 100, got %d", resCap.Limit)
	}

	// 4. Status filter: active (3), suspended (1), deleted (1), invalid -> 400
	recActive := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?status=active", "internal-test-token", validTok, nil)
	var resActive struct {
		Items []*models.UserDTO `json:"items"`
		Total int               `json:"total"`
	}
	_ = json.Unmarshal(recActive.Body.Bytes(), &resActive)
	if resActive.Total != 3 {
		t.Fatalf("expected 3 active users, got %d", resActive.Total)
	}

	recSusp := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?status=suspended", "internal-test-token", validTok, nil)
	var resSusp struct {
		Items []*models.UserDTO `json:"items"`
		Total int               `json:"total"`
	}
	_ = json.Unmarshal(recSusp.Body.Bytes(), &resSusp)
	if resSusp.Total != 1 || resSusp.Items[0].ID != "usr-3" {
		t.Fatalf("expected 1 suspended user (usr-3), got %d", resSusp.Total)
	}

	recDel := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?status=deleted", "internal-test-token", validTok, nil)
	var resDel struct {
		Items []*models.UserDTO `json:"items"`
		Total int               `json:"total"`
	}
	_ = json.Unmarshal(recDel.Body.Bytes(), &resDel)
	if resDel.Total != 1 || resDel.Items[0].ID != "usr-4" {
		t.Fatalf("expected 1 deleted user (usr-4), got %d", resDel.Total)
	}

	recBadStatus := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?status=invalid_status", "internal-test-token", validTok, nil)
	if recBadStatus.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d", recBadStatus.Code)
	}

	// 5. Search by name, email, exact id, phone
	recSearchName := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?search=sara", "internal-test-token", validTok, nil)
	var resName struct {
		Items []*models.UserDTO `json:"items"`
	}
	_ = json.Unmarshal(recSearchName.Body.Bytes(), &resName)
	if len(resName.Items) != 1 || resName.Items[0].ID != "usr-2" {
		t.Fatalf("search name sara failed, got %+v", resName.Items)
	}

	recSearchMail := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?search=kareem@example.com", "internal-test-token", validTok, nil)
	var resMail struct {
		Items []*models.UserDTO `json:"items"`
	}
	_ = json.Unmarshal(recSearchMail.Body.Bytes(), &resMail)
	if len(resMail.Items) != 1 || resMail.Items[0].ID != "usr-3" {
		t.Fatalf("search email kareem failed, got %+v", resMail.Items)
	}

	recSearchID := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?search=usr-1", "internal-test-token", validTok, nil)
	var resID struct {
		Items []*models.UserDTO `json:"items"`
	}
	_ = json.Unmarshal(recSearchID.Body.Bytes(), &resID)
	if len(resID.Items) != 1 || resID.Items[0].ID != "usr-1" {
		t.Fatalf("search exact ID usr-1 failed, got %+v", resID.Items)
	}

	// Phone normalized Egyptian search (01011111111 -> +201011111111)
	recSearchPhone := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?search=01011111111", "internal-test-token", validTok, nil)
	var resPhone struct {
		Items []*models.UserDTO `json:"items"`
	}
	_ = json.Unmarshal(recSearchPhone.Body.Bytes(), &resPhone)
	if len(resPhone.Items) != 1 || resPhone.Items[0].ID != "usr-1" {
		t.Fatalf("search normalized phone failed, got %+v", resPhone.Items)
	}

	// 6. Search regex escaping: ".*" matches literally, NOT all users
	recRegex := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?search=.*", "internal-test-token", validTok, nil)
	var resRegex struct {
		Items []*models.UserDTO `json:"items"`
		Total int               `json:"total"`
	}
	_ = json.Unmarshal(recRegex.Body.Bytes(), &resRegex)
	if resRegex.Total != 1 || len(resRegex.Items) != 1 || resRegex.Items[0].ID != "usr-5" {
		t.Fatalf("expected search '.*' to match ONLY usr-5 literally, got total=%d items=%+v", resRegex.Total, resRegex.Items)
	}

	// 7. DTO has no secret fields
	var rawBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rawBody)
	itemsRaw, ok := rawBody["items"].([]any)
	if !ok || len(itemsRaw) == 0 {
		t.Fatalf("items not found in raw response")
	}
	for i, item := range itemsRaw {
		itemMap := item.(map[string]any)
		for _, secretField := range []string{"password_hash", "otp", "otp_code", "code", "reset_token", "reset_token_expires_at"} {
			if _, exists := itemMap[secretField]; exists {
				t.Fatalf("item %d leaks secret field %q in response: %+v", i, secretField, itemMap)
			}
		}
	}
}

func TestAdmin_Accounts_SearchCap_Arabic_And_Regex(t *testing.T) {
	s, validTok, _ := setupAdminTestEnv(t, nil)
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed users: one with Arabic name, one with literal regex characters
	uArabic := &models.User{
		ID:        "usr-ar-1",
		FullName:  "أحمد علي",
		Email:     "ahmed.ali@example.com",
		Phone:     "+201099112233",
		Status:    models.StatusActive,
		CreatedAt: now.Add(-2 * time.Minute),
	}
	uRegex := &models.User{
		ID:        "usr-regex-literal",
		FullName:  "User .* Literal",
		Email:     "literal@example.com",
		Phone:     "+201099445566",
		Status:    models.StatusActive,
		CreatedAt: now.Add(-1 * time.Minute),
	}
	for _, u := range []*models.User{uArabic, uRegex} {
		if err := s.Store.Create(ctx, u); err != nil {
			t.Fatalf("Create user %s: %v", u.ID, err)
		}
	}

	// 1. Exactly 100 runes search -> OK (200)
	query100 := strings.Repeat("a", 100)
	rec100 := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?search="+query100, "internal-test-token", validTok, nil)
	if rec100.Code != http.StatusOK {
		t.Fatalf("expected 200 for 100-rune search, got %d (%s)", rec100.Code, rec100.Body.String())
	}

	// 2. 101 runes ASCII search -> 400 "search too long"
	query101 := strings.Repeat("a", 101)
	rec101 := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?search="+query101, "internal-test-token", validTok, nil)
	if rec101.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for 101-rune search, got %d (%s)", rec101.Code, rec101.Body.String())
	}
	var errResp101 map[string]any
	if err := json.Unmarshal(rec101.Body.Bytes(), &errResp101); err == nil {
		if errResp101["error"] != "search too long" {
			t.Fatalf("expected 'search too long' message, got %v", errResp101["error"])
		}
	}

	// 3. 101 runes Arabic search -> 400 "search too long" (multibyte UTF-8 verification)
	queryArabic101 := strings.Repeat("أ", 101)
	recArabic101 := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?search="+url.QueryEscape(queryArabic101), "internal-test-token", validTok, nil)
	if recArabic101.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for 101-rune arabic search, got %d (%s)", recArabic101.Code, recArabic101.Body.String())
	}

	// 4. Arabic name search works (e.g. "أحمد" finds "أحمد علي")
	recSearchArabic := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?search="+url.QueryEscape("أحمد"), "internal-test-token", validTok, nil)
	if recSearchArabic.Code != http.StatusOK {
		t.Fatalf("expected 200 searching arabic name 'أحمد', got %d (%s)", recSearchArabic.Code, recSearchArabic.Body.String())
	}
	var resArabic struct {
		Items []*models.UserDTO `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(recSearchArabic.Body.Bytes(), &resArabic); err != nil {
		t.Fatalf("unmarshal arabic search: %v", err)
	}
	if resArabic.Total != 1 || len(resArabic.Items) != 1 || resArabic.Items[0].FullName != "أحمد علي" {
		t.Fatalf("expected to find 'أحمد علي', got total=%d items=%+v", resArabic.Total, resArabic.Items)
	}

	// 5. ".*" matches literally only (finds usr-regex-literal, does not match usr-ar-1)
	recRegex := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?search="+url.QueryEscape(".*"), "internal-test-token", validTok, nil)
	if recRegex.Code != http.StatusOK {
		t.Fatalf("expected 200 searching regex literal, got %d (%s)", recRegex.Code, recRegex.Body.String())
	}
	var resRegex struct {
		Items []*models.UserDTO `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(recRegex.Body.Bytes(), &resRegex); err != nil {
		t.Fatalf("unmarshal regex search: %v", err)
	}
	if resRegex.Total != 1 || len(resRegex.Items) != 1 || resRegex.Items[0].ID != "usr-regex-literal" {
		t.Fatalf("expected '.*' to match only 'usr-regex-literal', got total=%d items=%+v", resRegex.Total, resRegex.Items)
	}
}

func TestAdmin_Suspend_Flow_And_R7(t *testing.T) {
	s, validTok, _ := setupAdminTestEnv(t, nil)
	ctx := context.Background()

	// 1. Create active user with password
	pw := "SecretPassword123"
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	u := &models.User{
		ID:            "u-suspend-r7",
		Email:         "student-suspend@example.com",
		PasswordHash:  string(hash),
		EmailVerified: true,
		Role:          models.RoleUser,
		Status:        models.StatusActive,
		CreatedAt:     time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	// Setup public router for testing login and refresh
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/auth/login", s.Login)
	publicMux.HandleFunc("/auth/refresh", s.Refresh)
	publicMux.HandleFunc("/auth/verify-otp", s.VerifyOTP)
	publicHandler := s.GatewayAuth(publicMux)

	// User can log in before suspension
	loginBody, _ := json.Marshal(map[string]string{
		"email":     "student-suspend@example.com",
		"password":  pw,
		"device_id": "11111111-1111-4111-8111-111111111111",
	})
	reqLogin := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	reqLogin.Header.Set("Content-Type", "application/json")
	reqLogin.Header.Set("X-Gateway-Secret", "gw-secret")
	recLogin := httptest.NewRecorder()
	publicHandler.ServeHTTP(recLogin, reqLogin)
	if recLogin.Code != http.StatusOK {
		t.Fatalf("login before suspend failed: %d (%s)", recLogin.Code, recLogin.Body.String())
	}
	var loginRes struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.Unmarshal(recLogin.Body.Bytes(), &loginRes)
	if loginRes.RefreshToken == "" {
		t.Fatalf("missing refresh token in login response")
	}

	// 2. Admin suspends user with reason containing CR/LF
	rawReason := "Terms of service violation:\r\nAbusive behavior in chat\nRepeated spam"
	suspendRec := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-suspend-r7/suspend", "internal-test-token", validTok, map[string]string{
		"reason": rawReason,
	})
	if suspendRec.Code != http.StatusOK {
		t.Fatalf("suspend user failed: %d (%s)", suspendRec.Code, suspendRec.Body.String())
	}

	// 3. Verify user in store
	gotUser, err := s.Store.FindByID(ctx, "u-suspend-r7")
	if err != nil || gotUser == nil {
		t.Fatalf("FindByID after suspend: %v", err)
	}
	if gotUser.EffectiveStatus() != models.StatusSuspended {
		t.Fatalf("expected status suspended, got %q", gotUser.EffectiveStatus())
	}
	if gotUser.SuspendedAt.IsZero() {
		t.Fatalf("expected SuspendedAt to be set")
	}
	// CR/LF must be stripped
	expectedReason := "Terms of service violation:  Abusive behavior in chat Repeated spam"
	if gotUser.StatusReason != expectedReason {
		t.Fatalf("expected stripped reason %q, got %q", expectedReason, gotUser.StatusReason)
	}

	// 4. Verify audit log recorded
	logs, total, err := s.Store.ListAuditLogs(ctx, 1, 10)
	if err != nil || total == 0 || len(logs) == 0 {
		t.Fatalf("audit log not found: %v", err)
	}
	audit := logs[0]
	if audit.Action != "account_suspend" || audit.TargetID != "u-suspend-r7" || audit.ActorID != "adm-001" {
		t.Fatalf("unexpected audit log: %+v", audit)
	}
	if audit.Detail != expectedReason {
		t.Fatalf("expected stripped audit detail %q, got %q", expectedReason, audit.Detail)
	}

	// 5. Verify jwtutil revocation
	if !jwtutil.IsUserRevoked("u-suspend-r7") {
		t.Fatalf("expected jwtutil.IsUserRevoked(u-suspend-r7) to be true")
	}

	// 6. R7 enforcement: Login, Refresh, VerifyOTP refused (401)
	// Login refused
	reqLogin2 := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	reqLogin2.Header.Set("Content-Type", "application/json")
	reqLogin2.Header.Set("X-Gateway-Secret", "gw-secret")
	recLogin2 := httptest.NewRecorder()
	publicHandler.ServeHTTP(recLogin2, reqLogin2)
	if recLogin2.Code != http.StatusUnauthorized {
		t.Fatalf("R7: expected login 401 after suspend, got %d (%s)", recLogin2.Code, recLogin2.Body.String())
	}

	// Refresh refused
	refBody, _ := json.Marshal(map[string]string{"refresh_token": loginRes.RefreshToken})
	reqRef := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(refBody))
	reqRef.Header.Set("Content-Type", "application/json")
	reqRef.Header.Set("X-Gateway-Secret", "gw-secret")
	recRef := httptest.NewRecorder()
	publicHandler.ServeHTTP(recRef, reqRef)
	if recRef.Code != http.StatusUnauthorized {
		t.Fatalf("R7: expected refresh 401 after suspend, got %d (%s)", recRef.Code, recRef.Body.String())
	}

	// VerifyOTP refused
	otpBody, _ := json.Marshal(map[string]string{
		"email":     "student-suspend@example.com",
		"code":      "123456",
		"device_id": "11111111-1111-4111-8111-111111111111",
	})
	reqOTP := httptest.NewRequest(http.MethodPost, "/auth/verify-otp", bytes.NewReader(otpBody))
	reqOTP.Header.Set("Content-Type", "application/json")
	reqOTP.Header.Set("X-Gateway-Secret", "gw-secret")
	recOTP := httptest.NewRecorder()
	publicHandler.ServeHTTP(recOTP, reqOTP)
	if recOTP.Code != http.StatusUnauthorized {
		t.Fatalf("R7: expected verify-otp 401 after suspend, got %d (%s)", recOTP.Code, recOTP.Body.String())
	}

	// 7. Suspend twice -> 409 Conflict
	suspendRec2 := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-suspend-r7/suspend", "internal-test-token", validTok, map[string]string{
		"reason": "second suspend attempt",
	})
	if suspendRec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 when suspending already suspended account, got %d (%s)", suspendRec2.Code, suspendRec2.Body.String())
	}
}

func TestAdmin_Reactivate_Flow(t *testing.T) {
	s, validTok, _ := setupAdminTestEnv(t, nil)
	ctx := context.Background()

	u := &models.User{
		ID:        "u-reactivate-test",
		Email:     "student-react@example.com",
		Status:    models.StatusSuspended,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	// 1. Reactivate suspended account -> 200
	rec := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-reactivate-test/reactivate", "internal-test-token", validTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reactivate user failed: %d (%s)", rec.Code, rec.Body.String())
	}

	gotUser, err := s.Store.FindByID(ctx, "u-reactivate-test")
	if err != nil || gotUser == nil {
		t.Fatalf("FindByID after reactivate: %v", err)
	}
	if gotUser.EffectiveStatus() != models.StatusActive {
		t.Fatalf("expected status active, got %q", gotUser.EffectiveStatus())
	}
	if gotUser.ReactivatedAt.IsZero() {
		t.Fatalf("expected ReactivatedAt to be set")
	}

	// 2. Verify audit log entry
	logs, _, err := s.Store.ListAuditLogs(ctx, 1, 10)
	if err != nil || len(logs) == 0 {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if logs[0].Action != "account_reactivate" || logs[0].TargetID != "u-reactivate-test" {
		t.Fatalf("unexpected audit entry: %+v", logs[0])
	}

	// 3. Reactivate already active account -> 409 Conflict
	rec2 := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-reactivate-test/reactivate", "internal-test-token", validTok, nil)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 when reactivating already active account, got %d (%s)", rec2.Code, rec2.Body.String())
	}
}

func TestAdmin_Delete_Flow_And_Blocklist_SignupRefusal(t *testing.T) {
	s, validTok, _ := setupAdminTestEnv(t, nil)
	ctx := context.Background()

	email := "abusive-user@example.com"
	phone := "+201088776655"
	u := &models.User{
		ID:        "u-del-test",
		FullName:  "Abusive User",
		Email:     email,
		Phone:     phone,
		Status:    models.StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	// Setup public router for signup testing
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/auth/signup", s.Signup)
	publicHandler := s.GatewayAuth(publicMux)

	// 1. Delete account via admin listener
	delRec := doAdminRequest(s, http.MethodDelete, "/internal/admin/accounts/u-del-test", "internal-test-token", validTok, map[string]string{
		"reason": "Permanent ban for repeated fraud",
	})
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete user failed: %d (%s)", delRec.Code, delRec.Body.String())
	}

	// 2. Verify user in store is deleted
	gotUser, err := s.Store.FindByID(ctx, "u-del-test")
	if err != nil || gotUser == nil {
		t.Fatalf("FindByID after delete: %v", err)
	}
	if gotUser.EffectiveStatus() != models.StatusDeleted {
		t.Fatalf("expected status deleted, got %q", gotUser.EffectiveStatus())
	}
	if gotUser.DeletedAt.IsZero() {
		t.Fatalf("expected DeletedAt to be set")
	}

	// 3. Verify jwtutil revocation
	if !jwtutil.IsUserRevoked("u-del-test") {
		t.Fatalf("expected jwtutil.IsUserRevoked(u-del-test) to be true")
	}

	// 4. Verify audit log entry
	logs, _, err := s.Store.ListAuditLogs(ctx, 1, 10)
	if err != nil || len(logs) == 0 {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if logs[0].Action != "account_delete" || logs[0].TargetID != "u-del-test" {
		t.Fatalf("unexpected audit entry: %+v", logs[0])
	}

	// 5. Reactivate deleted account -> 409 Conflict
	reactRec := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-del-test/reactivate", "internal-test-token", validTok, nil)
	if reactRec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when reactivating deleted account, got %d (%s)", reactRec.Code, reactRec.Body.String())
	}

	// 6. Suspend deleted account -> 409 Conflict
	suspRec := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-del-test/suspend", "internal-test-token", validTok, map[string]string{
		"reason": "cannot suspend deleted",
	})
	if suspRec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when suspending deleted account, got %d (%s)", suspRec.Code, suspRec.Body.String())
	}

	// 7. Delete already deleted account -> 409 Conflict
	delRec2 := doAdminRequest(s, http.MethodDelete, "/internal/admin/accounts/u-del-test", "internal-test-token", validTok, map[string]string{
		"reason": "repeat delete",
	})
	if delRec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 when deleting already deleted account, got %d (%s)", delRec2.Code, delRec2.Body.String())
	}

	// 8. Blocklist entries check
	bKey := s.blocklistKey()
	emailHash := computeHMAC(bKey, email)
	phoneHash := computeHMAC(bKey, phone)

	emailBlocked, err := s.Store.IsBlocked(ctx, "email", emailHash)
	if err != nil || !emailBlocked {
		t.Fatalf("expected email hash to be blocked, got blocked=%v, err=%v", emailBlocked, err)
	}
	phoneBlocked, err := s.Store.IsBlocked(ctx, "phone", phoneHash)
	if err != nil || !phoneBlocked {
		t.Fatalf("expected phone hash to be blocked, got blocked=%v, err=%v", phoneBlocked, err)
	}

	// 9. Attempt signup with blocked email -> 409 Conflict ("unable to complete registration")
	signupEmailPayload, _ := json.Marshal(map[string]string{
		"full_name": "New Identity",
		"email":     email,
		"phone":     "+201099990000",
		"password":  "ValidPassword123!",
	})
	reqSignup1 := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(signupEmailPayload))
	reqSignup1.Header.Set("Content-Type", "application/json")
	reqSignup1.Header.Set("X-Gateway-Secret", "gw-secret")
	recSignup1 := httptest.NewRecorder()
	publicHandler.ServeHTTP(recSignup1, reqSignup1)
	if recSignup1.Code != http.StatusConflict {
		t.Fatalf("expected 409 when registering with blocked email, got %d (%s)", recSignup1.Code, recSignup1.Body.String())
	}

	// 10. Attempt signup with blocked phone -> 409 Conflict ("unable to complete registration")
	signupPhonePayload, _ := json.Marshal(map[string]string{
		"full_name": "New Identity 2",
		"email":     "fresh-email-unrelated@example.com",
		"phone":     phone,
		"password":  "ValidPassword123!",
	})
	reqSignup2 := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(signupPhonePayload))
	reqSignup2.Header.Set("Content-Type", "application/json")
	reqSignup2.Header.Set("X-Gateway-Secret", "gw-secret")
	recSignup2 := httptest.NewRecorder()
	publicHandler.ServeHTTP(recSignup2, reqSignup2)
	if recSignup2.Code != http.StatusConflict {
		t.Fatalf("expected 409 when registering with blocked phone, got %d (%s)", recSignup2.Code, recSignup2.Body.String())
	}
}

func TestAdmin_Delete_EmailNormalization_BlocklistSignupRefusal(t *testing.T) {
	s, validTok, _ := setupAdminTestEnv(t, nil)
	ctx := context.Background()

	// Create user with email " Ahmed@Mail.COM " via the store directly
	rawUser := &models.User{
		ID:        "u-raw-unnormalized",
		FullName:  "Ahmed Unnormalized",
		Email:     " Ahmed@Mail.COM ",
		Phone:     "+201099887711",
		Status:    models.StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, rawUser); err != nil {
		t.Fatalf("Create user with unnormalized email directly in store: %v", err)
	}

	// Setup public router for signup testing
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/auth/signup", s.Signup)
	publicHandler := s.GatewayAuth(publicMux)

	// Delete account via admin listener
	delRec := doAdminRequest(s, http.MethodDelete, "/internal/admin/accounts/u-raw-unnormalized", "internal-test-token", validTok, map[string]string{
		"reason": "testing unnormalized email deletion",
	})
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete user failed: %d (%s)", delRec.Code, delRec.Body.String())
	}

	// Attempt signup with "ahmed@mail.com" -> refused with generic refusal
	signupPayload, _ := json.Marshal(map[string]string{
		"full_name": "Ahmed New",
		"email":     "ahmed@mail.com",
		"phone":     "+201055443322",
		"password":  "ValidPassword123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(signupPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gateway-Secret", "gw-secret")
	rec := httptest.NewRecorder()
	publicHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict when signing up with ahmed@mail.com after deleting ' Ahmed@Mail.COM ', got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err == nil {
		if resp["error"] != "unable to complete registration" {
			t.Fatalf("expected generic refusal message 'unable to complete registration', got %v", resp["error"])
		}
	}
}

func TestAdmin_AuditLog_Listing(t *testing.T) {
	s, validTok, _ := setupAdminTestEnv(t, nil)
	ctx := context.Background()

	// Seed 3 audit entries with different timestamps
	t0 := time.Now().UTC().Add(-2 * time.Hour)
	t1 := time.Now().UTC().Add(-1 * time.Hour)
	t2 := time.Now().UTC()

	entries := []*models.AuditLog{
		{
			ID:         "log-1",
			ActorID:    "adm-001",
			ActorName:  "Alice Operator",
			Action:     "account_suspend",
			TargetType: "user",
			TargetID:   "u-1",
			Detail:     "violating terms",
			CreatedAt:  t0,
		},
		{
			ID:         "log-2",
			ActorID:    "adm-001",
			ActorName:  "Alice Operator",
			Action:     "account_reactivate",
			TargetType: "user",
			TargetID:   "u-1",
			CreatedAt:  t1,
		},
		{
			ID:         "log-3",
			ActorID:    "adm-001",
			ActorName:  "Alice Operator",
			Action:     "account_delete",
			TargetType: "user",
			TargetID:   "u-2",
			Detail:     "fraudulent",
			CreatedAt:  t2,
		},
	}
	for _, e := range entries {
		if err := s.Store.CreateAuditLog(ctx, e); err != nil {
			t.Fatalf("CreateAuditLog: %v", err)
		}
	}

	// 1. Get audit log: newest first (log-3, log-2, log-1)
	rec := doAdminRequest(s, http.MethodGet, "/internal/admin/audit-log?limit=2&page=1", "internal-test-token", validTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var res struct {
		Items []*models.AuditLog `json:"items"`
		Total int                `json:"total"`
		Page  int                `json:"page"`
		Limit int                `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Total != 3 || len(res.Items) != 2 {
		t.Fatalf("expected total=3, items=2, got total=%d items=%d", res.Total, len(res.Items))
	}
	if res.Items[0].Action != "account_delete" || res.Items[1].Action != "account_reactivate" {
		t.Fatalf("expected newest first order, got %s then %s", res.Items[0].Action, res.Items[1].Action)
	}

	// 2. Page 2
	rec2 := doAdminRequest(s, http.MethodGet, "/internal/admin/audit-log?limit=2&page=2", "internal-test-token", validTok, nil)
	var res2 struct {
		Items []*models.AuditLog `json:"items"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &res2)
	if len(res2.Items) != 1 || res2.Items[0].Action != "account_suspend" {
		t.Fatalf("expected page 2 item to be account_suspend, got %+v", res2.Items)
	}

	// 3. Limit capped to 100
	recCap := doAdminRequest(s, http.MethodGet, "/internal/admin/audit-log?limit=500", "internal-test-token", validTok, nil)
	var resCap struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(recCap.Body.Bytes(), &resCap)
	if resCap.Limit != 100 {
		t.Fatalf("expected limit capped to 100, got %d", resCap.Limit)
	}

	// 4. Verify schema matches SPEC Section 5 (no IP in audit log JSON)
	var rawBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rawBody)
	itemsRaw := rawBody["items"].([]any)
	for _, item := range itemsRaw {
		itemMap := item.(map[string]any)
		if _, hasIP := itemMap["ip"]; hasIP {
			t.Fatalf("audit log leaks IP address: %+v", itemMap)
		}
		if _, hasIP := itemMap["ip_address"]; hasIP {
			t.Fatalf("audit log leaks ip_address: %+v", itemMap)
		}
	}
}

func TestAdmin_ReasonValidation(t *testing.T) {
	s, validTok, _ := setupAdminTestEnv(t, nil)
	ctx := context.Background()

	u := &models.User{
		ID:        "u-reason-test",
		Email:     "reason@example.com",
		Status:    models.StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	// 1. Suspend: Empty reason -> 400
	recEmpty := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-reason-test/suspend", "internal-test-token", validTok, map[string]string{
		"reason": "   ",
	})
	if recEmpty.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty reason on suspend, got %d (%s)", recEmpty.Code, recEmpty.Body.String())
	}

	// 2. Suspend: Reason > 1000 runes -> 400
	longReason := strings.Repeat("A", 1001)
	recLong := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-reason-test/suspend", "internal-test-token", validTok, map[string]string{
		"reason": longReason,
	})
	if recLong.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >1000 reason on suspend, got %d (%s)", recLong.Code, recLong.Body.String())
	}

	// 3. Delete: Empty reason -> 400
	recDelEmpty := doAdminRequest(s, http.MethodDelete, "/internal/admin/accounts/u-reason-test", "internal-test-token", validTok, map[string]string{
		"reason": "",
	})
	if recDelEmpty.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty reason on delete, got %d (%s)", recDelEmpty.Code, recDelEmpty.Body.String())
	}

	// 4. Delete: Reason > 1000 runes -> 400
	recDelLong := doAdminRequest(s, http.MethodDelete, "/internal/admin/accounts/u-reason-test", "internal-test-token", validTok, map[string]string{
		"reason": longReason,
	})
	if recDelLong.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >1000 reason on delete, got %d (%s)", recDelLong.Code, recDelLong.Body.String())
	}
}

type failingAuditStore struct {
	store.Store
}

func (f *failingAuditStore) CreateAuditLog(ctx context.Context, entry *models.AuditLog) error {
	return errors.New("audit log write failure")
}

func TestAdmin_AuditWriteFailure_Resiliency(t *testing.T) {
	memStore := store.NewMemoryStore()
	auditFailingStore := &failingAuditStore{Store: memStore}
	s, validTok, _ := setupAdminTestEnv(t, auditFailingStore)
	ctx := context.Background()

	u := &models.User{
		ID:        "u-audit-fail",
		Email:     "audit-fail@example.com",
		Status:    models.StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := memStore.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	// Suspend with failing audit store must still succeed (200) and update user status
	rec := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-audit-fail/suspend", "internal-test-token", validTok, map[string]string{
		"reason": "valid suspension reason",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 despite audit failure, got %d (%s)", rec.Code, rec.Body.String())
	}

	gotUser, err := memStore.FindByID(ctx, "u-audit-fail")
	if err != nil || gotUser == nil || gotUser.EffectiveStatus() != models.StatusSuspended {
		t.Fatalf("expected user to be suspended in store despite audit log failure, got: %+v", gotUser)
	}
}

func TestAdmin_StudentNotification_BestEffort(t *testing.T) {
	s, validTok, _ := setupAdminTestEnv(t, nil)
	ctx := context.Background()

	// Point NotifyURL to an HTTP server that returns 500 Internal Server Error
	mockNotifyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "notification service down", http.StatusInternalServerError)
	}))
	defer mockNotifyServer.Close()

	s.NotifyURL = mockNotifyServer.URL
	s.NotifyToken = "internal-test-token"

	u := &models.User{
		ID:        "u-notify-test",
		Email:     "notify-test@example.com",
		Status:    models.StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	// Suspend user: should succeed despite notification service 500 error
	rec := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-notify-test/suspend", "internal-test-token", validTok, map[string]string{
		"reason": "notification test reason",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 despite notification failure, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Reactivate user: should succeed despite notification service 500 error
	recReactivate := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-notify-test/reactivate", "internal-test-token", validTok, nil)
	if recReactivate.Code != http.StatusOK {
		t.Fatalf("expected 200 despite notification failure, got %d (%s)", recReactivate.Code, recReactivate.Body.String())
	}

	// Delete user: should succeed despite notification service 500 error
	recDelete := doAdminRequest(s, http.MethodDelete, "/internal/admin/accounts/u-notify-test", "internal-test-token", validTok, map[string]string{
		"reason": "notification test reason delete",
	})
	if recDelete.Code != http.StatusOK {
		t.Fatalf("expected 200 despite notification failure, got %d (%s)", recDelete.Code, recDelete.Body.String())
	}
}

func TestAdmin_StoreAndRedisDown_Returns503(t *testing.T) {
	// 1. Store is down -> all endpoints return 503
	fStore := &failingStore{err: errors.New("database cluster connection lost")}
	s := newAdminTestServer(fStore, nil)
	validTok := "some-admin-token"

	recAccounts := doAdminRequest(s, http.MethodGet, "/internal/admin/accounts", "internal-test-token", validTok, nil)
	if recAccounts.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for accounts when store down, got %d (%s)", recAccounts.Code, recAccounts.Body.String())
	}

	recAudit := doAdminRequest(s, http.MethodGet, "/internal/admin/audit-log", "internal-test-token", validTok, nil)
	if recAudit.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for audit-log when store down, got %d (%s)", recAudit.Code, recAudit.Body.String())
	}

	recSuspend := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-1/suspend", "internal-test-token", validTok, map[string]string{
		"reason": "test reason",
	})
	if recSuspend.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for suspend when store down, got %d (%s)", recSuspend.Code, recSuspend.Body.String())
	}

	recReactivate := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/u-1/reactivate", "internal-test-token", validTok, nil)
	if recReactivate.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for reactivate when store down, got %d (%s)", recReactivate.Code, recReactivate.Body.String())
	}

	recDelete := doAdminRequest(s, http.MethodDelete, "/internal/admin/accounts/u-1", "internal-test-token", validTok, map[string]string{
		"reason": "test reason",
	})
	if recDelete.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for delete when store down, got %d (%s)", recDelete.Code, recDelete.Body.String())
	}

	// 2. Redis is down on suspend and delete -> 503
	sGoodStore, validTok2, _ := setupAdminTestEnv(t, nil)
	u := &models.User{
		ID:        "u-redis-fail",
		Email:     "redisfail@example.com",
		Status:    models.StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	_ = sGoodStore.Store.Create(context.Background(), u)

	// Set nil redis client to simulate redis failure
	jwtutil.SetRedisClient(nil)

	recRedisSusp := doAdminRequest(sGoodStore, http.MethodPost, "/internal/admin/accounts/u-redis-fail/suspend", "internal-test-token", validTok2, map[string]string{
		"reason": "test reason",
	})
	if recRedisSusp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for suspend when redis down, got %d (%s)", recRedisSusp.Code, recRedisSusp.Body.String())
	}

	recRedisDel := doAdminRequest(sGoodStore, http.MethodDelete, "/internal/admin/accounts/u-redis-fail", "internal-test-token", validTok2, map[string]string{
		"reason": "test reason",
	})
	if recRedisDel.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for delete when redis down, got %d (%s)", recRedisDel.Code, recRedisDel.Body.String())
	}
}

func TestAdmin_UnknownID_Returns404(t *testing.T) {
	s, validTok, _ := setupAdminTestEnv(t, nil)

	recSusp := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/nonexistent-user-id/suspend", "internal-test-token", validTok, map[string]string{
		"reason": "test reason",
	})
	if recSusp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown user on suspend, got %d (%s)", recSusp.Code, recSusp.Body.String())
	}

	recReactivate := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/nonexistent-user-id/reactivate", "internal-test-token", validTok, nil)
	if recReactivate.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown user on reactivate, got %d (%s)", recReactivate.Code, recReactivate.Body.String())
	}

	recDelete := doAdminRequest(s, http.MethodDelete, "/internal/admin/accounts/nonexistent-user-id", "internal-test-token", validTok, map[string]string{
		"reason": "test reason",
	})
	if recDelete.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown user on delete, got %d (%s)", recDelete.Code, recDelete.Body.String())
	}
}
