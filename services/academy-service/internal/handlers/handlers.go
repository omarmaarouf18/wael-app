// Package handlers implements the academy-service HTTP API handlers and middleware.
package handlers

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

const dbTimeout = 5 * time.Second

type contextKey string

const claimsContextKey contextKey = "claims"

// Server wires academy-service HTTP dependencies.
type Server struct {
	Store         store.Store
	AppEnv        string
	GatewaySecret string
	InternalToken string
	AuthURL       string
	ExposePrice   bool
}

// New creates a Server with dependencies.
func New(st store.Store, appEnv, gatewaySecret, internalToken, authURL string, exposePrice bool) *Server {
	return &Server{
		Store:         st,
		AppEnv:        appEnv,
		GatewaySecret: gatewaySecret,
		InternalToken: internalToken,
		AuthURL:       authURL,
		ExposePrice:   exposePrice,
	}
}

// Health is the unauthenticated liveness probe on the public listener.
func Health(w http.ResponseWriter, _ *http.Request) {
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GatewayAuth requires X-Gateway-Secret on every non-health route on the public listener.
func (s *Server) GatewayAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-Gateway-Secret")
		if s.GatewaySecret == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.GatewaySecret)) != 1 {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// StudentAuth verifies Bearer JWT using jwtutil.ValidateToken on every request.
// This ensures suspension takes immediate effect and token revocation is checked against Redis.
func (s *Server) StudentAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		token := handlerutil.BearerToken(r)
		if token == "" {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}

		claims, err := jwtutil.ValidateToken(token)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", err)
			return
		}

		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// StudentClaims retrieves validated claims from request context.
func StudentClaims(r *http.Request) *jwtutil.Claims {
	if c, ok := r.Context().Value(claimsContextKey).(*jwtutil.Claims); ok {
		return c
	}
	return nil
}

// InternalTokenAuth requires X-Internal-Token on every admin route (constant-time compare).
// Empty-secret guard ensures an empty configured token never authenticates.
func (s *Server) InternalTokenAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Internal-Token")
		if s.InternalToken == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.InternalToken)) != 1 {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetLevels serves GET /academy/levels.
// Returns the list of academic levels, hiding levels that have no published subjects.
func (s *Server) GetLevels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	levels, err := s.Store.ListLevels(ctx, true)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	dtos := make([]models.LevelDTO, len(levels))
	for i, l := range levels {
		dtos[i] = l.ToDTO()
	}
	if dtos == nil {
		dtos = []models.LevelDTO{}
	}

	studyTypesMap := make(map[string]*models.StudyTypeDTO)
	var studyTypeOrder []string

	for _, l := range levels {
		stKey := l.StudyType
		st, exists := studyTypesMap[stKey]
		if !exists {
			var title models.LocalizedText
			switch stKey {
			case models.StudyTypeBachelor:
				title = models.LocalizedText{Ar: "ليسانس الحقوق", En: "LL.B. (Bachelor)"}
			case models.StudyTypeDiploma:
				title = models.LocalizedText{Ar: "دبلومات الدراسات العليا", En: "Postgraduate Diplomas"}
			case models.StudyTypeVocational:
				title = models.LocalizedText{Ar: "التدريب المهني والعملي", En: "Vocational Training"}
			default:
				title = models.LocalizedText{Ar: stKey, En: stKey}
			}
			st = &models.StudyTypeDTO{
				Key:    stKey,
				Title:  title,
				Levels: []models.LevelDTO{},
			}
			studyTypesMap[stKey] = st
			studyTypeOrder = append(studyTypeOrder, stKey)
		}
		st.Levels = append(st.Levels, l.ToDTO())
	}

	var studyTypes []models.StudyTypeDTO
	for _, k := range studyTypeOrder {
		studyTypes = append(studyTypes, *studyTypesMap[k])
	}
	if studyTypes == nil {
		studyTypes = []models.StudyTypeDTO{}
	}

	handlerutil.WriteJSON(w, http.StatusOK, models.LevelsResponseDTO{
		Levels:     dtos,
		StudyTypes: studyTypes,
	})
}

