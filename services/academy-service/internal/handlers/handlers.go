// Package handlers implements the academy-service HTTP API handlers and middleware.
package handlers

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/limiter"
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
	Store           store.Store
	AppEnv          string
	GatewaySecret   string
	InternalToken   string
	AuthURL         string
	ExposePrice     bool
	SupportWhatsApp string
	Limiter         limiter.TierLimiter
}

// New creates a Server with dependencies.
func New(st store.Store, appEnv, gatewaySecret, internalToken, authURL string, exposePrice bool, supportWhatsApp string) *Server {
	return &Server{
		Store:           st,
		AppEnv:          appEnv,
		GatewaySecret:   gatewaySecret,
		InternalToken:   internalToken,
		AuthURL:         authURL,
		ExposePrice:     exposePrice,
		SupportWhatsApp: supportWhatsApp,
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
//
// It returns the whole catalog tree: all three study types in the fixed order
// bachelor, diploma, vocational, each with all of its levels, whether or not
// they have published subjects (SPEC Section 1 decision 2, amended
// 2026-10-02). The diploma study type is present with an empty levels list
// when no diploma exists. Subject lists and details still hide unpublished
// subjects; only the axes are always visible. Within a study type levels are
// ordered by position, then key. The flat `levels` list is the same levels in
// tree order.
func (s *Server) GetLevels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	levels, err := s.Store.ListLevels(ctx, false)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	byType := make(map[string][]*models.Level)
	var unknownOrder []string
	for _, l := range levels {
		if _, known := byType[l.StudyType]; !known && !isFixedStudyType(l.StudyType) {
			unknownOrder = append(unknownOrder, l.StudyType)
		}
		byType[l.StudyType] = append(byType[l.StudyType], l)
	}

	// The three fixed study types always come first, in order; a level whose
	// study type is none of them (bad data) is still returned after them.
	order := append(append([]string{}, models.StudyTypeOrder...), unknownOrder...)

	studyTypes := make([]models.StudyTypeDTO, 0, len(order))
	dtos := make([]models.LevelDTO, 0, len(levels))
	for _, key := range order {
		group := byType[key]
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].Position != group[j].Position {
				return group[i].Position < group[j].Position
			}
			return group[i].Key < group[j].Key
		})
		st := models.StudyTypeDTO{
			Key:    key,
			Title:  models.StudyTypeTitle(key),
			Levels: make([]models.LevelDTO, 0, len(group)),
		}
		for _, l := range group {
			dto := l.ToDTO()
			st.Levels = append(st.Levels, dto)
			dtos = append(dtos, dto)
		}
		studyTypes = append(studyTypes, st)
	}

	handlerutil.WriteJSON(w, http.StatusOK, models.LevelsResponseDTO{
		Levels:     dtos,
		StudyTypes: studyTypes,
	})
}

