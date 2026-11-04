package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
)

// Public catalog (ADR-0014 W2a): unauthenticated, read-only sales data for
// the website. Only published subjects under published levels, the same rule
// non-owners get in the app (R8). No per-user data, no content ids.
const (
	publicCatalogPath        = "/academy/public/catalog"
	publicSubjectsPrefix     = "/academy/public/subjects/"
	publicCacheControl       = "public, max-age=60"
	publicSubjectIDMaxLength = 64
	publicListPageSize       = 100
)

// isPublicCatalogPath reports whether path is one of the public catalog
// routes, which bypass StudentAuth. Exact matches only: a subject id holds no
// "/", so no other route can borrow the bypass.
func isPublicCatalogPath(path string) bool {
	if path == publicCatalogPath {
		return true
	}
	id, ok := strings.CutPrefix(path, publicSubjectsPrefix)
	return ok && id != "" && !strings.Contains(id, "/")
}

// GetPublicCatalog serves GET /academy/public/catalog: the three study types
// in the order of GET /academy/levels, each with its published levels, each
// with its published subjects (catalog order). Price always included.
func (s *Server) GetPublicCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	levelsCtx, levelsCancel := context.WithTimeout(r.Context(), dbTimeout)
	levels, err := s.Store.ListLevels(levelsCtx, false)
	levelsCancel()
	if err != nil {
		writePublicError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	var subjects []*models.Subject
	for page := 1; ; page++ {
		ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
		batch, total, err := s.Store.ListSubjects(ctx, store.SubjectFilter{
			Status: models.StatusPublished,
			Page:   page,
			Limit:  publicListPageSize,
		})
		cancel()
		if err != nil {
			writePublicError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		subjects = append(subjects, batch...)
		if len(batch) < publicListPageSize || len(subjects) >= total {
			break
		}
	}

	byLevel := make(map[string][]models.PublicSubjectDTO)
	for _, subj := range subjects {
		if subj.Status != models.StatusPublished {
			continue
		}
		dto, err := s.publicSubject(r, subj)
		if err != nil {
			writePublicError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		byLevel[subj.LevelKey] = append(byLevel[subj.LevelKey], dto)
	}

	byType := make(map[string][]*models.Level)
	for _, l := range levels {
		if l.Published {
			byType[l.StudyType] = append(byType[l.StudyType], l)
		}
	}

	// Only the three fixed study types: a level with an unknown study type
	// (bad data) is left off the public site.
	resp := models.PublicCatalogDTO{StudyTypes: make([]models.PublicStudyTypeDTO, 0, len(models.StudyTypeOrder))}
	for _, key := range models.StudyTypeOrder {
		group := byType[key]
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].Position != group[j].Position {
				return group[i].Position < group[j].Position
			}
			return group[i].Key < group[j].Key
		})
		st := models.PublicStudyTypeDTO{
			Key:    key,
			Title:  models.StudyTypeTitle(key),
			Levels: make([]models.PublicLevelDTO, 0, len(group)),
		}
		for _, l := range group {
			subs := byLevel[l.Key]
			if subs == nil {
				subs = []models.PublicSubjectDTO{}
			}
			st.Levels = append(st.Levels, models.PublicLevelDTO{
				Key:       l.Key,
				StudyType: l.StudyType,
				Title:     models.LocalizedText{Ar: l.TitleAr, En: l.TitleEn},
				Position:  l.Position,
				Subjects:  subs,
			})
		}
		resp.StudyTypes = append(resp.StudyTypes, st)
	}

	writePublicJSON(w, r, resp)
}

// GetPublicSubject serves GET /academy/public/subjects/{id}. Unknown, draft
// and hidden-level subjects are the same 404.
func (s *Server) GetPublicSubject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, publicSubjectsPrefix)
	if id == "" || strings.Contains(id, "/") || len(id) > publicSubjectIDMaxLength {
		writePublicError(w, r, http.StatusNotFound, "not_found", "subject not found", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	subj, err := s.Store.GetSubjectByID(ctx, id)
	cancel()
	if err != nil {
		writePublicError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil || subj.Status != models.StatusPublished {
		writePublicError(w, r, http.StatusNotFound, "not_found", "subject not found", nil)
		return
	}

	lvlCtx, lvlCancel := context.WithTimeout(r.Context(), dbTimeout)
	levels, err := s.Store.ListLevels(lvlCtx, false)
	lvlCancel()
	if err != nil {
		writePublicError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	var level *models.Level
	for _, l := range levels {
		if l.Key == subj.LevelKey && l.Published && isFixedStudyType(l.StudyType) {
			level = l
			break
		}
	}
	if level == nil {
		writePublicError(w, r, http.StatusNotFound, "not_found", "subject not found", nil)
		return
	}

	dto, err := s.publicSubject(r, subj)
	if err != nil {
		writePublicError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	writePublicJSON(w, r, models.PublicSubjectDetailDTO{PublicSubjectDTO: dto, Level: level.ToDTO()})
}

func (s *Server) publicSubject(r *http.Request, subj *models.Subject) (models.PublicSubjectDTO, error) {
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	counts, err := s.Store.GetSubjectCounts(ctx, subj.ID)
	cancel()
	if err != nil {
		return models.PublicSubjectDTO{}, err
	}
	return subj.ToPublicDTO(counts), nil
}

// writePublicJSON writes a cacheable public response with a strong ETag over
// the body; a matching If-None-Match gets 304 with no body.
func writePublicJSON(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writePublicError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "internal error", err)
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`

	h := w.Header()
	h.Set("Cache-Control", publicCacheControl)
	h.Set("ETag", etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	handlerutil.WriteBytes(w, body)
}

// etagMatches implements the weak comparison of If-None-Match (RFC 9110
// 13.1.2): "*" or any listed tag equal to etag, ignoring a W/ prefix.
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" || strings.TrimPrefix(tag, "W/") == etag {
			return true
		}
	}
	return false
}

// writePublicError answers a public catalog error that no cache may keep, so
// a subject published a moment later is not hidden behind a cached 404.
func writePublicError(w http.ResponseWriter, r *http.Request, status int, code, msg string, err error) {
	w.Header().Set("Cache-Control", "no-store")
	handlerutil.WriteSafeError(w, r, status, code, msg, err)
}
