package handlers

import (
	"context"
	"errors"
	"fmt"
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
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	SubjectID       string     `json:"subject_id"`
	SubjectTitleAr  string     `json:"subject_title_ar"`
	LevelNameAr     string     `json:"level_name_ar"`
	Price           int        `json:"price"`
	AccessExpiresAt time.Time  `json:"access_expires_at"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
	RejectReason    string     `json:"reject_reason,omitempty"`
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

	page := 1
	if pStr := strings.TrimSpace(q.Get("page")); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p >= 1 {
			page = p
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
		subj, _ := s.Store.GetSubjectByID(dbCtx, req.SubjectID)
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
		items = append(items, RequestAdminDTO{
			ID:              req.ID,
			UserID:          req.UserID,
			SubjectID:       req.SubjectID,
			SubjectTitleAr:  titleAr,
			LevelNameAr:     lvlName,
			Price:           price,
			AccessExpiresAt: expires,
			Status:          req.Status,
			CreatedAt:       req.CreatedAt,
			DecidedAt:       req.DecidedAt,
			RejectReason:    req.RejectReason,
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

// AcceptRequest handles POST /internal/admin/requests/{id}/accept following R4.
func (s *Server) AcceptRequest(w http.ResponseWriter, r *http.Request, reqID string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	if !validResourceID(reqID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_request_id", "invalid request id", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	// 1. Load the request and the subject.
	pr, err := s.Store.GetRequestByID(dbCtx, reqID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if pr == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "request_not_found", "request not found", nil)
		return
	}
	if pr.Status != models.RequestStatusPending {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "request_not_pending", "request is not pending", nil)
		return
	}

	subj, err := s.Store.GetSubjectByID(dbCtx, pr.SubjectID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "subject_not_found", "subject not found", nil)
		return
	}

	now := time.Now().UTC()
	// Subject access_expires_at in the past: 409 subject_expired (D20); create nothing.
	if !subj.AccessExpiresAt.IsZero() && subj.AccessExpiresAt.Before(now) {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "subject_expired", "subject access has expired", nil)
		return
	}

	// 2. Upsert the entitlement (idempotent): source=request, request_id,
	// expires_at = subject.AccessExpiresAt (D21), granted_by = admin.ID.
	// If student already holds an active entitlement for that subject (for example
	// a manual grant), reuse it: no second entitlement and no second payment record.
	activeEnt, err := s.Store.GetActiveEntitlement(dbCtx, pr.UserID, pr.SubjectID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	var entID string
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
		if err := s.Store.Grant(dbCtx, ent); err != nil {
			if errors.Is(err, store.ErrDuplicate) {
				reloaded, rErr := s.Store.GetActiveEntitlement(dbCtx, pr.UserID, pr.SubjectID)
				if rErr != nil || reloaded == nil {
					handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
					return
				}
				entID = reloaded.ID
			} else if errors.Is(err, store.ErrSubjectExpired) {
				handlerutil.WriteSafeError(w, r, http.StatusConflict, "subject_expired", "subject access has expired", nil)
				return
			} else {
				handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
				return
			}
		} else {
			entID = newEntID
		}
	}

	// 3. Insert the payment record for that entitlement.
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
		if err := s.Store.CreatePaymentRecord(dbCtx, payRec); err != nil && !errors.Is(err, store.ErrDuplicate) {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
	}

	// 4. Compare-and-set the request from pending to accepted (decided_at, decided_by).
	ok, err = s.Store.DecideRequest(dbCtx, pr.ID, models.RequestStatusPending, models.RequestStatusAccepted, adm.ID, now, "")
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "request_not_pending", "request is not pending", nil)
		return
	}

	// 5. Write the audit entry.
	detail := fmt.Sprintf("subject_id=%s user_id=%s amount=%d", pr.SubjectID, pr.UserID, subj.Price)
	if err := s.writeAdminAudit(r.Context(), adm, "request_accept", "request", pr.ID, detail); err != nil {
		logAuditFailure(adm, "request_accept", pr.ID, err)
	}

	// 6. Notify the student, best-effort.
	s.notifyStudent(r.Context(), pr.UserID, "request_accept", func(ctx context.Context) error {
		return notify.SubjectActivated(ctx, s.NotifyURL, s.NotifyToken, pr.UserID, subj.TitleAr, subj.TitleEn)
	})

	lvl, _ := s.Store.GetLevelByKey(dbCtx, subj.LevelKey)
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
	handlerutil.WriteJSON(w, http.StatusOK, dto)
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

	pr, err := s.Store.GetRequestByID(dbCtx, reqID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if pr == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "request_not_found", "request not found", nil)
		return
	}
	if pr.Status != models.RequestStatusPending {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "request_not_pending", "request is not pending", nil)
		return
	}

	now := time.Now().UTC()
	ok, err = s.Store.DecideRequest(dbCtx, pr.ID, models.RequestStatusPending, models.RequestStatusRejected, adm.ID, now, reason)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "request_not_pending", "request is not pending", nil)
		return
	}

	detail := fmt.Sprintf("subject_id=%s user_id=%s reason=%s", pr.SubjectID, pr.UserID, reason)
	if err := s.writeAdminAudit(r.Context(), adm, "request_reject", "request", pr.ID, detail); err != nil {
		logAuditFailure(adm, "request_reject", pr.ID, err)
	}

	subj, _ := s.Store.GetSubjectByID(dbCtx, pr.SubjectID)
	titleAr := ""
	titleEn := ""
	if subj != nil {
		titleAr = subj.TitleAr
		titleEn = subj.TitleEn
	}
	s.notifyStudent(r.Context(), pr.UserID, "request_reject", func(ctx context.Context) error {
		return notify.SubjectRejected(ctx, s.NotifyURL, s.NotifyToken, pr.UserID, titleAr, titleEn, reason)
	})

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
