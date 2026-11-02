package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
)

// DeleteAdminSubject handles DELETE /internal/admin/subjects/{id} (owner
// amendment 2026-11-02 to ADR-0012 decision 5). Only a draft subject that no
// student ever touched can be deleted: a published subject is 409
// subject_published (unpublish first), and a subject referenced by any
// entitlement, purchase request or payment record, in any state, is 409
// subject_has_students. The subject row, its videos and its file rows go;
// then the stored file objects, whose failures are logged and do not fail
// the call (like file_delete).
func (s *Server) DeleteAdminSubject(w http.ResponseWriter, r *http.Request, id string) {
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

	files, err := s.Store.DeleteDraftSubject(dbCtx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "subject_not_found", "subject not found", nil)
		return
	case errors.Is(err, store.ErrSubjectPublished):
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "subject_published", "unpublish the subject before deleting it", nil)
		return
	case errors.Is(err, store.ErrSubjectHasStudents):
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "subject_has_students", "students own or requested this subject", nil)
		return
	case err != nil:
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	for _, f := range files {
		if s.Files == nil {
			logStoredObjectLeft(f.ID, "subject_delete: file storage unconfigured")
			continue
		}
		s.removeStoredObject(f.StorageKey, f.ID, "subject_delete")
	}

	if err := s.writeAdminAudit(r.Context(), adm, "subject_delete", "subject", subj.ID, subj.TitleAr); err != nil {
		logAuditFailure(adm, "subject_delete", subj.ID, err)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