func isFixedStudyType(key string) bool {
	for _, k := range models.StudyTypeOrder {
		if k == key {
			return true
		}
	}
	return false
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
	var activeEnts map[string]*models.Entitlement
	if claims != nil && claims.UserID != "" {
		ownedCtx, ownedCancel := context.WithTimeout(r.Context(), dbTimeout)
		activeEnts, err = s.Store.GetActiveEntitlements(ownedCtx, claims.UserID)
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
		var ent *models.Entitlement
		if activeEnts != nil {
			ent = activeEnts[subj.ID]
			owned = ent != nil
		}
		dto := subj.ToListItemDTO(counts, owned, s.ExposePrice)
		if owned && ent != nil {
			dto.AccessExpiresAt = ent.ExpiresAt
		}
		items[i] = dto
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
// attached file metadata, and pending request status if unowned.
// Returns 404 for unknown or unpublished subjects.
func (s *Server) GetSubjectDetail(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	if id == "" {
		id = strings.TrimPrefix(r.URL.Path, "/academy/subjects/")
		id = strings.TrimSpace(id)
	}
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
	var activeEnt *models.Entitlement
	if claims != nil && claims.UserID != "" {
		ownedCtx, ownedCancel := context.WithTimeout(r.Context(), dbTimeout)
		activeEnt, err = s.Store.GetActiveEntitlement(ownedCtx, claims.UserID, id)
		ownedCancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		owned = activeEnt != nil
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
		videoDTOs[i] = v.ToDTO(owned)
	}

	fileDTOs := make([]models.FileMetadataDTO, len(files))
	for i, f := range files {
		fileDTOs[i] = f.ToDTO()
	}

	dto := subj.ToDetailDTO(counts, videoDTOs, fileDTOs, owned, s.ExposePrice)
	if owned && activeEnt != nil {
		dto.AccessExpiresAt = activeEnt.ExpiresAt
	}
	if !owned && claims != nil && claims.UserID != "" {
		reqCtx, reqCancel := context.WithTimeout(r.Context(), dbTimeout)
		pr, err := s.Store.GetPendingRequest(reqCtx, claims.UserID, id)
		reqCancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if pr != nil && pr.Status == models.RequestStatusPending {
			dto.Request = &models.SubjectRequestDTO{Status: models.RequestStatusPending}
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, dto)
}

// CreateAccessRequest handles POST /academy/subjects/{id}/access-request.
// Idempotent: returns the existing pending request (same id, 200) if one exists (R5).
// Refuses (409, generic) if the student already owns the subject (R1) or if
// access_expires_at is in the past (D20).
func (s *Server) CreateAccessRequest(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
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

	now := time.Now().UTC()
	// D20: Refuse if subject access has expired
	if !subj.AccessExpiresAt.IsZero() && subj.AccessExpiresAt.Before(now) {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "request conflict", nil)
		return
	}

	// R1: Refuse if student already owns the subject
	ownedCtx, ownedCancel := context.WithTimeout(r.Context(), dbTimeout)
	owned, err := s.Store.HasActiveEntitlement(ownedCtx, claims.UserID, id)
	ownedCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if owned {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, handlerutil.ErrCodeConflict, "request conflict", nil)
		return
	}

	reqID, _ := jwtutil.GenerateUUID()
	newReq := &models.PurchaseRequest{
		ID:        reqID,
		UserID:    claims.UserID,
		SubjectID: id,
		Status:    models.RequestStatusPending,
		CreatedAt: now,
	}

	prCtx, prCancel := context.WithTimeout(r.Context(), dbTimeout)
	pr, _, err := s.Store.CreateOrGetPendingRequest(prCtx, newReq)
	prCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	resp := models.AccessRequestResponseDTO{
		ID:          pr.ID,
		SubjectID:   pr.SubjectID,
		Status:      pr.Status,
		CreatedAt:   pr.CreatedAt,
		WhatsAppURL: models.FormatWhatsAppURL(s.SupportWhatsApp),
	}
	handlerutil.WriteJSON(w, http.StatusOK, resp)
}

// EnforceTier wraps an HTTP handler with tiered rate limiting per SPEC Section 2 (D13).
// Keyed on JWT user id; fails closed with 503 on backend failure (Redis down, limiter
// unconfigured outside dev, missing claims), and 429 + Retry-After only when over limit.
func (s *Server) EnforceTier(tier string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Limiter == nil {
			if s.AppEnv == "local" || s.AppEnv == "test" {
				next(w, r)
				return
			}
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("rate limiter unconfigured"))
			return
		}

		claims := StudentClaims(r)
		if claims == nil || claims.UserID == "" {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("missing claims"))
			return
		}

		limited, retryAfter, err := s.Limiter.CheckAndRecord(tier, claims.UserID)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}

		if limited {
			limiter.WriteRateLimitedResponse(w, retryAfter)
			return
		}

		next(w, r)
	}
}

// SubjectSubroute dispatches requests under /academy/subjects/.
// Handles:
// - GET /academy/subjects/{id} -> GetSubjectDetail (Read tier)
// - POST /academy/subjects/{id}/access-request -> CreateAccessRequest (Write tier)
func (s *Server) SubjectSubroute(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/academy/subjects/")
	subpath = strings.Trim(subpath, "/")
	if subpath == "" {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "subject not found", nil)
		return
	}

	parts := strings.Split(subpath, "/")
	if len(parts) == 1 && parts[0] != "" {
		s.EnforceTier(limiter.TierRead, func(w http.ResponseWriter, r *http.Request) {
			s.GetSubjectDetail(w, r, parts[0])
		})(w, r)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "access-request" {
		s.EnforceTier(limiter.TierWrite, func(w http.ResponseWriter, r *http.Request) {
			s.CreateAccessRequest(w, r, parts[0])
		})(w, r)
		return
	}

	handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
}

