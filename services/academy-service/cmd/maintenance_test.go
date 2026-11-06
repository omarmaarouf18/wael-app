package main

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
)

func TestCleanupOrphanWebOrders_RemovesAndLogs(t *testing.T) {
	st := store.NewMemoryStore()
	defer func() { _ = st.Close(context.Background()) }()
	ctx := context.Background()
	now := time.Now().UTC()

	if err := st.CreateOrderWithRequests(ctx, &models.Order{
		ID: "ord-cleanup-orphan", UserID: "u-cleanup",
		Source: models.OrderSourceWeb, Status: models.OrderStatusOpen,
		CreatedAt: now.Add(-10 * time.Minute),
	}, nil); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}
	if err := st.CreateOrderWithRequests(ctx, &models.Order{
		ID: "ord-cleanup-young", UserID: "u-cleanup",
		Source: models.OrderSourceWeb, Status: models.OrderStatusOpen,
		CreatedAt: now,
	}, nil); err != nil {
		t.Fatalf("seed young: %v", err)
	}

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	if n := cleanupOrphanWebOrders(ctx, st, 5*time.Minute); n != 1 {
		t.Fatalf("cleanup removed %d, want 1", n)
	}
	if got := buf.String(); !strings.Contains(got, "removed 1 orphan web order(s)") {
		t.Fatalf("expected removal log line, got %q", got)
	}
	if order, _ := st.GetOrderByID(ctx, "ord-cleanup-orphan"); order != nil {
		t.Fatalf("old orphan still present")
	}
	if order, _ := st.GetOrderByID(ctx, "ord-cleanup-young"); order == nil {
		t.Fatalf("young order was removed")
	}

	// Nothing left to remove: the helper stays quiet.
	buf.Reset()
	if n := cleanupOrphanWebOrders(ctx, st, 5*time.Minute); n != 0 {
		t.Fatalf("second cleanup removed %d, want 0", n)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no log on a no-op cleanup, got %q", buf.String())
	}
}
