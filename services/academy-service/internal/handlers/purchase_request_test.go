package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func TestCreatePurchaseRequest_WebSuccess(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	s.ExposePrice = true
	handler := s.PublicHandler()

	subj := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(24*time.Hour))
	token := makeWebStudentToken(t, "user-web-1")

	body := map[string]any{
		"payment_method":     "instapay",
		"transfer_reference": "TXN12345678",
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

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var res models.PurchaseRequestResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res.ID == "" {
		t.Errorf("expected non-empty ID")
	}
	if res.SubjectID != subj.ID {
		t.Errorf("expected subject_id %q, got %q", subj.ID, res.SubjectID)
	}
	if res.Status != "pending" {
		t.Errorf("expected status pending, got %q", res.Status)
	}
	if res.Source != "web" {
		t.Errorf("expected source web, got %q", res.Source)
	}
	if res.Amount != 150 {
		t.Errorf("expected amount 150, got %d", res.Amount)
	}

	// Verify in admin queue
	adminReq := httptest.NewRequest(http.MethodGet, "/internal/admin/requests?status=pending", nil)
	adminReq.Header.Set("X-Internal-Token", "test-internal-token")
	adminReq.Header.Set("X-Admin-Token", "adm-token")
	adminReq = adminReq.WithContext(context.WithValue(adminReq.Context(), adminIdentityContextKey, &AdminIdentity{ID: "adm-1", Name: "Admin"}))
	adminRec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(adminRec, adminReq)

	if adminRec.Code != http.StatusOK {
		t.Fatalf("expected admin requests 200, got %d", adminRec.Code)
	}
	var adminRes RequestsListResponseDTO
	_ = json.Unmarshal(adminRec.Body.Bytes(), &adminRes)
	if len(adminRes.Items) != 1 {
		t.Fatalf("expected 1 admin request item, got %d", len(adminRes.Items))
	}
	item := adminRes.Items[0]
	if item.Source != "web" {
		t.Errorf("expected item.Source web, got %q", item.Source)
	}
	if item.PaymentMethod != "instapay" {
		t.Errorf("expected item.PaymentMethod instapay, got %q", item.PaymentMethod)
	}
	if item.TransferReference != "TXN12345678" {
		t.Errorf("expected item.TransferReference TXN12345678, got %q", item.TransferReference)
	}
	if item.SenderNumber != "01012345678" {
		t.Errorf("expected item.SenderNumber 01012345678, got %q", item.SenderNumber)
	}
	if item.Amount != 150 {
		t.Errorf("expected item.Amount 150, got %d", item.Amount)
	}
	if item.AmountWarning {
		t.Errorf("expected AmountWarning to be false when amount matches price")
	}
}

func TestCreatePurchaseRequest_AmountWarning(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	s.ExposePrice = true
	handler := s.PublicHandler()

	subj := createTestSubject(t, s, "bachelor-y1", 200, time.Now().Add(24*time.Hour))
	token := makeWebStudentToken(t, "user-web-warning")

	body := map[string]any{
		"payment_method":     "wallet",
		"transfer_reference": "TXN_DIFF_AMOUNT",
		"sender_number":      "01012345678",
		"amount":             100, // differs from subject price 200
	}
	raw, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subj.ID+"/purchase-request", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify in admin queue that AmountWarning is true
	adminReq := httptest.NewRequest(http.MethodGet, "/internal/admin/requests?status=pending", nil)
	adminReq.Header.Set("X-Internal-Token", "test-internal-token")
	adminReq.Header.Set("X-Admin-Token", "adm-token")
	adminReq = adminReq.WithContext(context.WithValue(adminReq.Context(), adminIdentityContextKey, &AdminIdentity{ID: "adm-1", Name: "Admin"}))
	adminRec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(adminRec, adminReq)

	var adminRes RequestsListResponseDTO
	_ = json.Unmarshal(adminRec.Body.Bytes(), &adminRes)
	if len(adminRes.Items) != 1 {
		t.Fatalf("expected 1 admin request item, got %d", len(adminRes.Items))
	}
	if !adminRes.Items[0].AmountWarning {
		t.Errorf("expected AmountWarning to be true when req.Amount (100) != price (200)")
	}
}

