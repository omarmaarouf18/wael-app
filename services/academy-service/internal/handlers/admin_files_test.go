package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/storage"
)

var canonicalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// failingCreateStore fails CreateFile, for the insert-failure cleanup path.
type failingCreateStore struct {
	*store.MemoryStore
}

func (f *failingCreateStore) CreateFile(context.Context, *models.SubjectFile) error {
	return errors.New("insert failed")
}

// newFileAdminServer is an admin test server with real encrypted storage in a
// temp dir and the given PDF cap.
func newFileAdminServer(t *testing.T, maxPDF int64) (*Server, string) {
	t.Helper()
	s, _ := newAdminTestServer(t, okVerify)
	dir := t.TempDir()
	st, err := storage.NewLocalStorage(dir, "", "test")
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s.Files = st
	s.MaxPDFBytes = maxPDF
	return s, dir
}

// storedObjects lists the object names in the storage dir (temp files excluded).
func storedObjects(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read storage dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".upload-") {
			names = append(names, e.Name())
		}
	}
	return names
}

type formPart struct {
	name, filename, contentType string
	data                        []byte
}

func field(name, value string) formPart { return formPart{name: name, data: []byte(value)} }

func filePart(filename string, data []byte) formPart {
	return formPart{name: "file", filename: filename, contentType: "application/pdf", data: data}
}

func buildMultipart(t *testing.T, parts ...formPart) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		cd := `form-data; name="` + p.name + `"`
		if p.filename != "" {
			cd += `; filename="` + p.filename + `"`
		}
		h.Set("Content-Disposition", cd)
		if p.contentType != "" {
			h.Set("Content-Type", p.contentType)
		}
		w, err := mw.CreatePart(h)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		_, _ = w.Write(p.data)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

func pdfBytes(n int) []byte {
	b := bytes.Repeat([]byte{'x'}, n)
	copy(b, "%PDF-1.7\n")
	return b
}

func validUploadParts(content []byte) []formPart {
	return []formPart{field("kind", "note"), field("title_ar", "مذكرة الفصل الأول"), field("title_en", "Chapter 1 notes"), filePart("notes.pdf", content)}
}

func doUpload(t *testing.T, s *Server, subjectID string, parts ...formPart) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := buildMultipart(t, parts...)
	req := httptest.NewRequest(http.MethodPost, "/internal/admin/subjects/"+subjectID+"/files", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Internal-Token", "test-internal-token")
	req.Header.Set("X-Admin-Token", "test-admin-token")
	rec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(rec, req)
	return rec
}

func uploadOK(t *testing.T, s *Server, subjectID string, content []byte) FileAdminDTO {
	t.Helper()
	rec := doUpload(t, s, subjectID, validUploadParts(content)...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var dto FileAdminDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode file: %v", err)
	}
	return dto
}

func TestAdminFiles_AuthWiring(t *testing.T) {
	s, _ := newFileAdminServer(t, 1<<20)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/internal/admin/subjects/subj-1/files"},
		{http.MethodPost, "/internal/admin/subjects/subj-1/files"},
		{http.MethodDelete, "/internal/admin/subjects/subj-1/files/f-1"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		s.AdminHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without tokens: expected 401, got %d", tc.method, tc.path, rec.Code)
		}
	}

	unauth, _ := newAdminTestServer(t, unauthorizedVerify)
	unauth.Files = s.Files
	unauth.MaxPDFBytes = 1 << 20
	if rec := doUpload(t, unauth, "subj-1", validUploadParts(pdfBytes(100))...); rec.Code != http.StatusUnauthorized {
		t.Fatalf("upload with a rejected admin token: expected 401, got %d", rec.Code)
	}
}

