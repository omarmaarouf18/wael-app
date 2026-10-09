package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/shared/infra/storage"
)

// spyStorage counts every call that touches a stored object.
type spyStorage struct {
	storage.Storage
	touched atomic.Int64
}

func (s *spyStorage) Size(key string) (int64, error) {
	s.touched.Add(1)
	return s.Storage.Size(key)
}

func (s *spyStorage) OpenFile(key string) (io.ReadCloser, error) {
	s.touched.Add(1)
	return s.Storage.OpenFile(key)
}

type downloadFixture struct {
	s       *Server
	h       http.Handler
	spy     *spyStorage
	dir     string
	subj    *models.Subject
	other   *models.Subject
	file    *models.SubjectFile
	content []byte
}

const (
	dlOwner   = "user-dl-owner"
	dlStudent = "user-dl-other"
)

func newSubjectForDownload(t *testing.T, s *Server, id string, status string, expires time.Time) *models.Subject {
	t.Helper()
	subj := &models.Subject{
		ID: id, LevelKey: "bachelor-y1", Term: "first", TitleAr: "مادة " + id, TitleEn: "Subject " + id,
		Status: status, AccessExpiresAt: expires, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.Store.CreateSubject(context.Background(), subj); err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	return subj
}

func storeFile(t *testing.T, s *Server, st storage.Storage, subjectID, fileID string, content []byte) *models.SubjectFile {
	t.Helper()
	key := "0f0f0f0f-0000-4000-8000-" + strings.Repeat("0", 12-len(fileID)) + fileID
	if err := st.Upload(context.Background(), key, bytes.NewReader(content), "application/pdf"); err != nil {
		t.Fatalf("storage upload: %v", err)
	}
	f := &models.SubjectFile{
		ID: fileID, SubjectID: subjectID, Kind: "note", TitleAr: "مذكرة الباب الأول", TitleEn: "Chapter 1: notes",
		SizeBytes: int64(len(content)), StorageKey: key, CreatedAt: time.Now(),
	}
	if err := s.Store.CreateFile(context.Background(), f); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	return f
}

func newDownloadFixture(t *testing.T) *downloadFixture {
	t.Helper()
	s := newTestServer(false)
	dir := t.TempDir()
	ls, err := storage.NewLocalStorage(dir, "", "test")
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(func() { _ = ls.Close() })
	spy := &spyStorage{Storage: ls}
	s.Files = spy
	s.MaxPDFBytes = 20 << 20

	subj := newSubjectForDownload(t, s, "subj-dl-1", models.StatusPublished, time.Now().Add(48*time.Hour))
	other := newSubjectForDownload(t, s, "subj-dl-2", models.StatusPublished, time.Now().Add(48*time.Hour))
	content := pdfBytes(70000)
	file := storeFile(t, s, ls, subj.ID, "a1", content)
	spy.touched.Store(0)

	if err := s.Store.Grant(context.Background(), &models.Entitlement{UserID: dlOwner, SubjectID: subj.ID}); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	return &downloadFixture{s: s, h: s.PublicHandler(), spy: spy, dir: dir, subj: subj, other: other, file: file, content: content}
}

func (f *downloadFixture) get(t *testing.T, path, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	if userID != "" {
		req.Header.Set("Authorization", "Bearer "+makeStudentToken(t, userID))
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func downloadPath(subjectID, fileID string) string {
	return "/academy/subjects/" + subjectID + "/files/" + fileID + "/download"
}

// assertRefused checks a refusal: status, no-store (on answers from the
// download handler itself; the auth and rate-limit middleware answer before
// it), no PDF bytes, and that the stored object was never touched.
func (f *downloadFixture) assertRefused(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("expected %d, got %d (%s)", status, rec.Code, rec.Body.String())
	}
	if status == http.StatusForbidden || status == http.StatusNotFound {
		if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
			t.Fatalf("Cache-Control = %q on %d, want private, no-store", cc, status)
		}
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("%PDF-")) || strings.Contains(rec.Body.String(), f.file.StorageKey) {
		t.Fatalf("refusal body leaks file content or storage key: %s", rec.Body.String())
	}
	if n := f.spy.touched.Load(); n != 0 {
		t.Fatalf("stored object touched %d times before the ownership decision", n)
	}
}

func TestDownloadFile_OwnedStreamsExactBytes(t *testing.T) {
	f := newDownloadFixture(t)
	rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), f.content) {
		t.Fatalf("body differs from the stored PDF (%d vs %d bytes)", rec.Body.Len(), len(f.content))
	}
	want := map[string]string{
		"Content-Type":           "application/pdf",
		"Content-Length":         strconv.Itoa(len(f.content)),
		"Cache-Control":          "private, no-store",
		"X-Content-Type-Options": "nosniff",
		"Content-Disposition":    `attachment; filename="Chapter-1-notes.pdf"; filename*=UTF-8''%D9%85%D8%B0%D9%83%D8%B1%D8%A9-%D8%A7%D9%84%D8%A8%D8%A7%D8%A8-%D8%A7%D9%84%D8%A3%D9%88%D9%84.pdf`,
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if strings.Contains(rec.Header().Get("Content-Disposition"), f.file.StorageKey) {
		t.Fatal("Content-Disposition carries the storage key")
	}
}

