package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
)

// AdminRequestGroups handles GET /internal/admin/request-groups: matching
// requests grouped by student, then by order (2026-11-05 orders brief).
func (s *Server) AdminRequestGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	if status == "" {
		status = models.RequestStatusPending
	}
	if status != models.RequestStatusPending && status != models.RequestStatusAccepted && status != models.RequestStatusRejected {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_status", "invalid status filter", nil)
		return
	}
	userID := strings.TrimSpace(q.Get("user_id"))
	if userID != "" && !validResourceID(userID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_user_id", "invalid user id", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	requests, err := s.Store.ListRequestsForGroups(ctx, store.OrderRequestFilter{Status: status, UserID: userID})
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	pendingCount, err := s.Store.CountPendingRequests(ctx)
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
	if err := s.loadSubjects(ctx, requests, subjects); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	students := make(map[string]*adminStudentGroup)
	for _, req := range requests {
		sg := students[req.UserID]
		if sg == nil {
			sg = &adminStudentGroup{userID: req.UserID, groups: make(map[string]*adminOrderGroup)}
			students[req.UserID] = sg
		}
		key := req.OrderID
		isFallback := false
		if key == "" {
			key = "legacy"
			isFallback = true
		}
		og := sg.groups[key]
		if og == nil {
			og = &adminOrderGroup{isFallback: isFallback, sortAt: req.CreatedAt}
			if !isFallback {
				order, err := s.Store.GetOrderByID(ctx, req.OrderID)
				if err != nil {
					handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
					return
				}
				og.order = order
				if order != nil && !order.CreatedAt.IsZero() {
					og.sortAt = order.CreatedAt
				}
			}
			sg.groups[key] = og
			sg.keys = append(sg.keys, key)
		}
		og.reqs = append(og.reqs, req)
		if req.CreatedAt.After(og.sortAt) {
			og.sortAt = req.CreatedAt
		}
		if req.CreatedAt.After(sg.latest) {
			sg.latest = req.CreatedAt
		}
	}

	orderedStudents := make([]*adminStudentGroup, 0, len(students))
	for _, sg := range students {
		orderedStudents = append(orderedStudents, sg)
	}
	sort.SliceStable(orderedStudents, func(i, j int) bool {
		if orderedStudents[i].latest.Equal(orderedStudents[j].latest) {
			return orderedStudents[i].userID < orderedStudents[j].userID
		}
		return orderedStudents[i].latest.After(orderedStudents[j].latest)
	})

	items := make([]models.StudentOrderGroupDTO, 0, len(orderedStudents))
	totalOrders := 0
	for _, sg := range orderedStudents {
		orderedGroups := make([]*adminOrderGroup, 0, len(sg.groups))
		for _, key := range sg.keys {
			orderedGroups = append(orderedGroups, sg.groups[key])
		}
		sort.SliceStable(orderedGroups, func(i, j int) bool {
			return orderedGroups[i].sortAt.After(orderedGroups[j].sortAt)
		})

		orderDTOs := make([]models.AdminOrderDTO, 0, len(orderedGroups))
		for _, og := range orderedGroups {
			sort.SliceStable(og.reqs, func(i, j int) bool {
				return og.reqs[i].CreatedAt.Before(og.reqs[j].CreatedAt)
			})
			orderDTOs = append(orderDTOs, adminOrderDTO(og, subjects, levels))
		}
		totalOrders += len(orderDTOs)
		items = append(items, models.StudentOrderGroupDTO{UserID: sg.userID, Orders: orderDTOs})
	}

	handlerutil.WriteJSON(w, http.StatusOK, models.OrderGroupsResponseDTO{
		Items:         items,
		TotalStudents: len(items),
		TotalOrders:   totalOrders,
		TotalRequests: len(requests),
		PendingCount:  pendingCount,
	})
}

type adminOrderGroup struct {
	order      *models.Order
	reqs       []*models.PurchaseRequest
	isFallback bool
	sortAt     time.Time
}

type adminStudentGroup struct {
	userID string
	groups map[string]*adminOrderGroup
	keys   []string
	latest time.Time
}