func TestAdminFiles_UploadListDelete(t *testing.T) {
	s, dir := newFileAdminServer(t, 1<<20)
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	content := pdfBytes(5000)

	rec := doUpload(t, s, subj.ID, validUploadParts(content)...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "storage_key") {
		t.Fatalf("admin DTO leaks storage_key: %s", rec.Body.String())
	}
	var dto FileAdminDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !canonicalUUID.MatchString(dto.ID) || dto.SubjectID != subj.ID || dto.Kind != "note" ||
		dto.TitleAr != "مذكرة الفصل الأول" || dto.TitleEn != "Chapter 1 notes" || dto.SizeBytes != 5000 || dto.CreatedAt.IsZero() {
		t.Fatalf("unexpected DTO: %+v", dto)
	}

	row, err := s.Store.GetFile(context.Background(), subj.ID, dto.ID)
	if err != nil || row == nil {
		t.Fatalf("row not stored: %+v %v", row, err)
	}
	if !canonicalUUID.MatchString(row.StorageKey) || row.StorageKey == dto.ID {
		t.Fatalf("storage key %q must be its own server UUID", row.StorageKey)
	}
	if objs := storedObjects(t, dir); len(objs) != 1 || objs[0] != row.StorageKey {
		t.Fatalf("stored objects = %v, want [%s]", objs, row.StorageKey)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, row.StorageKey))
	if bytes.Contains(onDisk, []byte("%PDF-")) {
		t.Fatal("object on disk is not encrypted")
	}
	rc, err := s.Files.OpenFile(row.StorageKey)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, content) {
		t.Fatal("stored plaintext differs from the upload")
	}

	logs, _, _ := s.Store.ListAuditLogs(context.Background(), 1, 10)
	if len(logs) == 0 || logs[0].Action != "file_upload" || logs[0].TargetType != "file" || logs[0].TargetID != dto.ID || logs[0].ActorID != "adm-1" {
		t.Fatalf("missing file_upload audit entry: %+v", logs)
	}

	// The student-facing detail shows the file (titles only for non-owners).
	counts, _ := s.Store.GetSubjectCounts(context.Background(), subj.ID)
	if counts.Notes != 1 {
		t.Fatalf("notes count = %d, want 1", counts.Notes)
	}

	list := doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects/"+subj.ID+"/files", nil)
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "storage_key") || strings.Contains(list.Body.String(), row.StorageKey) {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var listed struct {
		Files []FileAdminDTO `json:"files"`
	}
	_ = json.Unmarshal(list.Body.Bytes(), &listed)
	if len(listed.Files) != 1 || listed.Files[0].ID != dto.ID {
		t.Fatalf("list = %+v", listed.Files)
	}

	// Delete of another subject's path is 404 and keeps the file.
	other := createSubject(t, s, baseSubjectBody("bachelor-y2"))
	if rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/"+other.ID+"/files/"+dto.ID, nil); rec.Code != http.StatusNotFound || adminCode(t, rec) != "file_not_found" {
		t.Fatalf("delete via other subject: %d %s", rec.Code, rec.Body.String())
	}
	if len(storedObjects(t, dir)) != 1 {
		t.Fatal("object removed by a delete through another subject")
	}

	del := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/"+subj.ID+"/files/"+dto.ID, nil)
	if del.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", del.Code, del.Body.String())
	}
	if row, _ := s.Store.GetFile(context.Background(), subj.ID, dto.ID); row != nil {
		t.Fatal("row still present after delete")
	}
	if objs := storedObjects(t, dir); len(objs) != 0 {
		t.Fatalf("object still on disk after delete: %v", objs)
	}
	logs, _, _ = s.Store.ListAuditLogs(context.Background(), 1, 10)
	if len(logs) == 0 || logs[0].Action != "file_delete" || logs[0].TargetID != dto.ID {
		t.Fatalf("missing file_delete audit entry: %+v", logs)
	}

	if rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/"+subj.ID+"/files/"+dto.ID, nil); rec.Code != http.StatusNotFound || adminCode(t, rec) != "file_not_found" {
		t.Fatalf("second delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdminFiles_DeleteWhenObjectAlreadyGone(t *testing.T) {
	s, dir := newFileAdminServer(t, 1<<20)
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	dto := uploadOK(t, s, subj.ID, pdfBytes(100))
	for _, name := range storedObjects(t, dir) {
		_ = os.Remove(filepath.Join(dir, name))
	}
	rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/"+subj.ID+"/files/"+dto.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete with object gone: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if row, _ := s.Store.GetFile(context.Background(), subj.ID, dto.ID); row != nil {
		t.Fatal("row still present")
	}
}

