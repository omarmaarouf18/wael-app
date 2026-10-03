package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// fakeNotifyServer runs a stub notification-service POST /internal/push.
func fakeNotifyServer(t *testing.T, calls *atomic.Int64, fail bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/push" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if calls != nil {
			calls.Add(1)
		}
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal_error"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"queued"}`))
	}))
}

func createTestSubject(t *testing.T, s *Server, levelKey string, price int, expiresAt time.Time) *models.Subject {
	t.Helper()
	ctx := context.Background()
	subj := &models.Subject{
		ID:              fmt.Sprintf("subj-%d", time.Now().UnixNano()),
		LevelKey:        levelKey,
		Term:            "first",
		TitleAr:         "مادة الاختبار",
		TitleEn:         "Test Subject",
		Price:           price,
		AccessExpiresAt: expiresAt,
		Status:          models.StatusPublished,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := s.Store.CreateSubject(ctx, subj); err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	return subj
}

func createTestRequest(t *testing.T, s *Server, userID, subjectID string) *models.PurchaseRequest {
	t.Helper()
	ctx := context.Background()
	pr := &models.PurchaseRequest{
		ID:        fmt.Sprintf("req-%d", time.Now().UnixNano()),
		UserID:    userID,
		SubjectID: subjectID,
		Status:    models.RequestStatusPending,
		CreatedAt: time.Now().UTC(),
	}
	actual, _, err := s.Store.CreateOrGetPendingRequest(ctx, pr)
	if err != nil {
		t.Fatalf("CreateOrGetPendingRequest: %v", err)
	}
	return actual
}

func TestAdminRequests_List(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().Add(24 * time.Hour).UTC()
	subj1 := createTestSubject(t, s, "bachelor-y1", 1500, future)
	subj2 := createTestSubject(t, s, "bachelor-y2", 2000, future)

	r1 := createTestRequest(t, s, "user-1", subj1.ID)
	r2 := createTestRequest(t, s, "user-2", subj2.ID)
	_ = r1
	_ = r2

	t.Run("list_all_pending", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/requests", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var resp RequestsListResponseDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.PendingCount != 2 {
			t.Fatalf("pending_count = %d, want 2", resp.PendingCount)
		}
		if len(resp.Items) != 2 {
			t.Fatalf("items len = %d, want 2", len(resp.Items))
		}
	})

	t.Run("filter_by_subject", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/requests?subject_id="+subj1.ID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp RequestsListResponseDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Items) != 1 || resp.Items[0].SubjectID != subj1.ID {
			t.Fatalf("expected 1 item for subj1, got %d", len(resp.Items))
		}
		if resp.PendingCount != 2 {
			t.Fatalf("pending_count should remain total pending = 2, got %d", resp.PendingCount)
		}
	})

	t.Run("filter_invalid_status", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/requests?status=bogus", nil)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_status" {
			t.Fatalf("expected 400 invalid_status, got %d %q", rec.Code, adminCode(t, rec))
		}
	})
}

