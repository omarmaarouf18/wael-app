package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/notify"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// EntitlementAdminDTO represents a student's entitlement for the admin view.
type EntitlementAdminDTO struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	SubjectID      string     `json:"subject_id"`
	SubjectTitleAr string     `json:"subject_title_ar"`
	LevelNameAr    string     `json:"level_name_ar"`
	Source         string     `json:"source"`
	GrantedAt      time.Time  `json:"granted_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	Active         bool       `json:"active"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	RevokedBy      string     `json:"revoked_by,omitempty"`
	RevokeReason   string     `json:"revoke_reason,omitempty"`
	IsRevoked      bool       `json:"is_revoked"`
	IsExpired      bool       `json:"is_expired"`
}

// AdminEntitlements dispatches GET (list by user) and POST (manual grant) on /internal/admin/entitlements.
func (s *Server) AdminEntitlements(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.ListAdminEntitlements(w, r)
	case http.MethodPost:
		s.GrantAdminEntitlement(w, r)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

// AdminEntitlementSubroute dispatches DELETE /internal/admin/entitlements/{id}.
func (s *Server) AdminEntitlementSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/internal/admin/entitlements/")
	rest = strings.Trim(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 1 || parts[0] == "" || !validResourceID(parts[0]) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_entitlement_id", "invalid entitlement id", nil)
		return
	}
	id := parts[0]

	if r.Method != http.MethodDelete {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	s.RevokeAdminEntitlement(w, r, id)
}

// ListAdminEntitlements handles GET /internal/admin/entitlements?user_id=.
func (s *Server) ListAdminEntitlements(w http.ResponseWriter, r *http.Request) {
	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if !validResourceID(userID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_user_id", "invalid user id", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	ents, err := s.Store.ListEntitlementsByUser(dbCtx, userID)
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

	now := time.Now().UTC()
	items := make([]EntitlementAdminDTO, 0, len(ents))
	for _, e := range ents {
		subj, _ := s.Store.GetSubjectByID(dbCtx, e.SubjectID)
		titleAr := ""
		lvlName := ""
		if subj != nil {
			titleAr = subj.TitleAr
			if lvl := levelsByKey[subj.LevelKey]; lvl != nil {
				lvlName = lvl.TitleAr
			}
		}
		items = append(items, EntitlementAdminDTO{
			ID:             e.ID,
			UserID:         e.UserID,
			SubjectID:      e.SubjectID,
			SubjectTitleAr: titleAr,
			LevelNameAr:    lvlName,
			Source:         e.Source,
			GrantedAt:      e.GrantedAt,
			ExpiresAt:      e.ExpiresAt,
			Active:         e.Active,
			RevokedAt:      e.RevokedAt,
			RevokedBy:      e.RevokedBy,
			RevokeReason:   e.RevokeReason,
			IsRevoked:      e.IsRevoked(),
			IsExpired:      e.IsExpired(now),
		})
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GrantAdminEntitlement handles POST /internal/admin/entitlements {user_id, subject_id}.
func (s *Server) GrantAdminEntitlement(w http.ResponseWriter, r *http.Request) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var body struct {
		UserID    string `json:"user_id"`
		SubjectID string `json:"subject_id"`
	}
	if !decodeAdminJSON(w, r, &body) {
		return
	}

	userID := strings.TrimSpace(body.UserID)
	if !validResourceID(userID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_user_id", "invalid user id", nil)
		return
	}
	subjectID := strings.TrimSpace(body.SubjectID)
	if !validResourceID(subjectID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_subject_id", "invalid subject id", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	subj, err := s.Store.GetSubjectByID(dbCtx, subjectID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "subject_not_found", "subject not found", nil)
		return
	}

	now := time.Now().UTC()
	if !subj.AccessExpiresAt.IsZero() && subj.AccessExpiresAt.Before(now) {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "subject_expired", "subject access has expired", nil)
		return
	}

	activeEnt, err := s.Store.GetActiveEntitlement(dbCtx, userID, subjectID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if activeEnt != nil {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "already_owned", "student already owns this subject", nil)
		return
	}

	entID, _ := jwtutil.GenerateUUID()
	ent := &models.Entitlement{
		ID:        entID,
		UserID:    userID,
		SubjectID: subjectID,
		ExpiresAt: subj.AccessExpiresAt,
		GrantedAt: now,
		Source:    models.EntitlementSourceAdminGrant,
		GrantedBy: adm.ID,
		Active:    true,
	}
	if err := s.Store.Grant(dbCtx, ent); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, "already_owned", "student already owns this subject", nil)
			return
		}
		if errors.Is(err, store.ErrSubjectExpired) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, "subject_expired", "subject access has expired", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	payID, _ := jwtutil.GenerateUUID()
	payRec := &models.PaymentRecord{
		ID:            payID,
		UserID:        userID,
		SubjectID:     subjectID,
		EntitlementID: entID,
		Amount:        subj.Price,
		PriceAtGrant:  subj.Price,
		Source:        models.PaymentSourceAdminGrant,
		RecordedBy:    adm.ID,
		RecordedAt:    now,
	}
	if err := s.Store.CreatePaymentRecord(dbCtx, payRec); err != nil && !errors.Is(err, store.ErrDuplicate) {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	detail := fmt.Sprintf("subject_id=%s user_id=%s amount=%d", subjectID, userID, subj.Price)
	if err := s.writeAdminAudit(r.Context(), adm, "entitlement_grant", "entitlement", entID, detail); err != nil {
		logAuditFailure(adm, "entitlement_grant", entID, err)
	}

	s.notifyStudent(r.Context(), userID, "entitlement_grant", func(ctx context.Context) error {
		return notify.SubjectActivated(ctx, s.NotifyURL, s.NotifyToken, userID, subj.TitleAr, subj.TitleEn)
	})

	lvl, _ := s.Store.GetLevelByKey(dbCtx, subj.LevelKey)
	lvlName := ""
	if lvl != nil {
		lvlName = lvl.TitleAr
	}
	dto := EntitlementAdminDTO{
		ID:             ent.ID,
		UserID:         ent.UserID,
		SubjectID:      ent.SubjectID,
		SubjectTitleAr: subj.TitleAr,
		LevelNameAr:    lvlName,
		Source:         ent.Source,
		GrantedAt:      ent.GrantedAt,
		ExpiresAt:      ent.ExpiresAt,
		Active:         ent.Active,
		IsRevoked:      false,
		IsExpired:      ent.IsExpired(now),
	}
	handlerutil.WriteJSON(w, http.StatusCreated, dto)
}

// RevokeAdminEntitlement handles DELETE /internal/admin/entitlements/{id} {reason 1-1000}.
func (s *Server) RevokeAdminEntitlement(w http.ResponseWriter, r *http.Request, entID string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	if !validResourceID(entID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_entitlement_id", "invalid entitlement id", nil)
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

	ent, err := s.Store.GetEntitlementByID(dbCtx, entID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if ent == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "entitlement_not_found", "entitlement not found", nil)
		return
	}

	now := time.Now().UTC()
	changed, err := s.Store.RevokeEntitlement(dbCtx, entID, adm.ID, reason, now)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "entitlement_not_found", "entitlement not found", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if changed {
		detail := fmt.Sprintf("subject_id=%s user_id=%s reason=%s", ent.SubjectID, ent.UserID, reason)
		if err := s.writeAdminAudit(r.Context(), adm, "entitlement_revoke", "entitlement", entID, detail); err != nil {
			logAuditFailure(adm, "entitlement_revoke", entID, err)
		}

		subj, _ := s.Store.GetSubjectByID(dbCtx, ent.SubjectID)
		titleAr := ""
		titleEn := ""
		if subj != nil {
			titleAr = subj.TitleAr
			titleEn = subj.TitleEn
		}
		s.notifyStudent(r.Context(), ent.UserID, "entitlement_revoke", func(ctx context.Context) error {
			return notify.SubjectRevoked(ctx, s.NotifyURL, s.NotifyToken, ent.UserID, titleAr, titleEn, reason)
		})
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
