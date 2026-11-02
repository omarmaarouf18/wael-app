package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func newDeleteTestSubject(t *testing.T, s Store, id, status string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	subj := &models.Subject{ID: id, LevelKey: "bachelor-y1", Term: "first", TitleAr: "مادة", Price: 100,
		Status: status, AccessExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := s.CreateSubject(context.Background(), subj); err != nil {
		t.Fatalf("CreateSubject %s: %v", id, err)
	}
}

func runDeleteDraftSubjectSuite(t *testing.T, s Store) {
	ctx := context.Background()
	now := time.Now().UTC()

	// A draft with a video and a file: deleted with its children.
	newDeleteTestSubject(t, s, "del-draft", models.StatusDraft)
	if err := s.CreateVideo(ctx, &models.Video{ID: "del-vid", SubjectID: "del-draft", Position: 1, TitleAr: "v",
		YouTubeVideoID: "dQw4w9WgXcQ", Published: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	if err := s.CreateFile(ctx, &models.SubjectFile{ID: "del-file", SubjectID: "del-draft", Kind: "note", TitleAr: "f",
		SizeBytes: 10, StorageKey: "key-1", CreatedAt: now}); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	files, err := s.DeleteDraftSubject(ctx, "del-draft")
	if err != nil {
		t.Fatalf("DeleteDraftSubject: %v", err)
	}
	if len(files) != 1 || files[0].StorageKey != "key-1" {
		t.Fatalf("returned files = %+v, want the one row with key-1", files)
	}
	if got, err := s.GetSubjectByID(ctx, "del-draft"); err != nil || got != nil {
		t.Fatalf("subject left: %+v %v", got, err)
	}
	if got, err := s.GetVideoByID(ctx, "del-vid"); err != nil || got != nil {
		t.Fatalf("video left: %+v %v", got, err)
	}
	if got, err := s.ListFilesBySubject(ctx, "del-draft"); err != nil || len(got) != 0 {
		t.Fatalf("file rows left: %+v %v", got, err)
	}
	if _, err := s.DeleteDraftSubject(ctx, "del-draft"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: err = %v, want ErrNotFound", err)
	}

	// Published: refused, nothing removed.
	newDeleteTestSubject(t, s, "del-pub", models.StatusPublished)
	if _, err := s.DeleteDraftSubject(ctx, "del-pub"); !errors.Is(err, ErrSubjectPublished) {
		t.Fatalf("published: err = %v, want ErrSubjectPublished", err)
	}
	if got, _ := s.GetSubjectByID(ctx, "del-pub"); got == nil {
		t.Fatal("published subject removed")
	}

	// Any reference, in any state, refuses the delete.
	newDeleteTestSubject(t, s, "del-ent", models.StatusDraft)
	if err := s.Grant(ctx, &models.Entitlement{ID: "del-ent-1", UserID: "u1", SubjectID: "del-ent", GrantedBy: "a", Active: true}); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if _, err := s.RevokeEntitlement(ctx, "del-ent-1", "a", "r", now); err != nil {
		t.Fatalf("RevokeEntitlement: %v", err)
	}
	newDeleteTestSubject(t, s, "del-req", models.StatusDraft)
	if _, _, err := s.CreateOrGetPendingRequest(ctx, &models.PurchaseRequest{ID: "del-req-1", UserID: "u2", SubjectID: "del-req",
		Status: models.RequestStatusPending, CreatedAt: now}); err != nil {
		t.Fatalf("CreateOrGetPendingRequest: %v", err)
	}
	newDeleteTestSubject(t, s, "del-pay", models.StatusDraft)
	if err := s.CreatePaymentRecord(ctx, &models.PaymentRecord{ID: "del-pay-1", UserID: "u3", SubjectID: "del-pay",
		EntitlementID: "e", Amount: 100, PriceAtGrant: 100, Source: "admin_grant", RecordedBy: "a", RecordedAt: now}); err != nil {
		t.Fatalf("CreatePaymentRecord: %v", err)
	}
	for _, id := range []string{"del-ent", "del-req", "del-pay"} {
		if _, err := s.DeleteDraftSubject(ctx, id); !errors.Is(err, ErrSubjectHasStudents) {
			t.Fatalf("%s: err = %v, want ErrSubjectHasStudents", id, err)
		}
		if got, _ := s.GetSubjectByID(ctx, id); got == nil {
			t.Fatalf("%s: referenced subject removed", id)
		}
	}
}

func TestMemoryStore_DeleteDraftSubject(t *testing.T) {
	s := NewMemoryStore()
	if err := s.SeedLevels(context.Background()); err != nil {
		t.Fatalf("SeedLevels: %v", err)
	}
	runDeleteDraftSubjectSuite(t, s)
}

func TestMongoStore_DeleteDraftSubject(t *testing.T) {
	mongoURI := requireDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := NewMongoStore(ctx, mongoURI, randomDBName("test_academy_subject_delete"))
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = s.Database().Drop(dropCtx)
		_ = s.Close(dropCtx)
	})
	if err := s.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := s.SeedLevels(ctx); err != nil {
		t.Fatalf("SeedLevels: %v", err)
	}
	runDeleteDraftSubjectSuite(t, s)
}
