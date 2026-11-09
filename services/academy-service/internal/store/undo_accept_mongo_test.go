package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// TestMongoStore_UndoAccept proves UndoAccept reverts only the accept made by
// the given admin at the given time (matching BSON's millisecond precision),
// clears decided_at and decided_by, and never touches another decision.
// Skipped when MONGO_URI is empty (reported as a skipped test).
func TestMongoStore_UndoAccept(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_academy_undo_accept")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	s, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = s.Database().Drop(dropCtx)
		_ = s.Close(dropCtx)
	})

	for _, id := range []string{"req-undo", "req-rejected"} {
		pr := &models.PurchaseRequest{ID: id, UserID: "stu-" + id, SubjectID: "subj-1", Status: models.RequestStatusPending, Source: "mobile", CreatedAt: time.Now().UTC()}
		if _, _, err := s.CreateOrGetPendingRequest(ctx, pr); err != nil {
			t.Fatalf("CreateOrGetPendingRequest %s: %v", id, err)
		}
	}

	// Nanosecond precision on purpose: the store truncates to milliseconds.
	at := time.Now().UTC()
	if ok, err := s.DecideRequest(ctx, "req-undo", models.RequestStatusPending, models.RequestStatusAccepted, "adm-1", at, ""); err != nil || !ok {
		t.Fatalf("DecideRequest accept = %v, %v", ok, err)
	}

	if ok, err := s.UndoAccept(ctx, "req-undo", "adm-2", at); err != nil || ok {
		t.Fatalf("UndoAccept by another admin = %v, %v; want false, nil", ok, err)
	}
	if ok, err := s.UndoAccept(ctx, "req-undo", "adm-1", at.Add(time.Second)); err != nil || ok {
		t.Fatalf("UndoAccept with another time = %v, %v; want false, nil", ok, err)
	}
	if ok, err := s.UndoAccept(ctx, "req-undo", "adm-1", at); err != nil || !ok {
		t.Fatalf("UndoAccept own claim = %v, %v; want true, nil", ok, err)
	}
	got, err := s.GetRequestByID(ctx, "req-undo")
	if err != nil {
		t.Fatalf("GetRequestByID: %v", err)
	}
	if got.Status != models.RequestStatusPending || got.DecidedAt != nil || got.DecidedBy != "" {
		t.Fatalf("after undo: status %s decided_at %v decided_by %q; want a clean pending row", got.Status, got.DecidedAt, got.DecidedBy)
	}
	// The pending partial unique index still holds after the revert.
	if _, created, err := s.CreateOrGetPendingRequest(ctx, &models.PurchaseRequest{ID: "req-dup", UserID: "stu-req-undo", SubjectID: "subj-1", CreatedAt: time.Now().UTC()}); err != nil || created {
		t.Fatalf("second pending request = created %v, %v; want the existing row", created, err)
	}

	// A rejected request is never turned back to pending.
	if ok, err := s.DecideRequest(ctx, "req-rejected", models.RequestStatusPending, models.RequestStatusRejected, "adm-1", at, "no"); err != nil || !ok {
		t.Fatalf("DecideRequest reject = %v, %v", ok, err)
	}
	if ok, err := s.UndoAccept(ctx, "req-rejected", "adm-1", at); err != nil || ok {
		t.Fatalf("UndoAccept on rejected = %v, %v; want false, nil", ok, err)
	}
	got, _ = s.GetRequestByID(ctx, "req-rejected")
	if got.Status != models.RequestStatusRejected {
		t.Fatalf("rejected request status = %s", got.Status)
	}
}
