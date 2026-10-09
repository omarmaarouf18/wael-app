package proxy

import (
	"bytes"
	"crypto/sha256"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// filesRoutes joins routes() so the generic guarantees hold for list/delete.
func filesRoutes() []route {
	return []route{
		{
			name: "files.list", handler: func(p *Proxy) http.HandlerFunc { return p.FilesList },
			method: http.MethodGet, target: "/api/files?subject_id=" + testID + "&page=9",
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/subjects/" + testID + "/files",
			upstreamStatus: 200, upstreamReply: `{"files":[]}`,
		},
		{
			name: "files.delete", handler: func(p *Proxy) http.HandlerFunc { return p.FilesDelete },
			method: http.MethodPost, target: "/api/files/delete",
			body:           `{"subject_id":"` + testID + `","id":"f-1"}`,
			upstreamMethod: http.MethodDelete, upstreamPath: "/internal/admin/subjects/" + testID + "/files/f-1",
			upstreamStatus: 200, upstreamReply: `{"status":"ok"}`,
		},
	}
}

// uploadUpstream is a fake academy that parses the multipart body as a
// stream and records what it saw (file bytes hashed, not kept).
type uploadUpstream struct {
	srv    *httptest.Server
	mu     sync.Mutex
	calls  int
	fields map[string]string
	file   struct {
		name, contentType string
		size              int64
		sum               [32]byte
	}
	header http.Header
	parts  []string
	status int
	reply  string
}

func newUploadUpstream(t *testing.T, status int, reply string) *uploadUpstream {
	t.Helper()
	u := &uploadUpstream{status: status, reply: reply}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.calls++
		u.header = r.Header.Clone()
		u.fields = map[string]string{}
		u.parts = nil
		u.mu.Unlock()
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/files") {
			http.NotFound(w, r)
			return
		}
		mr, err := r.MultipartReader()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad","code":"invalid_upload"}`))
			return
		}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"bad","code":"invalid_upload"}`))
				return
			}
			u.mu.Lock()
			u.parts = append(u.parts, part.FormName())
			u.mu.Unlock()
			if part.FormName() == "file" {
				h := sha256.New()
				n, err := io.Copy(h, part)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"bad","code":"invalid_upload"}`))
					return
				}
				u.mu.Lock()
				u.file.name, u.file.contentType, u.file.size = part.FileName(), part.Header.Get("Content-Type"), n
				copy(u.file.sum[:], h.Sum(nil))
				u.mu.Unlock()
				continue
			}
			b, _ := io.ReadAll(part)
			u.mu.Lock()
			u.fields[part.FormName()] = string(b)
			u.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(u.status)
		_, _ = w.Write([]byte(u.reply))
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *uploadUpstream) callCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

