package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestMongoStore_CheckoutCompensation_ExpiredParentContext proves that checkout
// compensation (deleteOrderWithRequests) cleans up partial orders and purchase requests
// even when the calling parent context is already expired or cancelled.
func TestMongoStore_CheckoutCompensation_ExpiredParentContext(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_checkout_compensation")
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

	orderID := "ord-comp-test"
	reqID := "req-comp-test"

	// Insert an order and a request row directly into MongoDB to simulate a partial create
	order := &models.Order{
		ID:        orderID,
		UserID:    "student-comp",
		Status:    models.OrderStatusOpen,
		Source:    models.OrderSourceWeb,
		CreatedAt: time.Now().UTC(),
	}
	if _, err := s.db.Collection("orders").InsertOne(ctx, order); err != nil {
		t.Fatalf("InsertOne order: %v", err)
	}
	req := &models.PurchaseRequest{
		ID:        reqID,
		OrderID:   orderID,
		UserID:    "student-comp",
		SubjectID: "subj-1",
		Status:    models.RequestStatusPending,
		Source:    models.OrderSourceWeb,
		CreatedAt: time.Now().UTC(),
	}
	if _, err := s.db.Collection("purchase_requests").InsertOne(ctx, req); err != nil {
		t.Fatalf("InsertOne request: %v", err)
	}

	// Create an already-expired / cancelled parent context
	expiredCtx, cancelExpired := context.WithCancel(context.Background())
	cancelExpired() // cancelled immediately

	// Run compensation under the expired context
	s.deleteOrderWithRequests(expiredCtx, orderID)

	// Verify that the order and request rows are deleted despite the expired parent context
	var foundOrder models.Order
	err = s.db.Collection("orders").FindOne(ctx, bson.M{"_id": orderID}).Decode(&foundOrder)
	if err == nil {
		t.Errorf("order %s was NOT deleted by compensation under expired parent context", orderID)
	}

	var foundReq models.PurchaseRequest
	err = s.db.Collection("purchase_requests").FindOne(ctx, bson.M{"_id": reqID}).Decode(&foundReq)
	if err == nil {
		t.Errorf("request %s was NOT deleted by compensation under expired parent context", reqID)
	}
}
