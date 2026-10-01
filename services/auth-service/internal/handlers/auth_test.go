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