func newUploadProxy(t *testing.T, academyURL string, maxPDF int64) *Proxy {
	t.Helper()
	p, err := New(Options{
		InternalToken: testInternal,
		AuthURL:       academyURL,
		AcademyURL:    academyURL,
		Timeout:       2 * time.Second,
		UploadTimeout: 10 * time.Second,
		MaxPDFBytes:   maxPDF,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

type mpart struct {
	name, filename, ctype string
	data                  []byte
}

func multipartBody(t *testing.T, parts ...mpart) (*bytes.Buffer, string) {
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
		if p.ctype != "" {
			h.Set("Content-Type", p.ctype)
		}
		w, _ := mw.CreatePart(h)
		_, _ = w.Write(p.data)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func pdf(n int) []byte {
	b := bytes.Repeat([]byte{'z'}, n)
	copy(b, "%PDF-1.4\n")
	return b
}

func goodParts(file []byte) []mpart {
	return []mpart{
		{name: "kind", data: []byte("book")},
		{name: "title_ar", data: []byte("كتاب القانون المدني")},
		{name: "title_en", data: []byte("Civil law")},
		{name: "file", filename: "../../../etc/evil.pdf", ctype: "image/png", data: file},
	}
}

func doUploadReq(p *Proxy, subjectID string, body io.Reader, ctype string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/files/upload?subject_id="+subjectID, body)
	r.RemoteAddr = "203.0.113.7:51000"
	r.Header.Set("Content-Type", ctype)
	for k, v := range headers {
		r.Header[http.CanonicalHeaderKey(k)] = []string{v}
	}
	w := httptest.NewRecorder()
	p.FilesUpload(w, r)
	return w
}

func TestFilesUpload_StreamsFreshBodyToAcademy(t *testing.T) {
	up := newUploadUpstream(t, http.StatusCreated, `{"id":"f-1","kind":"book"}`)
	p := newUploadProxy(t, up.srv.URL, 4<<20)
	file := pdf(3 << 20) // above the 1 MiB cap of every other route
	body, ct := multipartBody(t, goodParts(file)...)

	w := doUploadReq(p, testID, body, ct, withToken(InternalTokenHeader, "attacker", ClientIPHeader, "6.6.6.6"))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"id":"f-1"`) {
		t.Fatalf("upstream body not relayed: %s", w.Body.String())
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if up.calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", up.calls)
	}
	if got := strings.Join(up.parts, ","); got != "kind,title_ar,title_en,file" {
		t.Fatalf("upstream parts = %s", got)
	}
	if up.fields["kind"] != "book" || up.fields["title_ar"] != "كتاب القانون المدني" || up.fields["title_en"] != "Civil law" {
		t.Fatalf("upstream fields = %v", up.fields)
	}
	if up.file.name != "upload.pdf" || up.file.contentType != "application/pdf" {
		t.Fatalf("client file name/type forwarded: %q %q", up.file.name, up.file.contentType)
	}
	if up.file.size != int64(len(file)) || up.file.sum != sha256.Sum256(file) {
		t.Fatalf("file bytes differ upstream (%d vs %d)", up.file.size, len(file))
	}
	if got := up.header.Get(InternalTokenHeader); got != testInternal {
		t.Fatalf("X-Internal-Token upstream = %q, want the console's own", got)
	}
	if got := up.header.Get(ClientIPHeader); got != "203.0.113.7" {
		t.Fatalf("X-Admin-Client-IP upstream = %q, want the peer address", got)
	}
	if got := up.header.Get(AdminTokenHeader); got != testToken {
		t.Fatalf("admin token not forwarded: %q", got)
	}
	if mt, _, _ := mime.ParseMediaType(up.header.Get("Content-Type")); mt != "multipart/form-data" {
		t.Fatalf("upstream content type = %q", up.header.Get("Content-Type"))
	}
}

func TestFilesUpload_LocalRefusalsMakeNoUpstreamCall(t *testing.T) {
	up := newUploadUpstream(t, http.StatusCreated, `{}`)
	p := newUploadProxy(t, up.srv.URL, 4096)
	good, ct := multipartBody(t, goodParts(pdf(100))...)
	goodBytes := good.Bytes()

	t.Run("no_token_401", func(t *testing.T) {
		w := doUploadReq(p, testID, bytes.NewReader(goodBytes), ct, nil)
		assertSafeError(t, w, http.StatusUnauthorized, "unauthorized")
	})
	t.Run("get_405", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/files/upload?subject_id="+testID, nil)
		r.Header.Set(AdminTokenHeader, testToken)
		w := httptest.NewRecorder()
		p.FilesUpload(w, r)
		assertSafeError(t, w, http.StatusMethodNotAllowed, "method_not_allowed")
	})
	t.Run("bad_subject_id_400", func(t *testing.T) {
		for _, id := range []string{"", "a/b", "..", "x%20y", strings.Repeat("a", 101)} {
			w := doUploadReq(p, id, bytes.NewReader(goodBytes), ct, withToken())
			assertSafeError(t, w, http.StatusBadRequest, "bad_request")
		}
	})
	t.Run("not_multipart_400", func(t *testing.T) {
		w := doUploadReq(p, testID, strings.NewReader(`{"kind":"book"}`), "application/json", withToken())
		assertSafeError(t, w, http.StatusBadRequest, "bad_request")
	})
	t.Run("declared_length_over_cap_413", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/files/upload?subject_id="+testID, bytes.NewReader(goodBytes))
		r.Header.Set("Content-Type", ct)
		r.Header.Set(AdminTokenHeader, testToken)
		r.ContentLength = p.MaxUploadBodyBytes() + 1
		w := httptest.NewRecorder()
		p.FilesUpload(w, r)
		assertSafeError(t, w, http.StatusRequestEntityTooLarge, "file_too_large")
	})
	bad := []struct {
		name  string
		parts []mpart
		code  string
	}{
		{"bad_kind", []mpart{{name: "kind", data: []byte("video")}, {name: "title_ar", data: []byte("x")}, {name: "file", filename: "a.pdf", data: pdf(50)}}, "bad_request"},
		{"missing_title", []mpart{{name: "kind", data: []byte("book")}, {name: "file", filename: "a.pdf", data: pdf(50)}}, "bad_request"},
		{"long_title", []mpart{{name: "kind", data: []byte("book")}, {name: "title_ar", data: []byte(strings.Repeat("ع", 201))}, {name: "file", filename: "a.pdf", data: pdf(50)}}, "bad_request"},
		{"unknown_field", []mpart{{name: "kind", data: []byte("book")}, {name: "title_ar", data: []byte("x")}, {name: "storage_key", data: []byte("k")}, {name: "file", filename: "a.pdf", data: pdf(50)}}, "bad_request"},
		{"file_first", []mpart{{name: "file", filename: "a.pdf", data: pdf(50)}, {name: "kind", data: []byte("book")}, {name: "title_ar", data: []byte("x")}}, "bad_request"},
		{"no_file", []mpart{{name: "kind", data: []byte("book")}, {name: "title_ar", data: []byte("x")}}, "bad_request"},
		{"not_pdf", []mpart{{name: "kind", data: []byte("book")}, {name: "title_ar", data: []byte("x")}, {name: "file", filename: "a.pdf", ctype: "application/pdf", data: []byte("MZ\x90\x00 not a pdf")}}, "invalid_pdf"},
		{"empty_file", []mpart{{name: "kind", data: []byte("book")}, {name: "title_ar", data: []byte("x")}, {name: "file", filename: "a.pdf"}}, "invalid_pdf"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			b, ct := multipartBody(t, tc.parts...)
			w := doUploadReq(p, testID, b, ct, withToken())
			assertSafeError(t, w, http.StatusBadRequest, tc.code)
		})
	}
	if n := up.callCount(); n != 0 {
		t.Fatalf("local refusals reached the upstream %d times", n)
	}
}

func TestFilesUpload_SizeCap(t *testing.T) {
	up := newUploadUpstream(t, http.StatusCreated, `{"id":"f-1"}`)
	p := newUploadProxy(t, up.srv.URL, 8192)

	t.Run("exactly_max_ok", func(t *testing.T) {
		b, ct := multipartBody(t, goodParts(pdf(8192))...)
		if w := doUploadReq(p, testID, b, ct, withToken()); w.Code != http.StatusCreated {
			t.Fatalf("status = %d (%s)", w.Code, w.Body.String())
		}
	})
	t.Run("one_over_413", func(t *testing.T) {
		b, ct := multipartBody(t, goodParts(pdf(8193))...)
		w := doUploadReq(p, testID, b, ct, withToken())
		assertSafeError(t, w, http.StatusRequestEntityTooLarge, "file_too_large")
	})
	t.Run("chunked_body_over_form_cap_413", func(t *testing.T) {
		b, ct := multipartBody(t, goodParts(pdf(8192+int(uploadFormOverhead)+100))...)
		r := httptest.NewRequest(http.MethodPost, "/api/files/upload?subject_id="+testID, io.NopCloser(b))
		r.ContentLength = -1
		r.Header.Set("Content-Type", ct)
		r.Header.Set(AdminTokenHeader, testToken)
		w := httptest.NewRecorder()
		p.FilesUpload(w, r)
		assertSafeError(t, w, http.StatusRequestEntityTooLarge, "file_too_large")
	})
	t.Run("part_after_file_400", func(t *testing.T) {
		parts := append(goodParts(pdf(100)), mpart{name: "file", filename: "b.pdf", data: pdf(100)})
		b, ct := multipartBody(t, parts...)
		w := doUploadReq(p, testID, b, ct, withToken())
		assertSafeError(t, w, http.StatusBadRequest, "bad_request")
	})
}

// TestFilesCap_PerRoute: every JSON route keeps the 1 MiB request cap; only
// the upload route takes more.
func TestFilesCap_PerRoute(t *testing.T) {
	up := newUpstream(t, 200, `{}`)
	p := newProxy(t, up)
	big := `{"id":"` + testID + `","reason":"` + strings.Repeat("a", (1<<20)+10) + `"}`
	for _, rt := range routes() {
		if rt.method != http.MethodPost {
			continue
		}
		w := do(rt.handler(p), http.MethodPost, rt.target, big, withToken())
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: a body over 1 MiB answered %d, want 400", rt.name, w.Code)
		}
	}
	if n := len(up.calls()); n != 0 {
		t.Fatalf("oversized JSON bodies reached the upstream %d times", n)
	}

	upl := newUploadUpstream(t, http.StatusCreated, `{"id":"f-1"}`)
	pu := newUploadProxy(t, upl.srv.URL, 4<<20)
	b, ct := multipartBody(t, goodParts(pdf((1<<20)+10))...)
	if w := doUploadReq(pu, testID, b, ct, withToken()); w.Code != http.StatusCreated {
		t.Fatalf("upload over 1 MiB: status = %d (%s)", w.Code, w.Body.String())
	}
}

func TestFilesUpload_UpstreamAnswers(t *testing.T) {
	t.Run("4xx_relayed", func(t *testing.T) {
		up := newUploadUpstream(t, http.StatusNotFound, `{"error":"subject not found","code":"subject_not_found"}`)
		p := newUploadProxy(t, up.srv.URL, 1<<20)
		b, ct := multipartBody(t, goodParts(pdf(500))...)
		w := doUploadReq(p, testID, b, ct, withToken())
		assertSafeError(t, w, http.StatusNotFound, "subject_not_found")
	})
	t.Run("413_relayed", func(t *testing.T) {
		up := newUploadUpstream(t, http.StatusRequestEntityTooLarge, `{"error":"file too large","code":"file_too_large"}`)
		p := newUploadProxy(t, up.srv.URL, 1<<20)
		b, ct := multipartBody(t, goodParts(pdf(500))...)
		w := doUploadReq(p, testID, b, ct, withToken())
		assertSafeError(t, w, http.StatusRequestEntityTooLarge, "file_too_large")
	})
	t.Run("5xx_is_503", func(t *testing.T) {
		up := newUploadUpstream(t, http.StatusInternalServerError, `{"error":"boom","code":"internal"}`)
		p := newUploadProxy(t, up.srv.URL, 1<<20)
		b, ct := multipartBody(t, goodParts(pdf(500))...)
		w := doUploadReq(p, testID, b, ct, withToken())
		assertSafeError(t, w, http.StatusServiceUnavailable, "unavailable")
		if strings.Contains(w.Body.String(), "boom") {
			t.Fatal("upstream text leaked")
		}
	})
	t.Run("upstream_down_503", func(t *testing.T) {
		up := newUploadUpstream(t, http.StatusCreated, `{}`)
		url := up.srv.URL
		up.srv.Close()
		p := newUploadProxy(t, url, 1<<20)
		b, ct := multipartBody(t, goodParts(pdf(500))...)
		w := doUploadReq(p, testID, b, ct, withToken())
		assertSafeError(t, w, http.StatusServiceUnavailable, "unavailable")
	})
	t.Run("early_401_does_not_hang", func(t *testing.T) {
		// The academy refuses the token before reading the body.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized","code":"unauthorized"}`))
		}))
		defer srv.Close()
		p := newUploadProxy(t, srv.URL, 4<<20)
		b, ct := multipartBody(t, goodParts(pdf(3<<20))...)
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() { done <- doUploadReq(p, testID, b, ct, withToken()) }()
		select {
		case w := <-done:
			assertSafeError(t, w, http.StatusUnauthorized, "unauthorized")
		case <-time.After(5 * time.Second):
			t.Fatal("upload hung after an early upstream answer")
		}
	})
}

