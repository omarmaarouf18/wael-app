package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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

// migrationSnapshot is the count-only view used to prove that a second service
// start is idempotent.
type migrationSnapshot struct {
	orders               int
	openAppOrders        int
	requests             int
	pendingAppAttached   int
	pendingAppUnattached int
	historyWithOrder     int
}

func takeMigrationSnapshot(t *testing.T, db *mongo.Database) migrationSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	count := func(col string, filter bson.M) int {
		t.Helper()
		n, err := db.Collection(col).CountDocuments(ctx, filter)
		if err != nil {
			t.Fatalf("count %s %v: %v", col, filter, err)
		}
		return int(n)
	}

	legacyAppSource := bson.M{"$or": bson.A{
		bson.M{"source": bson.M{"$exists": false}},
		bson.M{"source": ""},
		bson.M{"source": "mobile"},
	}}

	return migrationSnapshot{
		orders:        count("orders", bson.M{}),
		openAppOrders: count("orders", bson.M{"source": models.OrderSourceApp, "status": models.OrderStatusOpen}),
		requests:      count("purchase_requests", bson.M{}),
		pendingAppAttached: count("purchase_requests", bson.M{
			"$and": bson.A{
				legacyAppSource,
				bson.M{"status": models.RequestStatusPending},
				bson.M{"order_id": bson.M{"$exists": true, "$ne": ""}},
			},
		}),
		pendingAppUnattached: count("purchase_requests", bson.M{
			"$and": bson.A{
				legacyAppSource,
				bson.M{"status": models.RequestStatusPending},
				bson.M{"$or": bson.A{
					bson.M{"order_id": bson.M{"$exists": false}},
					bson.M{"order_id": ""},
				}},
			},
		}),
		historyWithOrder: count("purchase_requests", bson.M{
			"status":   bson.M{"$in": bson.A{models.RequestStatusAccepted, models.RequestStatusRejected}},
			"order_id": bson.M{"$exists": true, "$ne": ""},
		}),
	}
}

