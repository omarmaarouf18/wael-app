package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func TestAdminSubjects_DeleteDraft(t *testing.T) {
	s, dir := newFileAdminServer(t, 1<<20)
	ctx := context.Background()
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	addStoreVideo(t, s, subj.ID, "vid-del-1")
	file := uploadOK(t, s, subj.ID, pdfBytes(3000))
	if objs := storedObjects(t, dir); len(objs) != 1 {
		t.Fatalf("stored objects before delete = %v", objs)
	}

	rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/"+subj.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	if got, err := s.Store.GetSubjectByID(ctx, subj.ID); err != nil || got != nil {
		t.Fatalf("subject still stored: %+v %v", got, err)
	}
	if got, err := s.Store.GetVideoByID(ctx, "vid-del-1"); err != nil || got != nil {
		t.Fatalf("video still stored: %+v %v", got, err)
	}
	if got, err := s.Store.GetFile(ctx, subj.ID, file.ID); err != nil || got != nil {
		t.Fatalf("file row still stored: %+v %v", got, err)
	}
	if objs := storedObjects(t, dir); len(objs) != 0 {
		t.Fatalf("stored objects after delete = %v, want none", objs)
	}

	logs, _, err := s.Store.ListAuditLogs(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	found := false
	for _, l := range logs {
		if l.Action == "subject_delete" && l.TargetType == "subject" && l.TargetID == subj.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("no subject_delete audit row")
	}

	again := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/"+subj.ID, nil)
	if again.Code != http.StatusNotFound || adminCode(t, again) != "subject_not_found" {
		t.Fatalf("second delete: expected 404 subject_not_found, got %d %q", again.Code, adminCode(t, again))
	}
}

func TestAdminSubjects_DeleteRefusals(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("published_409", func(t *testing.T) {
		subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
		addStoreVideo(t, s, subj.ID, "vid-del-pub")
		if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/publish", nil); rec.Code != http.StatusOK {
			t.Fatalf("publish: %d", rec.Code)
		}
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/"+subj.ID, nil)
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "subject_published" {
			t.Fatalf("expected 409 subject_published, got %d %q", rec.Code, adminCode(t, rec))
		}
		if got, _ := s.Store.GetSubjectByID(ctx, subj.ID); got == nil {
			t.Fatal("published subject was deleted")
		}
	})

	cases := []struct {
		name string
		seed func(t *testing.T, subjectID string)
	}{
		{"revoked_entitlement", func(t *testing.T, subjectID string) {
			revoked := now.Add(-time.Hour)
			e := &models.Entitlement{ID: "ent-" + subjectID, UserID: "student-1", SubjectID: subjectID,
				ExpiresAt: now.Add(24 * time.Hour), GrantedAt: now.Add(-48 * time.Hour), Source: models.EntitlementSourceRequest,
				GrantedBy: "admin", RevokedAt: &revoked, RevokedBy: "admin", RevokeReason: "test"}
			if err := s.Store.Grant(ctx, e); err != nil {
				t.Fatalf("Grant: %v", err)
			}
		}},
		{"rejected_request", func(t *testing.T, subjectID string) {
			pr := &models.PurchaseRequest{ID: "req-" + subjectID, UserID: "student-2", SubjectID: subjectID,
				Status: models.RequestStatusPending, CreatedAt: now}
			if _, _, err := s.Store.CreateOrGetPendingRequest(ctx, pr); err != nil {
				t.Fatalf("CreateOrGetPendingRequest: %v", err)
			}
			decided := now
			if ok, err := s.Store.DecideRequest(ctx, pr.ID, models.RequestStatusPending, models.RequestStatusRejected, "admin", decided, "no"); err != nil || !ok {
				t.Fatalf("DecideRequest: %v %v", ok, err)
			}
		}},
		{"payment_record", func(t *testing.T, subjectID string) {
			rec := &models.PaymentRecord{ID: "pay-" + subjectID, UserID: "student-3", SubjectID: subjectID,
				EntitlementID: "ent-x", Amount: 1800, PriceAtGrant: 1800, Source: "admin_grant", RecordedBy: "admin", RecordedAt: now}
			if err := s.Store.CreatePaymentRecord(ctx, rec); err != nil {
				t.Fatalf("CreatePaymentRecord: %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"_409", func(t *testing.T) {
			subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
			tc.seed(t, subj.ID)
			rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/"+subj.ID, nil)
			if rec.Code != http.StatusConflict || adminCode(t, rec) != "subject_has_students" {
				t.Fatalf("expected 409 subject_has_students, got %d %q", rec.Code, adminCode(t, rec))
			}
			if got, _ := s.Store.GetSubjectByID(ctx, subj.ID); got == nil {
				t.Fatal("referenced subject was deleted")
			}
		})
	}

	t.Run("unknown_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/subj-missing-1", nil)
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
			t.Fatalf("expected 404 subject_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("invalid_id_400", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/bad%20id!", nil)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_subject_id" {
			t.Fatalf("expected 400 invalid_subject_id, got %d %q", rec.Code, adminCode(t, rec))
		}
	})
}
