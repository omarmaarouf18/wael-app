package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
)

// fakeVerifyServer runs a stub auth-service POST /internal/admin/verify.
func fakeVerifyServer(t *testing.T, fn func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/admin/verify" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		fn(w, r)
	}))
}

func okVerify(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"admin_id":"adm-1","name":"Test Operator"}`))
}

func unauthorizedVerify(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized","code":"unauthorized"}`))
}

func lockedOutVerify(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"error":"too many attempts, retry later","code":"locked_out"}`))
}

// newAdminTestServer builds a Server whose admin auth verifies against fn.
func newAdminTestServer(t *testing.T, fn func(w http.ResponseWriter, r *http.Request)) (*Server, *httptest.Server) {
	t.Helper()
	vsrv := fakeVerifyServer(t, fn)
	t.Cleanup(vsrv.Close)
	st := store.NewMemoryStore()
	_ = st.SeedLevels(context.Background())
	s := New(st, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", false, "+201000000000")
	s.AuthAdminURL = vsrv.URL
	s.VerifyClient = vsrv.Client()
	return s, vsrv
}

func doAdmin(t *testing.T, s *Server, method, path, internalTok, adminTok, clientIP string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if internalTok != "" {
		req.Header.Set("X-Internal-Token", internalTok)
	}
	if adminTok != "" {
		req.Header.Set("X-Admin-Token", adminTok)
	}
	if clientIP != "" {
		req.Header.Set("X-Admin-Client-IP", clientIP)
	}
	rec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(rec, req)
	return rec
}

func TestAdminAuth_MiddlewareMatrix(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	t.Run("missing_internal_token_401", func(t *testing.T) {
		rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "", "some-admin-token", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("wrong_internal_token_401", func(t *testing.T) {
		rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "wrong-token", "some-admin-token", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing_admin_token_401", func(t *testing.T) {
		rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "test-internal-token", "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid_tokens_200", func(t *testing.T) {
		rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "test-internal-token", "some-admin-token", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
	})
}

func TestAdminAuth_BadAdminToken401(t *testing.T) {
	s, _ := newAdminTestServer(t, unauthorizedVerify)

	rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "test-internal-token", "bogus-token", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["code"] != "unauthorized" {
		t.Fatalf("expected code unauthorized, got %v", body["code"])
	}
}

func TestAdminAuth_LockedOut429(t *testing.T) {
	s, _ := newAdminTestServer(t, lockedOutVerify)

	rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "test-internal-token", "rate-limited-token", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["code"] != "locked_out" {
		t.Fatalf("expected code locked_out, got %v", body["code"])
	}
}

func TestAdminAuth_AuthDown503(t *testing.T) {
	st := store.NewMemoryStore()
	_ = st.SeedLevels(context.Background())
	s := New(st, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", false, "+201000000000")
	// Point at a closed port: connection refused must fail closed.
	s.AuthAdminURL = "http://127.0.0.1:1"
	s.VerifyClient = &http.Client{Timeout: time.Second}

	req := httptest.NewRequest(http.MethodGet, "/internal/admin/audit-log", nil)
	req.Header.Set("X-Internal-Token", "test-internal-token")
	req.Header.Set("X-Admin-Token", "some-admin-token")
	rec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAdminAuth_ForwardsTokensAndClientIP(t *testing.T) {
	var gotInternal, gotAdmin, gotIP string
	s, _ := newAdminTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotInternal = r.Header.Get("X-Internal-Token")
		gotAdmin = r.Header.Get("X-Admin-Token")
		gotIP = r.Header.Get("X-Admin-Client-IP")
		okVerify(w, r)
	})

	rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "test-internal-token", "operator-token-xyz", "203.0.113.7")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if gotInternal != "test-internal-token" {
		t.Errorf("verify saw X-Internal-Token %q", gotInternal)
	}
	if gotAdmin != "operator-token-xyz" {
		t.Errorf("verify saw X-Admin-Token %q", gotAdmin)
	}
	if gotIP != "203.0.113.7" {
		t.Errorf("verify saw X-Admin-Client-IP %q, want 203.0.113.7", gotIP)
	}
}

func TestAdminAuth_NoCaching(t *testing.T) {
	calls := 0
	s, _ := newAdminTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		okVerify(w, r)
	})

	for i := 0; i < 3; i++ {
		rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "test-internal-token", "same-token", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}
	if calls != 3 {
		t.Fatalf("expected 3 verify calls (no caching), got %d", calls)
	}
}

func TestAdminAuditLog_PaginationAndShape(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()

	t0 := time.Now().UTC().Add(-2 * time.Hour)
	t1 := time.Now().UTC().Add(-1 * time.Hour)
	t2 := time.Now().UTC()
	for i, e := range []*models.AuditLog{
		{ID: "log-1", ActorID: "adm-1", ActorName: "Test Operator", Action: "level_create", TargetType: "level", TargetID: "diploma-x", CreatedAt: t0},
		{ID: "log-2", ActorID: "adm-1", ActorName: "Test Operator", Action: "subject_publish", TargetType: "subject", TargetID: "subj-1", CreatedAt: t1},
		{ID: "log-3", ActorID: "adm-1", ActorName: "Test Operator", Action: "video_delete", TargetType: "video", TargetID: "vid-1", CreatedAt: t2},
	} {
		if err := s.Store.CreateAuditLog(ctx, e); err != nil {
			t.Fatalf("seed log %d: %v", i, err)
		}
	}

	rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log?limit=2&page=1", "test-internal-token", "tok", "")
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
		t.Fatalf("decode: %v", err)
	}
	if res.Total != 3 || len(res.Items) != 2 {
		t.Fatalf("expected total=3 items=2, got total=%d items=%d", res.Total, len(res.Items))
	}
	// Newest first.
	if res.Items[0].ID != "log-3" || res.Items[1].ID != "log-2" {
		t.Fatalf("expected newest-first order, got %s then %s", res.Items[0].ID, res.Items[1].ID)
	}

	// Page 2 carries the oldest entry.
	rec2 := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log?limit=2&page=2", "test-internal-token", "tok", "")
	var res2 struct {
		Items []*models.AuditLog `json:"items"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &res2)
	if len(res2.Items) != 1 || res2.Items[0].ID != "log-1" {
		t.Fatalf("page 2 = %+v, want [log-1]", res2.Items)
	}

	// Limit capped at 100.
	recCap := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log?limit=500", "test-internal-token", "tok", "")
	var resCap struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(recCap.Body.Bytes(), &resCap)
	if resCap.Limit != 100 {
		t.Fatalf("expected limit capped to 100, got %d", resCap.Limit)
	}

	// Same response shape as auth (exactly items/total/page/limit), no IP.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if len(raw) != 4 {
		t.Fatalf("audit-log response keys = %v, want exactly items/total/page/limit", keysOf(raw))
	}
	for _, k := range []string{"items", "total", "page", "limit"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("audit-log response missing %q", k)
		}
	}
	lower := strings.ToLower(rec.Body.String())
	for _, banned := range []string{`"ip"`, `"ip_address"`, `"client_ip"`} {
		if strings.Contains(lower, banned) {
			t.Fatalf("audit log must not persist IP addresses, body contains %s", banned)
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// failingAuditStore fails audit writes to prove mutations fail closed.
type failingAuditStore struct {
	store.Store
}

func (f *failingAuditStore) CreateAuditLog(_ context.Context, _ *models.AuditLog) error {
	return errors.New("audit log write failure")
}

func TestAdminAudit_WriteFailureBestEffort(t *testing.T) {
	mem := store.NewMemoryStore()
	s := New(&failingAuditStore{Store: mem}, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", false, "+201000000000")
	adm := &AdminIdentity{ID: "adm-1", Name: "Test Operator"}
	// The helper still reports the failure to its caller.
	if err := s.writeAdminAudit(context.Background(), adm, "level_create", "level", "diploma-x", "detail"); err == nil {
		t.Fatal("expected writeAdminAudit to report the store failure, got nil")
	}

	// But the admin call itself succeeds: the mutation is applied and the
	// failure is logged, mirroring auth-service account actions.
	srv, _ := newAdminTestServer(t, okVerify)
	srv.Store = &failingAuditStore{Store: srv.Store}
	rec := doAdminJSON(t, srv, http.MethodPost, "/internal/admin/levels", map[string]any{
		"study_type": "diploma", "name_ar": "دبلومة رغم تعطل السجل", "order": 6,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 despite audit failure, got %d (%s)", rec.Code, rec.Body.String())
	}
	var dto LevelAdminDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, err := srv.Store.GetLevelByKey(context.Background(), dto.Key)
	if err != nil || got == nil {
		t.Fatalf("expected the diploma to be created despite audit failure, got %+v err=%v", got, err)
	}
	logs, total, err := srv.Store.ListAuditLogs(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if total != 0 || len(logs) != 0 {
		t.Fatalf("expected no audit rows after write failure, got total=%d", total)
	}

	var okCount int
	okStore := store.NewMemoryStore()
	s2 := New(okStore, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", false, "+201000000000")
	if err := s2.writeAdminAudit(context.Background(), adm, "level_create", "level", "diploma-x", "line1\r\nline2"); err != nil {
		t.Fatalf("writeAdminAudit: %v", err)
	}
	logs2, total2, err := okStore.ListAuditLogs(context.Background(), 1, 10)
	if err != nil || total2 != 1 {
		t.Fatalf("expected 1 audit row, got total=%d err=%v", total2, err)
	}
	okCount = len(logs2)
	if okCount != 1 {
		t.Fatalf("expected 1 audit row, got %d", okCount)
	}
	if logs2[0].ActorID != "adm-1" || logs2[0].ActorName != "Test Operator" {
		t.Fatalf("audit actor = %+v", logs2[0])
	}
	if strings.Contains(logs2[0].Detail, "\r") || strings.Contains(logs2[0].Detail, "\n") {
		t.Fatalf("audit detail must strip CR/LF, got %q", logs2[0].Detail)
	}
}
