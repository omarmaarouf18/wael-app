package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// maxOrderSubjects is the cart limit (2026-11-05 orders brief).
const maxOrderSubjects = 50

// CreateOrderDTO is the JSON payload for POST /academy/orders (web cart checkout).
type CreateOrderDTO struct {
	SubjectIDs        []string `json:"subject_ids"`
	PaymentMethod     string   `json:"payment_method"`
	TransferReference string   `json:"transfer_reference"`
	SenderNumber      string   `json:"sender_number"`
	AmountPaid        int      `json:"amount_paid"`
}

// writeOrderError writes the typed bilingual business error. subjectID is
// empty for order-wide failures.
func writeOrderError(w http.ResponseWriter, status int, code, msgAr, msgEn, subjectID string) {
	handlerutil.WriteJSON(w, status, PurchaseRequestErrorResponse{
		Error:     code,
		Code:      code,
		MessageAr: msgAr,
		MessageEn: msgEn,
		SubjectID: subjectID,
	})
}

// CreateOrder handles POST /academy/orders: one web order for 1-50 subjects,
// all rows created or none. Every per-subject check names the failing subject
// in subject_id and in both messages.
func (s *Server) CreateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	if claims.Client != "web" {
		writeOrderError(w, http.StatusForbidden, "web_client_required",
			"الشراء متاح من الموقع فقط", "Checkout is only available from the website", "")
		return
	}

	var body CreateOrderDTO
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeOrderError(w, http.StatusBadRequest, "invalid_json",
			"بيانات الطلب غير صالحة", "Invalid request body", "")
		return
	}

	if len(body.SubjectIDs) < 1 || len(body.SubjectIDs) > maxOrderSubjects {
		writeOrderError(w, http.StatusBadRequest, "invalid_subjects",
			fmt.Sprintf("اختر من مادة واحدة إلى %d مادة", maxOrderSubjects),
			fmt.Sprintf("Choose between 1 and %d subjects", maxOrderSubjects), "")
		return
	}

	subjectIDs := make([]string, 0, len(body.SubjectIDs))
	seen := make(map[string]bool, len(body.SubjectIDs))
	for _, raw := range body.SubjectIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			writeOrderError(w, http.StatusBadRequest, "invalid_subjects",
				"اختر من مادة واحدة إلى 50 مادة", "Choose between 1 and 50 subjects", "")
			return
		}
		if seen[id] {
			writeOrderError(w, http.StatusBadRequest, "duplicate_subject",
				"المادة مكررة في السلة", "The subject appears twice in the cart", id)
			return
		}
		seen[id] = true
		subjectIDs = append(subjectIDs, id)
	}

	pm := strings.ToLower(strings.TrimSpace(body.PaymentMethod))
	if pm != "wallet" && pm != "instapay" {
		writeOrderError(w, http.StatusBadRequest, "invalid_payment_method",
			"طريقة الدفع غير صالحة (المحافظ الإلكترونية أو إنستاباي)",
			"Invalid payment method (wallet or instapay)", "")
		return
	}

	ref := strings.TrimSpace(body.TransferReference)
	refRunes := []rune(ref)
	if len(refRunes) < 3 || len(refRunes) > 100 {
		writeOrderError(w, http.StatusBadRequest, "invalid_transfer_reference",
			"رقم أو مرجع الحوالة يجب أن يكون بين 3 و 100 حرف",
			"Transfer reference must be between 3 and 100 characters", "")
		return
	}

	sender := strings.TrimSpace(body.SenderNumber)
	senderRunes := []rune(sender)
	if len(senderRunes) < 3 || len(senderRunes) > 50 {
		writeOrderError(w, http.StatusBadRequest, "invalid_sender_number",
			"رقم الهاتف أو العنوان المحول منه غير صالح",
			"Sender number or address must be between 3 and 50 characters", "")
		return
	}

	if body.AmountPaid <= 0 {
		writeOrderError(w, http.StatusBadRequest, "invalid_amount",
			"المبلغ المحول غير صالح", "Transfer amount must be greater than zero", "")
		return
	}

	normRef := strings.ToUpper(ref)

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	hasReqRef, err := s.Store.HasPurchaseRequestWithReference(ctx, normRef)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	hasOrderRef, err := s.Store.HasOrderWithNormalizedReference(ctx, normRef)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if hasReqRef || hasOrderRef {
		writeOrderError(w, http.StatusConflict, "duplicate_reference",
			"تم استخدام رقم الحوالة هذا في طلب سابق",
			"This transfer reference has already been used", "")
		return
	}

	levels, err := s.loadLevelsByKey(ctx)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	now := time.Now().UTC()
	orderID, _ := jwtutil.GenerateUUID()
	subjects := make(map[string]*models.Subject, len(subjectIDs))
	reqs := make([]*models.PurchaseRequest, 0, len(subjectIDs))

	for _, sid := range subjectIDs {
		subj, err := s.Store.GetSubjectByID(ctx, sid)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if subj == nil || subj.Status != models.StatusPublished {
			writeOrderError(w, http.StatusNotFound, "subject_not_found",
				fmt.Sprintf("المادة «%s» غير متاحة", sid),
				fmt.Sprintf("The subject %q is not available", sid), sid)
			return
		}
		label := subj.TitleAr
		if label == "" {
			label = sid
		}
		labelEn := subj.TitleEn
		if labelEn == "" {
			labelEn = sid
		}
		if !subj.AccessExpiresAt.IsZero() && subj.AccessExpiresAt.Before(now) {
			writeOrderError(w, http.StatusConflict, "subject_expired",
				fmt.Sprintf("انتهت فترة التسجيل في مادة «%s»", label),
				fmt.Sprintf("Registration period for the subject %q has expired", labelEn), sid)
			return
		}

		owned, err := s.Store.HasActiveEntitlement(ctx, claims.UserID, sid)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if owned {
			writeOrderError(w, http.StatusConflict, "already_owned",
				fmt.Sprintf("أنت مشترك في مادة «%s» بالفعل", label),
				fmt.Sprintf("You already own the subject %q", labelEn), sid)
			return
		}

		pending, err := s.Store.GetPendingRequest(ctx, claims.UserID, sid)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if pending != nil {
			writeOrderError(w, http.StatusConflict, "pending_request_exists",
				fmt.Sprintf("لديك طلب اشتراك معلق لمادة «%s» بالفعل", label),
				fmt.Sprintf("You already have a pending purchase request for the subject %q", labelEn), sid)
			return
		}

		subjects[sid] = subj
		reqID, _ := jwtutil.GenerateUUID()
		reqs = append(reqs, &models.PurchaseRequest{
			ID:                  reqID,
			UserID:              claims.UserID,
			SubjectID:           sid,
			OrderID:             orderID,
			Status:              models.RequestStatusPending,
			Source:              "web",
			PaymentMethod:       pm,
			TransferReference:   ref,
			NormalizedReference: normRef,
			SenderNumber:        sender,
			// The typed transfer amount lives once on the order (amount_paid);
			// each request row keeps its own subject price so the legacy admin
			// queue's per-item amount warning stays meaningful.
			Amount:    subj.Price,
			CreatedAt: now,
		})
	}

	order := &models.Order{
		ID:                  orderID,
		UserID:              claims.UserID,
		Source:              models.OrderSourceWeb,
		Status:              models.OrderStatusOpen,
		PaymentMethod:       pm,
		TransferReference:   ref,
		NormalizedReference: normRef,
		SenderNumber:        sender,
		AmountPaid:          body.AmountPaid,
		CreatedAt:           now,
	}

	if err := s.Store.CreateOrderWithRequests(ctx, order, reqs); err != nil {
		if errors.Is(err, store.ErrPendingRequestExists) {
			writeOrderError(w, http.StatusConflict, "pending_request_exists",
				"لديك طلب اشتراك معلق لمادة في السلة بالفعل",
				"You already have a pending purchase request for a subject in your cart", "")
			return
		}
		if errors.Is(err, store.ErrDuplicateReference) || errors.Is(err, store.ErrDuplicate) {
			writeOrderError(w, http.StatusConflict, "duplicate_reference",
				"تم استخدام رقم الحوالة هذا في طلب سابق",
				"This transfer reference has already been used", "")
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusCreated, orderDTO(order, reqs, subjects, levels))
}

