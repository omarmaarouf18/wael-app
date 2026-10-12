// Active owner ids for broadcast audiences (SPEC Phase 7, ADR-0013).
//
// GET /internal/admin/subjects/{id}/owner-ids?cursor=&limit= pages the user
// ids holding an active entitlement for the subject (SPEC R1: active flag,
// expires_at in the future, no revoked_at marker), ordered by user id. The
// subject must exist: subjects carry no deleted flag, so existence means not
// deleted. Dispatched from AdminSubjectSubroute; authenticated by requireAdmin
// like every other admin route.
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
)

// AdminBroadcastAudience serves the paged active-owner listing for one
// subject. Response: {subject_id, user_ids, next_cursor, total}. next_cursor
// is empty on the last page; pass it back as cursor for the next page.
func (s *Server) AdminBroadcastAudience(w http.ResponseWriter, r *http.Request, subjectID string) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	q := r.URL.Query()
	limit := 100
	if lStr := strings.TrimSpace(q.Get("limit")); lStr != "" {
		if l, err := strconv.Atoi(lStr); err != nil || l <= 0 {
			limit = 100
		} else if l > 1000 {
			limit = 1000
		} else {
			limit = l
		}
	}
	cursor := strings.TrimSpace(q.Get("cursor"))

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	subj, err := s.Store.GetSubjectByID(dbCtx, subjectID)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if subj == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "subject_not_found", "subject not found", nil)
		return
	}

	ownerStore, ok := s.Store.(interface {
		ListActiveOwnerIDsBySubject(ctx context.Context, subjectID, cursor string, limit int) ([]string, error)
		CountActiveOwnersBySubject(ctx context.Context, subjectID string) (int, error)
	})
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", nil)
		return
	}

	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	ids, err := ownerStore.ListActiveOwnerIDsBySubject(dbCtx, subjectID, cursor, limit)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	dbCtx, cancel = context.WithTimeout(r.Context(), dbTimeout)
	total, err := ownerStore.CountActiveOwnersBySubject(dbCtx, subjectID)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if ids == nil {
		ids = []string{}
	}
	nextCursor := ""
	if len(ids) == limit && limit > 0 {
		nextCursor = ids[len(ids)-1]
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"subject_id":  subjectID,
		"user_ids":    ids,
		"next_cursor": nextCursor,
		"total":       total,
	})
}