// ListSubjects serves GET /academy/subjects?level=<key>&term=<t>&page=<p>&limit=<l>.
// Returns published subjects with item counts. Unpublished subjects never appear.
func (s *Server) ListSubjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	query := r.URL.Query()
	levelKey := strings.TrimSpace(query.Get("level"))
	term := strings.TrimSpace(query.Get("term"))

	page := 1
	if pStr := strings.TrimSpace(query.Get("page")); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			page = p
		}
	}

	limit := 20
	if lStr := strings.TrimSpace(query.Get("limit")); lStr != "" {
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

	filter := store.SubjectFilter{
		LevelKey: levelKey,
		Term:     term,
		Status:   models.StatusPublished,
		Page:     page,
		Limit:    limit,
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	subjects, total, err := s.Store.ListSubjects(ctx, filter)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	claims := StudentClaims(r)
	var ownedMap map[string]bool
	if claims != nil && claims.UserID != "" {
		ownedCtx, ownedCancel := context.WithTimeout(r.Context(), dbTimeout)
		ownedMap, err = s.Store.GetActiveEntitlementSubjectIDs(ownedCtx, claims.UserID)
		ownedCancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
	}

	items := make([]models.SubjectListItemDTO, len(subjects))
	for i, subj := range subjects {
		countCtx, countCancel := context.WithTimeout(r.Context(), dbTimeout)
		counts, _ := s.Store.GetSubjectCounts(countCtx, subj.ID)
		countCancel()

		owned := false
		if ownedMap != nil {
			owned = ownedMap[subj.ID]
		}
		items[i] = subj.ToListItemDTO(counts, owned, s.ExposePrice)
	}
	if items == nil {
		items = []models.SubjectListItemDTO{}
	}

	handlerutil.WriteJSON(w, http.StatusOK, models.SubjectListResponseDTO{
		Items: items,
		Total: total,
		Page:  page,
		Limit: limit,
	})
}

// GetSubjectDetail serves GET /academy/subjects/{id}.
// Returns subject metadata, published video titles/descriptions (NEVER youtube_video_id),
// and attached file metadata. Returns 404 for unknown or unpublished subjects.
func (s *Server) GetSubjectDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/academy/subjects/")
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "subject not found", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	subj, err := s.Store.GetSubjectByID(ctx, id)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil || subj.Status != models.StatusPublished {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "subject not found", nil)
		return
	}

	claims := StudentClaims(r)
	owned := false
	if claims != nil && claims.UserID != "" {
		ownedCtx, ownedCancel := context.WithTimeout(r.Context(), dbTimeout)
		owned, err = s.Store.HasActiveEntitlement(ownedCtx, claims.UserID, id)
		ownedCancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
	}

	countsCtx, countsCancel := context.WithTimeout(r.Context(), dbTimeout)
	counts, err := s.Store.GetSubjectCounts(countsCtx, id)
	countsCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	vCtx, vCancel := context.WithTimeout(r.Context(), dbTimeout)
	videos, err := s.Store.ListVideosBySubject(vCtx, id, true) // only published videos
	vCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	fCtx, fCancel := context.WithTimeout(r.Context(), dbTimeout)
	files, err := s.Store.ListFilesBySubject(fCtx, id)
	fCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	videoDTOs := make([]models.VideoMetadataDTO, len(videos))
	for i, v := range videos {
		videoDTOs[i] = v.ToDTO()
	}

	fileDTOs := make([]models.FileMetadataDTO, len(files))
	for i, f := range files {
		fileDTOs[i] = f.ToDTO()
	}

	dto := subj.ToDetailDTO(counts, videoDTOs, fileDTOs, owned, s.ExposePrice)
	handlerutil.WriteJSON(w, http.StatusOK, dto)
}

// PublicHandler constructs the HTTP handler for the public listener.
func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", Health)
	mux.HandleFunc("/academy/levels", s.GetLevels)
	mux.HandleFunc("/academy/subjects", s.ListSubjects)
	mux.HandleFunc("/academy/subjects/", s.GetSubjectDetail)

	var h http.Handler = mux
	h = s.StudentAuth(h)
	h = s.GatewayAuth(h)
	h = handlerutil.MaxBytesMiddleware(1 << 20)(h)
	return h
}

// AdminHandler constructs the HTTP handler for the internal admin listener.
// It serves ONLY /internal/admin/* routes (empty for Phase 2.1-2.3, 404) behind
// X-Internal-Token and 404s on all public routes.
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/admin/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	var h http.Handler = mux
	h = s.InternalTokenAuth(h)
	h = handlerutil.MaxBytesMiddleware(1 << 20)(h)
	return h
}
