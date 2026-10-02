package handlers

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// AdminVideoSubroute dispatches PATCH and DELETE on /internal/admin/videos/{id}.
func (s *Server) AdminVideoSubroute(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/internal/admin/videos/")
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") || !validResourceID(id) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_video_id", "invalid video id", nil)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		s.PatchAdminVideo(w, r, id)
	case http.MethodDelete:
		s.DeleteAdminVideo(w, r, id)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

// AdminSubjectVideos handles GET (list) and POST (create) on
// /internal/admin/subjects/{id}/videos.
func (s *Server) AdminSubjectVideos(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		s.ListAdminVideos(w, r, id)
	case http.MethodPost:
		s.CreateAdminVideo(w, r, id)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

// ListAdminVideos handles GET /internal/admin/subjects/{id}/videos: the admin
// view of the subject's videos, including the YouTube id, in order.
func (s *Server) ListAdminVideos(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := AdminFromRequest(r); !ok {
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

	videos, err := s.Store.ListVideosBySubject(dbCtx, id, false)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	dtos := make([]models.VideoAdminDTO, 0, len(videos))
	for _, v := range videos {
		dtos = append(dtos, v.ToAdminDTO())
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"videos": dtos})
}

// createVideoRequest is the body of POST /internal/admin/subjects/{id}/videos.
type createVideoRequest struct {
	TitleAr         string `json:"title_ar"`
	YouTube         string `json:"youtube"`
	Order           *int   `json:"order"`
	DurationSeconds *int   `json:"duration_seconds"`
}

// CreateAdminVideo handles POST /internal/admin/subjects/{id}/videos.
// The youtube field accepts a full URL or a bare id; only the validated id is
// stored. Videos are admin-curated and created published: the subject's own
// draft/published state gates student visibility.
func (s *Server) CreateAdminVideo(w http.ResponseWriter, r *http.Request, id string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var req createVideoRequest
	if !decodeAdminJSON(w, r, &req) {
		return
	}

	titleAr, valid := cleanAdminName(req.TitleAr)
	if !valid {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_title", "title_ar must be 1-200 characters", nil)
		return
	}
	youtubeID, valid := parseYouTubeID(req.YouTube)
	if !valid {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_youtube_id", "invalid YouTube URL or id", nil)
		return
	}
	duration := 0
	if req.DurationSeconds != nil {
		if *req.DurationSeconds < 0 {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_duration_seconds", "duration_seconds must be >= 0", nil)
			return
		}
		duration = *req.DurationSeconds
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

	position := 0
	if req.Order != nil {
		position = *req.Order
	} else {
		existing, err := s.Store.ListVideosBySubject(dbCtx, id, false)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		for _, v := range existing {
			if v.Position >= position {
				position = v.Position + 1
			}
		}
		if position == 0 {
			position = 1
		}
	}

	vid, _ := jwtutil.GenerateUUID()
	now := time.Now().UTC()
	video := &models.Video{
		ID:              vid,
		SubjectID:       id,
		Position:        position,
		TitleAr:         titleAr,
		YouTubeVideoID:  youtubeID,
		DurationSeconds: duration,
		Published:       true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.Store.CreateVideo(dbCtx, video); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "video_create", "video", video.ID, video.TitleAr); err != nil {
		logAuditFailure(adm, "video_create", video.ID, err)
	}

	handlerutil.WriteJSON(w, http.StatusCreated, video.ToAdminDTO())
}

// patchVideoRequest is the body of PATCH /internal/admin/videos/{id}.
type patchVideoRequest struct {
	TitleAr         *string `json:"title_ar"`
	YouTube         *string `json:"youtube"`
	Order           *int    `json:"order"`
	DurationSeconds *int    `json:"duration_seconds"`
}

// PatchAdminVideo handles PATCH /internal/admin/videos/{id}.
func (s *Server) PatchAdminVideo(w http.ResponseWriter, r *http.Request, id string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var req patchVideoRequest
	if !decodeAdminJSON(w, r, &req) {
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()
	video, err := s.Store.GetVideoByID(dbCtx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if video == nil || video.Deleted {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "video_not_found", "video not found", nil)
		return
	}

	if req.TitleAr != nil {
		titleAr, valid := cleanAdminName(*req.TitleAr)
		if !valid {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_title", "title_ar must be 1-200 characters", nil)
			return
		}
		video.TitleAr = titleAr
	}
	if req.YouTube != nil {
		youtubeID, valid := parseYouTubeID(*req.YouTube)
		if !valid {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_youtube_id", "invalid YouTube URL or id", nil)
			return
		}
		video.YouTubeVideoID = youtubeID
	}
	if req.Order != nil {
		video.Position = *req.Order
	}
	if req.DurationSeconds != nil {
		if *req.DurationSeconds < 0 {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_duration_seconds", "duration_seconds must be >= 0", nil)
			return
		}
		video.DurationSeconds = *req.DurationSeconds
	}
	video.UpdatedAt = time.Now().UTC()

	if err := s.Store.UpdateVideo(dbCtx, video); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "video_update", "video", video.ID, video.TitleAr); err != nil {
		logAuditFailure(adm, "video_update", video.ID, err)
	}

	handlerutil.WriteJSON(w, http.StatusOK, video.ToAdminDTO())
}

// reorderVideosRequest is the body of POST
// /internal/admin/subjects/{id}/videos/reorder: the full ordered list.
type reorderVideosRequest struct {
	VideoIDs []string `json:"video_ids"`
}

// ReorderAdminVideos handles POST /internal/admin/subjects/{id}/videos/reorder.
// Every id must belong to the subject: no missing ids, no extras, no
// duplicates.
func (s *Server) ReorderAdminVideos(w http.ResponseWriter, r *http.Request, id string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var req reorderVideosRequest
	if !decodeAdminJSON(w, r, &req) {
		return
	}
	if req.VideoIDs == nil {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_video_order", "video_ids is required", nil)
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

	existing, err := s.Store.ListVideosBySubject(dbCtx, id, false)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	byID := make(map[string]*models.Video, len(existing))
	for _, v := range existing {
		byID[v.ID] = v
	}
	if len(req.VideoIDs) != len(existing) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_video_order", "video_ids must list every video of the subject exactly once", nil)
		return
	}
	seen := make(map[string]bool, len(req.VideoIDs))
	for _, vid := range req.VideoIDs {
		if !validResourceID(vid) || byID[vid] == nil || seen[vid] {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_video_order", "video_ids must list every video of the subject exactly once", nil)
			return
		}
		seen[vid] = true
	}

	now := time.Now().UTC()
	ordered := make([]*models.Video, 0, len(req.VideoIDs))
	for i, vid := range req.VideoIDs {
		v := byID[vid]
		v.Position = i + 1
		v.UpdatedAt = now
		if err := s.Store.UpdateVideo(dbCtx, v); err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		ordered = append(ordered, v)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Position < ordered[j].Position })

	if err := s.writeAdminAudit(r.Context(), adm, "video_reorder", "subject", id, strings.Join(req.VideoIDs, ",")); err != nil {
		logAuditFailure(adm, "video_reorder", id, err)
	}

	dtos := make([]models.VideoAdminDTO, 0, len(ordered))
	for _, v := range ordered {
		dtos = append(dtos, v.ToAdminDTO())
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"videos": dtos})
}

