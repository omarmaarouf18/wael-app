package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

func TestAdminEntitlements_List(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().Add(48 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 1500, future)

	ctx := context.Background()
	ent1 := &models.Entitlement{
		ID:        "ent-1",
		UserID:    "user-list-ents",
		SubjectID: subj.ID,
		ExpiresAt: future,
		GrantedAt: time.Now().UTC(),
		Source:    models.EntitlementSourceAdminGrant,
		GrantedBy: "adm-1",
		Active:    true,
	}
	if err := s.Store.Grant(ctx, ent1); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	t.Run("missing_or_invalid_user_id_400", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/entitlements", nil)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_user_id" {
			t.Fatalf("expected 400 invalid_user_id, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("returns_student_entitlements", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/entitlements?user_id=user-list-ents", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []EntitlementAdminDTO `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp.Items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(resp.Items))
		}
		item := resp.Items[0]
		if item.ID != "ent-1" || item.SubjectID != subj.ID || item.SubjectTitleAr != "مادة الاختبار" {
			t.Fatalf("item mismatch: %+v", item)
		}
		if item.IsRevoked || item.IsExpired || !item.Active {
			t.Fatalf("status flags wrong: %+v", item)
		}
	})
}

func TestAdminEntitlements_Grant(t *testing.T) {
	var notifyCalls atomic.Int64
	nsrv := fakeNotifyServer(t, &notifyCalls, false)
	defer nsrv.Close()

	s, _ := newAdminTestServer(t, okVerify)
	s.NotifyURL = nsrv.URL
	s.NotifyToken = "test-notify-token"

	future := time.Now().Add(48 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 1750, future)

	t.Run("invalid_user_id_400", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/entitlements", map[string]any{
			"user_id":    "",
			"subject_id": subj.ID,
		})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_user_id" {
			t.Fatalf("expected 400 invalid_user_id, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("unknown_subject_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/entitlements", map[string]any{
			"user_id":    "user-grant-1",
			"subject_id": "subj-missing",
		})
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
			t.Fatalf("expected 404 subject_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("subject_expired_409", func(t *testing.T) {
		past := time.Now().Add(-2 * time.Hour).UTC()
		expSubj := createTestSubject(t, s, "bachelor-y1", 1000, past)
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/entitlements", map[string]any{
			"user_id":    "user-grant-2",
			"subject_id": expSubj.ID,
		})
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "subject_expired" {
			t.Fatalf("expected 409 subject_expired, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("successful_grant", func(t *testing.T) {
		notifyCalls.Store(0)
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/entitlements", map[string]any{
			"user_id":    "user-grant-success",
			"subject_id": subj.ID,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
		}
		var dto EntitlementAdminDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if dto.UserID != "user-grant-success" || dto.SubjectID != subj.ID || dto.Source != models.EntitlementSourceAdminGrant {
			t.Fatalf("dto mismatch: %+v", dto)
		}

		ctx := context.Background()
		// Active entitlement in store
		ent, err := s.Store.GetActiveEntitlement(ctx, "user-grant-success", subj.ID)
		if err != nil || ent == nil {
			t.Fatalf("active entitlement not found: %v", err)
		}
		if ent.GrantedBy != "adm-1" {
			t.Fatalf("granted_by = %q, want adm-1", ent.GrantedBy)
		}

		// Payment record created with amount=price and source=admin_grant
		records, err := s.Store.ListPaymentRecordsByUser(ctx, "user-grant-success")
		if err != nil || len(records) != 1 {
			t.Fatalf("expected 1 payment record, got %d (%v)", len(records), err)
		}
		pr := records[0]
		if pr.Amount != 1750 || pr.PriceAtGrant != 1750 || pr.Source != models.PaymentSourceAdminGrant || pr.RecordedBy != "adm-1" {
			t.Fatalf("payment record mismatch: %+v", pr)
		}

		// Audit log written
		logs, _, err := s.Store.ListAuditLogs(ctx, 1, 10)
		if err != nil {
			t.Fatalf("ListAuditLogs: %v", err)
		}
		found := false
		for _, l := range logs {
			if l.Action == "entitlement_grant" && l.TargetID == ent.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("audit log for entitlement_grant not found")
		}

		// Notification sent
		if notifyCalls.Load() != 1 {
			t.Fatalf("expected 1 notify call, got %d", notifyCalls.Load())
		}
	})

	t.Run("already_owned_409", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/entitlements", map[string]any{
			"user_id":    "user-grant-success",
			"subject_id": subj.ID,
		})
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "already_owned" {
			t.Fatalf("expected 409 already_owned, got %d %q", rec.Code, adminCode(t, rec))
		}
	})
}

func TestAdminEntitlements_Revoke(t *testing.T) {
	var notifyCalls atomic.Int64
	nsrv := fakeNotifyServer(t, &notifyCalls, false)
	defer nsrv.Close()

	s, _ := newAdminTestServer(t, okVerify)
	s.NotifyURL = nsrv.URL
	s.NotifyToken = "test-notify-token"

	future := time.Now().Add(48 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 2000, future)

	ctx := context.Background()
	ent := &models.Entitlement{
		ID:        "ent-to-revoke-1",
		UserID:    "user-revoke-1",
		SubjectID: subj.ID,
		ExpiresAt: future,
		GrantedAt: time.Now().UTC(),
		Source:    models.EntitlementSourceAdminGrant,
		GrantedBy: "adm-1",
		Active:    true,
	}
	if err := s.Store.Grant(ctx, ent); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	// Create a payment record to verify it is NOT modified or deleted
	payRec := &models.PaymentRecord{
		ID:            "pay-rec-1",
		UserID:        "user-revoke-1",
		SubjectID:     subj.ID,
		EntitlementID: ent.ID,
		Amount:        2000,
		PriceAtGrant:  2000,
		Source:        models.PaymentSourceAdminGrant,
		RecordedBy:    "adm-1",
		RecordedAt:    time.Now().UTC(),
	}
	if err := s.Store.CreatePaymentRecord(ctx, payRec); err != nil {
		t.Fatalf("CreatePaymentRecord: %v", err)
	}

	t.Run("reason_validation", func(t *testing.T) {
		for _, reason := range []string{"", "   ", strings.Repeat("a", 1001)} {
			rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/entitlements/"+ent.ID, map[string]any{
				"reason": reason,
			})
			if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_reason" {
				t.Fatalf("reason %q: expected 400 invalid_reason, got %d %q", reason, rec.Code, adminCode(t, rec))
			}
		}
	})

	t.Run("unknown_entitlement_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/entitlements/ent-unknown", map[string]any{
			"reason": "استرداد المبلغ",
		})
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "entitlement_not_found" {
			t.Fatalf("expected 404 entitlement_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("successful_revoke", func(t *testing.T) {
		notifyCalls.Store(0)
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/entitlements/"+ent.ID, map[string]any{
			"reason": "تم إلغاء الاشتراك بناء على طلب الطالب",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}

		// 1. Entitlement is revoked in store
		updatedEnt, err := s.Store.GetEntitlementByID(ctx, ent.ID)
		if err != nil || updatedEnt == nil {
			t.Fatalf("GetEntitlementByID: %v", err)
		}
		if !updatedEnt.IsRevoked() || updatedEnt.Active {
			t.Fatalf("expected revoked and inactive: %+v", updatedEnt)
		}
		if updatedEnt.RevokedBy != "adm-1" || updatedEnt.RevokeReason != "تم إلغاء الاشتراك بناء على طلب الطالب" {
			t.Fatalf("revocation metadata mismatch: %+v", updatedEnt)
		}

		// 2. Active entitlement query returns nil
		active, _ := s.Store.GetActiveEntitlement(ctx, "user-revoke-1", subj.ID)
		if active != nil {
			t.Fatalf("expected no active entitlement, got %+v", active)
		}

		// 3. Payment record untouched (not edited, not deleted)
		records, err := s.Store.ListPaymentRecordsByUser(ctx, "user-revoke-1")
		if err != nil || len(records) != 1 {
			t.Fatalf("expected 1 payment record preserved, got %d (%v)", len(records), err)
		}
		if records[0].ID != "pay-rec-1" || records[0].Amount != 2000 {
			t.Fatalf("payment record was corrupted: %+v", records[0])
		}

		// 4. Audit log written
		logs, _, err := s.Store.ListAuditLogs(ctx, 1, 10)
		if err != nil {
			t.Fatalf("ListAuditLogs: %v", err)
		}
		found := false
		for _, l := range logs {
			if l.Action == "entitlement_revoke" && l.TargetID == ent.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("audit log for entitlement_revoke not found")
		}

		// 5. Notification sent
		if notifyCalls.Load() != 1 {
			t.Fatalf("expected 1 notify call, got %d", notifyCalls.Load())
		}
	})
}

func TestAdminEntitlements_RevocationImpactOnR1(t *testing.T) {
	jwtutil.Init("test-jwt-secret")
	s, _ := newAdminTestServer(t, okVerify)
	s.GatewaySecret = "test-gateway-secret"
	future := time.Now().Add(48 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 2000, future)

	// Add a video to the subject
	ctx := context.Background()
	video := &models.Video{
		ID:              "vid-r1-test",
		SubjectID:       subj.ID,
		TitleAr:         "فيديو الدرس الأول",
		TitleEn:         "Lesson 1 Video",
		YouTubeVideoID:  "12345678901",
		DurationSeconds: 600,
		Position:        1,
		Published:       true,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := s.Store.CreateVideo(ctx, video); err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}

	userID := "user-r1-impact"
	tok := makeStudentToken(t, userID)

	// Step 1: Grant entitlement
	grantRec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/entitlements", map[string]any{
		"user_id":    userID,
		"subject_id": subj.ID,
	})
	if grantRec.Code != http.StatusCreated {
		t.Fatalf("grant failed: %d (%s)", grantRec.Code, grantRec.Body.String())
	}
	var grantDTO EntitlementAdminDTO
	_ = json.Unmarshal(grantRec.Body.Bytes(), &grantDTO)

	doStudentReq := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		rec := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(rec, req)
		return rec
	}

	// Verify student owns subject initially
	// A. List subjects: owned=true
	recList := doStudentReq(http.MethodGet, "/academy/subjects?level=bachelor-y1&term=first")
	if recList.Code != http.StatusOK {
		t.Fatalf("list subjects: %d", recList.Code)
	}
	var listResp struct {
		Items []models.SubjectListItemDTO `json:"items"`
	}
	_ = json.Unmarshal(recList.Body.Bytes(), &listResp)
	if len(listResp.Items) != 1 || !listResp.Items[0].Owned {
		t.Fatalf("expected owned=true in list, got %+v", listResp.Items)
	}

	// B. Subject detail: owned=true, video playable=true
	recDetail := doStudentReq(http.MethodGet, "/academy/subjects/"+subj.ID)
	if recDetail.Code != http.StatusOK {
		t.Fatalf("subject detail: %d", recDetail.Code)
	}
	var detailResp models.SubjectDetailDTO
	_ = json.Unmarshal(recDetail.Body.Bytes(), &detailResp)
	if !detailResp.Owned {
		t.Fatalf("expected owned=true in detail")
	}

	// C. Play video: returns 200 with youtube id
	recPlay := doStudentReq(http.MethodPost, "/academy/videos/"+video.ID+"/play")
	if recPlay.Code != http.StatusOK {
		t.Fatalf("play video: %d (%s)", recPlay.Code, recPlay.Body.String())
	}

	// D. My entitlements: contains subj.ID
	recMyEnts := doStudentReq(http.MethodGet, "/academy/me/entitlements")
	if recMyEnts.Code != http.StatusOK {
		t.Fatalf("my entitlements: %d (%s)", recMyEnts.Code, recMyEnts.Body.String())
	}
	var myEntsResp struct {
		SubjectIDs []string `json:"subject_ids"`
	}
	_ = json.Unmarshal(recMyEnts.Body.Bytes(), &myEntsResp)
	if len(myEntsResp.SubjectIDs) != 1 || myEntsResp.SubjectIDs[0] != subj.ID {
		t.Fatalf("my entitlements wrong: %+v", myEntsResp.SubjectIDs)
	}

	// Step 2: Revoke entitlement
	recRevoke := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/entitlements/"+grantDTO.ID, map[string]any{
		"reason": "استرداد المصاريف",
	})
	if recRevoke.Code != http.StatusOK {
		t.Fatalf("revoke failed: %d (%s)", recRevoke.Code, recRevoke.Body.String())
	}

	// Verify student does NOT own subject after revocation
	// A. List subjects: owned=false
	recListRev := doStudentReq(http.MethodGet, "/academy/subjects?level=bachelor-y1&term=first")
	if recListRev.Code != http.StatusOK {
		t.Fatalf("list subjects: %d", recListRev.Code)
	}
	_ = json.Unmarshal(recListRev.Body.Bytes(), &listResp)
	if len(listResp.Items) != 1 || listResp.Items[0].Owned {
		t.Fatalf("expected owned=false after revocation, got %+v", listResp.Items)
	}

	// B. Subject detail: owned=false, video playable=false
	recDetailRev := doStudentReq(http.MethodGet, "/academy/subjects/"+subj.ID)
	if recDetailRev.Code != http.StatusOK {
		t.Fatalf("subject detail: %d", recDetailRev.Code)
	}
	_ = json.Unmarshal(recDetailRev.Body.Bytes(), &detailResp)
	if detailResp.Owned {
		t.Fatalf("expected owned=false after revocation in detail")
	}

	// C. Play video: returns 404 (non-owners cannot play)
	recPlayRev := doStudentReq(http.MethodPost, "/academy/videos/"+video.ID+"/play")
	if recPlayRev.Code != http.StatusNotFound {
		t.Fatalf("expected 404 play video after revocation, got %d (%s)", recPlayRev.Code, recPlayRev.Body.String())
	}

	// D. My entitlements: empty
	recMyEntsRev := doStudentReq(http.MethodGet, "/academy/me/entitlements")
	if recMyEntsRev.Code != http.StatusOK {
		t.Fatalf("my entitlements: %d (%s)", recMyEntsRev.Code, recMyEntsRev.Body.String())
	}
	var myEntsRevResp struct {
		SubjectIDs []string `json:"subject_ids"`
	}
	_ = json.Unmarshal(recMyEntsRev.Body.Bytes(), &myEntsRevResp)
	if len(myEntsRevResp.SubjectIDs) != 0 {
		t.Fatalf("expected 0 subject_ids after revocation, got %+v", myEntsRevResp.SubjectIDs)
	}

	// Step 3: Re-grant subject
	recRegrant := doAdminJSON(t, s, http.MethodPost, "/internal/admin/entitlements", map[string]any{
		"user_id":    userID,
		"subject_id": subj.ID,
	})
	if recRegrant.Code != http.StatusCreated {
		t.Fatalf("re-grant failed: %d (%s)", recRegrant.Code, recRegrant.Body.String())
	}

	// Play video works again
	recPlayAgain := doStudentReq(http.MethodPost, "/academy/videos/"+video.ID+"/play")
	if recPlayAgain.Code != http.StatusOK {
		t.Fatalf("expected 200 play video after re-grant, got %d", recPlayAgain.Code)
	}

	// My entitlements populated again
	recMyEntsAgain := doStudentReq(http.MethodGet, "/academy/me/entitlements")
	_ = json.Unmarshal(recMyEntsAgain.Body.Bytes(), &myEntsResp)
	if len(myEntsResp.SubjectIDs) != 1 || myEntsResp.SubjectIDs[0] != subj.ID {
		t.Fatalf("my entitlements should have 1 item after re-grant: %+v", myEntsResp.SubjectIDs)
	}
}