// TestFilesUpload_StreamsWithoutBuffering: a 20 MiB upload allocates far less
// than the file in the console (the fake academy hashes the stream too).
func TestFilesUpload_StreamsWithoutBuffering(t *testing.T) {
	up := newUploadUpstream(t, http.StatusCreated, `{"id":"f-1"}`)
	p := newUploadProxy(t, up.srv.URL, 20<<20)
	file := pdf(20 << 20)
	body, ct := multipartBody(t, goodParts(file)...)
	raw := body.Bytes()

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	w := doUploadReq(p, testID, bytes.NewReader(raw), ct, withToken())
	runtime.ReadMemStats(&after)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", w.Code, w.Body.String())
	}
	alloc := after.TotalAlloc - before.TotalAlloc
	t.Logf("20 MiB upload through the console: %d KiB allocated (console and fake academy together)", alloc>>10)
	if alloc > 8<<20 {
		t.Fatalf("console allocated %d MiB for a 20 MiB upload, want streaming", alloc>>20)
	}
	if up.file.size != int64(len(file)) || up.file.sum != sha256.Sum256(file) {
		t.Fatal("streamed file differs upstream")
	}
}

// slowReader dribbles data with pauses, like a slow admin link.
type slowReader struct {
	data  []byte
	chunk int
	pause time.Duration
}

