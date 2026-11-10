package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
)

// TestDuplicateKey_RaceMapping proves that racing requests on MongoDB correctly distinguish
// duplicate transfer references from duplicate pending requests:
// 1. Cart checkout race on pending request -> returns 409 pending_request_exists (not duplicate_reference).
// 2. Cart checkout race on transfer reference -> returns 409 duplicate_reference.
// 3. Single purchase request race on pending request -> returns 409 pending_request_exists (not duplicate_reference).
// 4. Single purchase request race on transfer reference -> returns 409 duplicate_reference.
func TestDuplicateKey_RaceMapping(t *testing.T) {
	mongoURI := requireMongoURI(t)
	dbName := randomMongoDatabase("test_dup_race_mapping")
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

	s := New(ms, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", false, "+201000000000")

	// Seed two published subjects
	subj1 := createTestSubject(t, s, "bachelor-y1", 100, time.Now().Add(48*time.Hour))
	subj2 := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(48*time.Hour))

	// 1. Cart checkout race on pending request:
	// Same student submits two carts for subj1 simultaneously, but with DIFFERENT transfer references.
	// One must succeed (201), the other must fail with pending_request_exists (NOT duplicate_reference).
	t.Run("cart checkout race on pending request yields pending_request_exists", func(t *testing.T) {
		studentID := "stu-cart-pending"
		token := makeWebStudentToken(t, studentID)

		var wg sync.WaitGroup
		var rec1, rec2 *httptest.ResponseRecorder

		wg.Add(2)
		go func() {
			defer wg.Done()
			rec1 = doOrder(t, s, token, orderBody([]string{subj1.ID}, "REF-PENDING-A", 100))
		}()
		go func() {
			defer wg.Done()
			rec2 = doOrder(t, s, token, orderBody([]string{subj1.ID}, "REF-PENDING-B", 100))
		}()
		wg.Wait()

		var successRec, failRec *httptest.ResponseRecorder
		if rec1.Code == http.StatusCreated {
			successRec, failRec = rec1, rec2
		} else {
			successRec, failRec = rec2, rec1
		}

		if successRec.Code != http.StatusCreated {
			t.Fatalf("neither cart checkout succeeded: rec1=%d (%s), rec2=%d (%s)",
				rec1.Code, rec1.Body.String(), rec2.Code, rec2.Body.String())
		}
		if failRec.Code != http.StatusConflict {
			t.Fatalf("racing cart checkout status = %d, want 409: %s", failRec.Code, failRec.Body.String())
		}

		var errRes PurchaseRequestErrorResponse
		if err := json.Unmarshal(failRec.Body.Bytes(), &errRes); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if errRes.Code != "pending_request_exists" {
			t.Errorf("racing cart checkout error code = %q, want 'pending_request_exists'", errRes.Code)
		}
	})

	// 2. Single purchase request race on pending request:
	// Same student submits two purchase requests for subj2 simultaneously with DIFFERENT transfer references.
	// One must succeed (201), the other must fail with pending_request_exists (NOT duplicate_reference).
	t.Run("single request race on pending request yields pending_request_exists", func(t *testing.T) {
		studentID := "stu-single-pending"
		token := makeWebStudentToken(t, studentID)

		var wg sync.WaitGroup
		var rec1, rec2 *httptest.ResponseRecorder

		wg.Add(2)
		go func() {
			defer wg.Done()
			rec1 = doPurchaseRequest(t, s, token, subj2.ID, map[string]any{
				"payment_method":     "wallet",
				"transfer_reference": "REF-SINGLE-A",
				"sender_number":      "01012345678",
				"amount":             150,
			})
		}()
		go func() {
			defer wg.Done()
			rec2 = doPurchaseRequest(t, s, token, subj2.ID, map[string]any{
				"payment_method":     "wallet",
				"transfer_reference": "REF-SINGLE-B",
				"sender_number":      "01012345678",
				"amount":             150,
			})
		}()
		wg.Wait()

		var successRec, failRec *httptest.ResponseRecorder
		if rec1.Code == http.StatusCreated {
			successRec, failRec = rec1, rec2
		} else {
			successRec, failRec = rec2, rec1
		}

		if successRec.Code != http.StatusCreated {
			t.Fatalf("neither single request succeeded: rec1=%d (%s), rec2=%d (%s)",
				rec1.Code, rec1.Body.String(), rec2.Code, rec2.Body.String())
		}
		if failRec.Code != http.StatusConflict {
			t.Fatalf("racing single request status = %d, want 409: %s", failRec.Code, failRec.Body.String())
		}

		var errRes PurchaseRequestErrorResponse
		if err := json.Unmarshal(failRec.Body.Bytes(), &errRes); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if errRes.Code != "pending_request_exists" {
			t.Errorf("racing single request error code = %q, want 'pending_request_exists'", errRes.Code)
		}
	})

	// 3. Cart checkout race on duplicate transfer reference:
	// Two different students checkout different subjects simultaneously with the SAME transfer reference.
	// One must succeed (201), the other must fail with duplicate_reference.
	t.Run("cart checkout race on duplicate reference yields duplicate_reference", func(t *testing.T) {
		tokenA := makeWebStudentToken(t, "stu-cart-ref-a")
		tokenB := makeWebStudentToken(t, "stu-cart-ref-b")

		var wg sync.WaitGroup
		var rec1, rec2 *httptest.ResponseRecorder

		wg.Add(2)
		go func() {
			defer wg.Done()
			rec1 = doOrder(t, s, tokenA, orderBody([]string{subj1.ID}, "REF-SHARED-CART", 100))
		}()
		go func() {
			defer wg.Done()
			rec2 = doOrder(t, s, tokenB, orderBody([]string{subj2.ID}, "REF-SHARED-CART", 150))
		}()
		wg.Wait()

		var successRec, failRec *httptest.ResponseRecorder
		if rec1.Code == http.StatusCreated {
			successRec, failRec = rec1, rec2
		} else {
			successRec, failRec = rec2, rec1
		}

		if successRec.Code != http.StatusCreated {
			t.Fatalf("neither cart checkout succeeded: rec1=%d (%s), rec2=%d (%s)",
				rec1.Code, rec1.Body.String(), rec2.Code, rec2.Body.String())
		}
		if failRec.Code != http.StatusConflict {
			t.Fatalf("racing cart checkout status = %d, want 409: %s", failRec.Code, failRec.Body.String())
		}

		var errRes PurchaseRequestErrorResponse
		if err := json.Unmarshal(failRec.Body.Bytes(), &errRes); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if errRes.Code != "duplicate_reference" {
			t.Errorf("racing cart checkout error code = %q, want 'duplicate_reference'", errRes.Code)
		}
	})

	// 4. Single request race on duplicate transfer reference:
	// Two different students submit single purchase requests for different subjects with the SAME transfer reference.
	// One must succeed (201), the other must fail with duplicate_reference.
	t.Run("single request race on duplicate reference yields duplicate_reference", func(t *testing.T) {
		tokenA := makeWebStudentToken(t, "stu-single-ref-a")
		tokenB := makeWebStudentToken(t, "stu-single-ref-b")

		var wg sync.WaitGroup
		var rec1, rec2 *httptest.ResponseRecorder

		wg.Add(2)
		go func() {
			defer wg.Done()
			rec1 = doPurchaseRequest(t, s, tokenA, subj1.ID, map[string]any{
				"payment_method":     "wallet",
				"transfer_reference": "REF-SHARED-SINGLE",
				"sender_number":      "01011111111",
				"amount":             100,
			})
		}()
		go func() {
			defer wg.Done()
			rec2 = doPurchaseRequest(t, s, tokenB, subj2.ID, map[string]any{
				"payment_method":     "wallet",
				"transfer_reference": "REF-SHARED-SINGLE",
				"sender_number":      "01022222222",
				"amount":             150,
			})
		}()
		wg.Wait()

		var successRec, failRec *httptest.ResponseRecorder
		if rec1.Code == http.StatusCreated {
			successRec, failRec = rec1, rec2
		} else {
			successRec, failRec = rec2, rec1
		}

		if successRec.Code != http.StatusCreated {
			t.Fatalf("neither single request succeeded: rec1=%d (%s), rec2=%d (%s)",
				rec1.Code, rec1.Body.String(), rec2.Code, rec2.Body.String())
		}
		if failRec.Code != http.StatusConflict {
			t.Fatalf("racing single request status = %d, want 409: %s", failRec.Code, failRec.Body.String())
		}

		var errRes PurchaseRequestErrorResponse
		if err := json.Unmarshal(failRec.Body.Bytes(), &errRes); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if errRes.Code != "duplicate_reference" {
			t.Errorf("racing single request error code = %q, want 'duplicate_reference'", errRes.Code)
		}
	})
}