// adminOrderDTO builds one order block of the grouped read model. A legacy
// request with no order_id falls back to a block with an empty order_id and
// the request's source (contract 4.1).
func adminOrderDTO(og *adminOrderGroup, subjects map[string]*models.Subject, levels map[string]*models.Level) models.AdminOrderDTO {
	items := orderItems(og.reqs, subjects, levels)
	total := orderTotal(items)

	if og.order != nil {
		dto := models.AdminOrderDTO{
			OrderID:   og.order.ID,
			Source:    og.order.Source,
			Status:    og.order.Status,
			CreatedAt: og.order.CreatedAt,
			Total:     total,
			Items:     items,
		}
		if og.order.Source == models.OrderSourceWeb {
			dto.PaymentMethod = og.order.PaymentMethod
			dto.TransferReference = og.order.TransferReference
			dto.SenderNumber = og.order.SenderNumber
			dto.AmountPaid = og.order.AmountPaid
			dto.AmountMismatch = og.order.AmountPaid != total
		}
		return dto
	}

	// Legacy fallback: group by the request's source and derive an order-like view.
	source := "mobile"
	if len(og.reqs) > 0 && og.reqs[0].Source != "" {
		source = og.reqs[0].Source
	}
	status := models.OrderStatusClosed
	var createdAt time.Time
	amountPaid := 0
	for _, req := range og.reqs {
		if req.Status == models.RequestStatusPending {
			status = models.OrderStatusOpen
		}
		if createdAt.IsZero() || req.CreatedAt.Before(createdAt) {
			createdAt = req.CreatedAt
		}
		amountPaid += req.Amount
	}
	dto := models.AdminOrderDTO{
		OrderID:   "",
		Source:    source,
		Status:    status,
		CreatedAt: createdAt,
		Total:     total,
		Items:     items,
	}
	if source == "web" {
		dto.AmountPaid = amountPaid
		dto.AmountMismatch = amountPaid != total
	}
	return dto
}

// BulkRequests handles POST /internal/admin/requests/bulk. Every id is
// processed independently with the single-endpoint rules; a valid request
// always answers 200 with one result per id, in input order.
func (s *Server) BulkRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var body struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
		Reason string   `json:"reason"`
	}
	if !decodeAdminJSON(w, r, &body) {
		return
	}

	action := strings.ToLower(strings.TrimSpace(body.Action))
	if action != "accept" && action != "reject" {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_action", "action must be accept or reject", nil)
		return
	}
	if len(body.IDs) < 1 || len(body.IDs) > 100 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_ids", "ids must contain 1-100 unique ids", nil)
		return
	}

	ids := make([]string, 0, len(body.IDs))
	seen := make(map[string]bool, len(body.IDs))
	for _, raw := range body.IDs {
		id := strings.TrimSpace(raw)
		if !validResourceID(id) || seen[id] {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_ids", "ids must contain 1-100 unique ids", nil)
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	reason := ""
	if action == "reject" {
		var valid bool
		reason, valid = cleanAdminReason(body.Reason)
		if !valid {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_reason", "reason must be 1-1000 characters", nil)
			return
		}
	}

	results := make([]models.BulkRequestResult, 0, len(ids))
	for _, id := range ids {
		itemCtx, itemCancel := context.WithTimeout(r.Context(), dbTimeout)
		var res decisionResult
		if action == "accept" {
			res = s.acceptRequestCore(itemCtx, adm, id, true)
		} else {
			res = s.rejectRequestCore(itemCtx, adm, id, reason, true)
		}
		itemCancel()
		if res.OK {
			results = append(results, models.BulkRequestResult{ID: id, Status: res.Status})
			continue
		}
		results = append(results, models.BulkRequestResult{
			ID:        id,
			Status:    "failed",
			ErrorCode: res.Code,
			MessageAr: res.MsgAr,
			MessageEn: res.MsgEn,
		})
	}

	summaryAction := "request_bulk_accept"
	if action == "reject" {
		summaryAction = "request_bulk_reject"
	}
	detail := fmt.Sprintf("count=%d ids=%s", len(ids), strings.Join(ids, ","))
	auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(r.Context()), dbTimeout)
	if err := s.writeAdminAudit(auditCtx, adm, summaryAction, "request_bulk", ids[0], detail); err != nil {
		logAuditFailure(adm, summaryAction, ids[0], err)
	}
	auditCancel()

	handlerutil.WriteJSON(w, http.StatusOK, models.BulkRequestsResponseDTO{Results: results})
}
