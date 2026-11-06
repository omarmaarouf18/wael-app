package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// ---- helpers ---------------------------------------------------------------

func doOrder(t *testing.T, s *Server, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal order body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/academy/orders", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	return rec
}

func doAccessRequest(t *testing.T, s *Server, token, subjectID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subjectID+"/access-request", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	return rec
}

func doGetStudent(t *testing.T, s *Server, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	return rec
}

func orderBody(subjectIDs []string, ref string, amount int) map[string]any {
	return map[string]any{
		"subject_ids":        subjectIDs,
		"payment_method":     "instapay",
		"transfer_reference": ref,
		"sender_number":      "01012345678",
		"amount_paid":        amount,
	}
}

func createDraftSubject(t *testing.T, s *Server, price int, expiresAt time.Time) *models.Subject {
	t.Helper()
	now := time.Now().UTC()
	subj := &models.Subject{
		ID:              fmt.Sprintf("subj-draft-%d", time.Now().UnixNano()),
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة مسودة",
		TitleEn:         "Draft Subject",
		Price:           price,
		AccessExpiresAt: expiresAt,
		Status:          models.StatusDraft,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.Store.CreateSubject(context.Background(), subj); err != nil {
		t.Fatalf("CreateSubject draft: %v", err)
	}
	return subj
}

// ---- app grouping ----------------------------------------------------------

func TestOrders_AppRequestsGroupIntoOneOpenOrder(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	user := "user-app-group"
	token := makeStudentToken(t, user)

	var requestIDs []string
	for i := 0; i < 3; i++ {
		subj := createTestSubject(t, s, "bachelor-y1", 100*(i+1), time.Now().Add(48*time.Hour))
		rec := doAccessRequest(t, s, token, subj.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("access-request %d: expected 200, got %d (%s)", i, rec.Code, rec.Body.String())
		}
		var resp models.AccessRequestResponseDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		requestIDs = append(requestIDs, resp.ID)
	}

	orders, err := s.Store.ListOrdersByUser(ctx, user)
	if err != nil {
		t.Fatalf("ListOrdersByUser: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("expected one open app order, got %d", len(orders))
	}
	order := orders[0]
	if order.Source != models.OrderSourceApp || order.Status != models.OrderStatusOpen {
		t.Fatalf("order source/status = %q/%q, want app/open", order.Source, order.Status)
	}
	reqs, err := s.Store.ListRequestsByOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("ListRequestsByOrder: %v", err)
	}
	if len(reqs) != 3 {
		t.Fatalf("expected 3 requests in one order, got %d", len(reqs))
	}

	// Accept all through the bulk endpoint: the order closes.
	bulk := map[string]any{"action": "accept", "ids": requestIDs}
	rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/bulk", bulk)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk accept: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var bulkResp models.BulkRequestsResponseDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &bulkResp)
	if len(bulkResp.Results) != 3 {
		t.Fatalf("expected 3 bulk results, got %d", len(bulkResp.Results))
	}
	for _, r := range bulkResp.Results {
		if r.Status != models.RequestStatusAccepted {
			t.Fatalf("expected accepted, got %+v", r)
		}
	}

	closed, err := s.Store.GetOrderByID(ctx, order.ID)
	if err != nil || closed == nil {
		t.Fatalf("GetOrderByID: %v %v", closed, err)
	}
	if closed.Status != models.OrderStatusClosed || closed.ClosedAt == nil {
		t.Fatalf("order not closed after all decisions: status=%q closed_at=%v", closed.Status, closed.ClosedAt)
	}

	// The next app request starts a new open order.
	subj4 := createTestSubject(t, s, "bachelor-y1", 500, time.Now().Add(48*time.Hour))
	if rec := doAccessRequest(t, s, token, subj4.ID); rec.Code != http.StatusOK {
		t.Fatalf("fourth access-request: expected 200, got %d", rec.Code)
	}
	orders, err = s.Store.ListOrdersByUser(ctx, user)
	if err != nil {
		t.Fatalf("ListOrdersByUser after fourth: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected two orders after the fourth request, got %d", len(orders))
	}
	newest := orders[0]
	if newest.ID == order.ID {
		t.Fatalf("expected a new order id, got the closed one again")
	}
	if newest.Status != models.OrderStatusOpen {
		t.Fatalf("new app order status = %q, want open", newest.Status)
	}
	newReqs, _ := s.Store.ListRequestsByOrder(ctx, newest.ID)
	if len(newReqs) != 1 {
		t.Fatalf("expected one request in the new order, got %d", len(newReqs))
	}
}

func TestOrders_WebCheckoutDuringOpenAppOrderIsSeparate(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	user := "user-mixed-source"
	appToken := makeStudentToken(t, user)
	webToken := makeWebStudentToken(t, user)

	appSubj := createTestSubject(t, s, "bachelor-y1", 100, time.Now().Add(48*time.Hour))
	if rec := doAccessRequest(t, s, appToken, appSubj.ID); rec.Code != http.StatusOK {
		t.Fatalf("app request: expected 200, got %d", rec.Code)
	}

	webSubj := createTestSubject(t, s, "bachelor-y1", 200, time.Now().Add(48*time.Hour))
	rec := doOrder(t, s, webToken, orderBody([]string{webSubj.ID}, "WEB-DURING-APP", 200))
	if rec.Code != http.StatusCreated {
		t.Fatalf("web checkout: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	orders, err := s.Store.ListOrdersByUser(ctx, user)
	if err != nil {
		t.Fatalf("ListOrdersByUser: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected separate app and web orders, got %d", len(orders))
	}
	sources := map[string]string{}
	for _, o := range orders {
		sources[o.Source] = o.Status
	}
	if sources[models.OrderSourceApp] != models.OrderStatusOpen {
		t.Fatalf("app order should stay open, got %q", sources[models.OrderSourceApp])
	}
	if _, ok := sources[models.OrderSourceWeb]; !ok {
		t.Fatalf("web order missing, sources=%v", sources)
	}
}

func TestOrders_ParallelAppRequestsOneOpenOrder(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	user := "user-parallel-app"
	token := makeStudentToken(t, user)

	const n = 6
	subjectIDs := make([]string, n)
	for i := 0; i < n; i++ {
		subjectIDs[i] = createTestSubject(t, s, "bachelor-y1", 100, time.Now().Add(48*time.Hour)).ID
	}

	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subjectIDs[i]+"/access-request", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
			rec := httptest.NewRecorder()
			s.PublicHandler().ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("parallel request %d: expected 200, got %d", i, code)
		}
	}

	orders, err := s.Store.ListOrdersByUser(ctx, user)
	if err != nil {
		t.Fatalf("ListOrdersByUser: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("parallel requests must converge on one open order, got %d", len(orders))
	}
	reqs, _ := s.Store.ListRequestsByOrder(ctx, orders[0].ID)
	if len(reqs) != n {
		t.Fatalf("expected %d requests in the single order, got %d", n, len(reqs))
	}
}

// ---- web checkout ----------------------------------------------------------

func TestOrders_WebCheckoutAllOrNoneAndMismatch(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	user := "user-web-atomic"
	token := makeWebStudentToken(t, user)

	good1 := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(48*time.Hour))
	good2 := createTestSubject(t, s, "bachelor-y1", 250, time.Now().Add(48*time.Hour))
	draft := createDraftSubject(t, s, 50, time.Now().Add(48*time.Hour))

	// One bad subject means nothing is written.
	rec := doOrder(t, s, token, orderBody([]string{good1.ID, draft.ID}, "ATOMIC-REF-1", 400))
	if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
		t.Fatalf("expected 404 subject_not_found, got %d %q", rec.Code, adminCode(t, rec))
	}
	orders, _ := s.Store.ListOrdersByUser(ctx, user)
	if len(orders) != 0 {
		t.Fatalf("all-or-none violated: %d orders created", len(orders))
	}
	reqs, _ := s.Store.ListRequestsByUser(ctx, user)
	if len(reqs) != 0 {
		t.Fatalf("all-or-none violated: %d requests created", len(reqs))
	}

	// Successful checkout with an amount mismatch flag.
	rec = doOrder(t, s, token, orderBody([]string{good1.ID, good2.ID}, "ATOMIC-REF-2", 300))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var dto models.OrderDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode order: %v", err)
	}
	if dto.ID == "" || dto.Source != models.OrderSourceWeb || dto.Status != models.OrderStatusOpen {
		t.Fatalf("unexpected order dto: %+v", dto)
	}
	if len(dto.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(dto.Items))
	}
	if dto.Total != 400 {
		t.Fatalf("total = %d, want 400", dto.Total)
	}
	if !dto.AmountMismatch {
		t.Fatalf("amount_mismatch = false, want true (paid 300, total 400)")
	}
	if dto.AmountPaid != 300 {
		t.Fatalf("amount_paid = %d, want 300", dto.AmountPaid)
	}

	orders, _ = s.Store.ListOrdersByUser(ctx, user)
	if len(orders) != 1 {
		t.Fatalf("expected one order, got %d", len(orders))
	}
	reqs, _ = s.Store.ListRequestsByOrder(ctx, orders[0].ID)
	if len(reqs) != 2 {
		t.Fatalf("expected two requests in the order, got %d", len(reqs))
	}

	// A second checkout that hits a pending subject writes nothing.
	good3 := createTestSubject(t, s, "bachelor-y1", 100, time.Now().Add(48*time.Hour))
	rec = doOrder(t, s, token, orderBody([]string{good3.ID, good1.ID}, "ATOMIC-REF-3", 250))
	if rec.Code != http.StatusConflict || adminCode(t, rec) != "pending_request_exists" {
		t.Fatalf("expected 409 pending_request_exists, got %d %q", rec.Code, adminCode(t, rec))
	}
	orders, _ = s.Store.ListOrdersByUser(ctx, user)
	if len(orders) != 1 {
		t.Fatalf("second failed checkout created an order; count=%d", len(orders))
	}
}

