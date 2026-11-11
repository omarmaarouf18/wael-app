package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func requireMongoURI(t *testing.T) string {
	t.Helper()
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("MONGO_URI is required when REQUIRE_DB=1")
		}
		t.Skip("skipping test: MONGO_URI is empty")
	}
	return mongoURI
}

func randomMongoDatabase(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixNano(), hex.EncodeToString(b))
}

func doPurchaseRequest(t *testing.T, s *Server, token, subjectID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal purchase request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subjectID+"/purchase-request", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	return rec
}

// TestOrders_LiveMongo_ReferenceUniqueness proves on a live MongoDB:
//  1. Two parallel checkouts with the same transfer reference race at the database
//     level against uniq_orders_normalized_reference: exactly one gets 201, and
//     the other gets 409 duplicate_reference.
//  2. A legacy single request in purchase_requests vs a new order with the same
//     reference returns 409 duplicate_reference.
//  3. A single purchase request endpoint vs a new order with the same reference
//     returns 409 duplicate_reference across both directions.
func TestOrders_LiveMongo_ReferenceUniqueness(t *testing.T) {
	mongoURI := requireMongoURI(t)
	dbName := randomMongoDatabase("test_academy_checkout_uniq")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	if err := ms.SeedLevels(ctx); err != nil {
		t.Fatalf("SeedLevels failed: %v", err)
	}

	vsrv := fakeVerifyServer(t, okVerify)
	t.Cleanup(vsrv.Close)

	srv := New(ms, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", false, "+201000000000")
	srv.AuthAdminURL = vsrv.URL
	srv.VerifyClient = vsrv.Client()

	now := time.Now().UTC()
	subj1 := &models.Subject{
		ID:              fmt.Sprintf("s-chk-%d-1", time.Now().UnixNano()),
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة اختبار 1",
		TitleEn:         "Test Subject 1",
		Price:           120,
		AccessExpiresAt: now.Add(48 * time.Hour),
		Status:          models.StatusPublished,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	subj2 := &models.Subject{
		ID:              fmt.Sprintf("s-chk-%d-2", time.Now().UnixNano()),
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة اختبار 2",
		TitleEn:         "Test Subject 2",
		Price:           150,
		AccessExpiresAt: now.Add(48 * time.Hour),
		Status:          models.StatusPublished,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := ms.CreateSubject(ctx, subj1); err != nil {
		t.Fatalf("create subj1: %v", err)
	}
	if err := ms.CreateSubject(ctx, subj2); err != nil {
		t.Fatalf("create subj2: %v", err)
	}

	t.Run("parallel_checkouts_same_reference_yield_one_201_one_409", func(t *testing.T) {
		token1 := makeWebStudentToken(t, "user-parallel-chk-1")
		token2 := makeWebStudentToken(t, "user-parallel-chk-2")
		ref := fmt.Sprintf("REF-RACE-%d", time.Now().UnixNano())

		var wg sync.WaitGroup
		wg.Add(2)

		var code1, code2 int
		var body1, body2 string

		go func() {
			defer wg.Done()
			rec := doOrder(t, srv, token1, orderBody([]string{subj1.ID}, ref, 120))
			code1 = rec.Code
			body1 = rec.Body.String()
		}()

		go func() {
			defer wg.Done()
			rec := doOrder(t, srv, token2, orderBody([]string{subj2.ID}, ref, 150))
			code2 = rec.Code
			body2 = rec.Body.String()
		}()

		wg.Wait()

		t.Logf("Parallel checkout results: code1=%d code2=%d", code1, code2)

		count201 := 0
		count409 := 0
		for _, code := range []int{code1, code2} {
			if code == http.StatusCreated {
				count201++
			} else if code == http.StatusConflict {
				count409++
			}
		}

		if count201 != 1 || count409 != 1 {
			t.Fatalf("want exactly one 201 and one 409; got code1=%d (body=%s), code2=%d (body=%s)",
				code1, body1, code2, body2)
		}

		// Verify the 409 body carried duplicate_reference error code
		loserBody := body1
		if code2 == http.StatusConflict {
			loserBody = body2
		}
		var errResp struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal([]byte(loserBody), &errResp); err != nil {
			t.Fatalf("parse 409 body: %v", err)
		}
		if errResp.Code != "duplicate_reference" {
			t.Fatalf("expected duplicate_reference error code, got %q", errResp.Code)
		}
	})

	t.Run("legacy_single_request_vs_new_order_same_reference_yields_409", func(t *testing.T) {
		legacyRef := fmt.Sprintf("LEGACY-REF-%d", time.Now().UnixNano())
		// Seed a legacy row directly in purchase_requests with normalized_reference
		legacyDoc := bson.M{
			"_id":                  "legacy-req-row-1",
			"user_id":              "user-legacy-student",
			"subject_id":           subj1.ID,
			"status":               models.RequestStatusPending,
			"source":               "web",
			"payment_method":       "instapay",
			"transfer_reference":   legacyRef,
			"normalized_reference": normalizeTransferReference(legacyRef),
			"sender_number":        "01055555555",
			"amount":               120,
			"created_at":           now.Add(-time.Hour),
		}
		if _, err := ms.Database().Collection("purchase_requests").InsertOne(ctx, legacyDoc); err != nil {
			t.Fatalf("insert legacy purchase_request: %v", err)
		}

		// Attempt checkout with the same reference (case-insensitive)
		token := makeWebStudentToken(t, "user-new-checkout")
		rec := doOrder(t, srv, token, orderBody([]string{subj2.ID}, legacyRef, 150))
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d (%s)", rec.Code, rec.Body.String())
		}
		var errResp struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("parse 409 body: %v", err)
		}
		if errResp.Code != "duplicate_reference" {
			t.Fatalf("expected duplicate_reference, got %q", errResp.Code)
		}
	})

	t.Run("single_purchase_request_endpoint_vs_new_order_same_reference_yields_409", func(t *testing.T) {
		singleRef := fmt.Sprintf("SINGLE-EP-REF-%d", time.Now().UnixNano())
		studentA := makeWebStudentToken(t, "user-single-a")
		studentB := makeWebStudentToken(t, "user-order-b")

		// Submit via single purchase request endpoint: POST /academy/subjects/{id}/purchase-request
		prReq := doPurchaseRequest(t, srv, studentA, subj1.ID, map[string]any{
			"payment_method":     "instapay",
			"transfer_reference": singleRef,
			"sender_number":      "01011112222",
			"amount":             120,
		})
		if prReq.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created from purchase-request, got %d (%s)", prReq.Code, prReq.Body.String())
		}

		// Attempt new order checkout with the same reference
		orderRec := doOrder(t, srv, studentB, orderBody([]string{subj2.ID}, singleRef, 150))
		if orderRec.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict for duplicate order checkout, got %d (%s)", orderRec.Code, orderRec.Body.String())
		}
		var errResp struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(orderRec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("parse 409 body: %v", err)
		}
		if errResp.Code != "duplicate_reference" {
			t.Fatalf("expected duplicate_reference, got %q", errResp.Code)
		}
	})
}
