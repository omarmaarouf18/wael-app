package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// CreatePurchaseRequestDTO is the JSON payload for POST /academy/subjects/{id}/purchase-request.
type CreatePurchaseRequestDTO struct {
	PaymentMethod     string `json:"payment_method"`
	TransferReference string `json:"transfer_reference"`
	SenderNumber      string `json:"sender_number"`
	Amount            int    `json:"amount"`
}

// PurchaseRequestErrorResponse is the typed error returned by purchase request endpoints.
type PurchaseRequestErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	MessageAr string `json:"message_ar"`
	MessageEn string `json:"message_en"`
	// SubjectID names the subject that failed, when the error is per-subject.
	SubjectID string `json:"subject_id,omitempty"`
}

func writePurchaseRequestError(w http.ResponseWriter, status int, code, msgAr, msgEn string) {
	handlerutil.WriteJSON(w, status, PurchaseRequestErrorResponse{
		Error:     code,
		Code:      code,
		MessageAr: msgAr,
		MessageEn: msgEn,
	})
}

// CreatePurchaseRequest handles POST /academy/subjects/{id}/purchase-request.
// Enforces web client, verified student, published unexpired subject, not owned,
// one open pending request per subject, unique normalized transfer reference,
// and strict JSON payload validation.
func (s *Server) CreatePurchaseRequest(w http.ResponseWriter, r *http.Request, id string) {
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
		handlerutil.WriteSafeError(w, r, http.StatusForbidden, "web_client_required", "purchase requests are only available from the web client", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	subj, err := s.Store.GetSubjectByID(ctx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil || subj.Status != models.StatusPublished {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "subject not found", nil)
		return
	}

	now := time.Now().UTC()
	// Refuse if subject access has expired
	if !subj.AccessExpiresAt.IsZero() && subj.AccessExpiresAt.Before(now) {
		writePurchaseRequestError(w, http.StatusConflict, "subject_expired", "انتهت فترة التسجيل في هذه المادة", "Registration period for this subject has expired")
		return
	}

	// Refuse if student already owns the subject
	owned, err := s.Store.HasActiveEntitlement(ctx, claims.UserID, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if owned {
		writePurchaseRequestError(w, http.StatusConflict, "already_owned", "أنت مشترك في هذه المادة بالفعل", "You already own this subject")
		return
	}

	// Refuse if student already has an open pending request for this subject
	pending, err := s.Store.GetPendingRequest(ctx, claims.UserID, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if pending != nil {
		writePurchaseRequestError(w, http.StatusConflict, "pending_request_exists", "لديك طلب اشتراك معلق لهذه المادة بالفعل", "You already have a pending purchase request for this subject")
		return
	}

	// Decode body with DisallowUnknownFields
	var req CreatePurchaseRequestDTO
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writePurchaseRequestError(w, http.StatusBadRequest, "invalid_json", "بيانات الطلب غير صالحة", "Invalid request body")
		return
	}

	pm := strings.ToLower(strings.TrimSpace(req.PaymentMethod))
	if pm != "wallet" && pm != "instapay" {
		writePurchaseRequestError(w, http.StatusBadRequest, "invalid_payment_method", "طريقة الدفع غير صالحة (المحافظ الإلكترونية أو إنستاباي)", "Invalid payment method (wallet or instapay)")
		return
	}

	ref := strings.TrimSpace(req.TransferReference)
	refRunes := []rune(ref)
	if len(refRunes) < 3 || len(refRunes) > 100 {
		writePurchaseRequestError(w, http.StatusBadRequest, "invalid_transfer_reference", "رقم أو مرجع الحوالة يجب أن يكون بين 3 و 100 حرف", "Transfer reference must be between 3 and 100 characters")
		return
	}

	sender := strings.TrimSpace(req.SenderNumber)
	senderRunes := []rune(sender)
	if len(senderRunes) < 3 || len(senderRunes) > 50 {
		writePurchaseRequestError(w, http.StatusBadRequest, "invalid_sender_number", "رقم الهاتف أو العنوان المحول منه غير صالح", "Sender number or address must be between 3 and 50 characters")
		return
	}

	if req.Amount <= 0 {
		writePurchaseRequestError(w, http.StatusBadRequest, "invalid_amount", "المبلغ المحول غير صالح", "Transfer amount must be greater than zero")
		return
	}

	normRef := strings.ToUpper(ref)
	hasRef, err := s.Store.HasPurchaseRequestWithReference(ctx, normRef)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	hasOrderRef, err := s.Store.HasOrderWithNormalizedReference(ctx, normRef)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if hasRef || hasOrderRef {
		writePurchaseRequestError(w, http.StatusConflict, "duplicate_reference", "تم استخدام رقم الحوالة هذا في طلب سابق", "This transfer reference has already been used")
		return
	}

	// The single-subject endpoint is kept for compatibility as an order of one
	// (2026-11-05 orders brief): one web order holds the proof, one request
	// keeps the same fields so the existing console list is unchanged.
	orderID, err := jwtutil.GenerateUUID()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "failed to generate order id", err)
		return
	}
	prID, err := jwtutil.GenerateUUID()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "failed to generate request id", err)
		return
	}
	newReq := &models.PurchaseRequest{
		ID:                  prID,
		UserID:              claims.UserID,
		SubjectID:           id,
		OrderID:             orderID,
		Status:              models.RequestStatusPending,
		Source:              "web",
		PaymentMethod:       pm,
		TransferReference:   ref,
		NormalizedReference: normRef,
		SenderNumber:        sender,
		Amount:              req.Amount,
		CreatedAt:           now,
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
		AmountPaid:          req.Amount,
		CreatedAt:           now,
	}

	if err := s.Store.CreateOrderWithRequests(ctx, order, []*models.PurchaseRequest{newReq}); err != nil {
		if errors.Is(err, store.ErrPendingRequestExists) {
			writePurchaseRequestError(w, http.StatusConflict, "pending_request_exists", "لديك طلب اشتراك معلق لهذه المادة بالفعل", "You already have a pending purchase request for this subject")
			return
		}
		if errors.Is(err, store.ErrDuplicateReference) || errors.Is(err, store.ErrDuplicate) {
			writePurchaseRequestError(w, http.StatusConflict, "duplicate_reference", "تم استخدام رقم الحوالة هذا في طلب سابق", "This transfer reference has already been used")
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusCreated, models.PurchaseRequestResponseDTO{
		ID:        newReq.ID,
		SubjectID: newReq.SubjectID,
		Status:    newReq.Status,
		Source:    newReq.Source,
		Amount:    newReq.Amount,
		CreatedAt: newReq.CreatedAt,
	})
}

