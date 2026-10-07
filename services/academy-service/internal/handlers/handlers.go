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
	AuthAdminURL    string
	NotifyURL       string
	NotifyToken     string
	VerifyClient    *http.Client
	ExposePrice     bool
	SupportWhatsApp string
	// Public app-config values (F-UX2 A7), set from config after New.
	TermsURL      string
	PrivacyURL    string
	MinVersion    string
	LatestVersion string
	UpdateURL     string
	Limiter       limiter.TierLimiter
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
// The public app config (/academy/app-config) bypasses StudentAuth: it carries
// no per-user data (F-UX2 A7).
func (s *Server) StudentAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/academy/app-config" {
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
// bachelor, diploma, vocational, each with all of its *published* levels,
// whether or not they have published subjects (SPEC Section 1 decision 2, as
// amended). Admin-created diplomas start unpublished and appear here once the
// admin publishes them; the diploma study type is present with an empty levels
// list when no published diploma exists. Subject lists and details still hide
// unpublished subjects; only the axes are always visible. Within a study type
// levels are ordered by position, then key. The flat `levels` list is the same
// levels in tree order.
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
		if !l.Published {
			continue
		}
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
// Returns published subjects with item counts. Visibility: owners with an
// active entitlement keep seeing their subjects (owned list/detail) even
// after unpublish, until the entitlement expires; non-owners see only
// published subjects under published levels.
func (s *Server) ListSubjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	query := r.URL.Query()
	levelKey := strings.TrimSpace(query.Get("level"))
	term := strings.TrimSpace(query.Get("term"))

	const maxPage = 10000
	page := 1
	if pStr := strings.TrimSpace(query.Get("page")); pStr != "" {
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

	claims := StudentClaims(r)
	var activeEnts map[string]*models.Entitlement
	if claims != nil && claims.UserID != "" {
		ownedCtx, ownedCancel := context.WithTimeout(r.Context(), dbTimeout)
		var err error
		activeEnts, err = s.Store.GetActiveEntitlements(ownedCtx, claims.UserID)
		ownedCancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
	}

	levelsCtx, levelsCancel := context.WithTimeout(r.Context(), dbTimeout)
	allLevels, err := s.Store.ListLevels(levelsCtx, false)
	levelsCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	levelsByKey := make(map[string]*models.Level, len(allLevels))
	allPublished := true
	for _, lvl := range allLevels {
		levelsByKey[lvl.Key] = lvl
		if !lvl.Published {
			allPublished = false
		}
	}

	// Owned subjects that the plain published query would miss (drafts, or
	// subjects under unpublished levels): owners keep seeing them.
	var ownedExtras []*models.Subject
	ownedExtraEnts := make(map[string]*models.Entitlement)
	if len(activeEnts) > 0 {
		for id, ent := range activeEnts {
			extraCtx, extraCancel := context.WithTimeout(r.Context(), dbTimeout)
			subj, err := s.Store.GetSubjectByID(extraCtx, id)
			extraCancel()
			if err != nil {
				handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
				return
			}
			if subj == nil {
				continue
			}
			if levelKey != "" && subj.LevelKey != levelKey {
				continue
			}
			if term != "" && subj.Term != term {
				continue
			}
			if subj.Status == models.StatusPublished && levelVisible(levelsByKey, subj.LevelKey) {
				continue // covered by the published query below
			}
			ownedExtras = append(ownedExtras, subj)
			ownedExtraEnts[subj.ID] = ent
		}
	}

	if allPublished && len(ownedExtras) == 0 {
		s.listPublishedSubjects(w, r, levelKey, term, page, limit, activeEnts, levelsByKey)
		return
	}

	// Merge path: every published subject plus owned extras, filtered for
	// non-owners to published subjects under published levels, then paginated.
	var published []*models.Subject
	for p := 1; ; p++ {
		mergeCtx, mergeCancel := context.WithTimeout(r.Context(), dbTimeout)
		batch, _, err := s.Store.ListSubjects(mergeCtx, store.SubjectFilter{
			LevelKey: levelKey,
			Term:     term,
			Status:   models.StatusPublished,
			Page:     p,
			Limit:    100,
		})
		mergeCancel()
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		published = append(published, batch...)
		if len(batch) < 100 {
			break
		}
	}

	seen := make(map[string]bool, len(published)+len(ownedExtras))
	var merged []*models.Subject
	for _, subj := range published {
		seen[subj.ID] = true
		if _, owned := activeEnts[subj.ID]; !owned && !levelVisible(levelsByKey, subj.LevelKey) {
			continue
		}
		merged = append(merged, subj)
	}
	for _, subj := range ownedExtras {
		if !seen[subj.ID] {
			merged = append(merged, subj)
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Order != merged[j].Order {
			return merged[i].Order < merged[j].Order
		}
		return merged[i].CreatedAt.Before(merged[j].CreatedAt)
	})

	total := len(merged)
	start := 0
	if page > 1 {
		if total == 0 || (page-1) > total/limit {
			start = total
		} else {
			start = (page - 1) * limit
		}
	}
	if start < 0 {
		start = 0
	} else if start > total {
		start = total
	}
	end := start + limit
	if end < start || end > total {
		end = total
	}
	paged := merged[start:end]

	items := make([]models.SubjectListItemDTO, 0, len(paged))
	for _, subj := range paged {
		dto, err := s.subjectListItem(r, subj, activeEnts)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		items = append(items, dto)
	}

	handlerutil.WriteJSON(w, http.StatusOK, models.SubjectListResponseDTO{
		Items: items,
		Total: total,
		Page:  page,
		Limit: limit,
	})
}