// GetMyOrders handles GET /academy/me/orders: the student's orders, newest first.
func (s *Server) GetMyOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	orders, err := s.Store.ListOrdersByUser(ctx, claims.UserID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	levels, err := s.loadLevelsByKey(ctx)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	subjects := make(map[string]*models.Subject)
	items := make([]models.OrderDTO, 0, len(orders))
	for _, order := range orders {
		reqs, err := s.Store.ListRequestsByOrder(ctx, order.ID)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if err := s.loadSubjects(ctx, reqs, subjects); err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		items = append(items, orderDTO(order, reqs, subjects, levels))
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetMyOrderByID handles GET /academy/me/orders/{id}. Another student's order,
// an unknown id or a malformed id is a generic 404 (IDOR protection).
func (s *Server) GetMyOrderByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	orderID := strings.TrimPrefix(r.URL.Path, "/academy/me/orders/")
	orderID = strings.Trim(orderID, "/")
	if orderID == "" || !validResourceID(orderID) {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "order not found", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	order, err := s.Store.GetOrderByID(ctx, orderID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if order == nil || order.UserID != claims.UserID {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "order not found", nil)
		return
	}

	reqs, err := s.Store.ListRequestsByOrder(ctx, order.ID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	levels, err := s.loadLevelsByKey(ctx)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	subjects := make(map[string]*models.Subject)
	if err := s.loadSubjects(ctx, reqs, subjects); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, orderDTO(order, reqs, subjects, levels))
}

// loadLevelsByKey returns every level keyed by its level key.
func (s *Server) loadLevelsByKey(ctx context.Context) (map[string]*models.Level, error) {
	levels, err := s.Store.ListLevels(ctx, false)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]*models.Level, len(levels))
	for _, lvl := range levels {
		byKey[lvl.Key] = lvl
	}
	return byKey, nil
}