// VideoSubroute dispatches requests under /academy/videos/.
// Handles:
// - POST /academy/videos/{id}/play -> PlayVideo (Read tier)
func (s *Server) VideoSubroute(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/academy/videos/")
	subpath = strings.Trim(subpath, "/")
	if subpath == "" {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
		return
	}

	parts := strings.Split(subpath, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] == "play" {
		if r.Method != http.MethodPost {
			handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		videoID, err := url.PathUnescape(parts[0])
		if err != nil {
			videoID = parts[0]
		}
		s.EnforceTier(limiter.TierPlay, func(w http.ResponseWriter, r *http.Request) {
			s.PlayVideo(w, r, videoID)
		})(w, r)
		return
	}

	handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
}

// PlayVideo handles POST /academy/videos/{id}/play.
// Authenticated with Bearer JWT (StudentAuth).
// Returns 200 {"video_id": "...", "youtube_video_id": "..."} with Cache-Control: private, no-store
// when the student owns the subject containing the video, the video and subject are published,
// and the video has a non-empty youtube_video_id.
// Returns a generic 404 with Cache-Control: private, no-store for any refusal
// (unknown/unpublished video or subject, unowned, expired, or empty youtube_video_id).
// Logs successful playback to video_plays; write failure is logged with IDs only and never blocks playback.
// Fails closed with 503 on store errors.
func (s *Server) PlayVideo(w http.ResponseWriter, r *http.Request, videoID string) {
	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	writeNotFound := func() {
		w.Header().Set("Cache-Control", "private, no-store")
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
	}

	// 1. Fetch video
	vCtx, vCancel := context.WithTimeout(r.Context(), dbTimeout)
	video, err := s.Store.GetVideoByID(vCtx, videoID)
	vCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if video == nil || !video.Published || strings.TrimSpace(video.YouTubeVideoID) == "" {
		writeNotFound()
		return
	}

	// 2. Fetch parent subject
	sCtx, sCancel := context.WithTimeout(r.Context(), dbTimeout)
	subj, err := s.Store.GetSubjectByID(sCtx, video.SubjectID)
	sCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil || subj.Status != models.StatusPublished {
		writeNotFound()
		return
	}

	// 3. Subject access must not be expired
	if !subj.AccessExpiresAt.IsZero() && time.Now().After(subj.AccessExpiresAt) {
		writeNotFound()
		return
	}

	// 4. Check active entitlement (ownership)
	eCtx, eCancel := context.WithTimeout(r.Context(), dbTimeout)
	owned, err := s.Store.HasActiveEntitlement(eCtx, claims.UserID, video.SubjectID)
	eCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !owned {
		writeNotFound()
		return
	}

	// 5. Append-only video_plays log (no IP; write failure logged with IDs only, never blocks playback)
	playLog := &models.VideoPlay{
		UserID:    claims.UserID,
		VideoID:   video.ID,
		SubjectID: video.SubjectID,
		PlayedAt:  time.Now().UTC(),
	}
	pCtx, pCancel := context.WithTimeout(context.Background(), dbTimeout)
	if logErr := s.Store.RecordVideoPlay(pCtx, playLog); logErr != nil {
		cleanUID := strings.ReplaceAll(strings.ReplaceAll(claims.UserID, "\r", ""), "\n", "")
		cleanVID := strings.ReplaceAll(strings.ReplaceAll(video.ID, "\r", ""), "\n", "")
		cleanSID := strings.ReplaceAll(strings.ReplaceAll(video.SubjectID, "\r", ""), "\n", "")
		// #nosec G706 -- clean IDs sanitized of CR/LF
		log.Printf("[ERROR] video_play_log_failed user_id=%s video_id=%s subject_id=%s: %v", cleanUID, cleanVID, cleanSID, logErr)
	}
	pCancel()

	w.Header().Set("Cache-Control", "private, no-store")
	resp := models.VideoPlayResponseDTO{
		VideoID:        video.ID,
		YouTubeVideoID: video.YouTubeVideoID,
	}
	handlerutil.WriteJSON(w, http.StatusOK, resp)
}

// PublicHandler constructs the HTTP handler for the public listener.
func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", Health)
	mux.HandleFunc("/academy/levels", s.EnforceTier(limiter.TierRead, s.GetLevels))
	mux.HandleFunc("/academy/subjects", s.EnforceTier(limiter.TierRead, s.ListSubjects))
	mux.HandleFunc("/academy/subjects/", s.SubjectSubroute)
	mux.HandleFunc("/academy/videos/", s.VideoSubroute)

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