// listPublishedSubjects serves the common catalog path: paginated published
// subjects, hiding subjects under unpublished levels from non-owners.
func (s *Server) listPublishedSubjects(w http.ResponseWriter, r *http.Request, levelKey, term string, page, limit int, activeEnts map[string]*models.Entitlement, levelsByKey map[string]*models.Level) {
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

	items := make([]models.SubjectListItemDTO, 0, len(subjects))
	hidden := 0
	for _, subj := range subjects {
		owned := false
		if activeEnts != nil {
			_, owned = activeEnts[subj.ID]
		}
		if !owned && !levelVisible(levelsByKey, subj.LevelKey) {
			hidden++
			continue
		}
		dto, err := s.subjectListItem(r, subj, activeEnts)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		items = append(items, dto)
	}

	handlerutil.WriteJSON(w, http.StatusOK, models.SubjectListResponseDTO{
		Items: items,
		Total: total - hidden,
		Page:  page,
		Limit: limit,
	})
}

// subjectListItem builds the student list DTO for one subject with its
// counts, ownership flag, and entitlement expiry override.
func (s *Server) subjectListItem(r *http.Request, subj *models.Subject, activeEnts map[string]*models.Entitlement) (models.SubjectListItemDTO, error) {
	countCtx, countCancel := context.WithTimeout(r.Context(), dbTimeout)
	counts, err := s.Store.GetSubjectCounts(countCtx, subj.ID)
	countCancel()
	if err != nil {
		return models.SubjectListItemDTO{}, err
	}

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
	return dto, nil
}

// levelVisible reports whether a level exists and is published. Subjects
// under missing or unpublished levels are hidden from non-owners.
func levelVisible(levelsByKey map[string]*models.Level, levelKey string) bool {
	lvl, ok := levelsByKey[levelKey]
	return ok && lvl != nil && lvl.Published
}

// studentCanSee reports whether a subject is visible to a student:
// owners with an active entitlement see it regardless of draft state or
// level visibility; others see only published subjects under published levels.
func studentCanSee(levelsByKey map[string]*models.Level, subj *models.Subject, owned bool) bool {
	if owned {
		return true
	}
	return subj.Status == models.StatusPublished && levelVisible(levelsByKey, subj.LevelKey)
}

// GetSubjectDetail serves GET /academy/subjects/{id}.
// Returns subject metadata, published video titles/descriptions (NEVER youtube_video_id),
// attached file metadata, and pending request status if unowned.
// Returns 404 for unknown subjects; unpublished subjects (or subjects under
// unpublished levels) are visible only to owners with an active entitlement.
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
	if subj == nil {
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

	// Owners keep seeing their subject after unpublish until the entitlement
	// expires; others see only published subjects under published levels.
	lvlCtx, lvlCancel := context.WithTimeout(r.Context(), dbTimeout)
	lvl, err := s.Store.GetLevelByKey(lvlCtx, subj.LevelKey)
	lvlCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if !studentCanSee(map[string]*models.Level{subj.LevelKey: lvl}, subj, owned) {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "subject not found", nil)
		return
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
			dto.Request = &models.SubjectRequestDTO{
				Status:      models.RequestStatusPending,
				WhatsappURL: models.FormatWhatsAppURLStrict(s.SupportWhatsApp),
			}
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
		WhatsAppURL: models.FormatWhatsAppURLStrict(s.SupportWhatsApp),
	}
	handlerutil.WriteJSON(w, http.StatusOK, resp)
}

