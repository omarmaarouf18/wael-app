package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestMongoStore_MigrateLegacyAppRequests_AttachFilterGuards proves that MigrateLegacyAppRequests
// only attaches requests that are still pending and still have an empty order_id at the moment of update.
// If a request was decided or assigned an order concurrently between Find and UpdateMany, it is not overwritten.
func TestMongoStore_MigrateLegacyAppRequests_AttachFilterGuards(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_migrate_app_attach_guard")
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

	userID := "u-legacy-attach-guard"
	now := time.Now().UTC()

	// Seed 3 legacy app requests:
	// 1. req-normal: stays pending, should be attached to the new order.
	// 2. req-became-accepted: concurrently becomes accepted before attach. Should NOT get an order_id!
	// 3. req-already-has-order: concurrently gets an order_id before attach. Should NOT be overwritten!
	legacyDocs := []any{
		bson.M{
			"_id": "req-normal", "user_id": userID, "subject_id": "subj-1",
			"status": models.RequestStatusPending, "source": "mobile", "created_at": now,
		},
		bson.M{
			"_id": "req-became-accepted", "user_id": userID, "subject_id": "subj-2",
			"status": models.RequestStatusPending, "source": "mobile", "created_at": now.Add(time.Second),
		},
		bson.M{
			"_id": "req-already-has-order", "user_id": userID, "subject_id": "subj-3",
			"status": models.RequestStatusPending, "source": "mobile", "created_at": now.Add(2 * time.Second),
		},
	}
	if _, err := s.Database().Collection("purchase_requests").InsertMany(ctx, legacyDocs); err != nil {
		t.Fatalf("InsertMany: %v", err)
	}

	// Hook simulates concurrent changes between Find and UpdateMany
	s.beforeMigrateAppRequestsAttach = func(ids []string) {
		// Concurrent admin accept: req-became-accepted is now accepted
		_, _ = s.Database().Collection("purchase_requests").UpdateOne(ctx,
			bson.M{"_id": "req-became-accepted"},
			bson.M{"$set": bson.M{"status": models.RequestStatusAccepted}},
		)
		// Concurrent order assignment: req-already-has-order got another order
		_, _ = s.Database().Collection("purchase_requests").UpdateOne(ctx,
			bson.M{"_id": "req-already-has-order"},
			bson.M{"$set": bson.M{"order_id": "ord-concurrent-other"}},
		)
	}

	if err := s.MigrateLegacyAppRequests(ctx); err != nil {
		t.Fatalf("MigrateLegacyAppRequests failed: %v", err)
	}

	// 1. req-normal should be attached to an open app order
	var normalReq models.PurchaseRequest
	if err := s.Database().Collection("purchase_requests").FindOne(ctx, bson.M{"_id": "req-normal"}).Decode(&normalReq); err != nil {
		t.Fatalf("find req-normal: %v", err)
	}
	if normalReq.OrderID == "" {
		t.Errorf("req-normal was not attached to an order")
	}

	// 2. req-became-accepted must NOT have received an order_id!
	var acceptedReq models.PurchaseRequest
	if err := s.Database().Collection("purchase_requests").FindOne(ctx, bson.M{"_id": "req-became-accepted"}).Decode(&acceptedReq); err != nil {
		t.Fatalf("find req-became-accepted: %v", err)
	}
	if acceptedReq.OrderID != "" {
		t.Errorf("req-became-accepted (status accepted) received order_id %q, want empty", acceptedReq.OrderID)
	}

	// 3. req-already-has-order must keep its original order_id "ord-concurrent-other", NOT overwritten!
	var alreadyOrderReq models.PurchaseRequest
	if err := s.Database().Collection("purchase_requests").FindOne(ctx, bson.M{"_id": "req-already-has-order"}).Decode(&alreadyOrderReq); err != nil {
		t.Fatalf("find req-already-has-order: %v", err)
	}
	if alreadyOrderReq.OrderID != "ord-concurrent-other" {
		t.Errorf("req-already-has-order had order_id %q overwritten with %q", "ord-concurrent-other", alreadyOrderReq.OrderID)
	}
}
