package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
)

// phoneSpyStore records phone lookups and blocklist writes so the tests can
// prove that an account without a phone never reaches FindByPhone with "",
// a phone blocklist check, or a phone blocklist entry.
type phoneSpyStore struct {
	store.Store
	mu             sync.Mutex
	emptyPhoneFind int
	phoneChecks    int
	added          []string
}

func (p *phoneSpyStore) FindByPhone(ctx context.Context, phone string) (*models.User, error) {
	if phone == "" {
		p.mu.Lock()
		p.emptyPhoneFind++
		p.mu.Unlock()
	}
	return p.Store.FindByPhone(ctx, phone)
}

func (p *phoneSpyStore) IsBlocked(ctx context.Context, kind, hash string) (bool, error) {
	if kind == "phone" {
		p.mu.Lock()
		p.phoneChecks++
		p.mu.Unlock()
	}
	return p.Store.IsBlocked(ctx, kind, hash)
}

func (p *phoneSpyStore) AddToBlocklist(ctx context.Context, kind, hash, reason string, at time.Time) error {
	p.mu.Lock()
	p.added = append(p.added, kind)
	p.mu.Unlock()
	return p.Store.AddToBlocklist(ctx, kind, hash, reason, at)
}

func decodeAny(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return out
}

// signupNoPhone signs up and verifies an account without a phone (the
// "phone" key absent, or blank when phone is non-nil) and returns its access
// token.
func signupNoPhone(t *testing.T, s *Server, email string, phone *string) string {
	t.Helper()
	body := map[string]string{"full_name": "No Phone User", "email": email, "password": "Password123!"}
	if phone != nil {
		body["phone"] = *phone
	}
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", body, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup %s = %d (%s)", email, rec.Code, rec.Body.String())
	}
	signup := decodeAny(t, rec.Body.Bytes())
	if v, ok := signup["phone"]; !ok || v != nil {
		t.Fatalf("signup response phone = %v (present %v), want null", v, ok)
	}
	code, _ := signup["dev_otp"].(string)
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": email, "code": code}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify %s = %d (%s)", email, rec.Code, rec.Body.String())
	}
	tok, _ := decodeAny(t, rec.Body.Bytes())["access_token"].(string)
	if tok == "" {
		t.Fatalf("verify %s returned no access token", email)
	}
	return tok
}

func TestSignup_WithoutPhone(t *testing.T) {
	s := testServer()
	spy := &phoneSpyStore{Store: s.Store}
	s.Store = spy

	access := signupNoPhone(t, s, "nophone1@example.com", nil)
	blank := "   "
	signupNoPhone(t, s, "nophone2@example.com", &blank)

	// Two verified accounts without a phone coexist; /auth/me says null.
	rec := doRequest(t, s, http.MethodGet, "/auth/me", nil, access)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d (%s)", rec.Code, rec.Body.String())
	}
	me := decodeAny(t, rec.Body.Bytes())
	if v, ok := me["phone"]; !ok || v != nil {
		t.Fatalf("me phone = %v (present %v), want null", v, ok)
	}
	if spy.emptyPhoneFind != 0 || spy.phoneChecks != 0 {
		t.Fatalf("no-phone signup reached FindByPhone(\"\") %d times and the phone blocklist %d times, want 0",
			spy.emptyPhoneFind, spy.phoneChecks)
	}
	u, err := s.Store.FindByEmail(context.Background(), "nophone2@example.com")
	if err != nil || u == nil || u.Phone != "" {
		t.Fatalf("stored no-phone account = %+v, %v", u, err)
	}
}

func TestSignup_WithPhoneKeepsRules(t *testing.T) {
	s := testServer()

	// A given phone is still normalized (default region EG) and returned.
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "With Phone", "email": "withphone@example.com", "phone": "01012345930", "password": "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup with phone = %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["phone"] != "+201012345930" {
		t.Fatalf("signup phone = %q, want +201012345930", body["phone"])
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "withphone@example.com", "code": body["dev_otp"]}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify = %d (%s)", rec.Code, rec.Body.String())
	}
	// A typed but invalid phone is still refused.
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Bad Phone", "email": "badphone@example.com", "phone": "12", "password": "password123",
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid phone = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	// A duplicate phone is still refused with the generic 409.
	rec = doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Dup Phone", "email": "dupphone@example.com", "phone": "+201012345930", "password": "password123",
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate phone = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