// EnforceIPTier wraps a public (unauthenticated) handler with tiered rate
// limiting keyed on the client IP (F-UX2 A7 app-config). It fails closed with
// 503 on backend failure (limiter unconfigured outside dev), and 429 +
// Retry-After only when over limit.
func (s *Server) EnforceIPTier(tier string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Limiter == nil {
			if s.AppEnv == "local" || s.AppEnv == "test" {
				next(w, r)
				return
			}
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("rate limiter unconfigured"))
			return
		}
		limited, retryAfter, err := s.Limiter.CheckAndRecord(tier, "ip:"+handlerutil.GetIP(r))
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

// GetAppConfig serves GET /academy/app-config (F-UX2 A7). Public (no student
// JWT), read tier (per-IP), cacheable for 5 min. It returns the support
// WhatsApp URL, the terms/privacy URLs, and the optional update metadata
// (empty means no update prompt). No payment wording.
func (s *Server) GetAppConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	handlerutil.WriteJSON(w, http.StatusOK, models.AppConfigDTO{
		SupportWhatsAppURL: models.FormatWhatsAppURLStrict(s.SupportWhatsApp),
		TermsURL:           s.TermsURL,
		PrivacyURL:         s.PrivacyURL,
		MinVersion:         s.MinVersion,
		LatestVersion:      s.LatestVersion,
		UpdateURL:          s.UpdateURL,
	})
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
// when the student owns the subject containing the video, the video is
// published with a non-empty youtube_video_id, and the subject is unexpired.
// Owners keep playing after unpublish until the entitlement expires;
// non-owners additionally need a published subject under a published level.
// Returns a generic 404 with Cache-Control: private, no-store for any refusal
// (unknown/deleted/unpublished video, unowned, expired, or empty youtube_video_id).
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
	if video == nil || video.Deleted || !video.Published || strings.TrimSpace(video.YouTubeVideoID) == "" {
		writeNotFound()
		return
	}

	// 2. Fetch parent subject. Published state is checked at step 4 (owners
	// keep playing after unpublish); unknown subjects are always 404.
	sCtx, sCancel := context.WithTimeout(r.Context(), dbTimeout)
	subj, err := s.Store.GetSubjectByID(sCtx, video.SubjectID)
	sCancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil {
		writeNotFound()
		return
	}

	// 3. Subject access must not be expired
	if !subj.AccessExpiresAt.IsZero() && time.Now().After(subj.AccessExpiresAt) {
		writeNotFound()
		return
	}

	// 4. Check active entitlement (ownership). Only owners play: they keep
	// playing after unpublish (or under an unpublished level) until the
	// entitlement expires. Non-owners always get the generic 404.
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

// GetMyEntitlements serves GET /academy/me/entitlements.
// Returns the list of subject IDs currently owned by the authenticated student.
// Excludes revoked and expired entitlements. Never leaks financial or admin metadata.
func (s *Server) GetMyEntitlements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	ownedMap, err := s.Store.GetActiveEntitlementSubjectIDs(ctx, claims.UserID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	subjectIDs := make([]string, 0, len(ownedMap))
	for id := range ownedMap {
		subjectIDs = append(subjectIDs, id)
	}
	sort.Strings(subjectIDs)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"subject_ids": subjectIDs,
	})
}

// PublicHandler constructs the HTTP handler for the public listener.
func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", Health)
	mux.HandleFunc("/academy/app-config", s.EnforceIPTier(limiter.TierRead, s.GetAppConfig))
	mux.HandleFunc("/academy/levels", s.EnforceTier(limiter.TierRead, s.GetLevels))
	mux.HandleFunc("/academy/subjects", s.EnforceTier(limiter.TierRead, s.ListSubjects))
	mux.HandleFunc("/academy/subjects/", s.SubjectSubroute)
	mux.HandleFunc("/academy/videos/", s.VideoSubroute)
	mux.HandleFunc("/academy/me/entitlements", s.EnforceTier(limiter.TierRead, s.GetMyEntitlements))

	var h http.Handler = mux
	h = s.StudentAuth(h)
	h = s.GatewayAuth(h)
	h = handlerutil.MaxBytesMiddleware(1 << 20)(h)
	return h
}

// AdminHandler lives in admin.go: the internal admin listener serves ONLY
// /internal/admin/* routes behind X-Internal-Token + X-Admin-Token
// (verified via auth-service) and 404s on all public routes.
