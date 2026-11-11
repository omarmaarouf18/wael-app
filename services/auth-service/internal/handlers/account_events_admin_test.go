package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func TestAccountEvents_AdminEndpoint_ExistsAndSecured(t *testing.T) {
	st := store.NewMemoryStore()
	s, validTok, revokedTok := setupAdminTestEnv(t, st)

	// 1. Requires internal token
	recNoInternal := doAdminRequest(s, http.MethodGet, "/internal/admin/account-events", "", validTok, nil)
	if recNoInternal.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when internal token missing, got %d", recNoInternal.Code)
	}

	// 2. Requires valid admin token
	recNoAdmin := doAdminRequest(s, http.MethodGet, "/internal/admin/account-events", "internal-test-token", "", nil)
	if recNoAdmin.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when admin token missing, got %d", recNoAdmin.Code)
	}

	recRevoked := doAdminRequest(s, http.MethodGet, "/internal/admin/account-events", "internal-test-token", revokedTok, nil)
	if recRevoked.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when admin token revoked, got %d", recRevoked.Code)
	}

	// 3. On valid admin token, must succeed (200 OK)
	recValid := doAdminRequest(s, http.MethodGet, "/internal/admin/account-events", "internal-test-token", validTok, nil)
	if recValid.Code != http.StatusOK {
		t.Fatalf("expected 200 on admin handler for /internal/admin/account-events, got %d (body: %s)", recValid.Code, recValid.Body.String())
	}
}

func TestAccountEvents_LifecycleEvents_Recorded_Filtered_NoPII(t *testing.T) {
	st := store.NewMemoryStore()
	s, validTok, _ := setupAdminTestEnv(t, st)

	now := time.Now().UTC()
	hash, _ := bcrypt.GenerateFromPassword([]byte("StrongPassword123!"), bcrypt.DefaultCost)
	rawEmail := "student.vip@example.com"
	rawPhone := "+201099998888"
	studentUser := &models.User{
		ID:            "stu-uuid-0001",
		FullName:      "VIP Student",
		Email:         rawEmail,
		Phone:         rawPhone,
		PasswordHash:  string(hash),
		Status:        models.StatusActive,
		EmailVerified: true,
		CreatedAt:     now.Add(-24 * time.Hour),
	}
	if err := st.Create(context.Background(), studentUser); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 1. Student login (records "login")
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"student.vip@example.com","password":"StrongPassword123!","device_id":"550e8400-e29b-41d4-a716-446655440001","device_label":"iPhone 15"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	s.Login(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %d (%s)", loginRec.Code, loginRec.Body.String())
	}

	var loginResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil || loginResp.AccessToken == "" {
		t.Fatalf("bad login response: %v", err)
	}

	// 2. Student logout (records "logout")
	logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	logoutRec := httptest.NewRecorder()
	s.Logout(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("logout failed: %d", logoutRec.Code)
	}

	// 3. Admin suspends student (records "account_suspended")
	suspendRec := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/stu-uuid-0001/suspend", "internal-test-token", validTok, map[string]string{
		"reason": "Policy violation",
	})
	if suspendRec.Code != http.StatusOK {
		t.Fatalf("suspend failed: %d (%s)", suspendRec.Code, suspendRec.Body.String())
	}

	// 4. Admin reactivates student (records "account_reactivated")
	reactivateRec := doAdminRequest(s, http.MethodPost, "/internal/admin/accounts/stu-uuid-0001/reactivate", "internal-test-token", validTok, nil)
	if reactivateRec.Code != http.StatusOK {
		t.Fatalf("reactivate failed: %d (%s)", reactivateRec.Code, reactivateRec.Body.String())
	}

	// 5. Query account events via admin listener filterable by student
	rec := doAdminRequest(s, http.MethodGet, "/internal/admin/account-events?student="+rawEmail, "internal-test-token", validTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /internal/admin/account-events, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	respBody := rec.Body.String()
	// STRICT PII CHECK: The response must NEVER contain the raw unmasked email, raw phone, or password hash
	if strings.Contains(respBody, rawEmail) {
		t.Fatalf("PII LEAK: raw student email %q found in account-events response: %s", rawEmail, respBody)
	}
	if strings.Contains(respBody, rawPhone) {
		t.Fatalf("PII LEAK: raw student phone %q found in account-events response: %s", rawPhone, respBody)
	}
	if strings.Contains(respBody, "StrongPassword123!") || strings.Contains(respBody, "$2a$") {
		t.Fatalf("PII LEAK: password or hash found in account-events response: %s", respBody)
	}

	var parsed struct {
		Items []struct {
			ID          string `json:"id"`
			UserID      string `json:"user_id"`
			Student     string `json:"student"`
			Type        string `json:"type"`
			DeviceLabel string `json:"device_label"`
			ActorType   string `json:"actor_type"`
			ActorID     string `json:"actor_id"`
			ActorName   string `json:"actor_name"`
			Detail      string `json:"detail"`
		} `json:"items"`
		Total int `json:"total"`
		Page  int `json:"page"`
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	eventTypes := make(map[string]bool)
	for _, it := range parsed.Items {
		eventTypes[it.Type] = true
	}

	for _, expectedType := range []string{"login", "logout", "account_suspended", "account_reactivated"} {
		if !eventTypes[expectedType] {
			t.Errorf("missing expected account event type %q in returned events", expectedType)
		}
	}

	// 6. Pagination test
	pRec := doAdminRequest(s, http.MethodGet, "/internal/admin/account-events?page=1&limit=2", "internal-test-token", validTok, nil)
	if pRec.Code != http.StatusOK {
		t.Fatalf("pagination failed: %d", pRec.Code)
	}
	var pParsed struct {
		Items []any `json:"items"`
		Total int   `json:"total"`
		Page  int   `json:"page"`
		Limit int   `json:"limit"`
	}
	_ = json.Unmarshal(pRec.Body.Bytes(), &pParsed)
	if len(pParsed.Items) > 2 || pParsed.Limit != 2 {
		t.Errorf("expected at most 2 items with limit 2, got %d", len(pParsed.Items))
	}

	// 7. Verify admin audit log still works independently (another admin action still appears)
	auditRec := doAdminRequest(s, http.MethodGet, "/internal/admin/audit-log", "internal-test-token", validTok, nil)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /internal/admin/audit-log, got %d", auditRec.Code)
	}
	var auditParsed struct {
		Items []*models.AuditLog `json:"items"`
		Total int                `json:"total"`
	}
	_ = json.Unmarshal(auditRec.Body.Bytes(), &auditParsed)
	foundAdminAction := false
	for _, logEntry := range auditParsed.Items {
		if logEntry.Action == "account_suspend" {
			foundAdminAction = true
			break
		}
	}
	if !foundAdminAction {
		t.Errorf("expected account_suspend to appear in admin audit log")
	}
}

