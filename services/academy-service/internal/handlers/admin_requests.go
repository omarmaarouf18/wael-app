package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/notify"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// RequestAdminDTO is the admin queue view of a purchase request.
type RequestAdminDTO struct {
	ID                string     `json:"id"`
	UserID            string     `json:"user_id"`
	SubjectID         string     `json:"subject_id"`
	SubjectTitleAr    string     `json:"subject_title_ar"`
	LevelNameAr       string     `json:"level_name_ar"`
	Price             int        `json:"price"`
	AccessExpiresAt   time.Time  `json:"access_expires_at"`
	Status            string     `json:"status"`
	Source            string     `json:"source"`
	PaymentMethod     string     `json:"payment_method,omitempty"`
	TransferReference string     `json:"transfer_reference,omitempty"`
	SenderNumber      string     `json:"sender_number,omitempty"`
	Amount            int        `json:"amount,omitempty"`
	AmountWarning     bool       `json:"amount_warning,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	DecidedAt         *time.Time `json:"decided_at,omitempty"`
	RejectReason      string     `json:"reject_reason,omitempty"`
}

// RequestsListResponseDTO wraps the request queue page and the pending count for badge.
type RequestsListResponseDTO struct {
	Items        []RequestAdminDTO `json:"items"`
	Total        int               `json:"total"`
	Page         int               `json:"page"`
	Limit        int               `json:"limit"`
	PendingCount int               `json:"pending_count"`
}

// AdminRequests dispatches GET /internal/admin/requests.
func (s *Server) AdminRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	s.ListAdminRequests(w, r)
}

// AdminRequestSubroute dispatches POST /internal/admin/requests/{id}/accept|reject.
func (s *Server) AdminRequestSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/internal/admin/requests/")
	rest = strings.Trim(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || !validResourceID(parts[0]) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_request_id", "invalid request id", nil)
		return
	}
	id := parts[0]
	action := parts[1]

	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	switch action {
	case "accept":
		s.AcceptRequest(w, r, id)
	case "reject":
		s.RejectRequest(w, r, id)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
	}
}

// ListAdminRequests handles GET /internal/admin/requests?status=&subject_id=&page=&limit=.
// Returns requests newest first with limit at most 100, and returns pending_count.
func (s *Server) ListAdminRequests(w http.ResponseWriter, r *http.Request) {
	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	if status != "" && status != models.RequestStatusPending && status != models.RequestStatusAccepted && status != models.RequestStatusRejected {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_status", "invalid status filter", nil)
		return
	}
	subjectID := strings.TrimSpace(q.Get("subject_id"))
	if subjectID != "" && !validResourceID(subjectID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_subject_id", "invalid subject id", nil)
		return
	}

	const maxPage = 10000
	page := 1
	if pStr := strings.TrimSpace(q.Get("page")); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil {
			if p < 1 {
				page = 1
			} else if p > maxPage {
				page = maxPage
			} else {
				page = p
			}
		}
	}
	limit := 20
	if lStr := strings.TrimSpace(q.Get("limit")); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil {
			if l <= 0 {
				limit = 20
			} else if l > 100 {
				limit = 100
			} else {
				limit = l
			}
		}
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	requests, total, err := s.Store.ListRequests(dbCtx, store.RequestFilter{
		Status:    status,
		SubjectID: subjectID,
		Page:      page,
		Limit:     limit,
	})
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	pendingCount, err := s.Store.CountPendingRequests(dbCtx)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	allLevels, err := s.Store.ListLevels(dbCtx, false)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	levelsByKey := make(map[string]*models.Level, len(allLevels))
	for _, l := range allLevels {
		levelsByKey[l.Key] = l
	}

	items := make([]RequestAdminDTO, 0, len(requests))
	for _, req := range requests {
		subj, err := s.Store.GetSubjectByID(dbCtx, req.SubjectID)
		if err != nil {
			cleanID := strings.ReplaceAll(strings.ReplaceAll(req.SubjectID, "\r", ""), "\n", "")
			// #nosec G706 -- cleanID sanitized of CR/LF
			log.Printf("[ACADEMY] request list: get subject %s: %v", cleanID, err)
		}
		titleAr := ""
		lvlName := ""
		price := 0
		var expires time.Time
		if subj != nil {
			titleAr = subj.TitleAr
			price = subj.Price
			expires = subj.AccessExpiresAt
			if lvl := levelsByKey[subj.LevelKey]; lvl != nil {
				lvlName = lvl.TitleAr
			}
		}
		src := req.Source
		if src == "" {
			src = "mobile"
		}
		amtWarning := false
		if src == "web" && req.Amount != price {
			amtWarning = true
		}
		items = append(items, RequestAdminDTO{
			ID:                req.ID,
			UserID:            req.UserID,
			SubjectID:         req.SubjectID,
			SubjectTitleAr:    titleAr,
			LevelNameAr:       lvlName,
			Price:             price,
			AccessExpiresAt:   expires,
			Status:            req.Status,
			Source:            src,
			PaymentMethod:     req.PaymentMethod,
			TransferReference: req.TransferReference,
			SenderNumber:      req.SenderNumber,
			Amount:            req.Amount,
			AmountWarning:     amtWarning,
			CreatedAt:         req.CreatedAt,
			DecidedAt:         req.DecidedAt,
			RejectReason:      req.RejectReason,
		})
	}

	handlerutil.WriteJSON(w, http.StatusOK, RequestsListResponseDTO{
		Items:        items,
		Total:        total,
		Page:         page,
		Limit:        limit,
		PendingCount: pendingCount,
	})
}

// decisionResult is the outcome of one accept/reject core call. The single
// endpoints map it to their existing responses; the bulk endpoint maps it to
// one per-id result. A failed result always carries bilingual messages.
type decisionResult struct {
	OK      bool
	Status  string // accepted | rejected
	Code    string
	HTTP    int
	MsgAr   string
	MsgEn   string
	Request *models.PurchaseRequest
	Subject *models.Subject
	DTO     *RequestAdminDTO
}

func decisionFailure(status int, code, msgAr, msgEn string) decisionResult {
	return decisionResult{HTTP: status, Code: code, MsgAr: msgAr, MsgEn: msgEn}
}

// writeDecisionFailure writes a failed accept/reject core result. The explicit
// request_not_found / request_not_pending cases keep those codes as literals
// in this file: the console error-catalog test scans admin handlers for the
// codes they can relay to the UI.
func writeDecisionFailure(w http.ResponseWriter, r *http.Request, res decisionResult) {
	switch res.Code {
	case "request_not_found":
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "request_not_found", res.MsgEn, nil)
	case "request_not_pending":
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "request_not_pending", res.MsgEn, nil)
	default:
		handlerutil.WriteSafeError(w, r, res.HTTP, res.Code, res.MsgEn, nil)
	}
}

// AcceptRequest handles POST /internal/admin/requests/{id}/accept following R4.
func (s *Server) AcceptRequest(w http.ResponseWriter, r *http.Request, reqID string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	res := s.acceptRequestCore(dbCtx, adm, reqID)
	if !res.OK {
		writeDecisionFailure(w, r, res)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, *res.DTO)
}

// acceptRequestCore applies today's single-accept rules to one request. It is
// shared by the single endpoint and the bulk endpoint so both behave the same.
func (s *Server) acceptRequestCore(ctx context.Context, adm *AdminIdentity, reqID string, async ...bool) decisionResult {
	if !validResourceID(reqID) {
		return decisionFailure(http.StatusBadRequest, "invalid_request_id", "معرّف الطلب غير صالح", "Invalid request id")
	}

	// 1. Load the request and the subject.
	pr, err := s.Store.GetRequestByID(ctx, reqID)
	if err != nil {
		return decisionFailure(http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "الخدمة غير متاحة مؤقتًا", "Service temporarily unavailable")
	}
	if pr == nil {
		return decisionFailure(http.StatusNotFound, "request_not_found", "الطلب غير موجود", "Request not found")
	}
	if pr.Status != models.RequestStatusPending {
		return decisionFailure(http.StatusConflict, "request_not_pending", "الطلب لم يعد معلقًا", "Request is not pending")
	}

	subj, err := s.Store.GetSubjectByID(ctx, pr.SubjectID)
	if err != nil {
		return decisionFailure(http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "الخدمة غير متاحة مؤقتًا", "Service temporarily unavailable")
	}
	if subj == nil {
		return decisionFailure(http.StatusNotFound, "subject_not_found", "المادة غير موجودة", "Subject not found")
	}

	now := time.Now().UTC()
	// Subject access_expires_at in the past: 409 subject_expired (D20); create nothing.
	if !subj.AccessExpiresAt.IsZero() && subj.AccessExpiresAt.Before(now) {
		return decisionFailure(http.StatusConflict, "subject_expired", "انتهت فترة التسجيل في هذه المادة", "Subject access has expired")
	}

	// 2. Claim the request: compare-and-set pending -> accepted (decided_at,
	// decided_by) BEFORE anything is granted (SPEC Section 7 R4, amended
	// 2026-11-09). A concurrent reject or accept that wins this CAS makes
	// this call a clean 409 with nothing written, so a rejected request can
	// never end up with an entitlement or a payment record.
	ok, err := s.Store.DecideRequest(ctx, pr.ID, models.RequestStatusPending, models.RequestStatusAccepted, adm.ID, now, "")
	if err != nil {
		return decisionFailure(http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "الخدمة غير متاحة مؤقتًا", "Service temporarily unavailable")
	}
	if !ok {
		return decisionFailure(http.StatusConflict, "request_not_pending", "الطلب لم يعد معلقًا", "Request is not pending")
	}

	// undo gives the claim back when a later step fails: the entitlement this
	// call created (if any) is revoked first, then the request returns to
	// pending, so accepting again is safe and a reject afterwards leaves no
	// access behind. It runs on its own deadline so an expired request (or
	// bulk) context cannot skip it.
	undo := func(createdEntID string) {
		undoCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbTimeout)
		defer cancel()
		if createdEntID != "" {
			if _, rErr := s.Store.RevokeEntitlement(undoCtx, createdEntID, adm.ID, "accept did not complete", time.Now().UTC()); rErr != nil {
				s.logAcceptUndoFailure(adm, pr.ID, "revoke entitlement", rErr)
			}
		}
		reverted, uErr := s.Store.UndoAccept(undoCtx, pr.ID, adm.ID, now)
		if uErr != nil {
			s.logAcceptUndoFailure(adm, pr.ID, "revert request", uErr)
		} else if !reverted {
			s.logAcceptUndoFailure(adm, pr.ID, "revert request", errors.New("request no longer holds this accept"))
		}
	}

	// 3. Upsert the entitlement (idempotent): source=request, request_id,
	// expires_at = subject.AccessExpiresAt (D21), granted_by = admin.ID.
	// If student already holds an active entitlement for that subject (for example
	// a manual grant), reuse it: no second entitlement and no second payment record.
	activeEnt, err := s.Store.GetActiveEntitlement(ctx, pr.UserID, pr.SubjectID)
	if err != nil {
		undo("")
		return decisionFailure(http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "الخدمة غير متاحة مؤقتًا", "Service temporarily unavailable")
	}

	var entID, createdEntID string
	skipPayment := false
	if activeEnt != nil {
		entID = activeEnt.ID
		if activeEnt.Source == models.EntitlementSourceAdminGrant {
			skipPayment = true
		}
	} else {
		newEntID, _ := jwtutil.GenerateUUID()
		ent := &models.Entitlement{
			ID:        newEntID,
			UserID:    pr.UserID,
			SubjectID: pr.SubjectID,
			ExpiresAt: subj.AccessExpiresAt,
			GrantedAt: now,
			Source:    models.EntitlementSourceRequest,
			GrantedBy: adm.ID,
			RequestID: pr.ID,
			Active:    true,
		}
		if err := s.Store.Grant(ctx, ent); err != nil {
			if errors.Is(err, store.ErrDuplicate) {
				reloaded, rErr := s.Store.GetActiveEntitlement(ctx, pr.UserID, pr.SubjectID)
				if rErr != nil || reloaded == nil {
					undo("")
					return decisionFailure(http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "الخدمة غير متاحة مؤقتًا", "Service temporarily unavailable")
				}
				entID = reloaded.ID
			} else if errors.Is(err, store.ErrSubjectExpired) {
				undo("")
				return decisionFailure(http.StatusConflict, "subject_expired", "انتهت فترة التسجيل في هذه المادة", "Subject access has expired")
			} else {
				undo("")
				return decisionFailure(http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "الخدمة غير متاحة مؤقتًا", "Service temporarily unavailable")
			}
		} else {
			entID = newEntID
			createdEntID = newEntID
		}
	}

	// 4. Insert the payment record for that entitlement.
	// A duplicate on entitlement_id means it was already recorded: treat it as success.
	if !skipPayment && entID != "" {
		payID, _ := jwtutil.GenerateUUID()
		payRec := &models.PaymentRecord{
			ID:            payID,
			UserID:        pr.UserID,
			SubjectID:     pr.SubjectID,
			EntitlementID: entID,
			RequestID:     pr.ID,
			Amount:        subj.Price,
			PriceAtGrant:  subj.Price,
			Source:        models.PaymentSourceRequest,
			RecordedBy:    adm.ID,
			RecordedAt:    now,
		}
		if err := s.Store.CreatePaymentRecord(ctx, payRec); err != nil && !errors.Is(err, store.ErrDuplicate) {
			undo(createdEntID)
			return decisionFailure(http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "الخدمة غير متاحة مؤقتًا", "Service temporarily unavailable")
		}
	}

	// 5. Close the order once its last pending request is decided. Closing is
	// idempotent bookkeeping; a failure is logged and never undoes the accept.
	if pr.OrderID != "" {
		if _, err := s.Store.CloseOrderIfNoPending(ctx, pr.OrderID, now); err != nil {
			s.logOrderCloseFailure(adm, pr.OrderID, err)
		}
	}

	// 6. Write the audit entry.
	detail := fmt.Sprintf("subject_id=%s user_id=%s amount=%d", pr.SubjectID, pr.UserID, subj.Price)
	if err := s.writeAdminAudit(ctx, adm, "request_accept", "request", pr.ID, detail); err != nil {
		logAuditFailure(adm, "request_accept", pr.ID, err)
	}

	// 7. Notify the student, best-effort.
	push := s.notifyStudent
	if len(async) > 0 && async[0] {
		push = s.notifyStudentAsync
	}
	push(ctx, pr.UserID, "request_accept", func(pushCtx context.Context) error {
		return notify.SubjectActivated(pushCtx, s.NotifyURL, s.NotifyToken, pr.UserID, pr.SubjectID, subj.TitleAr, subj.TitleEn)
	})

	lvl, _ := s.Store.GetLevelByKey(ctx, subj.LevelKey)
	lvlName := ""
	if lvl != nil {
		lvlName = lvl.TitleAr
	}
	dto := RequestAdminDTO{
		ID:              pr.ID,
		UserID:          pr.UserID,
		SubjectID:       pr.SubjectID,
		SubjectTitleAr:  subj.TitleAr,
		LevelNameAr:     lvlName,
		Price:           subj.Price,
		AccessExpiresAt: subj.AccessExpiresAt,
		Status:          models.RequestStatusAccepted,
		CreatedAt:       pr.CreatedAt,
		DecidedAt:       &now,
	}
	return decisionResult{OK: true, Status: models.RequestStatusAccepted, Request: pr, Subject: subj, DTO: &dto}
}

// RejectRequest handles POST /internal/admin/requests/{id}/reject {reason 1-1000}.
func (s *Server) RejectRequest(w http.ResponseWriter, r *http.Request, reqID string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	if !validResourceID(reqID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_request_id", "invalid request id", nil)
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeAdminJSON(w, r, &body) {
		return
	}

	reason, valid := cleanAdminReason(body.Reason)
	if !valid {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_reason", "reason must be 1-1000 characters", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	res := s.rejectRequestCore(dbCtx, adm, reqID, reason)
	if !res.OK {
		writeDecisionFailure(w, r, res)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// rejectRequestCore applies today's single-reject rules to one request. It is
// shared by the single endpoint and the bulk endpoint.
func (s *Server) rejectRequestCore(ctx context.Context, adm *AdminIdentity, reqID, reason string, async ...bool) decisionResult {
	if !validResourceID(reqID) {
		return decisionFailure(http.StatusBadRequest, "invalid_request_id", "معرّف الطلب غير صالح", "Invalid request id")
	}

	pr, err := s.Store.GetRequestByID(ctx, reqID)
	if err != nil {
		return decisionFailure(http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "الخدمة غير متاحة مؤقتًا", "Service temporarily unavailable")
	}
	if pr == nil {
		return decisionFailure(http.StatusNotFound, "request_not_found", "الطلب غير موجود", "Request not found")
	}
	if pr.Status != models.RequestStatusPending {
		return decisionFailure(http.StatusConflict, "request_not_pending", "الطلب لم يعد معلقًا", "Request is not pending")
	}

	now := time.Now().UTC()
	ok, err := s.Store.DecideRequest(ctx, pr.ID, models.RequestStatusPending, models.RequestStatusRejected, adm.ID, now, reason)
	if err != nil {
		return decisionFailure(http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "الخدمة غير متاحة مؤقتًا", "Service temporarily unavailable")
	}
	if !ok {
		return decisionFailure(http.StatusConflict, "request_not_pending", "الطلب لم يعد معلقًا", "Request is not pending")
	}

	// Close the order once its last pending request is decided. Closing is
	// idempotent bookkeeping; a failure is logged and never undoes the reject.
	if pr.OrderID != "" {
		if _, err := s.Store.CloseOrderIfNoPending(ctx, pr.OrderID, now); err != nil {
			s.logOrderCloseFailure(adm, pr.OrderID, err)
		}
	}

	detail := fmt.Sprintf("subject_id=%s user_id=%s reason=%s", pr.SubjectID, pr.UserID, reason)
	if err := s.writeAdminAudit(ctx, adm, "request_reject", "request", pr.ID, detail); err != nil {
		logAuditFailure(adm, "request_reject", pr.ID, err)
	}

	subj, err := s.Store.GetSubjectByID(ctx, pr.SubjectID)
	if err != nil {
		cleanID := strings.ReplaceAll(strings.ReplaceAll(pr.SubjectID, "\r", ""), "\n", "")
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ACADEMY] reject request: get subject %s: %v", cleanID, err)
	}
	titleAr := ""
	titleEn := ""
	if subj != nil {
		titleAr = subj.TitleAr
		titleEn = subj.TitleEn
	}
	push := s.notifyStudent
	if len(async) > 0 && async[0] {
		push = s.notifyStudentAsync
	}
	push(ctx, pr.UserID, "request_reject", func(pushCtx context.Context) error {
		return notify.SubjectRejected(pushCtx, s.NotifyURL, s.NotifyToken, pr.UserID, pr.SubjectID, titleAr, titleEn, reason)
	})

	return decisionResult{OK: true, Status: models.RequestStatusRejected, Request: pr, Subject: subj}
}

// logAcceptUndoFailure records a failed undo of an accept claim with ids
// only. The request may then stay accepted without its entitlement or
// payment record and needs an admin look; it never changes the response.
func (s *Server) logAcceptUndoFailure(adm *AdminIdentity, reqID, step string, err error) {
	cleanID := strings.ReplaceAll(strings.ReplaceAll(reqID, "\r", ""), "\n", "")
	// #nosec G706 -- cleanID sanitized of CR/LF; step is a fixed string
	log.Printf("[ERROR] accept undo failed step=%s actor_id=%s request_id=%s: %v", step, adm.ID, cleanID, err)
}

// logOrderCloseFailure records a best-effort order close failure. The decision
// itself already committed, so this never changes the response.
func (s *Server) logOrderCloseFailure(adm *AdminIdentity, orderID string, err error) {
	cleanID := strings.ReplaceAll(strings.ReplaceAll(orderID, "\r", ""), "\n", "")
	// #nosec G706 -- cleanID sanitized of CR/LF
	log.Printf("[ERROR] order close after decision failed actor_id=%s order_id=%s: %v", adm.ID, cleanID, err)
}
