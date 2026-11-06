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
	webOrders            int
	requests             int
	pendingAppAttached   int
	pendingAppUnattached int
	pendingWebAttached   int
	pendingWebUnattached int
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
		webOrders:     count("orders", bson.M{"source": models.OrderSourceWeb}),
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
		pendingWebAttached: count("purchase_requests", bson.M{
			"status":   models.RequestStatusPending,
			"source":   models.OrderSourceWeb,
			"order_id": bson.M{"$exists": true, "$ne": ""},
		}),
		pendingWebUnattached: count("purchase_requests", bson.M{
			"status": models.RequestStatusPending,
			"source": models.OrderSourceWeb,
			"$or": bson.A{
				bson.M{"order_id": bson.M{"$exists": false}},
				bson.M{"order_id": ""},
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
	// app row joins the app order, the web row gets its own web order of one.
	reqs = append(reqs,
		bson.M{
			"_id": "mixed-app-1", "user_id": "u-mixed", "subject_id": "s-mixed-app",
			"status": models.RequestStatusPending, "source": "mobile", "created_at": now.Add(-20 * time.Minute),
		},
		bson.M{
			"_id": "mixed-web-1", "user_id": "u-mixed", "subject_id": "s-mixed-web",
			"status": models.RequestStatusPending, "source": "web",
			"payment_method": "wallet", "transfer_reference": "mixed-web-ref",
			"normalized_reference": "MIXED-WEB-REF", "sender_number": "01055555555",
			"amount": 300, "created_at": now.Add(-20 * time.Minute),
		})
	// A second legacy pending web request with its own proof fields.
	reqs = append(reqs, bson.M{
		"_id": "legacy-web-2", "user_id": "u-web2", "subject_id": "s-web-2",
		"status": models.RequestStatusPending, "source": "web",
		"payment_method": "instapay", "transfer_reference": "legacy-web-ref-2",
		"normalized_reference": "LEGACY-WEB-REF-2", "sender_number": "01099999999",
		"amount": 250, "created_at": now.Add(-25 * time.Minute),
	})
	// Rejected web history: must keep no order.
	reqs = append(reqs, bson.M{
		"_id": "history-web-1", "user_id": "u-web-history", "subject_id": "s-web-history-1",
		"status": models.RequestStatusRejected, "source": "web",
		"payment_method": "wallet", "transfer_reference": "history-web-ref",
		"normalized_reference": "HISTORY-WEB-REF", "sender_number": "01022222222",
		"amount": 90, "created_at": now.Add(-time.Hour),
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
	if before.orders != 2 || before.requests != 42 || before.pendingAppAttached != 1 {
		t.Fatalf("unexpected seeded snapshot: %+v", before)
	}
	if before.pendingWebAttached != 1 || before.pendingWebUnattached != 2 {
		t.Fatalf("unexpected seeded web snapshot: %+v", before)
	}

	// First service start: NewMongoStore runs EnsureIndexes + migration.
	first, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	afterFirst := takeMigrationSnapshot(t, first.Database())
	assertProductionMigration(t, first)

	// The console's grouped read model must not lose the legacy web rows:
	// every pending row it returns carries an order id.
	grouped, err := first.ListRequestsForGroups(ctx, OrderRequestFilter{Status: models.RequestStatusPending})
	if err != nil {
		t.Fatalf("ListRequestsForGroups: %v", err)
	}
	for _, req := range grouped {
		if req.OrderID == "" {
			t.Fatalf("grouped read model returned %s without an order id", req.ID)
		}
	}
	groupedIDs := make(map[string]bool, len(grouped))
	for _, req := range grouped {
		groupedIDs[req.ID] = true
	}
	for _, id := range []string{"mixed-web-1", "legacy-web-2", "many-00", "few-0"} {
		if !groupedIDs[id] {
			t.Fatalf("grouped read model is missing %s", id)
		}
	}

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
	if afterFirst.orders != 7 || afterFirst.openAppOrders != 4 || afterFirst.webOrders != 3 {
		t.Fatalf("orders after migration = %+v, want 7 total / 4 open app / 3 web", afterFirst)
	}
	if afterFirst.requests != 42 {
		t.Fatalf("requests after migration = %d, want 42 untouched rows", afterFirst.requests)
	}
	if afterFirst.pendingAppAttached != 35 || afterFirst.pendingAppUnattached != 0 {
		t.Fatalf("pending app attachment = %d attached / %d unattached, want 35/0", afterFirst.pendingAppAttached, afterFirst.pendingAppUnattached)
	}
	if afterFirst.pendingWebAttached != 3 || afterFirst.pendingWebUnattached != 0 {
		t.Fatalf("pending web attachment = %d attached / %d unattached, want 3/0", afterFirst.pendingWebAttached, afterFirst.pendingWebUnattached)
	}
	if afterFirst.historyWithOrder != 0 {
		t.Fatalf("accepted/rejected history got an order: %d rows", afterFirst.historyWithOrder)
	}

	// The legacy pending web row that already had an order keeps it.
	var webRow models.PurchaseRequest
	if err := db.Collection("purchase_requests").FindOne(ctx, bson.M{"_id": "web-1"}).Decode(&webRow); err != nil {
		t.Fatalf("read web-1: %v", err)
	}
	if webRow.OrderID != "web-order-1" {
		t.Fatalf("web-1 order_id = %q, want web-order-1", webRow.OrderID)
	}
	uWebOrders, err := db.Collection("orders").CountDocuments(ctx, bson.M{"user_id": "u-web"})
	if err != nil {
		t.Fatalf("count u-web orders: %v", err)
	}
	if uWebOrders != 1 {
		t.Fatalf("u-web orders = %d, want 1 (no second order for the attached row)", uWebOrders)
	}

	// Each legacy pending web row without an order got its own web order of
	// one, with the proof fields copied.
	for _, tc := range []struct {
		requestID     string
		userID        string
		paymentMethod string
		transferRef   string
		normalizedRef string
		senderNumber  string
		amountPaid    int
	}{
		{"mixed-web-1", "u-mixed", "wallet", "mixed-web-ref", "MIXED-WEB-REF", "01055555555", 300},
		{"legacy-web-2", "u-web2", "instapay", "legacy-web-ref-2", "LEGACY-WEB-REF-2", "01099999999", 250},
	} {
		var row models.PurchaseRequest
		if err := db.Collection("purchase_requests").FindOne(ctx, bson.M{"_id": tc.requestID}).Decode(&row); err != nil {
			t.Fatalf("read %s: %v", tc.requestID, err)
		}
		if row.OrderID == "" {
			t.Fatalf("%s was left out of the grouped view: no order", tc.requestID)
		}
		var order models.Order
		if err := db.Collection("orders").FindOne(ctx, bson.M{"_id": row.OrderID}).Decode(&order); err != nil {
			t.Fatalf("read order of %s: %v", tc.requestID, err)
		}
		if order.Source != models.OrderSourceWeb || order.Status != models.OrderStatusOpen {
			t.Fatalf("%s order = %+v, want web/open", tc.requestID, order)
		}
		if order.UserID != tc.userID {
			t.Fatalf("%s order user = %q, want %q", tc.requestID, order.UserID, tc.userID)
		}
		if order.PaymentMethod != tc.paymentMethod || order.TransferReference != tc.transferRef ||
			order.NormalizedReference != tc.normalizedRef || order.SenderNumber != tc.senderNumber ||
			order.AmountPaid != tc.amountPaid {
			t.Fatalf("%s proof fields not copied: %+v", tc.requestID, order)
		}
		cursor, err := db.Collection("purchase_requests").Find(ctx, bson.M{"order_id": order.ID})
		if err != nil {
			t.Fatalf("list requests of %s: %v", tc.requestID, err)
		}
		var rows []models.PurchaseRequest
		if err := cursor.All(ctx, &rows); err != nil {
			t.Fatalf("decode requests of %s: %v", tc.requestID, err)
		}
		if len(rows) != 1 || rows[0].ID != tc.requestID {
			t.Fatalf("%s order holds %d request(s), want only itself", tc.requestID, len(rows))
		}
	}

	// Rejected web history keeps no order.
	var historyWeb models.PurchaseRequest
	if err := db.Collection("purchase_requests").FindOne(ctx, bson.M{"_id": "history-web-1"}).Decode(&historyWeb); err != nil {
		t.Fatalf("read history-web-1: %v", err)
	}
	if historyWeb.OrderID != "" {
		t.Fatalf("rejected web history got an order: %q", historyWeb.OrderID)
	}
	historyWebOrders, err := db.Collection("orders").CountDocuments(ctx, bson.M{"user_id": "u-web-history"})
	if err != nil {
		t.Fatalf("count u-web-history orders: %v", err)
	}
	if historyWebOrders != 0 {
		t.Fatalf("u-web-history got %d order(s), want 0", historyWebOrders)
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

// TestMongoStore_MultiSubjectWebOrder proves that a web order with multiple
// subjects sharing the same transfer reference can be created in Mongo without
// duplicate key errors on purchase_requests.
func TestMongoStore_MultiSubjectWebOrder(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_academy_multi_order")
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
	order := &models.Order{
		ID:                  "ord-multi-1",
		UserID:              "u-cart-student",
		Source:              models.OrderSourceWeb,
		Status:              models.OrderStatusOpen,
		PaymentMethod:       "wallet",
		TransferReference:   "TXN-CART-123",
		NormalizedReference: "TXN-CART-123",
		SenderNumber:        "01012345678",
		AmountPaid:          350,
		CreatedAt:           now,
	}
	reqs := []*models.PurchaseRequest{
		{
			ID:                  "req-1",
			UserID:              "u-cart-student",
			SubjectID:           "s-1",
			OrderID:             order.ID,
			Status:              models.RequestStatusPending,
			Source:              models.OrderSourceWeb,
			PaymentMethod:       order.PaymentMethod,
			TransferReference:   order.TransferReference,
			NormalizedReference: order.NormalizedReference,
			SenderNumber:        order.SenderNumber,
			Amount:              100,
			CreatedAt:           now,
		},
		{
			ID:                  "req-2",
			UserID:              "u-cart-student",
			SubjectID:           "s-2",
			OrderID:             order.ID,
			Status:              models.RequestStatusPending,
			Source:              models.OrderSourceWeb,
			PaymentMethod:       order.PaymentMethod,
			TransferReference:   order.TransferReference,
			NormalizedReference: order.NormalizedReference,
			SenderNumber:        order.SenderNumber,
			Amount:              120,
			CreatedAt:           now,
		},
		{
			ID:                  "req-3",
			UserID:              "u-cart-student",
			SubjectID:           "s-3",
			OrderID:             order.ID,
			Status:              models.RequestStatusPending,
			Source:              models.OrderSourceWeb,
			PaymentMethod:       order.PaymentMethod,
			TransferReference:   order.TransferReference,
			NormalizedReference: order.NormalizedReference,
			SenderNumber:        order.SenderNumber,
			Amount:              130,
			CreatedAt:           now,
		},
	}

	if err := s.CreateOrderWithRequests(ctx, order, reqs); err != nil {
		t.Fatalf("CreateOrderWithRequests multi-subject order failed: %v", err)
	}

	fetchedOrder, err := s.GetOrderByID(ctx, order.ID)
	if err != nil || fetchedOrder == nil {
		t.Fatalf("GetOrderByID failed: %v", err)
	}
	fetchedReqs, err := s.ListRequestsByOrder(ctx, order.ID)
	if err != nil || len(fetchedReqs) != 3 {
		t.Fatalf("ListRequestsByOrder failed: len=%d err=%v", len(fetchedReqs), err)
	}
}
