package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestMongoStore_AppOrdersParallelAndMigration proves the unique partial index
// makes parallel app requests converge on one open app order, and that the
// startup migration groups legacy pending requests while leaving history alone.
// Skipped when MONGO_URI is empty (reported as a skipped test).
func TestMongoStore_AppOrdersParallelAndMigration(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_academy_orders")
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

	// Parallel first requests for one student: exactly one open app order.
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := s.CreateOrGetPendingAppRequest(ctx, &models.PurchaseRequest{
				UserID:    "u-parallel",
				SubjectID: fmt.Sprintf("s-%d", i),
				CreatedAt: time.Now().UTC(),
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("parallel CreateOrGetPendingAppRequest: %v", err)
		}
	}

	orders, err := s.ListOrdersByUser(ctx, "u-parallel")
	if err != nil {
		t.Fatalf("ListOrdersByUser: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("parallel requests created %d orders, want 1", len(orders))
	}
	if orders[0].Source != models.OrderSourceApp || orders[0].Status != models.OrderStatusOpen {
		t.Fatalf("order = %+v, want app/open", orders[0])
	}
	reqs, err := s.ListRequestsByOrder(ctx, orders[0].ID)
	if err != nil {
		t.Fatalf("ListRequestsByOrder: %v", err)
	}
	if len(reqs) != n {
		t.Fatalf("order has %d requests, want %d", len(reqs), n)
	}

	// Legacy pending request (no order_id) and rejected history (no order).
	now := time.Now().UTC()
	legacy := []any{
		bson.M{
			"_id": "legacy-pending-1", "user_id": "u-legacy", "subject_id": "s-legacy-1",
			"status": models.RequestStatusPending, "source": "mobile",
			"created_at": now.Add(-2 * time.Hour),
		},
		bson.M{
			"_id": "legacy-pending-2", "user_id": "u-legacy", "subject_id": "s-legacy-2",
			"status": models.RequestStatusPending, "source": "mobile",
			"created_at": now.Add(-time.Hour),
		},
		bson.M{
			"_id": "legacy-rejected-1", "user_id": "u-legacy", "subject_id": "s-legacy-3",
			"status": models.RequestStatusRejected, "source": "mobile",
			"created_at": now.Add(-3 * time.Hour),
		},
	}
	if _, err := s.Database().Collection("purchase_requests").InsertMany(ctx, legacy); err != nil {
		t.Fatalf("insert legacy requests: %v", err)
	}

	if err := s.MigrateLegacyAppRequests(ctx); err != nil {
		t.Fatalf("MigrateLegacyAppRequests: %v", err)
	}
	legacyOrders, err := s.ListOrdersByUser(ctx, "u-legacy")
	if err != nil {
		t.Fatalf("ListOrdersByUser legacy: %v", err)
	}
	if len(legacyOrders) != 1 {
		t.Fatalf("legacy migration created %d orders, want 1", len(legacyOrders))
	}
	legacyReqs, err := s.ListRequestsByOrder(ctx, legacyOrders[0].ID)
	if err != nil {
		t.Fatalf("ListRequestsByOrder legacy: %v", err)
	}
	if len(legacyReqs) != 2 {
		t.Fatalf("legacy order has %d pending requests, want 2", len(legacyReqs))
	}

	var rejected models.PurchaseRequest
	if err := s.Database().Collection("purchase_requests").FindOne(ctx, bson.M{"_id": "legacy-rejected-1"}).Decode(&rejected); err != nil {
		t.Fatalf("find rejected legacy request: %v", err)
	}
	if rejected.OrderID != "" {
		t.Fatalf("accepted/rejected history must get no order, got %q", rejected.OrderID)
	}

	// Idempotent: a second migration creates no extra order.
	if err := s.MigrateLegacyAppRequests(ctx); err != nil {
		t.Fatalf("second MigrateLegacyAppRequests: %v", err)
	}
	legacyOrdersAfter, _ := s.ListOrdersByUser(ctx, "u-legacy")
	if len(legacyOrdersAfter) != 1 {
		t.Fatalf("second migration created extra orders: %d", len(legacyOrdersAfter))
	}
}