func TestDownloadFile_Refusals(t *testing.T) {
	t.Run("not_owned_403", func(t *testing.T) {
		f := newDownloadFixture(t)
		f.assertRefused(t, f.get(t, downloadPath(f.subj.ID, f.file.ID), dlStudent), http.StatusForbidden)
	})

	t.Run("revoked_403", func(t *testing.T) {
		f := newDownloadFixture(t)
		ent, _ := f.s.Store.GetActiveEntitlement(context.Background(), dlOwner, f.subj.ID)
		if ent == nil {
			t.Fatal("fixture entitlement missing")
		}
		if _, err := f.s.Store.RevokeEntitlement(context.Background(), ent.ID, "adm-1", "test", time.Now()); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		f.assertRefused(t, f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner), http.StatusForbidden)
	})

	t.Run("expired_entitlement_403_even_after_subject_date_moves", func(t *testing.T) {
		f := newDownloadFixture(t)
		short := newSubjectForDownload(t, f.s, "subj-dl-short", models.StatusPublished, time.Now().Add(40*time.Millisecond))
		file := storeFile(t, f.s, f.spy.Storage, short.ID, "b2", pdfBytes(100))
		if err := f.s.Store.Grant(context.Background(), &models.Entitlement{UserID: dlOwner, SubjectID: short.ID}); err != nil {
			t.Fatalf("Grant: %v", err)
		}
		time.Sleep(80 * time.Millisecond)
		short.AccessExpiresAt = time.Now().Add(48 * time.Hour) // D21: a later date does not revive it
		_ = f.s.Store.UpdateSubject(context.Background(), short)
		f.spy.touched.Store(0)
		f.assertRefused(t, f.get(t, downloadPath(short.ID, file.ID), dlOwner), http.StatusForbidden)
	})

	t.Run("owned_but_subject_access_date_passed_403", func(t *testing.T) {
		f := newDownloadFixture(t)
		f.subj.AccessExpiresAt = time.Now().Add(-time.Minute)
		_ = f.s.Store.UpdateSubject(context.Background(), f.subj)
		f.assertRefused(t, f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner), http.StatusForbidden)
	})

	t.Run("file_of_another_subject_404", func(t *testing.T) {
		f := newDownloadFixture(t)
		// The owner owns subj-dl-1; asking for its file under subj-dl-2 (owned
		// too) must not serve it.
		if err := f.s.Store.Grant(context.Background(), &models.Entitlement{UserID: dlOwner, SubjectID: f.other.ID}); err != nil {
			t.Fatalf("Grant other: %v", err)
		}
		f.assertRefused(t, f.get(t, downloadPath(f.other.ID, f.file.ID), dlOwner), http.StatusNotFound)
	})

	t.Run("unknown_file_404", func(t *testing.T) {
		f := newDownloadFixture(t)
		f.assertRefused(t, f.get(t, downloadPath(f.subj.ID, "nope"), dlOwner), http.StatusNotFound)
	})

	t.Run("unknown_subject_404", func(t *testing.T) {
		f := newDownloadFixture(t)
		f.assertRefused(t, f.get(t, downloadPath("subj-missing", f.file.ID), dlOwner), http.StatusNotFound)
	})

	t.Run("malformed_ids_404", func(t *testing.T) {
		f := newDownloadFixture(t)
		for _, p := range []string{
			"/academy/subjects/subj-dl-1/files/a%2e%2e/download",
			"/academy/subjects/subj%20dl/files/a1/download",
			"/academy/subjects/subj-dl-1/files/a1%2Fx/download",
		} {
			rec := f.get(t, p, dlOwner)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s: expected 404, got %d", p, rec.Code)
			}
		}
		// An empty segment is cleaned by ServeMux (redirect), never served.
		if rec := f.get(t, "/academy/subjects/subj-dl-1/files//download", dlOwner); rec.Code == http.StatusOK {
			t.Fatal("empty file id served")
		}
		if f.spy.touched.Load() != 0 {
			t.Fatal("storage touched for a malformed id")
		}
	})

	t.Run("unpublished_subject_non_owner_404_owner_200", func(t *testing.T) {
		f := newDownloadFixture(t)
		f.subj.Status = models.StatusDraft
		_ = f.s.Store.UpdateSubject(context.Background(), f.subj)
		f.assertRefused(t, f.get(t, downloadPath(f.subj.ID, f.file.ID), dlStudent), http.StatusNotFound)
		// ADR-0012 decision 9: owners keep access after unpublish.
		if rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner); rec.Code != http.StatusOK {
			t.Fatalf("owner of unpublished subject: expected 200, got %d", rec.Code)
		}
	})

	t.Run("auth_401", func(t *testing.T) {
		f := newDownloadFixture(t)
		f.assertRefused(t, f.get(t, downloadPath(f.subj.ID, f.file.ID), ""), http.StatusUnauthorized)
		req := httptest.NewRequest(http.MethodGet, downloadPath(f.subj.ID, f.file.ID), nil)
		req.Header.Set("Authorization", "Bearer "+makeStudentToken(t, dlOwner))
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("missing gateway secret: expected 401, got %d", rec.Code)
		}
	})

	t.Run("post_405", func(t *testing.T) {
		f := newDownloadFixture(t)
		req := httptest.NewRequest(http.MethodPost, downloadPath(f.subj.ID, f.file.ID), nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+makeStudentToken(t, dlOwner))
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed || f.spy.touched.Load() != 0 {
			t.Fatalf("POST: expected 405 without touching storage, got %d", rec.Code)
		}
	})
}

