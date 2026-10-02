package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
)

// LevelAdminDTO is the admin view of a level: all fields plus subject counts.
// Unpublished diplomas are included (students see published levels only).
type LevelAdminDTO struct {
	Key                   string `json:"key"`
	StudyType             string `json:"study_type"`
	NameAr                string `json:"name_ar"`
	NameEn                string `json:"name_en"`
	Order                 int    `json:"order"`
	Published             bool   `json:"published"`
	SubjectCount          int    `json:"subject_count"`
	PublishedSubjectCount int    `json:"published_subject_count"`
}

// adminLevelDTO builds the admin DTO for a level with its subject counts.
func (s *Server) adminLevelDTO(ctx context.Context, lvl *models.Level) LevelAdminDTO {
	total, err := s.Store.CountSubjectsByLevel(ctx, lvl.Key, "")
	if err != nil {
		total = 0
	}
	published, err := s.Store.CountSubjectsByLevel(ctx, lvl.Key, models.StatusPublished)
	if err != nil {
		published = 0
	}
	return LevelAdminDTO{
		Key:                   lvl.Key,
		StudyType:             lvl.StudyType,
		NameAr:                lvl.TitleAr,
		NameEn:                lvl.TitleEn,
		Order:                 lvl.Position,
		Published:             lvl.Published,
		SubjectCount:          total,
		PublishedSubjectCount: published,
	}
}

// sortAdminLevels orders levels by fixed study-type order (unknown types last
// in encounter order), then order, then key.
func sortAdminLevels(levels []*models.Level) {
	rank := map[string]int{
		models.StudyTypeBachelor:   0,
		models.StudyTypeDiploma:    1,
		models.StudyTypeVocational: 2,
	}
	sort.SliceStable(levels, func(i, j int) bool {
		ri, okI := rank[levels[i].StudyType]
		rj, okJ := rank[levels[j].StudyType]
		if okI != okJ {
			return okI
		}
		if okI && ri != rj {
			return ri < rj
		}
		if !okI && levels[i].StudyType != levels[j].StudyType {
			return levels[i].StudyType < levels[j].StudyType
		}
		if levels[i].Position != levels[j].Position {
			return levels[i].Position < levels[j].Position
		}
		return levels[i].Key < levels[j].Key
	})
}

// AdminLevels dispatches GET (list) and POST (create) on /internal/admin/levels.
func (s *Server) AdminLevels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.ListAdminLevels(w, r)
	case http.MethodPost:
		s.CreateAdminLevel(w, r)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

// ListAdminLevels handles GET /internal/admin/levels: all levels, including
// unpublished, with subject counts.
func (s *Server) ListAdminLevels(w http.ResponseWriter, r *http.Request) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	levels, err := s.Store.ListLevels(ctx, false)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	sortAdminLevels(levels)
	dtos := make([]LevelAdminDTO, 0, len(levels))
	for _, lvl := range levels {
		countCtx, countCancel := context.WithTimeout(r.Context(), dbTimeout)
		dto := s.adminLevelDTO(countCtx, lvl)
		countCancel()
		dtos = append(dtos, dto)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"levels": dtos})
}

// createLevelRequest is the body of POST /internal/admin/levels.
type createLevelRequest struct {
	StudyType string `json:"study_type"`
	NameAr    string `json:"name_ar"`
	NameEn    string `json:"name_en"`
	Order     *int   `json:"order"`
	Published *bool  `json:"published"`
}