func TestCreatePurchaseRequest_NonWebForbidden(t *testing.T) {
	s := newTestServer(true)
	handler := s.PublicHandler()

	subj := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(24*time.Hour))
	token := makeStudentToken(t, "user-mobile-1") // client claim is empty (mobile)

	body := map[string]any{
		"payment_method":     "instapay",
		"transfer_reference": "TXN_MOBILE",
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

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-web client, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreatePurchaseRequest_DisallowUnknownFields(t *testing.T) {
	s := newTestServer(true)
	handler := s.PublicHandler()

	subj := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(24*time.Hour))
	token := makeWebStudentToken(t, "user-web-extra")

	body := map[string]any{
		"payment_method":     "instapay",
		"transfer_reference": "TXN_EXTRA",
		"sender_number":      "01012345678",
		"amount":             150,
		"receipt_image":      "https://example.com/receipt.jpg", // forbidden unknown field
	}
	raw, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subj.ID+"/purchase-request", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for unknown field, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreatePurchaseRequest_DuplicateReference(t *testing.T) {
	s := newTestServer(true)
	handler := s.PublicHandler()

	subj := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(24*time.Hour))
	token1 := makeWebStudentToken(t, "user-web-dup1")
	token2 := makeWebStudentToken(t, "user-web-dup2")

	body1 := map[string]any{
		"payment_method":     "instapay",
		"transfer_reference": "SAME_REF_123",
		"sender_number":      "01012345678",
		"amount":             150,
	}
	raw1, _ := json.Marshal(body1)

	req1 := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subj.ID+"/purchase-request", bytes.NewReader(raw1))
	req1.Header.Set("Authorization", "Bearer "+token1)
	req1.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first request expected 201, got %d", rec1.Code)
	}

	// Second request with case-insensitive duplicate reference "same_ref_123"
	body2 := map[string]any{
		"payment_method":     "wallet",
		"transfer_reference": "same_ref_123",
		"sender_number":      "01099999999",
		"amount":             150,
	}
	raw2, _ := json.Marshal(body2)

	req2 := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subj.ID+"/purchase-request", bytes.NewReader(raw2))
	req2.Header.Set("Authorization", "Bearer "+token2)
	req2.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for duplicate reference, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var errRes PurchaseRequestErrorResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &errRes)
	if errRes.Code != "duplicate_reference" {
		t.Errorf("expected code duplicate_reference, got %q", errRes.Code)
	}
	if errRes.MessageAr == "" || errRes.MessageEn == "" {
		t.Errorf("expected non-empty ar and en error messages, got ar=%q en=%q", errRes.MessageAr, errRes.MessageEn)
	}
}

func TestCreatePurchaseRequest_PendingRequestExists(t *testing.T) {
	s := newTestServer(true)
	handler := s.PublicHandler()

	subj := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(24*time.Hour))
	token := makeWebStudentToken(t, "user-web-pending")

	body1 := map[string]any{
		"payment_method":     "instapay",
		"transfer_reference": "REF_ONE_1",
		"sender_number":      "01012345678",
		"amount":             150,
	}
	raw1, _ := json.Marshal(body1)

	req1 := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subj.ID+"/purchase-request", bytes.NewReader(raw1))
	req1.Header.Set("Authorization", "Bearer "+token)
	req1.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first request expected 201, got %d", rec1.Code)
	}

	// Try to submit another request for the same subject while pending
	body2 := map[string]any{
		"payment_method":     "instapay",
		"transfer_reference": "REF_TWO_2",
		"sender_number":      "01012345678",
		"amount":             150,
	}
	raw2, _ := json.Marshal(body2)

	req2 := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subj.ID+"/purchase-request", bytes.NewReader(raw2))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for existing pending request, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var errRes PurchaseRequestErrorResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &errRes)
	if errRes.Code != "pending_request_exists" {
		t.Errorf("expected code pending_request_exists, got %q", errRes.Code)
	}
}