func TestDownloadFile_RateLimit429(t *testing.T) {
	f := newDownloadFixture(t)
	_, tl := setupTestLimiter(t, 100, 100, 2, 100)
	f.s.Limiter = tl
	for i := 0; i < 2; i++ {
		if rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner); rec.Code != http.StatusOK {
			t.Fatalf("download %d: expected 200, got %d", i+1, rec.Code)
		}
	}
	rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third download: expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
	// Per user: another student is not limited by the owner's downloads.
	if rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlStudent); rec.Code != http.StatusForbidden {
		t.Fatalf("other student: expected 403 (own quota), got %d", rec.Code)
	}
}

func TestDownloadFile_FailClosed503(t *testing.T) {
	t.Run("limiter_backend_down", func(t *testing.T) {
		f := newDownloadFixture(t)
		mr, tl := setupTestLimiter(t, 100, 100, 100, 100)
		f.s.Limiter = tl
		mr.Close()
		f.assertRefused(t, f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner), http.StatusServiceUnavailable)
	})

	t.Run("object_missing", func(t *testing.T) {
		f := newDownloadFixture(t)
		_ = os.Remove(filepath.Join(f.dir, f.file.StorageKey))
		rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner)
		assertSafe503(t, rec, f.file.StorageKey)
	})

	t.Run("object_tampered", func(t *testing.T) {
		f := newDownloadFixture(t)
		p := filepath.Join(f.dir, f.file.StorageKey)
		data, _ := os.ReadFile(p)
		data[len(data)-1] ^= 0xff
		_ = os.WriteFile(p, data, 0600)
		rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner)
		assertSafe503(t, rec, f.file.StorageKey)
	})

	t.Run("no_storage", func(t *testing.T) {
		f := newDownloadFixture(t)
		f.s.Files = nil
		rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner)
		assertSafe503(t, rec, f.file.StorageKey)
	})
}

func assertSafe503(t *testing.T, rec *httptest.ResponseRecorder, storageKey string) {
	t.Helper()
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, storageKey) || strings.Contains(body, "storage") || strings.Contains(body, "decrypt") || strings.Contains(body, "%PDF") {
		t.Fatalf("503 body carries internal detail: %s", body)
	}
	if rec.Header().Get("Content-Type") == "application/pdf" || rec.Header().Get("Content-Disposition") != "" {
		t.Fatal("503 sent PDF headers")
	}
}