// DeleteAdminVideo handles DELETE /internal/admin/videos/{id}[?force=true].
// Soft delete: the video is hidden from students and from /play. Deleting
// the last video of a published subject needs ?force=true, which also
// unpublishes the subject.
func (s *Server) DeleteAdminVideo(w http.ResponseWriter, r *http.Request, id string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	force := false
	if raw := strings.TrimSpace(r.URL.Query().Get("force")); raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			force = v
		}
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()
	video, err := s.Store.GetVideoByID(dbCtx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if video == nil || video.Deleted {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "video_not_found", "video not found", nil)
		return
	}

	subj, err := s.Store.GetSubjectByID(dbCtx, video.SubjectID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", nil)
		return
	}

	remaining, err := s.subjectVideoCount(dbCtx, video.SubjectID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	// remaining includes this video (not deleted yet).
	lastOfPublished := subj.Status == models.StatusPublished && remaining <= 1
	if lastOfPublished && !force {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "last_video_of_published_subject", "last video of a published subject needs force", nil)
		return
	}

	now := time.Now().UTC()
	video.Deleted = true
	video.DeletedAt = now
	video.UpdatedAt = now
	if err := s.Store.UpdateVideo(dbCtx, video); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "video_delete", "video", video.ID, video.TitleAr); err != nil {
		logAuditFailure(adm, "video_delete", video.ID, err)
	}

	if lastOfPublished && force {
		subj.Status = models.StatusDraft
		subj.UpdatedAt = now
		if err := s.Store.UpdateSubject(dbCtx, subj); err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if err := s.writeAdminAudit(r.Context(), adm, "subject_unpublish", "subject", subj.ID, subj.TitleAr); err != nil {
			logAuditFailure(adm, "subject_unpublish", subj.ID, err)
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