func TestAdminRequests_Accept_R4Matrix(t *testing.T) {
	var notifyCalls atomic.Int64
	nsrv := fakeNotifyServer(t, &notifyCalls, false)
	defer nsrv.Close()

	s, _ := newAdminTestServer(t, okVerify)
	s.NotifyURL = nsrv.URL
	s.NotifyToken = "test-notify-token"

	future := time.Now().Add(48 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 1200, future)

	t.Run("unknown_request_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/req-missing/accept", nil)
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "request_not_found" {
			t.Fatalf("expected 404 request_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("subject_expired_409", func(t *testing.T) {
		past := time.Now().Add(-1 * time.Hour).UTC()
		expSubj := createTestSubject(t, s, "bachelor-y1", 900, past)
		req := createTestRequest(t, s, "user-exp", expSubj.ID)

		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/accept", nil)
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "subject_expired" {
			t.Fatalf("expected 409 subject_expired, got %d %q", rec.Code, adminCode(t, rec))
		}
		// Verify no entitlement or payment record was created
		ctx := context.Background()
		owned, _ := s.Store.HasActiveEntitlement(ctx, "user-exp", expSubj.ID)
		if owned {
			t.Fatal("expected user not to own expired subject")
		}
		payments, _ := s.Store.ListPaymentRecordsByUser(ctx, "user-exp")
		if len(payments) != 0 {
			t.Fatalf("expected 0 payments, got %d", len(payments))
		}
	})

	t.Run("successful_accept_complete_flow", func(t *testing.T) {
		notifyCalls.Store(0)
		req := createTestRequest(t, s, "user-accept-1", subj.ID)

		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/accept", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var dto RequestAdminDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if dto.Status != models.RequestStatusAccepted {
			t.Fatalf("dto status = %q, want accepted", dto.Status)
		}
		if dto.Price != 1200 {
			t.Fatalf("dto price = %d, want 1200", dto.Price)
		}
		if dto.DecidedAt == nil {
			t.Fatal("expected decided_at to be populated")
		}

		ctx := context.Background()
		// 1. Request status in store
		updatedReq, err := s.Store.GetRequestByID(ctx, req.ID)
		if err != nil || updatedReq == nil || updatedReq.Status != models.RequestStatusAccepted {
			t.Fatalf("store request not accepted: %+v", updatedReq)
		}

		// 2. Active entitlement created
		ent, err := s.Store.GetActiveEntitlement(ctx, "user-accept-1", subj.ID)
		if err != nil || ent == nil {
			t.Fatalf("active entitlement not found: %v", err)
		}
		if ent.Source != models.EntitlementSourceRequest || ent.RequestID != req.ID {
			t.Fatalf("entitlement source/request_id mismatch: %+v", ent)
		}
		if ent.GrantedBy != "adm-1" {
			t.Fatalf("entitlement granted_by = %q, want adm-1", ent.GrantedBy)
		}

		// 3. Payment record created
		records, err := s.Store.ListPaymentRecordsByUser(ctx, "user-accept-1")
		if err != nil || len(records) != 1 {
			t.Fatalf("expected 1 payment record, got %d (%v)", len(records), err)
		}
		pr := records[0]
		if pr.Amount != 1200 || pr.PriceAtGrant != 1200 || pr.EntitlementID != ent.ID || pr.RequestID != req.ID {
			t.Fatalf("payment record mismatch: %+v", pr)
		}
		if pr.RecordedBy != "adm-1" {
			t.Fatalf("payment recorded_by = %q, want adm-1", pr.RecordedBy)
		}

		// 4. Audit log written
		logs, total, err := s.Store.ListAuditLogs(ctx, 1, 10)
		if err != nil || total == 0 {
			t.Fatalf("expected audit log entry: %v", err)
		}
		foundAudit := false
		for _, al := range logs {
			if al.Action == "request_accept" && al.TargetID == req.ID {
				foundAudit = true
				break
			}
		}
		if !foundAudit {
			t.Fatalf("audit log for request_accept %s not found", req.ID)
		}

		// 5. Notification called
		if notifyCalls.Load() != 1 {
			t.Fatalf("expected 1 notify call, got %d", notifyCalls.Load())
		}
	})

	t.Run("already_accepted_409", func(t *testing.T) {
		req := createTestRequest(t, s, "user-double-accept", subj.ID)
		rec1 := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/accept", nil)
		if rec1.Code != http.StatusOK {
			t.Fatalf("first accept: %d", rec1.Code)
		}
		rec2 := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/accept", nil)
		if rec2.Code != http.StatusConflict || adminCode(t, rec2) != "request_not_pending" {
			t.Fatalf("expected 409 request_not_pending, got %d %q", rec2.Code, adminCode(t, rec2))
		}
	})
}

func TestAdminRequests_Accept_PreexistingManualGrant(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().Add(48 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 1000, future)

	ctx := context.Background()
	// Student already has active manual grant
	manualEnt := &models.Entitlement{
		ID:        "ent-manual-1",
		UserID:    "user-manual",
		SubjectID: subj.ID,
		ExpiresAt: future,
		GrantedAt: time.Now().UTC(),
		Source:    models.EntitlementSourceAdminGrant,
		GrantedBy: "adm-1",
		Active:    true,
	}
	if err := s.Store.Grant(ctx, manualEnt); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	req := createTestRequest(t, s, "user-manual", subj.ID)

	rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/accept", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Active entitlement must be the same manual grant
	ent, _ := s.Store.GetActiveEntitlement(ctx, "user-manual", subj.ID)
	if ent == nil || ent.ID != "ent-manual-1" {
		t.Fatalf("expected manual grant ent-manual-1 to be reused, got %+v", ent)
	}

	// No duplicate payment record created
	records, _ := s.Store.ListPaymentRecordsByUser(ctx, "user-manual")
	if len(records) != 0 {
		t.Fatalf("expected 0 payment records for pre-existing manual grant, got %d", len(records))
	}
}