func TestAdminFiles_ClientMetadataIgnored(t *testing.T) {
	s, dir := newFileAdminServer(t, 1<<20)
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))

	// A PDF sent as image/png with a traversal filename is stored under a UUID.
	parts := validUploadParts(pdfBytes(64))
	parts[3] = formPart{name: "file", filename: "../../../etc/passwd.pdf", contentType: "image/png", data: pdfBytes(64)}
	if rec := doUpload(t, s, subj.ID, parts...); rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	for _, name := range storedObjects(t, dir) {
		if !canonicalUUID.MatchString(name) {
			t.Fatalf("object stored under a client-derived name: %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "etc")); err == nil {
		t.Fatal("client filename created a path outside storage")
	}

	// Not a PDF, declared as application/pdf: refused.
	parts[3] = filePart("fake.pdf", []byte("<html>not a pdf</html>"))
	if rec := doUpload(t, s, subj.ID, parts...); rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_pdf" {
		t.Fatalf("non-PDF: expected 400 invalid_pdf, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAdminFiles_UploadValidation(t *testing.T) {
	s, dir := newFileAdminServer(t, 1<<20)
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	pdf := pdfBytes(200)
	long := strings.Repeat("ع", 201)

	cases := []struct {
		name  string
		parts []formPart
		code  string
	}{
		{"missing_kind", []formPart{field("title_ar", "مذكرة"), filePart("a.pdf", pdf)}, "invalid_upload"},
		{"missing_title_ar", []formPart{field("kind", "book"), filePart("a.pdf", pdf)}, "invalid_upload"},
		{"bad_kind", []formPart{field("kind", "video"), field("title_ar", "مذكرة"), filePart("a.pdf", pdf)}, "invalid_kind"},
		{"blank_title_ar", []formPart{field("kind", "book"), field("title_ar", "   "), filePart("a.pdf", pdf)}, "invalid_title"},
		{"long_title_ar", []formPart{field("kind", "book"), field("title_ar", long), filePart("a.pdf", pdf)}, "invalid_title"},
		{"long_title_en", []formPart{field("kind", "book"), field("title_ar", "كتاب"), field("title_en", long), filePart("a.pdf", pdf)}, "invalid_title"},
		{"unknown_field", []formPart{field("kind", "book"), field("title_ar", "كتاب"), field("storage_key", "x"), filePart("a.pdf", pdf)}, "invalid_upload"},
		{"duplicate_field", []formPart{field("kind", "book"), field("kind", "note"), field("title_ar", "كتاب"), filePart("a.pdf", pdf)}, "invalid_upload"},
		{"file_before_fields", []formPart{filePart("a.pdf", pdf), field("kind", "book"), field("title_ar", "كتاب")}, "invalid_upload"},
		{"no_file", []formPart{field("kind", "book"), field("title_ar", "كتاب")}, "invalid_upload"},
		{"file_part_without_filename", []formPart{field("kind", "book"), field("title_ar", "كتاب"), {name: "file", data: pdf}}, "invalid_upload"},
		{"two_files", []formPart{field("kind", "book"), field("title_ar", "كتاب"), filePart("a.pdf", pdf), filePart("b.pdf", pdf)}, "invalid_upload"},
		{"field_after_file", []formPart{field("kind", "book"), field("title_ar", "كتاب"), filePart("a.pdf", pdf), field("title_en", "x")}, "invalid_upload"},
		{"empty_file", []formPart{field("kind", "book"), field("title_ar", "كتاب"), filePart("a.pdf", nil)}, "invalid_pdf"},
		{"four_byte_file", []formPart{field("kind", "book"), field("title_ar", "كتاب"), filePart("a.pdf", []byte("%PDF"))}, "invalid_pdf"},
		{"lowercase_magic", []formPart{field("kind", "book"), field("title_ar", "كتاب"), filePart("a.pdf", []byte("%pdf-1.7 body"))}, "invalid_pdf"},
		{"oversized_text_field", []formPart{field("kind", "book"), field("title_ar", strings.Repeat("a", 5000)), filePart("a.pdf", pdf)}, "invalid_upload"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doUpload(t, s, subj.ID, tc.parts...)
			if rec.Code != http.StatusBadRequest || adminCode(t, rec) != tc.code {
				t.Fatalf("expected 400 %s, got %d (%s)", tc.code, rec.Code, rec.Body.String())
			}
		})
	}
	if objs := storedObjects(t, dir); len(objs) != 0 {
		t.Fatalf("refused uploads left objects on disk: %v", objs)
	}
	files, _ := s.Store.ListFilesBySubject(context.Background(), subj.ID)
	if len(files) != 0 {
		t.Fatalf("refused uploads left rows: %d", len(files))
	}

	t.Run("not_multipart", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/files", map[string]any{"kind": "book"})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_upload" {
			t.Fatalf("JSON body: expected 400 invalid_upload, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown_subject_404", func(t *testing.T) {
		rec := doUpload(t, s, "no-such-subject", validUploadParts(pdf)...)
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
			t.Fatalf("expected 404 subject_not_found, got %d (%s)", rec.Code, rec.Body.String())
		}
		lrec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects/no-such-subject/files", nil)
		if lrec.Code != http.StatusNotFound || adminCode(t, lrec) != "subject_not_found" {
			t.Fatalf("list unknown subject: expected 404, got %d", lrec.Code)
		}
	})

	t.Run("bad_ids_400", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/"+subj.ID+"/files/bad%20id", nil)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_file_id" {
			t.Fatalf("bad file id: expected 400 invalid_file_id, got %d (%s)", rec.Code, rec.Body.String())
		}
		rec = doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects/bad%20id/files", nil)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_subject_id" {
			t.Fatalf("bad subject id: expected 400 invalid_subject_id, got %d", rec.Code)
		}
	})

	t.Run("wrong_methods_405", func(t *testing.T) {
		if rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/subjects/"+subj.ID+"/files", nil); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("PUT files: expected 405, got %d", rec.Code)
		}
		if rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects/"+subj.ID+"/files/f-1", nil); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET file: expected 405, got %d", rec.Code)
		}
	})
}