// CreateAdminLevel handles POST /internal/admin/levels. Only study_type
// diploma can be created (bachelor years and vocational stay seeded).
// The key is server-generated; published defaults to false.
func (s *Server) CreateAdminLevel(w http.ResponseWriter, r *http.Request) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var req createLevelRequest
	if !decodeAdminJSON(w, r, &req) {
		return
	}

	if strings.TrimSpace(req.StudyType) != models.StudyTypeDiploma {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_study_type", "only diplomas can be created", nil)
		return
	}

	nameAr, valid := cleanAdminName(req.NameAr)
	if !valid {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_name", "name_ar must be 1-200 characters", nil)
		return
	}

	nameEn := ""
	if strings.TrimSpace(req.NameEn) != "" {
		var ok bool
		nameEn, ok = cleanAdminName(req.NameEn)
		if !ok {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_name", "name_en must be 1-200 characters", nil)
			return
		}
	}

	order := 0
	if req.Order != nil {
		order = *req.Order
	}
	published := false
	if req.Published != nil {
		published = *req.Published
	}

	lvl := &models.Level{
		StudyType: models.StudyTypeDiploma,
		TitleAr:   nameAr,
		TitleEn:   nameEn,
		Position:  order,
		Published: published,
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()
	created := false
	for range 3 {
		lvl.Key = "diploma-" + randomHex(4)
		if err := s.Store.CreateLevel(dbCtx, lvl); err == nil {
			created = true
			break
		} else if !errors.Is(err, store.ErrDuplicate) {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
	}
	if !created {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("level key collision"))
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "level_create", "level", lvl.Key, nameAr); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	dto := s.adminLevelDTO(dbCtx, lvl)
	dto.SubjectCount = 0
	dto.PublishedSubjectCount = 0
	handlerutil.WriteJSON(w, http.StatusCreated, dto)
}

// randomHex returns n random bytes as hex.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// AdminLevelSubroute dispatches PATCH and DELETE on /internal/admin/levels/{id}.
func (s *Server) AdminLevelSubroute(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/internal/admin/levels/")
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") || !validLevelKey(id) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_level_id", "invalid level id", nil)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		s.PatchAdminLevel(w, r, id)
	case http.MethodDelete:
		s.DeleteAdminLevel(w, r, id)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

// patchLevelRequest is the body of PATCH /internal/admin/levels/{id};
// every field is optional. study_type is immutable.
type patchLevelRequest struct {
	StudyType *string `json:"study_type"`
	NameAr    *string `json:"name_ar"`
	NameEn    *string `json:"name_en"`
	Order     *int    `json:"order"`
	Published *bool   `json:"published"`
}

// PatchAdminLevel handles PATCH /internal/admin/levels/{id}.
// Seeded fixed levels can be renamed, reordered and (un)published, but the
// study_type never changes and deletion is handled separately.
func (s *Server) PatchAdminLevel(w http.ResponseWriter, r *http.Request, id string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var req patchLevelRequest
	if !decodeAdminJSON(w, r, &req) {
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()
	lvl, err := s.Store.GetLevelByKey(dbCtx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if lvl == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "level_not_found", "level not found", nil)
		return
	}

	if req.StudyType != nil && strings.TrimSpace(*req.StudyType) != lvl.StudyType {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_study_type", "study_type cannot be changed", nil)
		return
	}
	if req.NameAr != nil {
		nameAr, valid := cleanAdminName(*req.NameAr)
		if !valid {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_name", "name_ar must be 1-200 characters", nil)
			return
		}
		lvl.TitleAr = nameAr
	}
	if req.NameEn != nil {
		if strings.TrimSpace(*req.NameEn) == "" {
			lvl.TitleEn = ""
		} else {
			nameEn, valid := cleanAdminName(*req.NameEn)
			if !valid {
				handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_name", "name_en must be 1-200 characters", nil)
				return
			}
			lvl.TitleEn = nameEn
		}
	}
	if req.Order != nil {
		lvl.Position = *req.Order
	}
	if req.Published != nil {
		lvl.Published = *req.Published
	}

	if err := s.Store.UpdateLevel(dbCtx, lvl); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "level_not_found", "level not found", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "level_update", "level", lvl.Key, lvl.TitleAr); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, s.adminLevelDTO(dbCtx, lvl))
}

// DeleteAdminLevel handles DELETE /internal/admin/levels/{id}.
// Only a diploma with zero subjects can be deleted; seeded fixed levels are
// never deleted.
func (s *Server) DeleteAdminLevel(w http.ResponseWriter, r *http.Request, id string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()
	lvl, err := s.Store.GetLevelByKey(dbCtx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if lvl == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "level_not_found", "level not found", nil)
		return
	}
	if lvl.StudyType != models.StudyTypeDiploma {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "level_not_deletable", "only diplomas can be deleted", nil)
		return
	}

	count, err := s.Store.CountSubjectsByLevel(dbCtx, id, "")
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if count > 0 {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "level_has_subjects", "diploma still has subjects", nil)
		return
	}

	if err := s.Store.DeleteLevel(dbCtx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "level_not_found", "level not found", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "level_delete", "level", id, lvl.TitleAr); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