func TestAccountEvents_AllEventTypesRecorded(t *testing.T) {
	st := store.NewMemoryStore()
	s, validTok, _ := setupAdminTestEnv(t, st)

	now := time.Now().UTC()
	u := &models.User{
		ID:        "stu-uuid-0002",
		FullName:  "Test Student 2",
		Email:     "student2@example.com",
		Phone:     "+201012345678",
		Status:    models.StatusActive,
		CreatedAt: now.Add(-10 * time.Hour),
	}
	if err := st.Create(context.Background(), u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	allTypes := []string{
		models.AccountEventPasswordChanged,
		models.AccountEventProfileChanged,
		models.AccountEventEmailChangeSent,
		models.AccountEventEmailChanged,
		models.AccountEventDeletionRequested,
		models.AccountEventDeletionCancelled,
		models.AccountEventAccountPurged,
		models.AccountEventSessionRevoked,
		models.AccountEventLogin,
		models.AccountEventLogout,
		models.AccountEventSessionEndedCap,
		models.AccountEventAccountSuspended,
		models.AccountEventAccountReactivated,
		models.AccountEventAccountDeleted,
	}

	for i, evtType := range allTypes {
		evtTime := now.Add(time.Duration(i) * time.Minute)
		s.recordAccountEventDetailed(context.Background(), &models.AccountEvent{
			UserID:      u.ID,
			Type:        evtType,
			DeviceLabel: "Chrome / Linux",
			ActorType:   "student",
			ActorID:     u.ID,
			CreatedAt:   evtTime,
		})
	}
	s.evtWg.Wait()

	// 1. Query all events for student by phone
	recPhone := doAdminRequest(s, http.MethodGet, "/internal/admin/account-events?student=%2B201012345678&limit=100", "internal-test-token", validTok, nil)
	if recPhone.Code != http.StatusOK {
		t.Fatalf("search by phone failed: %d (%s)", recPhone.Code, recPhone.Body.String())
	}

	var parsedPhone struct {
		Items []struct {
			Type    string `json:"type"`
			Student string `json:"student"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(recPhone.Body.Bytes(), &parsedPhone); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsedPhone.Total != len(allTypes) {
		t.Fatalf("expected %d total events, got %d", len(allTypes), parsedPhone.Total)
	}

	foundMap := make(map[string]bool)
	for _, it := range parsedPhone.Items {
		foundMap[it.Type] = true
		if !strings.Contains(it.Student, "***@") {
			t.Errorf("expected masked student email, got %s", it.Student)
		}
	}
	for _, expected := range allTypes {
		if !foundMap[expected] {
			t.Errorf("missing event type %q in list", expected)
		}
	}

	// 2. Query with type filter
	recType := doAdminRequest(s, http.MethodGet, "/internal/admin/account-events?type=session_ended_cap", "internal-test-token", validTok, nil)
	if recType.Code != http.StatusOK {
		t.Fatalf("filter by type failed: %d", recType.Code)
	}
	var parsedType struct {
		Items []struct {
			Type string `json:"type"`
		} `json:"items"`
		Total int `json:"total"`
	}
	_ = json.Unmarshal(recType.Body.Bytes(), &parsedType)
	if parsedType.Total != 1 || len(parsedType.Items) != 1 || parsedType.Items[0].Type != "session_ended_cap" {
		t.Errorf("expected exactly 1 session_ended_cap event, got total=%d items=%d", parsedType.Total, len(parsedType.Items))
	}

	// 3. Query with date range
	fromStr := now.Add(2 * time.Minute).Format(time.RFC3339)
	toStr := now.Add(5 * time.Minute).Format(time.RFC3339)
	recDate := doAdminRequest(s, http.MethodGet, "/internal/admin/account-events?from="+fromStr+"&to="+toStr, "internal-test-token", validTok, nil)
	if recDate.Code != http.StatusOK {
		t.Fatalf("filter by date failed: %d", recDate.Code)
	}
	var parsedDate struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(recDate.Body.Bytes(), &parsedDate)
	if parsedDate.Total < 1 {
		t.Errorf("expected at least 1 event in date range, got %d", parsedDate.Total)
	}
}
