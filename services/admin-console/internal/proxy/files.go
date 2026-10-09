// File proxy routes (SPEC Phase 5): list, upload and delete a subject's PDF
// files over the academy-service admin API.
//
// The guarantees match the catalog routes: X-Admin-Token is required (401 with
// no upstream call), input is validated locally (400 with no upstream call),
// the upstream request is built from scratch against ACADEMY_ADMIN_URL over
// mTLS, and upstream failures become a safe 503. The academy stays the
// authority on every rule; the console checks only what it can cheaply.
//
// Upload is the one route whose body may exceed 1 MiB: it is capped at
// MAX_PDF_BYTES plus the form overhead, and streamed. The console reads the
// three text fields, checks the %PDF- magic, then re-encodes a fresh
// multipart body (fields plus one file part named upload.pdf) through a pipe
// to the academy while the file is still arriving, so the file is never held
// in memory. Only this handler extends its own read and write deadlines; the
// server-wide timeouts stay as they are.
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

const (
	// UploadTimeout bounds one upload end to end (slow admin links).
	UploadTimeout = 10 * time.Minute
	// DefaultMaxPDFBytes is the MAX_PDF_BYTES default (20 MB, SPEC D14).
	DefaultMaxPDFBytes int64 = 20 * 1024 * 1024
	// uploadFormOverhead is the slack above MAX_PDF_BYTES for the multipart
	// boundaries, part headers and the text fields (same as the academy).
	uploadFormOverhead int64 = 64 << 10
	// maxUploadFieldBytes caps one text field of the upload form.
	maxUploadFieldBytes = 4 << 10
)

var (
	pdfMagic = []byte("%PDF-")

	fileKinds = map[string]bool{"book": true, "note": true}

	errUploadTooLarge = errors.New("upload: file larger than MAX_PDF_BYTES")
	errUploadExtra    = errors.New("upload: a part follows the file")
)

// MaxUploadBodyBytes is the body cap of the upload route.
func (p *Proxy) MaxUploadBodyBytes() int64 {
	return p.maxPDFBytes + uploadFormOverhead
}

// FilesList handles GET /api/files?subject_id -> GET academy
// /internal/admin/subjects/{id}/files.
func (p *Proxy) FilesList(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	subjectID := strings.TrimSpace(r.URL.Query().Get("subject_id"))
	if !validCatalogID(subjectID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "files.list", method: http.MethodGet,
		path:     "/internal/admin/subjects/" + subjectID + "/files",
		targetID: subjectID,
	})
}

// fileDeleteRequest is the body of POST /api/files/delete.
type fileDeleteRequest struct {
	SubjectID string `json:"subject_id"`
	ID        string `json:"id"`
}

// FilesDelete handles POST /api/files/delete {subject_id, id} -> DELETE
// academy /internal/admin/subjects/{subject_id}/files/{id}.
func (p *Proxy) FilesDelete(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in fileDeleteRequest
	if !decodeStrict(w, r, &in) {
		return
	}
	if !validCatalogID(in.SubjectID) || !validCatalogID(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "files.delete", method: http.MethodDelete,
		path:     "/internal/admin/subjects/" + in.SubjectID + "/files/" + in.ID,
		targetID: in.ID,
	})
}

// uploadFields are the validated text fields of an upload.
type uploadFields struct {
	kind, titleAr, titleEn string
}

// clientError marks a failure caused by the browser's body (malformed,
// truncated, oversized), as opposed to the upstream closing the pipe.
type clientError struct {
	status int
	code   string
}

func (e *clientError) Error() string { return "upload: client body: " + e.code }

func tooLargeOr(err error, fallback *clientError) *clientError {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) || errors.Is(err, errUploadTooLarge) {
		return &clientError{http.StatusRequestEntityTooLarge, "file_too_large"}
	}
	return fallback
}

var errBadUpload = &clientError{http.StatusBadRequest, "bad_request"}

// cappedReader fails once more than limit bytes were read from the file part.
type cappedReader struct {
	r     io.Reader
	n     int64
	limit int64
}

func (c *cappedReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n += int64(n)
	if c.n > c.limit {
		return n, errUploadTooLarge
	}
	return n, err
}

// readUploadFields reads kind, title_ar and title_en (each once, before the
// file part) and returns them with the file part. The rules mirror the
// academy (admin_files.go).
func readUploadFields(mr *multipart.Reader) (uploadFields, *multipart.Part, *clientError) {
	var f uploadFields
	seen := map[string]bool{}
	for {
		part, err := mr.NextPart()
		if err != nil {
			return f, nil, tooLargeOr(err, errBadUpload)
		}
		name := part.FormName()
		if name == "file" {
			if part.FileName() == "" || !seen["kind"] || !seen["title_ar"] {
				return f, nil, errBadUpload
			}
			return f, part, nil
		}
		if seen[name] || part.FileName() != "" {
			return f, nil, errBadUpload
		}
		raw, err := io.ReadAll(io.LimitReader(part, maxUploadFieldBytes+1))
		if err != nil {
			return f, nil, tooLargeOr(err, errBadUpload)
		}
		if len(raw) > maxUploadFieldBytes {
			return f, nil, errBadUpload
		}
		value := string(raw)
		switch name {
		case "kind":
			kind := strings.TrimSpace(value)
			if !fileKinds[kind] {
				return f, nil, errBadUpload
			}
			f.kind = kind
		case "title_ar":
			v, ok := cleanCatalogName(value)
			if !ok {
				return f, nil, errBadUpload
			}
			f.titleAr = v
		case "title_en":
			if strings.TrimSpace(value) != "" {
				v, ok := cleanCatalogName(value)
				if !ok {
					return f, nil, errBadUpload
				}
				f.titleEn = v
			}
		default:
			return f, nil, errBadUpload
		}
		seen[name] = true
	}
}