func (s *slowReader) Read(b []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(s.pause)
	n := s.chunk
	if n > len(s.data) {
		n = len(s.data)
	}
	if n > len(b) {
		n = len(b)
	}
	copy(b, s.data[:n])
	s.data = s.data[n:]
	return n, nil
}

// TestFilesUpload_OutlivesServerReadTimeout: the server-wide ReadTimeout stays
// short, yet a slow upload completes because only this route extends its own
// deadlines.
func TestFilesUpload_OutlivesServerReadTimeout(t *testing.T) {
	up := newUploadUpstream(t, http.StatusCreated, `{"id":"f-1"}`)
	p := newUploadProxy(t, up.srv.URL, 1<<20)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(p.FilesUpload))
	srv.Config.ReadTimeout = 300 * time.Millisecond
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Start()
	defer srv.Close()

	body, ct := multipartBody(t, goodParts(pdf(64<<10))...)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/files/upload?subject_id="+testID,
		&slowReader{data: body.Bytes(), chunk: 8 << 10, pause: 120 * time.Millisecond})
	req.Header.Set("Content-Type", ct)
	req.Header.Set(AdminTokenHeader, testToken)
	start := time.Now()
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("slow upload failed after %v: %v", time.Since(start), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if time.Since(start) < 600*time.Millisecond {
		t.Fatalf("upload took %v; the test needs it slower than the 300 ms ReadTimeout", time.Since(start))
	}
}
