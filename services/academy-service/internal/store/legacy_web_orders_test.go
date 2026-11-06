package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// TestMemoryStore_MigrateLegacyWebRequests covers the legacy web wrapping in
// the memory store: a pending web request without an order becomes a web order
// of one with its proof fields copied, an already-attached request and the
// accepted/rejected history are untouched, app requests are not touched, and a
// second run changes nothing.
func TestMemoryStore_MigrateLegacyWebRequests(t *testing.T) {
	s := NewMemoryStore()
	defer func() { _ = s.Close(context.Background()) }()
	ctx := context.Background()
	now := time.Now().UTC()

	// A pending legacy web request without an order.
	legacy := &models.PurchaseRequest{
		ID: "legacy-web-1", UserID: "u-legacy-web", SubjectID: "s-legacy-web-1",
		Status: models.RequestStatusPending, Source: models.OrderSourceWeb,
		PaymentMethod: "instapay", TransferReference: "legacy-ref-1",
		NormalizedReference: "LEGACY-REF-1", SenderNumber: "01099999999",
		Amount: 250, CreatedAt: now,
	}
	if err := s.CreatePurchaseRequest(ctx, legacy); err != nil {
		t.Fatalf("CreatePurchaseRequest: %v", err)
	}

	// A pending web request that already belongs to a web order.
	attached := &models.PurchaseRequest{
		ID: "legacy-web-attached", UserID: "u-legacy-web", SubjectID: "s-legacy-web-2",
		Status: models.RequestStatusPending, Source: models.OrderSourceWeb,
		PaymentMethod: "wallet", TransferReference: "attached-ref",
		NormalizedReference: "ATTACHED-REF", SenderNumber: "01011111111",
		Amount: 100, CreatedAt: now,
	}
	if err := s.CreateOrderWithRequests(ctx, &models.Order{
		ID: "existing-web-order", UserID: "u-legacy-web",
		Source: models.OrderSourceWeb, Status: models.OrderStatusOpen,
		NormalizedReference: "ATTACHED-REF", AmountPaid: 100, CreatedAt: now,
	}, []*models.PurchaseRequest{attached}); err != nil {
		t.Fatalf("CreateOrderWithRequests: %v", err)
	}

	// Accepted/rejected history must never get an order.
	rejected := &models.PurchaseRequest{
		ID: "legacy-web-rejected", UserID: "u-legacy-history", SubjectID: "s-legacy-web-3",
		Status: models.RequestStatusRejected, Source: models.OrderSourceWeb,
		PaymentMethod: "wallet", TransferReference: "history-ref",
		NormalizedReference: "HISTORY-REF", SenderNumber: "01022222222",
		Amount: 90, CreatedAt: now,
	}
	if err := s.CreatePurchaseRequest(ctx, rejected); err != nil {
		t.Fatalf("CreatePurchaseRequest(rejected): %v", err)
	}

	// A pending app request is the app migration's business, not this one.
	appReq := &models.PurchaseRequest{
		ID: "legacy-app-1", UserID: "u-legacy-app", SubjectID: "s-legacy-app-1",
		Status: models.RequestStatusPending, Source: "mobile", CreatedAt: now,
	}
	if err := s.CreatePurchaseRequest(ctx, appReq); err != nil {
		t.Fatalf("CreatePurchaseRequest(app): %v", err)
	}

	if err := s.MigrateLegacyWebRequests(ctx); err != nil {
		t.Fatalf("MigrateLegacyWebRequests: %v", err)
	}

	// The legacy pending web request now has its own web order of one.
	migrated, err := s.GetRequestByID(ctx, legacy.ID)
	if err != nil || migrated == nil {
		t.Fatalf("GetRequestByID: %v %v", migrated, err)
	}
	if migrated.OrderID == "" {
		t.Fatalf("legacy pending web request was not attached to an order")
	}
	order, err := s.GetOrderByID(ctx, migrated.OrderID)
	if err != nil || order == nil {
		t.Fatalf("GetOrderByID: %v %v", order, err)
	}
	if order.Source != models.OrderSourceWeb || order.Status != models.OrderStatusOpen {
		t.Fatalf("migrated order = %+v, want web/open", order)
	}
	if order.UserID != "u-legacy-web" || order.AmountPaid != 250 {
		t.Fatalf("migrated order user/amount = %q/%d, want u-legacy-web/250", order.UserID, order.AmountPaid)
	}
	if order.PaymentMethod != "instapay" || order.TransferReference != "legacy-ref-1" ||
		order.NormalizedReference != "LEGACY-REF-1" || order.SenderNumber != "01099999999" {
		t.Fatalf("migrated order proof fields not copied: %+v", order)
	}
	reqs, err := s.ListRequestsByOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("ListRequestsByOrder: %v", err)
	}
	if len(reqs) != 1 || reqs[0].ID != legacy.ID {
		t.Fatalf("migrated order holds %d request(s), want the legacy one", len(reqs))
	}

	// The already-attached request keeps its order and gains no second one.
	stillAttached, err := s.GetRequestByID(ctx, attached.ID)
	if err != nil || stillAttached == nil {
		t.Fatalf("GetRequestByID(attached): %v %v", stillAttached, err)
	}
	if stillAttached.OrderID != "existing-web-order" {
		t.Fatalf("attached request order_id = %q, want existing-web-order", stillAttached.OrderID)
	}
	userOrders, err := s.ListOrdersByUser(ctx, "u-legacy-web")
	if err != nil {
		t.Fatalf("ListOrdersByUser: %v", err)
	}
	if len(userOrders) != 2 {
		t.Fatalf("u-legacy-web orders = %d, want 2 (existing + migrated)", len(userOrders))
	}

	// History and app rows are untouched.
	history, err := s.GetRequestByID(ctx, rejected.ID)
	if err != nil || history == nil {
		t.Fatalf("GetRequestByID(history): %v %v", history, err)
	}
	if history.OrderID != "" {
		t.Fatalf("rejected history got an order: %q", history.OrderID)
	}
	historyOrders, err := s.ListOrdersByUser(ctx, "u-legacy-history")
	if err != nil {
		t.Fatalf("ListOrdersByUser(history): %v", err)
	}
	if len(historyOrders) != 0 {
		t.Fatalf("history user got %d order(s), want 0", len(historyOrders))
	}
	appRow, err := s.GetRequestByID(ctx, appReq.ID)
	if err != nil || appRow == nil {
		t.Fatalf("GetRequestByID(app): %v %v", appRow, err)
	}
	if appRow.OrderID != "" {
		t.Fatalf("app request was wrapped into a web order: %q", appRow.OrderID)
	}
	appOrders, err := s.ListOrdersByUser(ctx, "u-legacy-app")
	if err != nil {
		t.Fatalf("ListOrdersByUser(app): %v", err)
	}
	if len(appOrders) != 0 {
		t.Fatalf("app user got %d order(s), want 0", len(appOrders))
	}

	// Idempotent: a second run creates nothing.
	before, err := s.ListOrdersByUser(ctx, "u-legacy-web")
	if err != nil {
		t.Fatalf("ListOrdersByUser: %v", err)
	}
	if err := s.MigrateLegacyWebRequests(ctx); err != nil {
		t.Fatalf("second MigrateLegacyWebRequests: %v", err)
	}
	after, err := s.ListOrdersByUser(ctx, "u-legacy-web")
	if err != nil {
		t.Fatalf("ListOrdersByUser: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("second migration changed the order count: %d -> %d", len(before), len(after))
	}
}