func TestGetPaymentMethods(t *testing.T) {
	s := newTestServer(true)
	handler := s.PublicHandler()

	// Seed settings with payment methods
	ctx := context.Background()
	_ = s.Store.SaveAppSettings(ctx, &models.AppSettings{
		ID: models.AppSettingsID,
		PaymentMethods: []models.PaymentMethod{
			{Kind: "wallet", Label: "Vodafone Cash", NumberOrAddress: "01000000001"},
			{Kind: "instapay", Label: "InstaPay", NumberOrAddress: "wael@instapay"},
		},
	})

	webToken := makeWebStudentToken(t, "user-web-pm")
	req := httptest.NewRequest(http.MethodGet, "/academy/payment-methods", nil)
	req.Header.Set("Authorization", "Bearer "+webToken)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var res struct {
		Items []models.PaymentMethod `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 payment methods, got %d", len(res.Items))
	}
	if res.Items[0].Label != "Vodafone Cash" || res.Items[1].Label != "InstaPay" {
		t.Errorf("unexpected payment methods: %+v", res.Items)
	}

	// Mobile token must be refused
	mobileToken := makeStudentToken(t, "user-mobile-pm")
	mReq := httptest.NewRequest(http.MethodGet, "/academy/payment-methods", nil)
	mReq.Header.Set("Authorization", "Bearer "+mobileToken)
	mReq.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	mRec := httptest.NewRecorder()
	handler.ServeHTTP(mRec, mReq)
	if mRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for mobile client, got %d", mRec.Code)
	}
}

func TestStudentRequests_IDOR(t *testing.T) {
	s := newTestServer(true)
	handler := s.PublicHandler()

	subj := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(24*time.Hour))
	tokenA := makeWebStudentToken(t, "student-A")
	tokenB := makeWebStudentToken(t, "student-B")

	// Student A creates a purchase request
	body := map[string]any{
		"payment_method":     "instapay",
		"transfer_reference": "TXN_STUDENT_A",
		"sender_number":      "01012345678",
		"amount":             150,
	}
	raw, _ := json.Marshal(body)
	reqA := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subj.ID+"/purchase-request", bytes.NewReader(raw))
	reqA.Header.Set("Authorization", "Bearer "+tokenA)
	reqA.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	recA := httptest.NewRecorder()
	handler.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusCreated {
		t.Fatalf("create request: %d", recA.Code)
	}
	var created models.PurchaseRequestResponseDTO
	_ = json.Unmarshal(recA.Body.Bytes(), &created)

	// Student B attempts to read Student A's request by ID -> MUST return 404
	reqB := httptest.NewRequest(http.MethodGet, "/academy/me/requests/"+created.ID, nil)
	reqB.Header.Set("Authorization", "Bearer "+tokenB)
	reqB.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	recB := httptest.NewRecorder()
	handler.ServeHTTP(recB, reqB)

	if recB.Code != http.StatusNotFound {
		t.Fatalf("IDOR vulnerability: Student B got status %d when reading Student A's request, expected 404", recB.Code)
	}

	// Student A reads own request by ID -> returns 200
	reqAOwn := httptest.NewRequest(http.MethodGet, "/academy/me/requests/"+created.ID, nil)
	reqAOwn.Header.Set("Authorization", "Bearer "+tokenA)
	reqAOwn.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	recAOwn := httptest.NewRecorder()
	handler.ServeHTTP(recAOwn, reqAOwn)

	if recAOwn.Code != http.StatusOK {
		t.Fatalf("Student A reading own request expected 200, got %d", recAOwn.Code)
	}
	var ownItem models.StudentPurchaseRequestItemDTO
	_ = json.Unmarshal(recAOwn.Body.Bytes(), &ownItem)
	if ownItem.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, ownItem.ID)
	}
	if ownItem.Amount != 150 {
		t.Errorf("expected Amount 150, got %d", ownItem.Amount)
	}

	// Student A lists own requests -> 1 item
	reqAList := httptest.NewRequest(http.MethodGet, "/academy/me/requests", nil)
	reqAList.Header.Set("Authorization", "Bearer "+tokenA)
	reqAList.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	recAList := httptest.NewRecorder()
	handler.ServeHTTP(recAList, reqAList)

	if recAList.Code != http.StatusOK {
		t.Fatalf("Student A listing own requests expected 200, got %d", recAList.Code)
	}
	var listRes struct {
		Items []models.StudentPurchaseRequestItemDTO `json:"items"`
	}
	_ = json.Unmarshal(recAList.Body.Bytes(), &listRes)
	if len(listRes.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(listRes.Items))
	}
	if listRes.Items[0].ID != created.ID {
		t.Errorf("expected item ID %q, got %q", created.ID, listRes.Items[0].ID)
	}
}

func TestAdminSettings_PaymentMethodsAudit(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	adminHandler := s.AdminHandler()

	body := map[string]any{
		"show_prices":       true,
		"support_whatsapp":  "201000000000",
		"center_name_ar":    "مركز المتر",
		"center_name_en":    "El Metr Center",
		"center_address_ar": "شارع الجامعة",
		"center_address_en": "University Street",
		"center_hours_ar":   "يوميا من 9 صباحا",
		"center_hours_en":   "Daily from 9 AM",
		"center_map_url":    "https://maps.example.com",
		"payment_methods": []map[string]string{
			{"kind": "wallet", "label": "Vodafone Cash", "number_or_address": "01000000001"},
		},
	}
	raw, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/internal/admin/settings", bytes.NewReader(raw))
	req.Header.Set("X-Internal-Token", "test-internal-token")
	req.Header.Set("X-Admin-Token", "adm-token")
	req = req.WithContext(context.WithValue(req.Context(), adminIdentityContextKey, &AdminIdentity{ID: "adm-1", Name: "Admin Wael"}))
	rec := httptest.NewRecorder()

	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify audit log has "payment_methods" in changed field names
	ctx := context.Background()
	logs, _, err := s.Store.ListAuditLogs(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected at least 1 audit log")
	}
	found := false
	for _, l := range logs {
		if l.Action == "settings_updated" && strings.Contains(l.Detail, "payment_methods") {
			found = true
			// Crucial check: verify audit log detail does NOT contain the secret wallet number
			if strings.Contains(l.Detail, "01000000001") {
				t.Fatalf("security violation: audit log contains sensitive payment method value %q", l.Detail)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected audit log to record 'payment_methods' changed field, got: %+v", logs[0])
	}
}