// GetMyRequests handles GET /academy/me/requests.
// Returns all purchase requests of the authenticated student, newest first.
func (s *Server) GetMyRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	requests, err := s.Store.ListRequestsByUser(dbCtx, claims.UserID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	// Order newest first
	sort.Slice(requests, func(i, j int) bool {
		return requests[i].CreatedAt.After(requests[j].CreatedAt)
	})

	items := make([]models.StudentPurchaseRequestItemDTO, 0, len(requests))
	for _, req := range requests {
		subj, err := s.Store.GetSubjectByID(dbCtx, req.SubjectID)
		if err != nil {
			cleanID := strings.ReplaceAll(strings.ReplaceAll(req.SubjectID, "\r", ""), "\n", "")
			// #nosec G706 -- cleanID sanitized of CR/LF
			log.Printf("[ACADEMY] student request list: get subject %s: %v", cleanID, err)
		}
		titleAr := ""
		titleEn := ""
		if subj != nil {
			titleAr = subj.TitleAr
			titleEn = subj.TitleEn
		}
		src := req.Source
		if src == "" {
			src = "mobile"
		}
		items = append(items, models.StudentPurchaseRequestItemDTO{
			ID:             req.ID,
			SubjectID:      req.SubjectID,
			SubjectTitleAr: titleAr,
			SubjectTitleEn: titleEn,
			Status:         req.Status,
			Source:         src,
			CreatedAt:      req.CreatedAt,
			DecidedAt:      req.DecidedAt,
			RejectReason:   req.RejectReason,
			Amount:         req.Amount,
		})
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

// GetMyRequestByID handles GET /academy/me/requests/{id}.
// Enforces IDOR protection: if the request belongs to another student, 404 not_found is returned.
func (s *Server) GetMyRequestByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	reqID := strings.TrimPrefix(r.URL.Path, "/academy/me/requests/")
	reqID = strings.Trim(reqID, "/")
	if reqID == "" || !validResourceID(reqID) {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "request not found", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	pr, err := s.Store.GetRequestByID(dbCtx, reqID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if pr == nil || pr.UserID != claims.UserID {
		// IDOR protection: return generic 404 if not found OR owned by another student
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "request not found", nil)
		return
	}

	subj, err := s.Store.GetSubjectByID(dbCtx, pr.SubjectID)
	if err != nil {
		cleanID := strings.ReplaceAll(strings.ReplaceAll(pr.SubjectID, "\r", ""), "\n", "")
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ACADEMY] student get request: get subject %s: %v", cleanID, err)
	}
	titleAr := ""
	titleEn := ""
	if subj != nil {
		titleAr = subj.TitleAr
		titleEn = subj.TitleEn
	}
	src := pr.Source
	if src == "" {
		src = "mobile"
	}

	handlerutil.WriteJSON(w, http.StatusOK, models.StudentPurchaseRequestItemDTO{
		ID:             pr.ID,
		SubjectID:      pr.SubjectID,
		SubjectTitleAr: titleAr,
		SubjectTitleEn: titleEn,
		Status:         pr.Status,
		Source:         src,
		CreatedAt:      pr.CreatedAt,
		DecidedAt:      pr.DecidedAt,
		RejectReason:   pr.RejectReason,
		Amount:         pr.Amount,
	})
}

// GetPaymentMethods serves GET /academy/payment-methods.
// Accessible only to authenticated web clients (claims.Client == "web").
func (s *Server) GetPaymentMethods(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	if claims.Client != "web" {
		handlerutil.WriteSafeError(w, r, http.StatusForbidden, "web_client_required", "payment methods are only available for web clients", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	settings, err := s.effectiveAppSettings(dbCtx)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	methods := settings.PaymentMethods
	if methods == nil {
		methods = []models.PaymentMethod{}
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"items": methods,
	})
}
