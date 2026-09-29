package store

import (
	"context"
	"testing"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
)

func TestMemoryStore_ListPaginationAndMarkRead(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	for i, title := range []string{"a", "b", "c"} {
		_ = i
		if err := s.Create(ctx, &models.Notification{ID: "n-" + title, UserID: "u1", Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Create(ctx, &models.Notification{ID: "n-x", UserID: "u2", Title: "x"}); err != nil {
		t.Fatal(err)
	}
	page1, err := s.List(ctx, "u1", 1, 2)
	if err != nil || len(page1) != 2 {
		t.Fatalf("page1 = %v, %v", page1, err)
	}
	page2, err := s.List(ctx, "u1", 2, 2)
	if err != nil || len(page2) != 1 {
		t.Fatalf("page2 = %v, %v", page2, err)
	}
	if err := s.MarkRead(ctx, "u1", page1[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRead(ctx, "u2", page1[0].ID); err == nil {
		t.Fatal("expected cross-user mark-read error")
	}
}
