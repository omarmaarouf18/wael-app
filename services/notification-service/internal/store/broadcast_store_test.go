package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
)

func runBroadcastStoreSuite(t *testing.T, s Store) {
	ctx := context.Background()

	b := &models.Broadcast{
		ID:        "bc-1",
		Audience:  "subject:s1",
		Title:     "عنوان",
		TitleAr:   "عنوان",
		Body:      "نص",
		BodyAr:    "نص",
		SubjectID: "s1",
		CreatedBy: "admin-1",
		Status:    models.BroadcastQueued,
		Total:     2,
	}
	if err := s.CreateBroadcast(ctx, b); err != nil {
		t.Fatalf("CreateBroadcast: %v", err)
	}
	if err := s.CreateBroadcast(ctx, b); err == nil {
		t.Fatal("expected duplicate broadcast error, got nil")
	}
	got, err := s.GetBroadcast(ctx, "bc-1")
	if err != nil || got.Audience != "subject:s1" || got.Total != 2 {
		t.Fatalf("GetBroadcast = %+v, %v", got, err)
	}
	if _, err := s.GetBroadcast(ctx, "missing"); err == nil {
		t.Fatal("expected broadcast-not-found error, got nil")
	}

	// Targets: upsert is idempotent.
	for _, uid := range []string{"u1", "u2"} {
		inserted, err := s.UpsertBroadcastTarget(ctx, &models.BroadcastTarget{
			ID:          TargetID("bc-1", uid),
			BroadcastID: "bc-1",
			UserID:      uid,
			Status:      models.TargetPending,
		})
		if err != nil || !inserted {
			t.Fatalf("UpsertBroadcastTarget %s = %v, %v", uid, inserted, err)
		}
	}
	inserted, err := s.UpsertBroadcastTarget(ctx, &models.BroadcastTarget{
		ID:          TargetID("bc-1", "u1"),
		BroadcastID: "bc-1",
		UserID:      "u1",
		Status:      models.TargetPending,
	})
	if err != nil || inserted {
		t.Fatalf("re-upsert = %v, %v; want untouched", inserted, err)
	}

	pend, err := s.ListPendingTargets(ctx, "bc-1", 100)
	if err != nil || len(pend) != 2 || pend[0].UserID != "u1" || pend[1].UserID != "u2" {
		t.Fatalf("pending = %+v, %v", pend, err)
	}
	if err := s.MarkBroadcastTarget(ctx, "bc-1", "u1", models.TargetSent, ""); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if err := s.MarkBroadcastTarget(ctx, "bc-1", "u2", models.TargetFailed, "boom"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := s.MarkBroadcastTarget(ctx, "bc-1", "u9", models.TargetSent, ""); err == nil {
		t.Fatal("expected target-not-found error, got nil")
	}
	if n, _ := s.CountTargets(ctx, "bc-1", models.TargetSent); n != 1 {
		t.Fatalf("sent count = %d, want 1", n)
	}
	if n, _ := s.CountTargets(ctx, "bc-1", models.TargetFailed); n != 1 {
		t.Fatalf("failed count = %d, want 1", n)
	}
	pend, err = s.ListPendingTargets(ctx, "bc-1", 100)
	if err != nil || len(pend) != 0 {
		t.Fatalf("pending after marking = %+v, %v", pend, err)
	}

	if n, err := s.ResetFailedTargets(ctx, "bc-1"); err != nil || n != 1 {
		t.Fatalf("reset = %d, %v; want 1", n, err)
	}
	pend, err = s.ListPendingTargets(ctx, "bc-1", 100)
	if err != nil || len(pend) != 1 || pend[0].UserID != "u2" {
		t.Fatalf("pending after reset = %+v, %v", pend, err)
	}
	if n, err := s.ResetFailedTargets(ctx, "bc-1"); err != nil || n != 0 {
		t.Fatalf("second reset = %d, %v; want 0", n, err)
	}

	// Broadcast update + list ordering.
	b.Status = models.BroadcastPartial
	b.Delivered = 1
	b.Failed = 1
	if err := s.UpdateBroadcast(ctx, b); err != nil {
		t.Fatalf("UpdateBroadcast: %v", err)
	}
	if err := s.UpdateBroadcast(ctx, &models.Broadcast{ID: "missing"}); err == nil {
		t.Fatal("expected update-missing error, got nil")
	}
	time.Sleep(2 * time.Millisecond)
	b2 := &models.Broadcast{ID: "bc-2", Audience: "all", Title: "t", TitleAr: "t", Body: "b", BodyAr: "b", CreatedBy: "admin-1", Status: models.BroadcastQueued}
	if err := s.CreateBroadcast(ctx, b2); err != nil {
		t.Fatalf("CreateBroadcast bc-2: %v", err)
	}
	items, total, err := s.ListBroadcasts(ctx, 1, 10)
	if err != nil || total != 2 || len(items) != 2 || items[0].ID != "bc-2" || items[1].ID != "bc-1" {
		t.Fatalf("list = %+v total=%d err=%v", items, total, err)
	}
	items, total, err = s.ListBroadcasts(ctx, 2, 1)
	if err != nil || total != 2 || len(items) != 1 || items[0].ID != "bc-1" {
		t.Fatalf("page2 = %+v total=%d err=%v", items, total, err)
	}

	// Audit log round-trip.
	if err := s.CreateAuditLog(ctx, &models.AuditLog{
		ActorID: "admin-1", ActorName: "Sara", Action: "broadcast_send",
		TargetType: "broadcast", TargetID: "bc-1", Detail: "audience=all total=2 delivered=0 failed=0",
	}); err != nil {
		t.Fatalf("CreateAuditLog: %v", err)
	}
	logs, atotal, err := s.ListAuditLogs(ctx, 1, 10)
	if err != nil || atotal != 1 || len(logs) != 1 {
		t.Fatalf("audit list = %+v total=%d err=%v", logs, atotal, err)
	}
	if logs[0].Action != "broadcast_send" || logs[0].ActorName != "Sara" || logs[0].ID == "" || logs[0].CreatedAt.IsZero() {
		t.Fatalf("audit entry = %+v", logs[0])
	}
}

func TestMemoryStore_Broadcast(t *testing.T) {
	runBroadcastStoreSuite(t, NewMemoryStore())
}

func TestMongoStore_Broadcast(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_notif_broadcast")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = s.coll.Database().Drop(dropCtx)
	})

	runBroadcastStoreSuite(t, s)
}