// loadSubjects fills the cache with every subject referenced by reqs.
func (s *Server) loadSubjects(ctx context.Context, reqs []*models.PurchaseRequest, cache map[string]*models.Subject) error {
	for _, req := range reqs {
		if _, ok := cache[req.SubjectID]; ok {
			continue
		}
		subj, err := s.Store.GetSubjectByID(ctx, req.SubjectID)
		if err != nil {
			return err
		}
		cache[req.SubjectID] = subj
	}
	return nil
}

// orderItems builds the item DTOs for an order's requests.
func orderItems(reqs []*models.PurchaseRequest, subjects map[string]*models.Subject, levels map[string]*models.Level) []models.OrderItemDTO {
	items := make([]models.OrderItemDTO, 0, len(reqs))
	for _, req := range reqs {
		item := models.OrderItemDTO{
			RequestID:    req.ID,
			SubjectID:    req.SubjectID,
			Status:       req.Status,
			CreatedAt:    req.CreatedAt,
			DecidedAt:    req.DecidedAt,
			RejectReason: req.RejectReason,
		}
		if subj := subjects[req.SubjectID]; subj != nil {
			item.TitleAr = subj.TitleAr
			item.TitleEn = subj.TitleEn
			item.Price = subj.Price
			if lvl := levels[subj.LevelKey]; lvl != nil {
				item.LevelNameAr = lvl.TitleAr
			}
		}
		items = append(items, item)
	}
	return items
}

// orderTotal sums the current subject prices of an order's items.
func orderTotal(items []models.OrderItemDTO) int {
	total := 0
	for _, item := range items {
		total += item.Price
	}
	return total
}

// orderDTO builds the student view of an order.
func orderDTO(order *models.Order, reqs []*models.PurchaseRequest, subjects map[string]*models.Subject, levels map[string]*models.Level) models.OrderDTO {
	items := orderItems(reqs, subjects, levels)
	total := orderTotal(items)
	web := order.Source == models.OrderSourceWeb
	dto := models.OrderDTO{
		ID:         order.ID,
		Source:     order.Source,
		Status:     order.Status,
		CreatedAt:  order.CreatedAt,
		DecidedAt:  order.ClosedAt,
		Total:      total,
		Items:      items,
		AmountPaid: order.AmountPaid,
	}
	if web {
		dto.PaymentMethod = order.PaymentMethod
		dto.TransferReference = order.TransferReference
		dto.SenderNumber = order.SenderNumber
		dto.AmountMismatch = order.AmountPaid != total
	}
	return dto
}