func TestSignup_ReplaceUnverifiedDropsPhone(t *testing.T) {
	s := testServer()
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "First Try", "email": "replacenp@example.com", "phone": "+201012345931", "password": "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("first signup = %d (%s)", rec.Code, rec.Body.String())
	}
	signupNoPhone(t, s, "replacenp@example.com", nil)
	u, err := s.Store.FindByEmail(context.Background(), "replacenp@example.com")
	if err != nil || u == nil || u.Phone != "" {
		t.Fatalf("replaced account = %+v, %v; want no phone", u, err)
	}
}

func TestAccount_AddPhoneLater(t *testing.T) {
	s, _, cleanup := accountTestServer(t)
	defer cleanup()

	access := signupNoPhone(t, s, "addphone@example.com", nil)
	signupVerifyTokens(t, s, "holder@example.com", "+201012345940", "Password123!", devB)

	// A name change first: the phone has its own limit, and adding is exempt.
	rec := doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"full_name": "Renamed", "current_password": "Password123!",
	}, access)
	if rec.Code != http.StatusOK {
		t.Fatalf("name change = %d (%s)", rec.Code, rec.Body.String())
	}
	// A phone held by another verified account: generic 409.
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"phone": "+201012345940", "current_password": "Password123!",
	}, access)
	if rec.Code != http.StatusConflict {
		t.Fatalf("taken phone = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// A blocklisted phone: generic 409.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := s.Store.AddToBlocklist(ctx, "phone", computeHMAC(s.BlocklistHMACKey, "+201012345941"), "test", time.Now().UTC())
	cancel()
	if err != nil {
		t.Fatalf("add blocklist: %v", err)
	}
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"phone": "+201012345941", "current_password": "Password123!",
	}, access)
	if rec.Code != http.StatusConflict {
		t.Fatalf("blocklisted phone = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// Adding a free phone works at once.
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"phone": "01012345942", "current_password": "Password123!",
	}, access)
	if rec.Code != http.StatusOK {
		t.Fatalf("add phone = %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeAny(t, rec.Body.Bytes())["phone"]; got != "+201012345942" {
		t.Fatalf("add phone response phone = %v", got)
	}
	rec = doRequest(t, s, http.MethodGet, "/auth/me", nil, access)
	if got := decodeAny(t, rec.Body.Bytes())["phone"]; got != "+201012345942" {
		t.Fatalf("me phone after add = %v", got)
	}
	// Changing the phone now keeps the 30-day rule.
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"phone": "+201012345943", "current_password": "Password123!",
	}, access)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("change right after add = %d, want 429 (%s)", rec.Code, rec.Body.String())
	}
	s.Clock = func() time.Time { return time.Now().UTC().Add(31 * 24 * time.Hour) }
	defer func() { s.Clock = nil }()
	rec = doRequest(t, s, http.MethodPatch, "/auth/me", map[string]string{
		"phone": "+201012345943", "current_password": "Password123!",
	}, access)
	if rec.Code != http.StatusOK {
		t.Fatalf("change after 31 days = %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAdmin_Delete_WithoutPhone(t *testing.T) {
	spy := &phoneSpyStore{Store: store.NewMemoryStore()}
	s, validTok, _ := setupAdminTestEnv(t, spy)
	ctx := context.Background()
	u := &models.User{
		ID: "u-del-nophone", FullName: "No Phone", Email: "delnophone@example.com",
		EmailVerified: true, Status: models.StatusActive, CreatedAt: time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("create: %v", err)
	}
	rec := doAdminRequest(s, http.MethodDelete, "/internal/admin/accounts/u-del-nophone", "internal-test-token", validTok, map[string]string{
		"reason": "test ban without phone",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d (%s)", rec.Code, rec.Body.String())
	}
	if len(spy.added) != 1 || spy.added[0] != "email" {
		t.Fatalf("blocklist writes = %v, want [email] only", spy.added)
	}
	got, err := s.Store.FindByID(ctx, "u-del-nophone")
	if err != nil || got == nil || got.EffectiveStatus() != models.StatusDeleted {
		t.Fatalf("after delete: %+v, %v", got, err)
	}
	// The admin accounts view shows an empty phone, which the console renders as "—".
	rec = doAdminRequest(s, http.MethodGet, "/internal/admin/accounts?status=deleted", "internal-test-token", validTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list accounts = %d (%s)", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("list accounts body = %s (%v)", rec.Body.String(), err)
	}
	if list.Items[0]["phone"] != "" {
		t.Fatalf("admin account phone = %v, want empty", list.Items[0]["phone"])
	}
}
