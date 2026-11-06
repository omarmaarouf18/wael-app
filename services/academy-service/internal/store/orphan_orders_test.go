package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// seedOrphanCases creates the four cases the cleanup must distinguish:
// an old orphan web order (remove), a young orphan web order (keep), an old
// web order with a request (keep), and an old orphan app order (keep).
func seedOrphanCases(t *testing.T, s Store, now time.Time) {
	t.Helper()
	ctx := context.Background()
	old := now.Add(-10 * time.Minute)

	cases := []struct {
		order *models.Order
		reqs  []*models.PurchaseRequest
	}{
		{
			order: &models.Order{ID: "ord-orphan-web-old", UserID: "u-orphan", Source: models.OrderSourceWeb, Status: models.OrderStatusOpen, CreatedAt: old},
		},
		{
			order: &models.Order{ID: "ord-orphan-web-young", UserID: "u-orphan", Source: models.OrderSourceWeb, Status: models.OrderStatusOpen, CreatedAt: now},
		},
		{
			order: &models.Order{ID: "ord-web-has-request", UserID: "u-orphan", Source: models.OrderSourceWeb, Status: models.OrderStatusOpen, CreatedAt: old},
			reqs: []*models.PurchaseRequest{{
				ID: "req-web-has-request", UserID: "u-orphan", SubjectID: "subj-1",
				Status: models.RequestStatusPending, Source: models.OrderSourceWeb, CreatedAt: old,
			}},
		},
		{
			order: &models.Order{ID: "ord-orphan-app-old", UserID: "u-orphan", Source: models.OrderSourceApp, Status: models.OrderStatusOpen, CreatedAt: old},
		},
	}
	for _, tc := range cases {
		if err := s.CreateOrderWithRequests(ctx, tc.order, tc.reqs); err != nil {
			t.Fatalf("create %s: %v", tc.order.ID, err)
		}
	}
}

func assertOrphanCleanupResult(t *testing.T, s Store, now time.Time, wantRemoved int) {
	t.Helper()
	ctx := context.Background()

	removed, err := s.DeleteOrphanWebOrders(ctx, now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("DeleteOrphanWebOrders: %v", err)
	}
	if removed != wantRemoved {
		t.Fatalf("removed = %d, want %d", removed, wantRemoved)
	}

	expect := map[string]bool{
		"ord-orphan-web-old":   false, // old web orphan: removed
		"ord-orphan-web-young": true,  // too young: kept
		"ord-web-has-request":  true,  // has a request: kept
		"ord-orphan-app-old":   true,  // app order: never touched
	}
	for id, want := range expect {
		order, err := s.GetOrderByID(ctx, id)
		if err != nil {
			t.Fatalf("GetOrderByID(%s): %v", id, err)
		}
		if got := order != nil; got != want {
			t.Fatalf("order %s exists = %t, want %t", id, got, want)
		}
	}
}

func TestMemoryStore_DeleteOrphanWebOrders(t *testing.T) {
	s := NewMemoryStore()
	defer func() { _ = s.Close(context.Background()) }()
	now := time.Now().UTC()

	seedOrphanCases(t, s, now)
	assertOrphanCleanupResult(t, s, now, 1)

	// Idempotent: a second sweep removes nothing.
	removed, err := s.DeleteOrphanWebOrders(context.Background(), now.Add(-5*time.Minute))
	if err != nil || removed != 0 {
		t.Fatalf("second sweep = %d, %v; want 0, nil", removed, err)
	}
}

func TestMongoStore_DeleteOrphanWebOrders(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_academy_orphans")
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

	now := time.Now().UTC()
	seedOrphanCases(t, s, now)
	assertOrphanCleanupResult(t, s, now, 1)

	removed, err := s.DeleteOrphanWebOrders(ctx, now.Add(-5*time.Minute))
	if err != nil || removed != 0 {
		t.Fatalf("second sweep = %d, %v; want 0, nil", removed, err)
	}
}
