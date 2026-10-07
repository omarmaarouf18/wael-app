package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// SubjectAdminDTO is the admin view of a subject. Unlike the student DTOs it
// always carries the price, the draft/published state, and the video count.
type SubjectAdminDTO struct {
	ID              string    `json:"id"`
	LevelID         string    `json:"level_id"`
	Term            string    `json:"term"`
	TitleAr         string    `json:"title_ar"`
	TitleEn         string    `json:"title_en"`
	DescriptionAr   string    `json:"description_ar"`
	DescriptionEn   string    `json:"description_en"`
	Price           int       `json:"price"`
	Currency        string    `json:"currency"`
	AccessExpiresAt time.Time `json:"access_expires_at"`
	Order           int       `json:"order"`
	Published       bool      `json:"published"`
	VideoCount      int       `json:"video_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toSubjectAdminDTO(subj *models.Subject, videoCount int) SubjectAdminDTO {
	return SubjectAdminDTO{
		ID:              subj.ID,
		LevelID:         subj.LevelKey,
		Term:            subj.Term,
		TitleAr:         subj.TitleAr,
		TitleEn:         subj.TitleEn,
		DescriptionAr:   subj.DescriptionAr,
		DescriptionEn:   subj.DescriptionEn,
		Price:           subj.Price,
		Currency:        "EGP",
		AccessExpiresAt: subj.AccessExpiresAt,
		Order:           subj.Order,
		Published:       subj.Status == models.StatusPublished,
		VideoCount:      videoCount,
		CreatedAt:       subj.CreatedAt,
		UpdatedAt:       subj.UpdatedAt,
	}
}

// subjectVideoCount counts the subject's videos (used for the admin view and
// the publish gate). A store error fails closed.
func (s *Server) subjectVideoCount(ctx context.Context, subjectID string) (int, error) {
	videos, err := s.Store.ListVideosBySubject(ctx, subjectID, false)
	if err != nil {
		return 0, err
	}
	return len(videos), nil
}

// validSubjectTerm reports whether term is an allowed subject term.
func validSubjectTerm(term string) bool {
	return term == "" || term == "first" || term == "second"
}

// validateStudyTypeTerm reports whether term is valid for the given study type.
// Bachelor and diploma levels require "first" or "second"; vocational requires empty "".
func validateStudyTypeTerm(studyType, term string) bool {
	switch studyType {
	case models.StudyTypeBachelor, models.StudyTypeDiploma:
		return term == "first" || term == "second"
	case models.StudyTypeVocational:
		return term == ""
	default:
		return false
	}
}

// AdminSubjects dispatches GET (list) and POST (create) on /internal/admin/subjects.
func (s *Server) AdminSubjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.ListAdminSubjects(w, r)
	case http.MethodPost:
		s.CreateAdminSubject(w, r)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

// ListAdminSubjects handles GET /internal/admin/subjects with optional
// level_id and published filters and page/limit pagination (max 100).
// Draft subjects are included: unpublish is the only way to hide a subject.
func (s *Server) ListAdminSubjects(w http.ResponseWriter, r *http.Request) {
	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	q := r.URL.Query()
	levelID := strings.TrimSpace(q.Get("level_id"))
	if levelID != "" && !validLevelKey(levelID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_level_id", "invalid level id", nil)
		return
	}

	status := ""
	if raw := strings.TrimSpace(q.Get("published")); raw != "" {
		switch strings.ToLower(raw) {
		case "true", "1":
			status = models.StatusPublished
		case "false", "0":
			status = models.StatusDraft
		default:
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_published", "published must be true or false", nil)
			return
		}
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
	subjects, total, err := s.Store.ListSubjects(dbCtx, store.SubjectFilter{
		LevelKey: levelID,
		Status:   status,
		Page:     page,
		Limit:    limit,
	})
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	items := make([]SubjectAdminDTO, 0, len(subjects))
	for _, subj := range subjects {
		countCtx, countCancel := context.WithTimeout(r.Context(), dbTimeout)
		n, err := s.subjectVideoCount(countCtx, subj.ID)
		countCancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		items = append(items, toSubjectAdminDTO(subj, n))
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// subjectWriteRequest is the shared body of POST and PATCH /internal/admin/subjects.
// Every field is optional on PATCH; on POST the required fields are validated.
type subjectWriteRequest struct {
	LevelID         *string `json:"level_id"`
	TitleAr         *string `json:"title_ar"`
	TitleEn         *string `json:"title_en"`
	DescriptionAr   *string `json:"description_ar"`
	DescriptionEn   *string `json:"description_en"`
	Term            *string `json:"term"`
	Price           *int    `json:"price"`
	AccessExpiresAt *string `json:"access_expires_at"`
	Order           *int    `json:"order"`
	Published       *bool   `json:"published"`
}

// parseAccessExpiresAt parses an RFC3339 timestamp that must lie in the future.
func parseAccessExpiresAt(raw string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, false
	}
	if !t.After(time.Now()) {
		return time.Time{}, false
	}
	return t, true
}

// CreateAdminSubject handles POST /internal/admin/subjects.
// Subjects start as drafts unless published=true; publishing still requires
// at least one video via the publish endpoint.
func (s *Server) CreateAdminSubject(w http.ResponseWriter, r *http.Request) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var req subjectWriteRequest
	if !decodeAdminJSON(w, r, &req) {
		return
	}

	if req.LevelID == nil || !validLevelKey(strings.TrimSpace(*req.LevelID)) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_level_id", "invalid level id", nil)
		return
	}
	levelKey := strings.TrimSpace(*req.LevelID)

	if req.TitleAr == nil {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_title", "title_ar is required", nil)
		return
	}
	titleAr, valid := cleanAdminName(*req.TitleAr)
	if !valid {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_title", "title_ar must be 1-200 characters", nil)
		return
	}

	subj, code, msg := s.buildSubject(req, &models.Subject{LevelKey: levelKey}, true)
	if code != "" {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, code, msg, nil)
		return
	}
	subj.TitleAr = titleAr

	// A new subject has no videos yet, so it always starts as a draft:
	// publishing requires at least one video via the publish endpoint.
	if subj.Status == models.StatusPublished {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "subject_has_no_videos", "subject has no videos", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()
	lvl, err := s.Store.GetLevelByKey(dbCtx, levelKey)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if lvl == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "level_not_found", "level not found", nil)
		return
	}

	if !validateStudyTypeTerm(lvl.StudyType, subj.Term) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_term", "invalid term for study type", nil)
		return
	}

	id, _ := jwtutil.GenerateUUID()
	now := time.Now().UTC()
	subj.ID = id
	subj.CreatedAt = now
	subj.UpdatedAt = now

	if err := s.Store.CreateSubject(dbCtx, subj); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "subject_create", "subject", subj.ID, subj.TitleAr); err != nil {
		logAuditFailure(adm, "subject_create", subj.ID, err)
	}

	handlerutil.WriteJSON(w, http.StatusCreated, toSubjectAdminDTO(subj, 0))
}

// buildSubject applies the optional write fields to subj, returning the safe
// error code/message on validation failure (empty code means valid).
// access_expires_at is required on create; on patch it is optional and the
// existing value is kept when absent.
func (s *Server) buildSubject(req subjectWriteRequest, subj *models.Subject, requireExpires bool) (*models.Subject, string, string) {
	if req.TitleEn != nil {
		if strings.TrimSpace(*req.TitleEn) == "" {
			subj.TitleEn = ""
		} else if v, ok := cleanAdminName(*req.TitleEn); !ok {
			return nil, "invalid_title", "title_en must be 1-200 characters"
		} else {
			subj.TitleEn = v
		}
	}
	if req.DescriptionAr != nil {
		v, ok := cleanAdminText(*req.DescriptionAr, 5000)
		if !ok {
			return nil, "invalid_description", "description_ar must be at most 5000 characters"
		}
		subj.DescriptionAr = v
	}
	if req.DescriptionEn != nil {
		v, ok := cleanAdminText(*req.DescriptionEn, 5000)
		if !ok {
			return nil, "invalid_description", "description_en must be at most 5000 characters"
		}
		subj.DescriptionEn = v
	}
	if req.Term != nil {
		term := strings.TrimSpace(*req.Term)
		if !validSubjectTerm(term) {
			return nil, "invalid_term", "term must be first, second or empty"
		}
		subj.Term = term
	}
	if req.Price != nil {
		if *req.Price < 0 {
			return nil, "invalid_price", "price must be >= 0"
		}
		subj.Price = *req.Price
	}
	if req.AccessExpiresAt == nil {
		if requireExpires {
			return nil, "invalid_expires_at", "access_expires_at is required and must be a future RFC3339 timestamp"
		}
	} else {
		expires, ok := parseAccessExpiresAt(*req.AccessExpiresAt)
		if !ok {
			return nil, "invalid_expires_at", "access_expires_at must be a future RFC3339 timestamp"
		}
		subj.AccessExpiresAt = expires
	}
	if req.Order != nil {
		subj.Order = *req.Order
	}
	if req.Published != nil {
		if *req.Published {
			subj.Status = models.StatusPublished
		} else {
			subj.Status = models.StatusDraft
		}
	} else if subj.Status == "" {
		subj.Status = models.StatusDraft
	}
	return subj, "", ""
}

// AdminSubjectSubroute dispatches PATCH /internal/admin/subjects/{id} and
// POST /internal/admin/subjects/{id}/publish|unpublish.
func (s *Server) AdminSubjectSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/internal/admin/subjects/")
	rest = strings.Trim(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" || !validResourceID(parts[0]) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_subject_id", "invalid subject id", nil)
		return
	}
	id := parts[0]

	if len(parts) == 1 && r.Method == http.MethodPatch {
		s.PatchAdminSubject(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "videos" {
		s.AdminSubjectVideos(w, r, id)
		return
	}
	if len(parts) == 3 && parts[1] == "videos" && parts[2] == "reorder" {
		if r.Method != http.MethodPost {
			handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		s.ReorderAdminVideos(w, r, id)
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "publish":
			s.PublishAdminSubject(w, r, id)
			return
		case "unpublish":
			s.UnpublishAdminSubject(w, r, id)
			return
		}
	}
	handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
}

// PatchAdminSubject handles PATCH /internal/admin/subjects/{id}: the same
// fields as create, all optional.
func (s *Server) PatchAdminSubject(w http.ResponseWriter, r *http.Request, id string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var req subjectWriteRequest
	if !decodeAdminJSON(w, r, &req) {
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()
	subj, err := s.Store.GetSubjectByID(dbCtx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "subject_not_found", "subject not found", nil)
		return
	}

	if req.LevelID != nil {
		levelKey := strings.TrimSpace(*req.LevelID)
		if !validLevelKey(levelKey) {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_level_id", "invalid level id", nil)
			return
		}
		lvl, err := s.Store.GetLevelByKey(dbCtx, levelKey)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if lvl == nil {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "level_not_found", "level not found", nil)
			return
		}
		subj.LevelKey = levelKey
	}
	if req.TitleAr != nil {
		titleAr, valid := cleanAdminName(*req.TitleAr)
		if !valid {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_title", "title_ar must be 1-200 characters", nil)
			return
		}
		subj.TitleAr = titleAr
	}
	// access_expires_at is required on create; on patch it is optional and
	// the existing value is kept when absent.
	wasDraft := subj.Status != models.StatusPublished
	updated, code, msg := s.buildSubject(req, subj, false)
	if code != "" {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, code, msg, nil)
		return
	}
	subj = updated

	currentLvl, err := s.Store.GetLevelByKey(dbCtx, subj.LevelKey)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if currentLvl == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "level_not_found", "level not found", nil)
		return
	}
	if !validateStudyTypeTerm(currentLvl.StudyType, subj.Term) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_term", "invalid term for study type", nil)
		return
	}

	if wasDraft && subj.Status == models.StatusPublished {
		n, err := s.subjectVideoCount(dbCtx, subj.ID)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if n == 0 {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, "subject_has_no_videos", "subject has no videos", nil)
			return
		}
	}
	subj.UpdatedAt = time.Now().UTC()

	if err := s.Store.UpdateSubject(dbCtx, subj); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "subject_update", "subject", subj.ID, subj.TitleAr); err != nil {
		logAuditFailure(adm, "subject_update", subj.ID, err)
	}

	n, err := s.subjectVideoCount(dbCtx, subj.ID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, toSubjectAdminDTO(subj, n))
}

// PublishAdminSubject handles POST /internal/admin/subjects/{id}/publish.
// Publishing requires at least one video.
func (s *Server) PublishAdminSubject(w http.ResponseWriter, r *http.Request, id string) {
	s.setSubjectStatus(w, r, id, models.StatusPublished, "subject_publish")
}

// UnpublishAdminSubject handles POST /internal/admin/subjects/{id}/unpublish.
// Unpublish is the only way to hide a subject (no hard delete: students may
// own it).
func (s *Server) UnpublishAdminSubject(w http.ResponseWriter, r *http.Request, id string) {
	s.setSubjectStatus(w, r, id, models.StatusDraft, "subject_unpublish")
}

func (s *Server) setSubjectStatus(w http.ResponseWriter, r *http.Request, id, status, action string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()
	subj, err := s.Store.GetSubjectByID(dbCtx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "subject_not_found", "subject not found", nil)
		return
	}

	if status == models.StatusPublished {
		n, err := s.subjectVideoCount(dbCtx, id)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if n == 0 {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, "subject_has_no_videos", "subject has no videos", nil)
			return
		}
	}

	subj.Status = status
	subj.UpdatedAt = time.Now().UTC()
	if err := s.Store.UpdateSubject(dbCtx, subj); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, action, "subject", subj.ID, subj.TitleAr); err != nil {
		logAuditFailure(adm, action, subj.ID, err)
	}

	n, err := s.subjectVideoCount(dbCtx, subj.ID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, toSubjectAdminDTO(subj, n))
}
