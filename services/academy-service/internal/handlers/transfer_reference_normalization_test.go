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
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// TestTransferReference_NormalizationVariants_Item2_4 verifies item 2.4:
// Transfer reference normalization must strip whitespace, dashes, underscores, separators
// and convert Arabic-Indic digits to ASCII before ToUpper, so that variations like
// "AB 123", "ab-123", "ab_123", and "ab-١٢٣" are recognized as the same reference.
func TestTransferReference_NormalizationVariants_Item2_4(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"AB 123", "AB123"},
		{"ab-123", "AB123"},
		{"ab_123", "AB123"},
		{"ab/123", "AB123"},
		{"AB ١٢٣", "AB123"},
		{"ab-١٢٣", "AB123"},
		{"INSTA-987-XYZ", "INSTA987XYZ"},
		{"insta_٩٨٧_xyz", "INSTA987XYZ"},
	}

	for _, tc := range cases {
		got := normalizeTransferReference(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeTransferReference(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// TestOrders_TransferReference_RejectsVariants verifies that creating an order
// with "TXN 123" prevents subsequent orders with "txn-123" or "txn ١٢٣".
func TestOrders_TransferReference_RejectsVariants(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	memStore := store.NewMemoryStore()
	_, tl := setupTestLimiter(t, 100, 100, 100, 100)
	s := &Server{
		Store:         memStore,
		GatewaySecret: "test-gw-secret",
		Limiter:       tl,
	}

	ctx := context.Background()
	subjID := "subj-math-101"
	_ = memStore.CreateSubject(ctx, &models.Subject{
		ID:       subjID,
		TitleAr:  "Math 101",
		Price:    150,
		Status:   models.StatusPublished,
		LevelKey: "bachelor_1",
	})

	tok1, err := jwtutil.GenerateTokenWithSessionAndClient("user-ref-test1", "student", "ref1@example.com", "sid-1", "web")
	if err != nil {
		t.Fatal(err)
	}
	tok2, err := jwtutil.GenerateTokenWithSessionAndClient("user-ref-test2", "student", "ref2@example.com", "sid-2", "web")
	if err != nil {
		t.Fatal(err)
	}

	handler := s.PublicHandler()

	createOrder := func(tok, ref string) int {
		body := map[string]any{
			"subject_ids":        []string{subjID},
			"payment_method":     "instapay",
			"sender_number":      "01012345678",
			"transfer_reference": ref,
			"amount_paid":        150,
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/academy/orders", bytes.NewReader(bodyBytes))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("X-Gateway-Secret", "test-gw-secret")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// 1. First order with spaced reference succeeds
	if code := createOrder(tok1, "TXN 123"); code != http.StatusCreated {
		t.Fatalf("first order with 'TXN 123' expected 201, got %d", code)
	}

	// 2. Second order with hyphen variant "txn-123" by student 2 must be rejected as duplicate (409)
	if code := createOrder(tok2, "txn-123"); code != http.StatusConflict {
		t.Errorf("second order with 'txn-123' expected 409 conflict, got %d", code)
	}

	// 3. Third order with Arabic-Indic digits "txn ١٢٣" by student 2 must be rejected as duplicate (409)
	if code := createOrder(tok2, "txn ١٢٣"); code != http.StatusConflict {
		t.Errorf("third order with 'txn ١٢٣' expected 409 conflict, got %d", code)
	}
}

// TestPurchaseRequests_TransferReference_RejectsVariants verifies that creating a single
// purchase request with "TXN 456" prevents subsequent requests with "txn-456" or "txn ٤٥٦".
func TestPurchaseRequests_TransferReference_RejectsVariants(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	s := newTestServer(true)
	_, tl := setupTestLimiter(t, 100, 100, 100, 100)
	s.Limiter = tl
	handler := s.PublicHandler()

	subj := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(24*time.Hour))
	token1 := makeWebStudentToken(t, "user-pr-dup1")
	token2 := makeWebStudentToken(t, "user-pr-dup2")

	createPR := func(token, ref string) int {
		body := map[string]any{
			"payment_method":     "instapay",
			"transfer_reference": ref,
			"sender_number":      "01012345678",
			"amount":             150,
		}
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subj.ID+"/purchase-request", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// 1. First request with "TXN 456" succeeds
	if code := createPR(token1, "TXN 456"); code != http.StatusCreated {
		t.Fatalf("first purchase request expected 201, got %d", code)
	}

	// 2. Second request with "txn-456" rejected with 409
	if code := createPR(token2, "txn-456"); code != http.StatusConflict {
		t.Errorf("second purchase request with 'txn-456' expected 409, got %d", code)
	}

	// 3. Third request with Arabic-Indic "txn ٤٥٦" rejected with 409
	if code := createPR(token2, "txn ٤٥٦"); code != http.StatusConflict {
		t.Errorf("third purchase request with 'txn ٤٥٦' expected 409, got %d", code)
	}
}
