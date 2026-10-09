package handlers

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/storage"
)

// Admin PDF files (SPEC Phase 5.1 and 5.3, Section 8 item 6, ADR-0009).
//
//   - GET    /internal/admin/subjects/{id}/files           list
//   - POST   /internal/admin/subjects/{id}/files           upload (multipart)
//   - DELETE /internal/admin/subjects/{id}/files/{fileId}  delete
//
// Subjects are never hard-deleted (ADR-0012 decision 5: unpublish is the only
// way to hide one), so a subject's files live as long as the subject; owners
// keep downloading an unpublished subject's files until their entitlement
// expires, exactly like its videos (ADR-0012 decision 9).

const (
	// uploadFormOverhead is the slack above MAX_PDF_BYTES allowed for the
	// multipart boundaries, part headers and the three text fields.
	uploadFormOverhead int64 = 64 << 10
	// maxUploadFieldBytes caps one text field of the upload form.
	maxUploadFieldBytes = 4 << 10
)

// pdfMagic is the required start of every uploaded file (SPEC Section 8.6).
var pdfMagic = []byte("%PDF-")

// FileAdminDTO is the admin view of a subject file. It never carries the
// storage key.
type FileAdminDTO struct {
	ID        string    `json:"id"`
	SubjectID string    `json:"subject_id"`
	Kind      string    `json:"kind"`
	TitleAr   string    `json:"title_ar"`
	TitleEn   string    `json:"title_en"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

func toFileAdminDTO(f *models.SubjectFile) FileAdminDTO {
	return FileAdminDTO{
		ID:        f.ID,
		SubjectID: f.SubjectID,
		Kind:      f.Kind,
		TitleAr:   f.TitleAr,
		TitleEn:   f.TitleEn,
		SizeBytes: f.SizeBytes,
		CreatedAt: f.CreatedAt,
	}
}

// validFileKind reports whether kind is an allowed subject file kind (D5).
func validFileKind(kind string) bool {
	return kind == "book" || kind == "note"
}

// isAdminUploadRoute reports whether the request is a file upload, the only
// admin route whose body may exceed 1 MiB.
func isAdminUploadRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	rest := strings.TrimPrefix(r.URL.Path, "/internal/admin/subjects/")
	if rest == r.URL.Path {
		return false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] == "files"
}

// maxUploadBodyBytes is the request body cap of the upload route.
func (s *Server) maxUploadBodyBytes() int64 {
	return s.MaxPDFBytes + uploadFormOverhead
}

// adminBodyLimit caps every admin request body at 1 MiB, except the upload
// route, which is capped at MAX_PDF_BYTES plus the form overhead.
func (s *Server) adminBodyLimit(next http.Handler) http.Handler {
	small := handlerutil.MaxBytesMiddleware(1 << 20)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAdminUploadRoute(r) && s.MaxPDFBytes > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadBodyBytes())
			next.ServeHTTP(w, r)
			return
		}
		small.ServeHTTP(w, r)
	})
}

// AdminSubjectFiles dispatches GET (list) and POST (upload) on
// /internal/admin/subjects/{id}/files.
func (s *Server) AdminSubjectFiles(w http.ResponseWriter, r *http.Request, subjectID string) {
	switch r.Method {
	case http.MethodGet:
		s.ListAdminFiles(w, r, subjectID)
	case http.MethodPost:
		s.UploadAdminFile(w, r, subjectID)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

// ListAdminFiles handles GET /internal/admin/subjects/{id}/files, oldest first.
func (s *Server) ListAdminFiles(w http.ResponseWriter, r *http.Request, subjectID string) {
	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
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

	files, err := s.Store.ListFilesBySubject(dbCtx, subjectID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	dtos := make([]FileAdminDTO, 0, len(files))
	for _, f := range files {
		dtos = append(dtos, toFileAdminDTO(f))
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"files": dtos})
}

// uploadForm holds the validated text fields of an upload.
type uploadForm struct {
	kind    string
	titleAr string
	titleEn string
}

// errUploadTooLarge marks a file part above MAX_PDF_BYTES.
var errUploadTooLarge = errors.New("upload: file larger than MAX_PDF_BYTES")

// cappedReader counts the bytes read and fails once more than limit bytes
// have been read, so a file part above MAX_PDF_BYTES is refused exactly.
type cappedReader struct {
	r     io.Reader
	n     int64
	limit int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.n > c.limit {
		return n, errUploadTooLarge
	}
	return n, err
}

// isBodyTooLarge reports whether err comes from a size cap.
func isBodyTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe) || errors.Is(err, errUploadTooLarge)
}

// UploadAdminFile handles POST /internal/admin/subjects/{id}/files.
//
// The body is multipart/form-data with exactly the fields kind (book|note),
// title_ar (1-200 characters), title_en (empty or 1-200), then file, in that
// order: the text fields must come before the file part so the file can be
// streamed to storage after they are validated. One PDF per request. The
// client content type and filename are ignored; the file must start with
// %PDF- and be at most MAX_PDF_BYTES (413 above). The storage key is a
// server-generated UUID. If the row insert fails, the stored object is
// deleted again. 201 with the admin DTO.
func (s *Server) UploadAdminFile(w http.ResponseWriter, r *http.Request, subjectID string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	if s.Files == nil || s.MaxPDFBytes <= 0 {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("file storage unconfigured"))
		return
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_upload", "body must be multipart/form-data", nil)
		return
	}
	// A declared length above the cap is refused before anything is read.
	if r.ContentLength > s.maxUploadBodyBytes() {
		handlerutil.WriteSafeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "file too large", nil)
		return
	}

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

	mr, err := r.MultipartReader()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_upload", "body must be multipart/form-data", nil)
		return
	}

	form, filePart, code, status := readUploadFields(mr)
	if code != "" {
		handlerutil.WriteSafeError(w, r, status, code, uploadErrorMessage(code), nil)
		return
	}

	// Sniff the magic bytes before anything is stored.
	capped := &cappedReader{r: filePart, limit: s.MaxPDFBytes}
	br := bufio.NewReaderSize(capped, 4096)
	head, err := br.Peek(len(pdfMagic))
	if err != nil {
		if isBodyTooLarge(err) {
			handlerutil.WriteSafeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "file too large", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_pdf", "file is not a PDF", nil)
		return
	}
	if !bytes.Equal(head, pdfMagic) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_pdf", "file is not a PDF", nil)
		return
	}

	fileID, err := jwtutil.GenerateUUID()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	storageKey, err := jwtutil.GenerateUUID()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	// LocalStorage reads the part until EOF and does not watch the context;
	// the transfer is bounded by the body cap above and by the caller's own
	// deadline (admin-console extends only the upload route, 10 minutes).
	if err := s.Files.Upload(r.Context(), storageKey, br, "application/pdf"); err != nil {
		if isBodyTooLarge(err) {
			handlerutil.WriteSafeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "file too large", nil)
			return
		}
		// A client that stops sending mid-file is a bad request, not an outage.
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, multipart.ErrMessageTooLarge) {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_upload", "incomplete upload", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	// Nothing may follow the file part (one PDF per request).
	if _, err := mr.NextPart(); !errors.Is(err, io.EOF) {
		s.removeStoredObject(storageKey, fileID, "upload_extra_part")
		if err != nil && isBodyTooLarge(err) {
			handlerutil.WriteSafeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "file too large", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_upload", "exactly one file per request", nil)
		return
	}

	file := &models.SubjectFile{
		ID:         fileID,
		SubjectID:  subj.ID,
		Kind:       form.kind,
		TitleAr:    form.titleAr,
		TitleEn:    form.titleEn,
		SizeBytes:  capped.n,
		StorageKey: storageKey,
		CreatedAt:  time.Now().UTC(),
	}
	insCtx, insCancel := context.WithTimeout(context.WithoutCancel(r.Context()), dbTimeout)
	err = s.Store.CreateFile(insCtx, file)
	insCancel()
	if err != nil {
		s.removeStoredObject(storageKey, fileID, "upload_insert_failed")
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "file_upload", "file", file.ID, file.Kind+" "+file.TitleAr); err != nil {
		logAuditFailure(adm, "file_upload", file.ID, err)
	}

	handlerutil.WriteJSON(w, http.StatusCreated, toFileAdminDTO(file))
}

// readUploadFields reads the text parts kind, title_ar and title_en (each at
// most once, in any order among themselves) up to the file part, and returns
// the validated fields and the file part. Unknown or repeated parts, a missing
// field, or no file part are 400; a size cap hit is 413.
func readUploadFields(mr *multipart.Reader) (uploadForm, *multipart.Part, string, int) {
	var form uploadForm
	seen := map[string]bool{}
	for {
		part, err := mr.NextPart()
		if err != nil {
			if isBodyTooLarge(err) {
				return form, nil, "file_too_large", http.StatusRequestEntityTooLarge
			}
			return form, nil, "invalid_upload", http.StatusBadRequest
		}
		name := part.FormName()
		if name == "file" {
			if part.FileName() == "" || !seen["kind"] || !seen["title_ar"] {
				return form, nil, "invalid_upload", http.StatusBadRequest
			}
			return form, part, "", 0
		}
		if seen[name] || part.FileName() != "" {
			return form, nil, "invalid_upload", http.StatusBadRequest
		}
		raw, err := io.ReadAll(io.LimitReader(part, maxUploadFieldBytes+1))
		if err != nil {
			if isBodyTooLarge(err) {
				return form, nil, "file_too_large", http.StatusRequestEntityTooLarge
			}
			return form, nil, "invalid_upload", http.StatusBadRequest
		}
		if len(raw) > maxUploadFieldBytes {
			return form, nil, "invalid_upload", http.StatusBadRequest
		}
		value := string(raw)
		switch name {
		case "kind":
			kind := strings.TrimSpace(value)
			if !validFileKind(kind) {
				return form, nil, "invalid_kind", http.StatusBadRequest
			}
			form.kind = kind
		case "title_ar":
			v, ok := cleanAdminName(value)
			if !ok {
				return form, nil, "invalid_title", http.StatusBadRequest
			}
			form.titleAr = v
		case "title_en":
			if strings.TrimSpace(value) != "" {
				v, ok := cleanAdminName(value)
				if !ok {
					return form, nil, "invalid_title", http.StatusBadRequest
				}
				form.titleEn = v
			}
		default:
			return form, nil, "invalid_upload", http.StatusBadRequest
		}
		seen[name] = true
	}
}

func uploadErrorMessage(code string) string {
	switch code {
	case "invalid_kind":
		return "kind must be book or note"
	case "invalid_title":
		return "title_ar must be 1-200 characters, title_en empty or 1-200"
	case "file_too_large":
		return "file too large"
	default:
		return "invalid upload: fields kind, title_ar, title_en, then one file"
	}
}

// removeStoredObject deletes an object whose row was never written (or is
// gone). A failure is logged with ids only and does not change the response.
func (s *Server) removeStoredObject(storageKey, fileID, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	if err := s.Files.Delete(ctx, storageKey); err != nil {
		cleanID := strings.ReplaceAll(strings.ReplaceAll(fileID, "\r", ""), "\n", "")
		// #nosec G706 -- cleanID sanitized of CR/LF; the storage key is a server UUID
		log.Printf("[ERROR] file object cleanup failed (%s) file_id=%s storage_key=%s: %v", reason, cleanID, storageKey, err)
	}
}

// DeleteAdminFile handles DELETE /internal/admin/subjects/{id}/files/{fileId}.
// The row is deleted first, so students stop seeing the file at once; then
// the stored object. An object that is already gone, or fails to delete, is
// logged and does not fail the call.
func (s *Server) DeleteAdminFile(w http.ResponseWriter, r *http.Request, subjectID, fileID string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	if s.Files == nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", errors.New("file storage unconfigured"))
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()
	file, err := s.Store.GetFile(dbCtx, subjectID, fileID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if file == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "file_not_found", "file not found", nil)
		return
	}

	if err := s.Store.DeleteFile(dbCtx, subjectID, fileID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "file_not_found", "file not found", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if _, err := s.Files.Size(file.StorageKey); errors.Is(err, storage.ErrNotFound) {
		cleanID := strings.ReplaceAll(strings.ReplaceAll(file.ID, "\r", ""), "\n", "")
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[WARN] file_delete: stored object already gone file_id=%s", cleanID)
	} else {
		s.removeStoredObject(file.StorageKey, file.ID, "file_delete")
	}

	if err := s.writeAdminAudit(r.Context(), adm, "file_delete", "file", file.ID, file.Kind+" "+file.TitleAr); err != nil {
		logAuditFailure(adm, "file_delete", file.ID, err)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