func TestOrders_CheckoutValidation(t *testing.T) {
	future := time.Now().Add(48 * time.Hour)

	t.Run("mobile_token_forbidden", func(t *testing.T) {
		s, _ := newAdminTestServer(t, okVerify)
		subj := createTestSubject(t, s, "bachelor-y1", 100, future)
		rec := doOrder(t, s, makeStudentToken(t, "u-mobile"), orderBody([]string{subj.ID}, "REF-MOBILE", 100))
		if rec.Code != http.StatusForbidden || adminCode(t, rec) != "web_client_required" {
			t.Fatalf("got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("unknown_field", func(t *testing.T) {
		s, _ := newAdminTestServer(t, okVerify)
		subj := createTestSubject(t, s, "bachelor-y1", 100, future)
		body := orderBody([]string{subj.ID}, "REF-UNKNOWN", 100)
		body["extra"] = true
		rec := doOrder(t, s, makeWebStudentToken(t, "u-unknown"), body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_json" {
			t.Fatalf("got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("subject_count_bounds", func(t *testing.T) {
		s, _ := newAdminTestServer(t, okVerify)
		token := makeWebStudentToken(t, "u-count")
		if rec := doOrder(t, s, token, orderBody(nil, "REF-EMPTY", 100)); rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_subjects" {
			t.Fatalf("empty: got %d %q", rec.Code, adminCode(t, rec))
		}
		ids := make([]string, maxOrderSubjects+1)
		for i := range ids {
			ids[i] = fmt.Sprintf("subj-%d", i)
		}
		if rec := doOrder(t, s, token, orderBody(ids, "REF-MANY", 100)); rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_subjects" {
			t.Fatalf("too many: got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("duplicate_subject", func(t *testing.T) {
		s, _ := newAdminTestServer(t, okVerify)
		subj := createTestSubject(t, s, "bachelor-y1", 100, future)
		rec := doOrder(t, s, makeWebStudentToken(t, "u-dup-subj"), orderBody([]string{subj.ID, subj.ID}, "REF-DUP-SUBJ", 200))
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "duplicate_subject" {
			t.Fatalf("got %d %q", rec.Code, adminCode(t, rec))
		}
		var errRes PurchaseRequestErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errRes)
		if errRes.SubjectID != subj.ID {
			t.Fatalf("subject_id = %q, want %q", errRes.SubjectID, subj.ID)
		}
	})

	t.Run("invalid_payment_method", func(t *testing.T) {
		s, _ := newAdminTestServer(t, okVerify)
		subj := createTestSubject(t, s, "bachelor-y1", 100, future)
		body := orderBody([]string{subj.ID}, "REF-PM", 100)
		body["payment_method"] = "cash"
		rec := doOrder(t, s, makeWebStudentToken(t, "u-pm"), body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_payment_method" {
			t.Fatalf("got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("invalid_reference_and_sender_and_amount", func(t *testing.T) {
		s, _ := newAdminTestServer(t, okVerify)
		subj := createTestSubject(t, s, "bachelor-y1", 100, future)
		token := makeWebStudentToken(t, "u-fields")

		body := orderBody([]string{subj.ID}, "ab", 100)
		if rec := doOrder(t, s, token, body); rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_transfer_reference" {
			t.Fatalf("ref: got %d %q", rec.Code, adminCode(t, rec))
		}
		body = orderBody([]string{subj.ID}, "REF-SENDER", 100)
		body["sender_number"] = "12"
		if rec := doOrder(t, s, token, body); rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_sender_number" {
			t.Fatalf("sender: got %d %q", rec.Code, adminCode(t, rec))
		}
		body = orderBody([]string{subj.ID}, "REF-AMOUNT", 0)
		if rec := doOrder(t, s, token, body); rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_amount" {
			t.Fatalf("amount: got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("duplicate_reference", func(t *testing.T) {
		s, _ := newAdminTestServer(t, okVerify)
		token := makeWebStudentToken(t, "u-dup-ref")
		subj1 := createTestSubject(t, s, "bachelor-y1", 100, future)
		subj2 := createTestSubject(t, s, "bachelor-y1", 100, future)
		if rec := doOrder(t, s, token, orderBody([]string{subj1.ID}, "shared-ref-1", 100)); rec.Code != http.StatusCreated {
			t.Fatalf("first order: %d", rec.Code)
		}
		rec := doOrder(t, s, token, orderBody([]string{subj2.ID}, "SHARED-REF-1", 100))
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "duplicate_reference" {
			t.Fatalf("got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("already_owned", func(t *testing.T) {
		s, _ := newAdminTestServer(t, okVerify)
		ctx := context.Background()
		user := "u-owned"
		subj := createTestSubject(t, s, "bachelor-y1", 100, future)
		if err := s.Store.Grant(ctx, &models.Entitlement{UserID: user, SubjectID: subj.ID, Source: models.EntitlementSourceAdminGrant}); err != nil {
			t.Fatalf("Grant: %v", err)
		}
		rec := doOrder(t, s, makeWebStudentToken(t, user), orderBody([]string{subj.ID}, "REF-OWNED", 100))
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "already_owned" {
			t.Fatalf("got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("subject_expired", func(t *testing.T) {
		s, _ := newAdminTestServer(t, okVerify)
		subj := createTestSubject(t, s, "bachelor-y1", 100, time.Now().Add(-time.Hour))
		rec := doOrder(t, s, makeWebStudentToken(t, "u-expired"), orderBody([]string{subj.ID}, "REF-EXPIRED", 100))
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "subject_expired" {
			t.Fatalf("got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("unpublished_or_unknown_subject", func(t *testing.T) {
		s, _ := newAdminTestServer(t, okVerify)
		draft := createDraftSubject(t, s, 100, future)
		token := makeWebStudentToken(t, "u-missing")
		for _, id := range []string{draft.ID, "subj-does-not-exist"} {
			rec := doOrder(t, s, token, orderBody([]string{id}, "REF-MISSING-"+id, 100))
			if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
				t.Fatalf("%s: got %d %q", id, rec.Code, adminCode(t, rec))
			}
		}
	})
}

func TestOrders_MyOrdersAndIDOR(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	owner := "user-orders-owner"
	ownerToken := makeWebStudentToken(t, owner)
	otherToken := makeStudentToken(t, "user-orders-other")

	subj := createTestSubject(t, s, "bachelor-y1", 175, time.Now().Add(48*time.Hour))
	rec := doOrder(t, s, ownerToken, orderBody([]string{subj.ID}, "REF-MY-ORDERS", 175))
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout: %d (%s)", rec.Code, rec.Body.String())
	}
	var created models.OrderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = doGetStudent(t, s, ownerToken, "/academy/me/orders")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET me/orders: %d", rec.Code)
	}
	var list struct {
		Items []models.OrderDTO `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("unexpected order list: %+v", list.Items)
	}

	rec = doGetStudent(t, s, ownerToken, "/academy/me/orders/"+created.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET me/orders/{id}: %d", rec.Code)
	}
	var one models.OrderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &one)
	if one.ID != created.ID || len(one.Items) != 1 || one.Items[0].SubjectID != subj.ID {
		t.Fatalf("unexpected order: %+v", one)
	}

	// Another student, an unknown id and a malformed id are all a generic 404.
	for _, path := range []string{
		"/academy/me/orders/" + created.ID,
		"/academy/me/orders/ord-does-not-exist",
		"/academy/me/orders/bad*id",
	} {
		rec = doGetStudent(t, s, otherToken, path)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d", path, rec.Code)
		}
	}
}

func TestOrders_SinglePurchaseRequestIsOrderOfOne(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	user := "user-single-order"
	token := makeWebStudentToken(t, user)
	subj := createTestSubject(t, s, "bachelor-y1", 120, time.Now().Add(48*time.Hour))

	body := map[string]any{
		"payment_method":     "wallet",
		"transfer_reference": "SINGLE-ORDER-1",
		"sender_number":      "01012345678",
		"amount":             120,
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/academy/subjects/"+subj.ID+"/purchase-request", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp models.PurchaseRequestResponseDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.ID == "" || resp.Source != "web" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	orders, err := s.Store.ListOrdersByUser(ctx, user)
	if err != nil || len(orders) != 1 {
		t.Fatalf("expected one web order of one, got %d (%v)", len(orders), err)
	}
	if orders[0].Source != models.OrderSourceWeb || orders[0].AmountPaid != 120 {
		t.Fatalf("unexpected order: %+v", orders[0])
	}
	reqs, _ := s.Store.ListRequestsByOrder(ctx, orders[0].ID)
	if len(reqs) != 1 || reqs[0].ID != resp.ID {
		t.Fatalf("request not attached to the order: %+v", reqs)
	}
}

// ---- console read model and bulk ------------------------------------------

func TestAdminRequestGroups_GroupedReadModel(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	webUser := "user-groups-web"
	webToken := makeWebStudentToken(t, webUser)
	web1 := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(48*time.Hour))
	web2 := createTestSubject(t, s, "bachelor-y1", 250, time.Now().Add(48*time.Hour))
	if rec := doOrder(t, s, webToken, orderBody([]string{web1.ID, web2.ID}, "GROUP-WEB-REF", 100)); rec.Code != http.StatusCreated {
		t.Fatalf("web checkout: %d (%s)", rec.Code, rec.Body.String())
	}

	appUser := "user-groups-app"
	appToken := makeStudentToken(t, appUser)
	for i := 0; i < 2; i++ {
		subj := createTestSubject(t, s, "bachelor-y1", 200, time.Now().Add(48*time.Hour))
		if rec := doAccessRequest(t, s, appToken, subj.ID); rec.Code != http.StatusOK {
			t.Fatalf("app request: %d", rec.Code)
		}
	}

	rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/request-groups", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("request-groups: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp models.OrderGroupsResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	if resp.TotalStudents != 2 || resp.TotalOrders != 2 || resp.TotalRequests != 4 {
		t.Fatalf("totals = students %d orders %d requests %d, want 2/2/4", resp.TotalStudents, resp.TotalOrders, resp.TotalRequests)
	}
	if resp.PendingCount != 4 {
		t.Fatalf("pending_count = %d, want 4", resp.PendingCount)
	}

	byUser := map[string]models.StudentOrderGroupDTO{}
	for _, g := range resp.Items {
		byUser[g.UserID] = g
	}
	webGroup, ok := byUser[webUser]
	if !ok || len(webGroup.Orders) != 1 {
		t.Fatalf("missing web group: %+v", webGroup)
	}
	webOrder := webGroup.Orders[0]
	if webOrder.Source != models.OrderSourceWeb || webOrder.Total != 400 || webOrder.AmountPaid != 100 || !webOrder.AmountMismatch {
		t.Fatalf("unexpected web order block: %+v", webOrder)
	}
	if len(webOrder.Items) != 2 {
		t.Fatalf("web order items = %d, want 2", len(webOrder.Items))
	}
	for _, item := range webOrder.Items {
		if item.TitleAr == "" || item.Price == 0 || item.LevelNameAr == "" {
			t.Fatalf("item missing title/level/price: %+v", item)
		}
	}

	appGroup, ok := byUser[appUser]
	if !ok || len(appGroup.Orders) != 1 {
		t.Fatalf("missing app group: %+v", appGroup)
	}
	appOrder := appGroup.Orders[0]
	if appOrder.Source != models.OrderSourceApp || appOrder.AmountMismatch {
		t.Fatalf("unexpected app order block: %+v", appOrder)
	}
	if appOrder.AmountPaid != 0 {
		t.Fatalf("app order must not expose amount paid, got %d", appOrder.AmountPaid)
	}
	if len(appOrder.Items) != 2 {
		t.Fatalf("app order items = %d, want 2", len(appOrder.Items))
	}
}

func TestAdminBulkRequests_MixedResultsAuditAndPayment(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	future := time.Now().Add(48 * time.Hour)

	s1 := createTestSubject(t, s, "bachelor-y1", 111, future)
	s2 := createTestSubject(t, s, "bachelor-y1", 222, future)
	s3 := createTestSubject(t, s, "bachelor-y1", 333, future)

	r1 := createTestRequest(t, s, "user-bulk-1", s1.ID)
	r2 := createTestRequest(t, s, "user-bulk-1", s2.ID)
	r3 := createTestRequest(t, s, "user-bulk-2", s3.ID)

	bulk := map[string]any{"action": "accept", "ids": []string{r1.ID, "req-missing-1", r2.ID}}
	rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/bulk", bulk)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk accept: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp models.BulkRequestsResponseDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 3 {
		t.Fatalf("expected one result per id, got %d", len(resp.Results))
	}
	wantStatuses := []string{models.RequestStatusAccepted, "failed", models.RequestStatusAccepted}
	for i, want := range wantStatuses {
		if resp.Results[i].Status != want {
			t.Fatalf("result %d status = %q, want %q", i, resp.Results[i].Status, want)
		}
		if resp.Results[i].ID == "" {
			t.Fatalf("result %d has no id", i)
		}
	}
	if resp.Results[1].ErrorCode != "request_not_found" || resp.Results[1].MessageAr == "" || resp.Results[1].MessageEn == "" {
		t.Fatalf("failed result missing typed reason: %+v", resp.Results[1])
	}

	// Entitlements and payment records with price_at_grant.
	for _, pair := range []struct {
		userID, subjectID string
		price             int
	}{{"user-bulk-1", s1.ID, 111}, {"user-bulk-1", s2.ID, 222}} {
		owned, err := s.Store.HasActiveEntitlement(ctx, pair.userID, pair.subjectID)
		if err != nil || !owned {
			t.Fatalf("expected entitlement for %s/%s (err=%v)", pair.userID, pair.subjectID, err)
		}
		pays, err := s.Store.ListPaymentRecordsByUser(ctx, pair.userID)
		if err != nil {
			t.Fatalf("ListPaymentRecordsByUser: %v", err)
		}
		found := false
		for _, p := range pays {
			if p.SubjectID == pair.subjectID {
				found = true
				if p.PriceAtGrant != pair.price || p.Amount != pair.price {
					t.Fatalf("payment record price_at_grant = %d amount = %d, want %d", p.PriceAtGrant, p.Amount, pair.price)
				}
			}
		}
		if !found {
			t.Fatalf("no payment record for %s", pair.subjectID)
		}
	}

	// Per-request audit plus one bulk summary, actor = verified admin.
	logs, _, err := s.Store.ListAuditLogs(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	counts := map[string]int{}
	for _, l := range logs {
		counts[l.Action]++
		if l.ActorID != "adm-1" {
			t.Fatalf("audit actor = %q, want adm-1", l.ActorID)
		}
	}
	if counts["request_accept"] != 2 {
		t.Fatalf("request_accept audit count = %d, want 2", counts["request_accept"])
	}
	if counts["request_bulk_accept"] != 1 {
		t.Fatalf("request_bulk_accept audit count = %d, want 1", counts["request_bulk_accept"])
	}
	for _, l := range logs {
		if l.Action == "request_bulk_accept" && l.TargetType != "request_bulk" {
			t.Fatalf("bulk summary target_type = %q, want request_bulk", l.TargetType)
		}
	}

	// Bulk reject stores the shared reason and audits it.
	reject := map[string]any{"action": "reject", "ids": []string{r3.ID}, "reason": "  التحويل غير مطابق  "}
	rec = doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/bulk", reject)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk reject: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var rejectResp models.BulkRequestsResponseDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &rejectResp)
	if len(rejectResp.Results) != 1 || rejectResp.Results[0].Status != models.RequestStatusRejected {
		t.Fatalf("unexpected reject result: %+v", rejectResp.Results)
	}
	rejected, _ := s.Store.GetRequestByID(ctx, r3.ID)
	if rejected == nil || rejected.Status != models.RequestStatusRejected || rejected.RejectReason != "التحويل غير مطابق" {
		t.Fatalf("reject reason not stored/sanitized: %+v", rejected)
	}

	logs, _, _ = s.Store.ListAuditLogs(ctx, 1, 100)
	counts = map[string]int{}
	for _, l := range logs {
		counts[l.Action]++
	}
	if counts["request_reject"] != 1 || counts["request_bulk_reject"] != 1 {
		t.Fatalf("reject audit counts = %v", counts)
	}

	// Validation: invalid action, invalid ids, missing reject reason.
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/bulk", map[string]any{"action": "delete", "ids": []string{"req-1"}}); rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_action" {
		t.Fatalf("invalid action: got %d %q", rec.Code, adminCode(t, rec))
	}
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/bulk", map[string]any{"action": "accept", "ids": []string{}}); rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_ids" {
		t.Fatalf("empty ids: got %d %q", rec.Code, adminCode(t, rec))
	}
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/requests/bulk", map[string]any{"action": "reject", "ids": []string{"req-1"}}); rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_reason" {
		t.Fatalf("missing reason: got %d %q", rec.Code, adminCode(t, rec))
	}
}

// TestOrders_ContractShape pins the wire shape documented in
// docs/contracts/orders-api.md; tests/contracts executes it by name.
func TestOrders_ContractShape(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	subj := createTestSubject(t, s, "bachelor-y1", 150, time.Now().Add(48*time.Hour))
	token := makeWebStudentToken(t, "user-contract-shape")

	rec := doOrder(t, s, token, orderBody([]string{subj.ID}, "CONTRACT-SHAPE-1", 150))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{
		"id", "source", "status", "created_at", "payment_method",
		"transfer_reference", "sender_number", "amount_paid", "total",
		"amount_mismatch", "items",
	} {
		if _, ok := body[key]; !ok {
			t.Errorf("order response missing key %q: %v", key, body)
		}
	}
	if body["source"] != models.OrderSourceWeb || body["status"] != models.OrderStatusOpen {
		t.Errorf("source/status = %v/%v", body["source"], body["status"])
	}
	if body["amount_mismatch"] != false {
		t.Errorf("amount_mismatch = %v, want false", body["amount_mismatch"])
	}

	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %v", body["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item is not an object: %v", items[0])
	}
	for _, key := range []string{
		"request_id", "subject_id", "title_ar", "title_en",
		"level_name_ar", "price", "status", "created_at",
	} {
		if _, ok := item[key]; !ok {
			t.Errorf("item missing key %q: %v", key, item)
		}
	}
	if item["subject_id"] != subj.ID || item["status"] != models.RequestStatusPending {
		t.Errorf("unexpected item: %v", item)
	}
}
