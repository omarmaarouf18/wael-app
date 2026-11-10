package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// TestMongoStore_CreateOrGetPendingAppRequest_ClosedOrderRace proves that if an open app order
// is closed concurrently (for example by CloseOrderIfNoPending on an accept of the last pending item)
// after ensureOpenAppOrder finds it, adding the new app request detects that the order is no longer open,
// opens a fresh order, and retries once so the request is never attached to a closed order.
func TestMongoStore_CreateOrGetPendingAppRequest_ClosedOrderRace(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_app_order_race")
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

	userID := "stu-app-race-1"

	// 1. Setup an initial open app order for the student
	initialOrderID, err := s.ensureOpenAppOrder(ctx, userID, time.Now().UTC())
	if err != nil {
		t.Fatalf("ensureOpenAppOrder: %v", err)
	}

	// 2. Simulate the race: right after ensureOpenAppOrder reads the open order ID,
	// CloseOrderIfNoPending closes it once.
	s.beforeAppOrderInsert = func(orderID string) {
		s.beforeAppOrderInsert = nil
		closed, cErr := s.CloseOrderIfNoPending(ctx, orderID, time.Now().UTC())
		if cErr != nil || !closed {
			t.Fatalf("CloseOrderIfNoPending: closed=%v err=%v", closed, cErr)
		}
	}

	req := &models.PurchaseRequest{
		ID:        "req-app-race-1",
		UserID:    userID,
		SubjectID: "subj-app-1",
		Source:    "mobile",
		CreatedAt: time.Now().UTC(),
	}

	createdReq, created, err := s.CreateOrGetPendingAppRequest(ctx, req)
	if err != nil {
		t.Fatalf("CreateOrGetPendingAppRequest failed: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true, got %v", created)
	}

	// 3. Verify the attached order is open and NOT the closed initial order
	if createdReq.OrderID == initialOrderID {
		t.Errorf("request was attached to closed order %s", initialOrderID)
	}

	attachedOrder, err := s.GetOrderByID(ctx, createdReq.OrderID)
	if err != nil {
		t.Fatalf("GetOrderByID: %v", err)
	}
	if attachedOrder.Status != models.OrderStatusOpen {
		t.Errorf("attached order status = %q, want 'open'", attachedOrder.Status)
	}
}