func TestAdminFiles_SizeCap(t *testing.T) {
	const maxPDF = 4096
	s, dir := newFileAdminServer(t, maxPDF)
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))

	t.Run("exactly_max_accepted", func(t *testing.T) {
		dto := uploadOK(t, s, subj.ID, pdfBytes(maxPDF))
		if dto.SizeBytes != maxPDF {
			t.Fatalf("size = %d, want %d", dto.SizeBytes, maxPDF)
		}
	})

	t.Run("one_byte_over_413", func(t *testing.T) {
		before := len(storedObjects(t, dir))
		rec := doUpload(t, s, subj.ID, validUploadParts(pdfBytes(maxPDF+1))...)
		if rec.Code != http.StatusRequestEntityTooLarge || adminCode(t, rec) != "file_too_large" {
			t.Fatalf("expected 413 file_too_large, got %d (%s)", rec.Code, rec.Body.String())
		}
		if len(storedObjects(t, dir)) != before {
			t.Fatal("oversized upload left an object")
		}
	})

	t.Run("body_over_form_cap_413", func(t *testing.T) {
		rec := doUpload(t, s, subj.ID, validUploadParts(pdfBytes(maxPDF+int(uploadFormOverhead)+10))...)
		if rec.Code != http.StatusRequestEntityTooLarge || adminCode(t, rec) != "file_too_large" {
			t.Fatalf("expected 413 file_too_large, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("declared_length_over_cap_413_before_reading", func(t *testing.T) {
		body, ct := buildMultipart(t, validUploadParts(pdfBytes(10))...)
		req := httptest.NewRequest(http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/files", body)
		req.Header.Set("Content-Type", ct)
		req.Header.Set("X-Internal-Token", "test-internal-token")
		req.Header.Set("X-Admin-Token", "test-admin-token")
		req.ContentLength = maxPDF + uploadFormOverhead + 1
		rec := httptest.NewRecorder()
		s.AdminHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("other_admin_routes_keep_1MiB", func(t *testing.T) {
		huge := baseSubjectBody("bachelor-y1")
		huge["description_ar"] = strings.Repeat("a", 2<<20)
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", huge)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("2 MiB JSON on a non-upload route: expected 400, got %d", rec.Code)
		}
	})
}

func TestAdminFiles_FailureCleanup(t *testing.T) {
	t.Run("insert_failure_deletes_object_503", func(t *testing.T) {
		s, dir := newFileAdminServer(t, 1<<20)
		subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
		s.Store = &failingCreateStore{MemoryStore: s.Store.(*store.MemoryStore)}
		rec := doUpload(t, s, subj.ID, validUploadParts(pdfBytes(300))...)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d (%s)", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "insert failed") {
			t.Fatal("503 body leaks internal detail")
		}
		if objs := storedObjects(t, dir); len(objs) != 0 {
			t.Fatalf("object left behind after failed insert: %v", objs)
		}
	})

	t.Run("no_storage_503", func(t *testing.T) {
		s, _ := newFileAdminServer(t, 1<<20)
		subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
		s.Files = nil
		if rec := doUpload(t, s, subj.ID, validUploadParts(pdfBytes(10))...); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("upload without storage: expected 503, got %d", rec.Code)
		}
		if rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/subjects/"+subj.ID+"/files/f-1", nil); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("delete without storage: expected 503, got %d", rec.Code)
		}
	})
}

func TestIsAdminUploadRoute(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, "/internal/admin/subjects/s1/files", true},
		{http.MethodPost, "/internal/admin/subjects/s1/files/", true},
		{http.MethodGet, "/internal/admin/subjects/s1/files", false},
		{http.MethodDelete, "/internal/admin/subjects/s1/files/f1", false},
		{http.MethodPost, "/internal/admin/subjects/s1/files/f1", false},
		{http.MethodPost, "/internal/admin/subjects//files", false},
		{http.MethodPost, "/internal/admin/subjects/s1/videos", false},
		{http.MethodPost, "/internal/admin/subjects", false},
		{http.MethodPost, "/internal/admin/settings/files", false},
		{http.MethodPost, "/x/internal/admin/subjects/s1/files", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if got := isAdminUploadRoute(req); got != tc.want {
			t.Errorf("%s %s: got %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