// FilesUpload handles POST /api/files/upload?subject_id (multipart/form-data:
// kind, title_ar, title_en, then one file) -> POST academy
// /internal/admin/subjects/{id}/files, streamed.
func (p *Proxy) FilesUpload(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	subjectID := strings.TrimSpace(r.URL.Query().Get("subject_id"))
	if !validCatalogID(subjectID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if r.ContentLength > p.MaxUploadBodyBytes() {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large")
		return
	}

	// This route alone may take longer than the server-wide timeouts.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(p.uploadTimeout))
	_ = rc.SetWriteDeadline(time.Now().Add(p.uploadTimeout + 30*time.Second))

	body := http.MaxBytesReader(w, r.Body, p.MaxUploadBodyBytes())
	mr := multipart.NewReader(body, params["boundary"])
	fields, filePart, cerr := readUploadFields(mr)
	if cerr != nil {
		writeError(w, cerr.status, cerr.code)
		return
	}

	capped := &cappedReader{r: filePart, limit: p.maxPDFBytes}
	file := bufio.NewReaderSize(capped, 32<<10)
	head, err := file.Peek(len(pdfMagic))
	if err != nil {
		cerr := tooLargeOr(err, &clientError{http.StatusBadRequest, "invalid_pdf"})
		writeError(w, cerr.status, cerr.code)
		return
	}
	if !bytes.Equal(head, pdfMagic) {
		writeError(w, http.StatusBadRequest, "invalid_pdf")
		return
	}

	c := upstreamCall{
		route: "files.upload", method: http.MethodPost,
		path:     "/internal/admin/subjects/" + subjectID + "/files",
		targetID: subjectID, academy: true,
	}
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	done := make(chan error, 1)
	go func() {
		err := writeUploadBody(mw, fields, file, mr)
		if err != nil {
			_ = pw.CloseWithError(err)
		} else {
			_ = pw.Close()
		}
		done <- err
	}()

	ctx, cancel := context.WithTimeout(r.Context(), p.uploadTimeout)
	defer cancel()
	// #nosec G704 //nolint:gosec -- scheme and host come from validated config; the path is built from a validated id
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.academyURL+c.path, pr)
	if err != nil {
		_ = pr.CloseWithError(err)
		<-done
		logOutcome(c, 0, "build_error")
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set(InternalTokenHeader, p.internalToken)
	req.Header.Set(AdminTokenHeader, token)
	if ip := p.ClientIP(r); ip != "" {
		req.Header.Set(ClientIPHeader, ip)
	}

	// #nosec G704 //nolint:gosec -- same request as above
	resp, doErr := p.uploadClient.Do(req)
	// The upstream may answer before reading the whole body (e.g. 401, 404):
	// closing the reader ends the writer instead of leaving it blocked.
	_ = pr.CloseWithError(io.ErrClosedPipe)
	writeErr := <-done

	// A broken browser body wins over whatever the upstream made of it.
	var ce *clientError
	if errors.As(writeErr, &ce) {
		if resp != nil {
			_ = resp.Body.Close()
		}
		logOutcome(c, 0, "client_body")
		writeError(w, ce.status, ce.code)
		return
	}
	status, data, ok := p.finish(w, c, resp, doErr)
	if !ok {
		return
	}
	relay(w, status, data)
}

// writeUploadBody writes the fresh multipart body: the validated fields, then
// the file part (fixed name, fixed content type), streamed from file. It
// returns a *clientError when the browser body is at fault.
func writeUploadBody(mw *multipart.Writer, f uploadFields, file io.Reader, mr *multipart.Reader) error {
	for _, kv := range [][2]string{{"kind", f.kind}, {"title_ar", f.titleAr}, {"title_en", f.titleEn}} {
		if kv[0] == "title_en" && kv[1] == "" {
			continue
		}
		if err := mw.WriteField(kv[0], kv[1]); err != nil {
			return err
		}
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="upload.pdf"`)
	h.Set("Content-Type", "application/pdf")
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := copyFile(part, file); err != nil {
		return err
	}
	// Nothing may follow the file part (one PDF per request).
	if _, err := mr.NextPart(); !errors.Is(err, io.EOF) {
		if err != nil {
			return tooLargeOr(err, errBadUpload)
		}
		return &clientError{http.StatusBadRequest, "bad_request"}
	}
	return mw.Close()
}

// copyFile copies the file part, telling browser-side read errors (client)
// apart from write errors (the upstream pipe closed).
func copyFile(dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32<<10)
	var total int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		}
		if errors.Is(rerr, io.EOF) {
			return total, nil
		}
		if rerr != nil {
			return total, tooLargeOr(rerr, errBadUpload)
		}
	}
}
