package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
)

// TestAdminBulkRequests_MongoStore_SlowPushServer verifies that bulk accept/reject
// on a live MongoStore handles slow notification pushes without starving subsequent items.
// Each item gets its own DB timeout, and pushes run asynchronously without blocking the DB budget.
func TestAdminBulkRequests_MongoStore_SlowPushServer(t *testing.T) {
	mongoURI := requireMongoURI(t)
	dbName := randomMongoDatabase("test_bulk_slow_push")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ms, err := store.NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = ms.Database().Drop(dropCtx)
		_ = ms.Close(dropCtx)
	})

	// Slow push server: sleeps 2 seconds per push
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer slowServer.Close()

	vsrv := fakeVerifyServer(t, okVerify)
	defer vsrv.Close()

	s := New(ms, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", false, "+201000000000")
	s.AuthAdminURL = vsrv.URL
	s.VerifyClient = vsrv.Client()
	s.NotifyURL = slowServer.URL
	s.NotifyToken = "test-notify-token"

	// Seed subject
	subj := &models.Subject{
		ID:              "subj-bulk-slow",
		TitleAr:         "مادة الاختبار",
		TitleEn:         "Test Subject",
		LevelKey:        "secondary-y1",
		Status:          models.StatusPublished,
		Price:           100,
		AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := ms.CreateSubject(ctx, subj); err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	// Seed 5 pending requests for 5 different students
	reqIDs := []string{"req-slow-1", "req-slow-2", "req-slow-3", "req-slow-4", "req-slow-5"}
	for i, id := range reqIDs {
		pr := &models.PurchaseRequest{
			ID:        id,
			UserID:    "student-" + id,
			SubjectID: subj.ID,
			Status:    models.RequestStatusPending,
			Source:    "web",
			Amount:    100,
			CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Minute),
		}
		if _, _, err := ms.CreateOrGetPendingRequest(ctx, pr); err != nil {
			t.Fatalf("CreateOrGetPendingRequest %s: %v", id, err)
		}
	}

	// Send bulk accept for all 5 requests
	body, _ := json.Marshal(map[string]any{
		"action": "accept",
		"ids":    reqIDs,
	})
	req := httptest.NewRequest(http.MethodPost, "/internal/admin/requests/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", "test-internal-token")
	req.Header.Set("X-Admin-Token", "adm-token")

	rec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Bulk accept status = %d, want 200. Body: %s", rec.Code, rec.Body.String())
	}

	var resp models.BulkRequestsResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// Verify all items succeeded
	for _, res := range resp.Results {
		if res.Status != "accepted" {
			t.Errorf("item %s status = %q (code: %s, err: %s), want 'accepted'", res.ID, res.Status, res.ErrorCode, res.MessageEn)
		}
	}

	// Verify all 5 requests in MongoStore are accepted and none left pending
	for _, id := range reqIDs {
		got, err := ms.GetRequestByID(ctx, id)
		if err != nil {
			t.Fatalf("GetRequestByID %s: %v", id, err)
		}
		if got.Status != models.RequestStatusAccepted {
			t.Errorf("request %s in DB has status = %s, want accepted", id, got.Status)
		}
		ent, err := ms.GetActiveEntitlement(ctx, got.UserID, subj.ID)
		if err != nil || ent == nil {
			t.Errorf("request %s has no active entitlement: %v", id, err)
		}
	}
}
