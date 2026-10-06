package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
)

func runNotificationStoreSuite(t *testing.T, s Store) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)

	for i, title := range []string{"a", "b", "c"} {
		n := &models.Notification{
			ID:        "n-" + title,
			UserID:    "u1",
			Title:     title,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		if title == "b" {
			n.SubjectID = "subj-store-7"
		}
		if err := s.Create(ctx, n); err != nil {
			t.Fatalf("Create n-%s: %v", title, err)
		}
	}
	if err := s.Create(ctx, &models.Notification{ID: "n-x", UserID: "u2", Title: "x", CreatedAt: now}); err != nil {
		t.Fatalf("Create n-x: %v", err)
	}

	// Duplicate ID on Create errors
	if err := s.Create(ctx, &models.Notification{ID: "n-a", UserID: "u1", Title: "dup"}); err == nil {
		t.Fatal("expected duplicate notification ID error, got nil")
	}

	// List page 1 (limit 2): newest first -> n-c, n-b
	page1, err := s.List(ctx, "u1", 1, 2)
	if err != nil || len(page1) != 2 {
		t.Fatalf("page1 = %v, %v", page1, err)
	}
	if page1[0].ID != "n-c" || page1[1].ID != "n-b" {
		t.Fatalf("page1 unexpected order: [%s, %s]", page1[0].ID, page1[1].ID)
	}
	if page1[1].SubjectID != "subj-store-7" {
		t.Fatalf("n-b subject_id = %q, want subj-store-7", page1[1].SubjectID)
	}
	if page1[0].SubjectID != "" {
		t.Fatalf("n-c subject_id = %q, want empty (pre-subject_id row)", page1[0].SubjectID)
	}

	// List page 2 (limit 2): should return n-a
	page2, err := s.List(ctx, "u1", 2, 2)
	if err != nil || len(page2) != 1 {
		t.Fatalf("page2 = %v, %v", page2, err)
	}
	if page2[0].ID != "n-a" {
		t.Fatalf("page2 unexpected element: %s", page2[0].ID)
	}

	// List page 3 (limit 2): empty
	page3, err := s.List(ctx, "u1", 3, 2)
	if err != nil || len(page3) != 0 {
		t.Fatalf("page3 expected empty, got %v (err: %v)", page3, err)
	}

	// List for non-existent user: empty
	missingUserList, err := s.List(ctx, "nonexistent-user", 1, 10)
	if err != nil || len(missingUserList) != 0 {
		t.Fatalf("missing user list expected empty, got %v (err: %v)", missingUserList, err)
	}

	// MarkRead for owner succeeds
	if err := s.MarkRead(ctx, "u1", "n-c"); err != nil {
		t.Fatalf("MarkRead owner: %v", err)
	}

	// Cross-user MarkRead errors
	if err := s.MarkRead(ctx, "u2", "n-c"); err == nil {
		t.Fatal("expected cross-user mark-read error, got nil")
	}

	// MarkRead for nonexistent notification errors
	if err := s.MarkRead(ctx, "u1", "n-nonexistent"); err == nil {
		t.Fatal("expected error on MarkRead for nonexistent notification, got nil")
	}
}

func TestMemoryStore_ListPaginationAndMarkRead(t *testing.T) {
	s := NewMemoryStore()
	runNotificationStoreSuite(t, s)
}

func TestMongoStore_ListPaginationAndMarkRead(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_notif_store")
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

	runNotificationStoreSuite(t, s)
}