func TestAdminRequests_Accept_Concurrency(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().Add(48 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 1500, future)
	req := createTestRequest(t, s, "user-concurrent", subj.ID)

	const parallel = 10
	var wg sync.WaitGroup
	codes := make([]int, parallel)

	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/accept", nil)
			codes[idx] = rec.Code
		}(i)
	}
	wg.Wait()

	okCount := 0
	conflictCount := 0
	for _, c := range codes {
		if c == http.StatusOK {
			okCount++
		} else if c == http.StatusConflict {
			conflictCount++
		} else {
			t.Errorf("unexpected status code %d", c)
		}
	}
	if okCount != 1 {
		t.Fatalf("expected exactly 1 successful accept, got %d", okCount)
	}
	if conflictCount != parallel-1 {
		t.Fatalf("expected %d conflicts, got %d", parallel-1, conflictCount)
	}

	ctx := context.Background()
	records, err := s.Store.ListPaymentRecordsByUser(ctx, "user-concurrent")
	if err != nil || len(records) != 1 {
		t.Fatalf("expected exactly 1 payment record, got %d (%v)", len(records), err)
	}

	entList, err := s.Store.ListEntitlementsByUser(ctx, "user-concurrent")
	if err != nil || len(entList) != 1 {
		t.Fatalf("expected exactly 1 entitlement, got %d (%v)", len(entList), err)
	}
}

func TestAdminRequests_Reject(t *testing.T) {
	var notifyCalls atomic.Int64
	nsrv := fakeNotifyServer(t, &notifyCalls, false)
	defer nsrv.Close()

	s, _ := newAdminTestServer(t, okVerify)
	s.NotifyURL = nsrv.URL
	s.NotifyToken = "test-notify-token"

	future := time.Now().Add(24 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 1500, future)
	req := createTestRequest(t, s, "user-reject-1", subj.ID)

	t.Run("reason_validation", func(t *testing.T) {
		for _, reason := range []string{"", "   ", strings.Repeat("x", 1001)} {
			rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/reject", map[string]any{
				"reason": reason,
			})
			if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_reason" {
				t.Fatalf("reason %q: expected 400 invalid_reason, got %d %q", reason, rec.Code, adminCode(t, rec))
			}
		}
	})

	t.Run("successful_reject", func(t *testing.T) {
		notifyCalls.Store(0)
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/reject", map[string]any{
			"reason": "لم يتم تأكيد التحويل من إنستاباي",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}

		ctx := context.Background()
		r, err := s.Store.GetRequestByID(ctx, req.ID)
		if err != nil || r == nil || r.Status != models.RequestStatusRejected {
			t.Fatalf("request not marked rejected: %+v", r)
		}
		if r.RejectReason != "لم يتم تأكيد التحويل من إنستاباي" {
			t.Fatalf("reject_reason = %q", r.RejectReason)
		}
		if r.DecidedBy != "adm-1" || r.DecidedAt == nil {
			t.Fatalf("decided metadata missing: %+v", r)
		}

		if notifyCalls.Load() != 1 {
			t.Fatalf("expected 1 notify call, got %d", notifyCalls.Load())
		}

		// R1: Student does NOT own the subject
		owned, _ := s.Store.HasActiveEntitlement(ctx, "user-reject-1", subj.ID)
		if owned {
			t.Fatal("student owns subject after rejection")
		}
	})

	t.Run("reject_already_decided_409", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/reject", map[string]any{
			"reason": "already rejected",
		})
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "request_not_pending" {
			t.Fatalf("expected 409 request_not_pending, got %d %q", rec.Code, adminCode(t, rec))
		}
	})
}

func TestAdminRequests_NotificationBestEffort(t *testing.T) {
	// Failing notification server returns 500
	var notifyCalls atomic.Int64
	nsrv := fakeNotifyServer(t, &notifyCalls, true)
	defer nsrv.Close()

	s, _ := newAdminTestServer(t, okVerify)
	s.NotifyURL = nsrv.URL
	s.NotifyToken = "test-notify-token"

	future := time.Now().Add(24 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 1000, future)

	t.Run("accept_with_failing_notification_succeeds", func(t *testing.T) {
		req := createTestRequest(t, s, "user-fail-notify-1", subj.ID)
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/accept", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 despite notify failure, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("reject_with_failing_notification_succeeds", func(t *testing.T) {
		req := createTestRequest(t, s, "user-fail-notify-2", subj.ID)
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/"+req.ID+"/reject", map[string]any{
			"reason": "إلغاء الطلب",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 despite notify failure, got %d (%s)", rec.Code, rec.Body.String())
		}
	})
}