// TestMongoStore_MigrationProductionShapedData seeds data shaped like
// production (several students, one with 30 pending app requests, accepted/
// rejected history and web rows) and runs the real startup path twice.
// It proves: one open app order per student with all their pending app
// requests, history and web rows untouched, and an idempotent second start.
func TestMongoStore_MigrationProductionShapedData(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_academy_migration_shape")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	db := client.Database(dbName)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = db.Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
	})

	now := time.Now().UTC()
	reqs := make([]any, 0, 40)
	// One student with 30 pending app requests (the first has no source field,
	// as an older row would).
	for i := 0; i < 30; i++ {
		doc := bson.M{
			"_id":        fmt.Sprintf("many-%02d", i),
			"user_id":    "u-many",
			"subject_id": fmt.Sprintf("s-many-%02d", i),
			"status":     models.RequestStatusPending,
			"created_at": now.Add(time.Duration(i) * time.Minute),
		}
		if i > 0 {
			doc["source"] = "mobile"
		}
		reqs = append(reqs, doc)
	}
	// A student with a few pending app requests.
	for i := 0; i < 3; i++ {
		reqs = append(reqs, bson.M{
			"_id": fmt.Sprintf("few-%d", i), "user_id": "u-few", "subject_id": fmt.Sprintf("s-few-%d", i),
			"status": models.RequestStatusPending, "source": "mobile",
			"created_at": now.Add(time.Duration(i) * time.Minute),
		})
	}
	// Accepted/rejected history: must keep no order.
	for i, status := range []string{models.RequestStatusAccepted, models.RequestStatusAccepted, models.RequestStatusRejected} {
		reqs = append(reqs, bson.M{
			"_id": fmt.Sprintf("history-%d", i), "user_id": "u-history", "subject_id": fmt.Sprintf("s-history-%d", i),
			"status": status, "source": "mobile", "created_at": now.Add(-time.Hour),
		})
	}
	// A legacy single web purchase request that already belongs to a web order.
	reqs = append(reqs, bson.M{
		"_id": "web-1", "user_id": "u-web", "subject_id": "s-web-1",
		"status": models.RequestStatusPending, "source": "web", "order_id": "web-order-1",
		"created_at": now.Add(-30 * time.Minute),
	})
	// One student with both a pending app request and a pending web row: the
	// migration must attach only the app row.
	reqs = append(reqs,
		bson.M{
			"_id": "mixed-app-1", "user_id": "u-mixed", "subject_id": "s-mixed-app",
			"status": models.RequestStatusPending, "source": "mobile", "created_at": now.Add(-20 * time.Minute),
		},
		bson.M{
			"_id": "mixed-web-1", "user_id": "u-mixed", "subject_id": "s-mixed-web",
			"status": models.RequestStatusPending, "source": "web", "created_at": now.Add(-20 * time.Minute),
		})
	// An app request that already has an order: migration must not move it.
	reqs = append(reqs, bson.M{
		"_id": "existing-app-1", "user_id": "u-existing", "subject_id": "s-existing-1",
		"status": models.RequestStatusPending, "source": "mobile", "order_id": "existing-order",
		"created_at": now.Add(-15 * time.Minute),
	})
	if _, err := db.Collection("purchase_requests").InsertMany(ctx, reqs); err != nil {
		t.Fatalf("seed requests: %v", err)
	}

	orders := []any{
		bson.M{"_id": "web-order-1", "user_id": "u-web", "source": models.OrderSourceWeb,
			"status": models.OrderStatusOpen, "amount_paid": 150, "created_at": now.Add(-30 * time.Minute)},
		bson.M{"_id": "existing-order", "user_id": "u-existing", "source": models.OrderSourceApp,
			"status": models.OrderStatusOpen, "created_at": now.Add(-15 * time.Minute)},
	}
	if _, err := db.Collection("orders").InsertMany(ctx, orders); err != nil {
		t.Fatalf("seed orders: %v", err)
	}

	before := takeMigrationSnapshot(t, db)
	if before.orders != 2 || before.requests != 40 || before.pendingAppAttached != 1 {
		t.Fatalf("unexpected seeded snapshot: %+v", before)
	}

	// First service start: NewMongoStore runs EnsureIndexes + migration.
	first, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	afterFirst := takeMigrationSnapshot(t, first.Database())
	assertProductionMigration(t, first)

	// Second service start: must change nothing.
	if err := first.Close(ctx); err != nil {
		t.Fatalf("close first store: %v", err)
	}
	second, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	afterSecond := takeMigrationSnapshot(t, second.Database())
	assertProductionMigration(t, second)
	if err := second.Close(ctx); err != nil {
		t.Fatalf("close second store: %v", err)
	}

	if afterFirst != afterSecond {
		t.Fatalf("second start changed data:\nfirst  = %+v\nsecond = %+v", afterFirst, afterSecond)
	}
	if afterFirst.orders != 5 || afterFirst.openAppOrders != 4 {
		t.Fatalf("orders after migration = %+v, want 5 total / 4 open app", afterFirst)
	}
	if afterFirst.requests != 40 {
		t.Fatalf("requests after migration = %d, want 40 untouched rows", afterFirst.requests)
	}
	if afterFirst.pendingAppAttached != 35 || afterFirst.pendingAppUnattached != 0 {
		t.Fatalf("pending app attachment = %d attached / %d unattached, want 35/0", afterFirst.pendingAppAttached, afterFirst.pendingAppUnattached)
	}
	if afterFirst.historyWithOrder != 0 {
		t.Fatalf("accepted/rejected history got an order: %d rows", afterFirst.historyWithOrder)
	}

	// Web rows are untouched, including the same student who got an app order.
	var webRow, mixedWebRow models.PurchaseRequest
	if err := db.Collection("purchase_requests").FindOne(ctx, bson.M{"_id": "web-1"}).Decode(&webRow); err != nil {
		t.Fatalf("read web-1: %v", err)
	}
	if webRow.OrderID != "web-order-1" {
		t.Fatalf("web-1 order_id = %q, want web-order-1", webRow.OrderID)
	}
	if err := db.Collection("purchase_requests").FindOne(ctx, bson.M{"_id": "mixed-web-1"}).Decode(&mixedWebRow); err != nil {
		t.Fatalf("read mixed-web-1: %v", err)
	}
	if mixedWebRow.OrderID != "" {
		t.Fatalf("mixed web row was attached by migration: order_id=%q", mixedWebRow.OrderID)
	}
}

// assertProductionMigration checks the per-student shape after a start.
func assertProductionMigration(t *testing.T, s *MongoStore) {
	t.Helper()
	ctx := context.Background()

	wantRequests := map[string]int{"u-many": 30, "u-few": 3, "u-mixed": 1, "u-existing": 1}
	for user, want := range wantRequests {
		orders, err := s.ListOrdersByUser(ctx, user)
		if err != nil {
			t.Fatalf("ListOrdersByUser(%s): %v", user, err)
		}
		var openApp []*models.Order
		for _, o := range orders {
			if o.Source == models.OrderSourceApp && o.Status == models.OrderStatusOpen {
				openApp = append(openApp, o)
			}
		}
		if len(openApp) != 1 {
			t.Fatalf("%s: open app orders = %d, want 1", user, len(openApp))
		}
		reqs, err := s.ListRequestsByOrder(ctx, openApp[0].ID)
		if err != nil {
			t.Fatalf("ListRequestsByOrder(%s): %v", user, err)
		}
		if len(reqs) != want {
			t.Fatalf("%s: requests in the open app order = %d, want %d", user, len(reqs), want)
		}
	}

	historyOrders, err := s.ListOrdersByUser(ctx, "u-history")
	if err != nil {
		t.Fatalf("ListOrdersByUser(u-history): %v", err)
	}
	if len(historyOrders) != 0 {
		t.Fatalf("u-history got %d order(s), want 0", len(historyOrders))
	}
	webOrders, err := s.ListOrdersByUser(ctx, "u-web")
	if err != nil {
		t.Fatalf("ListOrdersByUser(u-web): %v", err)
	}
	if len(webOrders) != 1 || webOrders[0].Source != models.OrderSourceWeb {
		t.Fatalf("u-web orders = %+v, want the untouched web order", webOrders)
	}
}
