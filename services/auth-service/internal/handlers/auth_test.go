package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/mailer"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
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
	doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Lock User",
		"email":     "lock@example.com",
		"phone":     "+201012345680",
		"password":  "password123",
	}, "")
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

	// Register user 1
	rec1 := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "User One",
		"email":     "user1@example.com",
		"phone":     "+201012345678",
		"password":  "password123",
	}, "")
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first signup status = %d (%s)", rec1.Code, rec1.Body.String())
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
			lockout.mu.Lock()
			f := lockout.data["login:email:status_suspended@example.com"]
			var count int
			if f != nil {
				count = f.count
			}
			lockout.mu.Unlock()
			if count != 1 {
				t.Fatalf("expected failure count 1, got %d", count)
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
	if err != nil || val != u.ID {
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
	consumeErr error
}

func (f *failingCodesStore) ConsumeWithAttempts(ctx context.Context, key, hash string, maxAttempts int, ttl time.Duration) (bool, error) {
	if f.consumeErr != nil {
		return false, f.consumeErr
	}
	return f.Store.ConsumeWithAttempts(ctx, key, hash, maxAttempts, ttl)
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

	rec := doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{
		"email": "redisdown@example.com",
		"code":  "123456",
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