// TestDownloadFile_20MBWithinGatewayBudget measures a MAX_PDF_BYTES (20 MiB)
// download through a real HTTP server: the gateway gives academy 2 s to start
// its response. The v1 storage format is one AES-GCM seal, so the whole file
// is read and authenticated before the first byte; this records how long that
// takes and how much it allocates.
func TestDownloadFile_20MBWithinGatewayBudget(t *testing.T) {
	f := newDownloadFixture(t)
	big := pdfBytes(20 << 20)
	file := storeFile(t, f.s, f.spy.Storage, f.subj.ID, "c3", big)

	srv := httptest.NewServer(f.h)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+downloadPath(f.subj.ID, file.ID), nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+makeStudentToken(t, dlOwner))

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	ttfb := time.Since(start)
	got, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	total := time.Since(start)
	runtime.ReadMemStats(&after)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, read error %v", resp.StatusCode, err)
	}
	if !bytes.Equal(got, big) {
		t.Fatal("20 MiB body differs")
	}
	if resp.ContentLength != int64(len(big)) {
		t.Fatalf("Content-Length = %d, want %d", resp.ContentLength, len(big))
	}
	t.Logf("20 MiB download: time to headers %v, total %v, allocated %d MiB (server and client together; the client copy is 20 MiB+)",
		ttfb, total, (after.TotalAlloc-before.TotalAlloc)>>20)
	if ttfb > 2*time.Second {
		t.Fatalf("time to first byte %v exceeds the gateway's 2 s budget", ttfb)
	}

	// Server side alone: the handler writing into a discarding writer.
	dreq := httptest.NewRequest(http.MethodGet, downloadPath(f.subj.ID, file.ID), nil)
	dreq.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	dreq.Header.Set("Authorization", "Bearer "+makeStudentToken(t, dlOwner))
	runtime.GC()
	runtime.ReadMemStats(&before)
	dw := &discardWriter{h: http.Header{}}
	f.h.ServeHTTP(dw, dreq)
	runtime.ReadMemStats(&after)
	if dw.status != http.StatusOK || dw.n != int64(len(big)) {
		t.Fatalf("server-side run: status %d, %d bytes", dw.status, dw.n)
	}
	allocMiB := (after.TotalAlloc - before.TotalAlloc) >> 20
	t.Logf("20 MiB download, server side only: allocated %d MiB per download", allocMiB)
	// One ciphertext buffer, decrypted in place: about the file size, not 2-3x.
	if allocMiB > 30 {
		t.Fatalf("server allocated %d MiB for a 20 MiB file, want about one copy", allocMiB)
	}
}

type discardWriter struct {
	h      http.Header
	status int
	n      int64
}

func (d *discardWriter) Header() http.Header { return d.h }
func (d *discardWriter) WriteHeader(code int) {
	if d.status == 0 {
		d.status = code
	}
}
func (d *discardWriter) Write(p []byte) (int, error) {
	if d.status == 0 {
		d.status = http.StatusOK
	}
	d.n += int64(len(p))
	return len(p), nil
}

func TestContentDisposition_Sanitized(t *testing.T) {
	cases := []struct {
		name string
		file models.SubjectFile
		want string
	}{
		{"english_and_arabic", models.SubjectFile{Kind: "book", TitleAr: "كتاب القانون", TitleEn: "Civil Law"},
			`attachment; filename="Civil-Law.pdf"; filename*=UTF-8''%D9%83%D8%AA%D8%A7%D8%A8-%D8%A7%D9%84%D9%82%D8%A7%D9%86%D9%88%D9%86.pdf`},
		{"arabic_only_falls_back_to_kind", models.SubjectFile{Kind: "note", TitleAr: "مذكرة"},
			`attachment; filename="note.pdf"; filename*=UTF-8''%D9%85%D8%B0%D9%83%D8%B1%D8%A9.pdf`},
		{"header_injection", models.SubjectFile{Kind: "book", TitleAr: "x\"; filename=evil.exe\r\nSet-Cookie: a=b", TitleEn: "a\"; filename=\"evil.exe\r\nX: y"},
			`attachment; filename="a-filename-evil-exe-X-y.pdf"; filename*=UTF-8''x-filename-evil-exe-Set-Cookie-a-b.pdf`},
		{"path_traversal", models.SubjectFile{Kind: "book", TitleAr: "../../etc/passwd", TitleEn: "..\\..\\boot.ini"},
			`attachment; filename="boot-ini.pdf"; filename*=UTF-8''etc-passwd.pdf`},
		{"nothing_usable", models.SubjectFile{Kind: "", TitleAr: "...", TitleEn: "///"},
			`attachment; filename="document.pdf"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := contentDisposition(&tc.file)
			if got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
			if strings.ContainsAny(got, "\r\n") {
				t.Fatal("header value carries CR/LF")
			}
		})
	}

	long := models.SubjectFile{Kind: "book", TitleAr: strings.Repeat("ب", 300), TitleEn: strings.Repeat("a", 300)}
	got := contentDisposition(&long)
	if !strings.Contains(got, `filename="`+strings.Repeat("a", 80)+`.pdf"`) {
		t.Fatalf("ASCII name not capped at 80: %s", got)
	}
	if strings.Count(got, "%D8%A8") != 80 {
		t.Fatalf("UTF-8 name not capped at 80 runes: %s", got)
	}
}
