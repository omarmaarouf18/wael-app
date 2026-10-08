package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

const (
	testInternal = "internal-secret-value"
	testToken    = "tok_ABCdef-123_456"
	testID       = "0f8fad5b-d9cb-469f-a165-70867728950e"
)

// logWriter redirects the standard logger and returns the restore function.
func logWriter(w io.Writer) func() {
	prevW, prevF := log.Writer(), log.Flags()
	log.SetOutput(w)
	log.SetFlags(0)
	return func() {
		log.SetOutput(prevW)
		log.SetFlags(prevF)
	}
}

// ---------------------------------------------------------------------------
// Fake upstream
// ---------------------------------------------------------------------------

type captured struct {
	Method, Path, RawQuery, Body string
	Header                       http.Header
}

type fakeUpstream struct {
	srv    *httptest.Server
	mu     sync.Mutex
	reqs   []captured
	status int
	body   string
	header map[string]string
	delay  time.Duration
}

func newUpstream(t *testing.T, status int, body string) *fakeUpstream {
	t.Helper()
	u := &fakeUpstream{status: status, body: body}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.reqs = append(u.reqs, captured{r.Method, r.URL.Path, r.URL.RawQuery, string(b), r.Header.Clone()})
		status, body, delay, hdr := u.status, u.body, u.delay, u.header
		u.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *fakeUpstream) calls() []captured {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]captured(nil), u.reqs...)
}

func (u *fakeUpstream) set(status int, body string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status, u.body = status, body
}

func newProxy(t *testing.T, up *fakeUpstream, trusted ...string) *Proxy {
	t.Helper()
	var prefixes []netip.Prefix
	for _, s := range trusted {
		prefixes = append(prefixes, netip.MustParsePrefix(s))
	}
	p, err := New(Options{
		InternalToken:  testInternal,
		AuthURL:        up.srv.URL,
		AcademyURL:     up.srv.URL,
		TrustedProxies: prefixes,
		Timeout:        2 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// do runs a handler directly. headers are set verbatim (no validation), so
// hostile values can be tested.
func do(h http.HandlerFunc, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, target, rd)
	r.RemoteAddr = "203.0.113.7:51000"
	for k, v := range headers {
		r.Header[http.CanonicalHeaderKey(k)] = []string{v}
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func withToken(extra ...string) map[string]string {
	h := map[string]string{AdminTokenHeader: testToken}
	for i := 0; i+1 < len(extra); i += 2 {
		h[extra[i]] = extra[i+1]
	}
	return h
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not JSON: %q (%v)", w.Body.String(), err)
	}
	return m
}

func assertSafeError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d (body %q)", w.Code, status, w.Body.String())
	}
	m := decode(t, w)
	if m["code"] != code {
		t.Fatalf("code = %v, want %q", m["code"], code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
}

// route describes one allowlisted route for the table tests.
type route struct {
	name           string
	handler        func(*Proxy) http.HandlerFunc
	method         string
	target         string
	body           string
	upstreamMethod string
	upstreamPath   string
	upstreamBody   string
	upstreamQuery  string
	upstreamStatus int
	upstreamReply  string
	// wantStatus is the console status for the happy path (upstreamStatus is
	// relayed; 0 means 200).
	wantStatus int
}

func (rt route) want() int {
	if rt.wantStatus != 0 {
		return rt.wantStatus
	}
	return http.StatusOK
}

func routes() []route {
	return append(append(append(baseRoutes(), catalogRoutes()...), requestsRoutes()...), settingsRoutes()...)
}

func baseRoutes() []route {
	return []route{
		{
			name: "whoami", handler: func(p *Proxy) http.HandlerFunc { return p.Whoami },
			method: http.MethodGet, target: "/api/whoami",
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/verify",
			upstreamStatus: 200, upstreamReply: `{"admin_id":"a1","name":"Wael"}`,
		},
		{
			name: "accounts", handler: func(p *Proxy) http.HandlerFunc { return p.Accounts },
			method: http.MethodGet, target: "/api/accounts?search=ali&status=active&page=2&limit=15",
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/accounts",
			upstreamQuery:  "limit=15&page=2&search=ali&status=active",
			upstreamStatus: 200, upstreamReply: `{"items":[],"total":0,"page":2,"limit":15}`,
		},
		{
			name: "suspend", handler: func(p *Proxy) http.HandlerFunc { return p.Suspend },
			method: http.MethodPost, target: "/api/accounts/suspend",
			body:           `{"id":"` + testID + `","reason":"spam"}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/accounts/" + testID + "/suspend",
			upstreamBody:   `{"reason":"spam"}`,
			upstreamStatus: 200, upstreamReply: `{"status":"ok"}`,
		},
		{
			name: "reactivate", handler: func(p *Proxy) http.HandlerFunc { return p.Reactivate },
			method: http.MethodPost, target: "/api/accounts/reactivate",
			body:           `{"id":"` + testID + `"}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/accounts/" + testID + "/reactivate",
			upstreamStatus: 200, upstreamReply: `{"status":"ok"}`,
		},
		{
			name: "delete", handler: func(p *Proxy) http.HandlerFunc { return p.Delete },
			method: http.MethodPost, target: "/api/accounts/delete",
			body:           `{"id":"` + testID + `","reason":"fraud"}`,
			upstreamMethod: http.MethodDelete, upstreamPath: "/internal/admin/accounts/" + testID,
			upstreamBody:   `{"reason":"fraud"}`,
			upstreamStatus: 200, upstreamReply: `{"status":"ok"}`,
		},
		{
			name: "audit", handler: func(p *Proxy) http.HandlerFunc { return p.Audit },
			method: http.MethodGet, target: "/api/audit?page=3&limit=20",
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/audit-log",
			upstreamQuery:  "limit=20&page=3",
			upstreamStatus: 200, upstreamReply: `{"items":[],"total":0,"page":3,"limit":20}`,
		},
	}
}

// ---------------------------------------------------------------------------
// Per-route table tests
// ---------------------------------------------------------------------------

func TestRoutes_MissingOrMalformedTokenIs401WithoutUpstreamCall(t *testing.T) {
	bad := map[string]string{
		"missing":     "",
		"space":       "a b",
		"newline":     "abc\ndef",
		"carriage":    "abc\rdef",
		"nul":         "abc\x00def",
		"tab":         "abc\tdef",
		"non-ascii":   "tok\u00e9",
		"leading-sp":  " abc",
		"too-long":    strings.Repeat("a", maxTokenLen+1),
		"del-char":    "abc\x7fdef",
		"only-spaces": "   ",
	}
	for _, rt := range routes() {
		for name, tok := range bad {
			t.Run(rt.name+"/"+name, func(t *testing.T) {
				up := newUpstream(t, 200, `{}`)
				p := newProxy(t, up)
				h := map[string]string{}
				if name != "missing" {
					h[AdminTokenHeader] = tok
				}
				w := do(rt.handler(p), rt.method, rt.target, rt.body, h)
				assertSafeError(t, w, http.StatusUnauthorized, "unauthorized")
				if n := len(up.calls()); n != 0 {
					t.Fatalf("upstream was called %d times", n)
				}
			})
		}
	}
}

func TestRoutes_WrongMethodIs405WithoutUpstreamCall(t *testing.T) {
	for _, rt := range routes() {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			if method == rt.method {
				continue
			}
			t.Run(rt.name+"/"+method, func(t *testing.T) {
				up := newUpstream(t, 200, `{}`)
				p := newProxy(t, up)
				w := do(rt.handler(p), method, rt.target, rt.body, withToken())
				assertSafeError(t, w, http.StatusMethodNotAllowed, "method_not_allowed")
				if w.Header().Get("Allow") != rt.method {
					t.Fatalf("Allow = %q, want %q", w.Header().Get("Allow"), rt.method)
				}
				if len(up.calls()) != 0 {
					t.Fatal("upstream was called")
				}
			})
		}
	}
}

func TestRoutes_HappyPathForwardsExactlyOneUpstreamRequest(t *testing.T) {
	for _, rt := range routes() {
		t.Run(rt.name, func(t *testing.T) {
			up := newUpstream(t, rt.upstreamStatus, rt.upstreamReply)
			p := newProxy(t, up)
			w := do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
			if w.Code != rt.want() {
				t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
			}
			calls := up.calls()
			if len(calls) != 1 {
				t.Fatalf("upstream calls = %d, want 1", len(calls))
			}
			c := calls[0]
			if c.Method != rt.upstreamMethod || c.Path != rt.upstreamPath || c.RawQuery != rt.upstreamQuery || c.Body != rt.upstreamBody {
				t.Fatalf("upstream got %s %s?%s body=%q; want %s %s?%s body=%q",
					c.Method, c.Path, c.RawQuery, c.Body, rt.upstreamMethod, rt.upstreamPath, rt.upstreamQuery, rt.upstreamBody)
			}
			if c.Header.Get(InternalTokenHeader) != testInternal {
				t.Fatalf("X-Internal-Token not set from config")
			}
			if c.Header.Get(AdminTokenHeader) != testToken {
				t.Fatalf("X-Admin-Token not forwarded")
			}
			if c.Header.Get(ClientIPHeader) != "203.0.113.7" {
				t.Fatalf("X-Admin-Client-IP = %q", c.Header.Get(ClientIPHeader))
			}
			if rt.upstreamBody != "" && c.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("Content-Type = %q", c.Header.Get("Content-Type"))
			}
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("response content-type = %q", w.Header().Get("Content-Type"))
			}
		})
	}
}

func TestRoutes_ClientHeadersAreStrippedNotForwarded(t *testing.T) {
	hostile := withToken(
		InternalTokenHeader, "attacker-internal",
		ClientIPHeader, "6.6.6.6",
		"X-Forwarded-For", "7.7.7.7",
		"X-Real-Ip", "8.8.8.8",
		"Cookie", "session=steal",
		"Authorization", "Bearer student-jwt",
		"X-Gateway-Secret", "guess",
		"X-Admin-Token-Extra", "x",
		"Origin", "https://evil.example",
	)
	for _, rt := range routes() {
		t.Run(rt.name, func(t *testing.T) {
			up := newUpstream(t, rt.upstreamStatus, rt.upstreamReply)
			p := newProxy(t, up)
			w := do(rt.handler(p), rt.method, rt.target, rt.body, hostile)
			if w.Code != rt.want() {
				t.Fatalf("status = %d", w.Code)
			}
			c := up.calls()[0]
			if got := c.Header.Get(InternalTokenHeader); got != testInternal {
				t.Fatalf("internal token = %q (client value must be replaced)", got)
			}
			if got := c.Header.Get(ClientIPHeader); got != "203.0.113.7" {
				t.Fatalf("client ip = %q (client value must be replaced by the real peer)", got)
			}
			for _, name := range []string{"Cookie", "Authorization", "X-Forwarded-For", "X-Real-Ip", "X-Gateway-Secret", "X-Admin-Token-Extra", "Origin"} {
				if v := c.Header.Get(name); v != "" {
					t.Fatalf("client header %s reached the upstream: %q", name, v)
				}
			}
		})
	}
}

func TestRoutes_TrustedProxyClientIPReachesUpstream(t *testing.T) {
	up := newUpstream(t, 200, `{"admin_id":"a","name":"Wael"}`)
	p := newProxy(t, up, "198.51.100.0/24")
	r := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	r.RemoteAddr = "198.51.100.10:443"
	r.Header.Set(AdminTokenHeader, testToken)
	r.Header.Set("X-Forwarded-For", "192.0.2.55")
	w := httptest.NewRecorder()
	p.Whoami(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if got := up.calls()[0].Header.Get(ClientIPHeader); got != "192.0.2.55" {
		t.Fatalf("X-Admin-Client-IP = %q, want the forwarded browser address", got)
	}
}

func TestRoutes_UpstreamFailuresBecomeSafe503(t *testing.T) {
	leak := "secret-upstream-detail-mongo-host-9"
	for _, rt := range routes() {
		for _, status := range []int{500, 502, 503, 504} {
			t.Run(rt.name+"/"+http.StatusText(status), func(t *testing.T) {
				up := newUpstream(t, status, `{"error":"`+leak+`"}`)
				p := newProxy(t, up)
				w := do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
				assertSafeError(t, w, http.StatusServiceUnavailable, "unavailable")
				if strings.Contains(w.Body.String(), leak) {
					t.Fatalf("upstream text leaked: %q", w.Body.String())
				}
			})
		}
		t.Run(rt.name+"/timeout", func(t *testing.T) {
			up := newUpstream(t, 200, rt.upstreamReply)
			up.delay = 400 * time.Millisecond
			p, err := New(Options{InternalToken: testInternal, AuthURL: up.srv.URL, AcademyURL: up.srv.URL, Timeout: 50 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			w := do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
			assertSafeError(t, w, http.StatusServiceUnavailable, "unavailable")
			if time.Since(start) > 350*time.Millisecond {
				t.Fatalf("handler did not give up at the timeout: %v", time.Since(start))
			}
		})
		t.Run(rt.name+"/connection-refused", func(t *testing.T) {
			up := newUpstream(t, 200, `{}`)
			p := newProxy(t, up)
			up.srv.Close()
			w := do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
			assertSafeError(t, w, http.StatusServiceUnavailable, "unavailable")
		})
		t.Run(rt.name+"/redirect-is-503", func(t *testing.T) {
			up := newUpstream(t, 302, ``)
			up.header = map[string]string{"Location": "http://elsewhere.invalid/"}
			p := newProxy(t, up)
			w := do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
			assertSafeError(t, w, http.StatusServiceUnavailable, "unavailable")
			if len(up.calls()) != 1 {
				t.Fatalf("redirect was followed: %d calls", len(up.calls()))
			}
		})
		t.Run(rt.name+"/non-json-2xx-is-503", func(t *testing.T) {
			up := newUpstream(t, 200, `<html>not json</html>`)
			p := newProxy(t, up)
			w := do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
			assertSafeError(t, w, http.StatusServiceUnavailable, "unavailable")
		})
	}
}

func TestRoutes_UpstreamClientErrorsAreRelayed(t *testing.T) {
	for _, rt := range routes() {
		for _, status := range []int{400, 401, 404, 409, 429} {
			t.Run(rt.name+"/"+http.StatusText(status), func(t *testing.T) {
				up := newUpstream(t, status, `{"error":"x","code":"c"}`)
				up.header = map[string]string{"Retry-After": "30"}
				p := newProxy(t, up)
				w := do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
				if w.Code != status {
					t.Fatalf("status = %d, want %d", w.Code, status)
				}
				if decode(t, w)["code"] != "c" {
					t.Fatalf("body not relayed: %q", w.Body.String())
				}
				if w.Header().Get("Retry-After") != "30" {
					t.Fatalf("Retry-After not relayed")
				}
			})
		}
	}
}

func TestRoutes_UpstreamClientErrorWithBadBodyKeepsStatus(t *testing.T) {
	up := newUpstream(t, 409, `plain text from upstream`)
	p := newProxy(t, up)
	w := do(p.Reactivate, http.MethodPost, "/api/accounts/reactivate", `{"id":"`+testID+`"}`, withToken())
	if w.Code != 409 {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "plain text") || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("unusable upstream body was relayed: %q", w.Body.String())
	}
}

func TestRoutes_HugeUpstreamBodyIs503(t *testing.T) {
	up := newUpstream(t, 200, `{"a":"`+strings.Repeat("x", maxResponseBody+10)+`"}`)
	p := newProxy(t, up)
	w := do(p.Audit, http.MethodGet, "/api/audit", "", withToken())
	assertSafeError(t, w, http.StatusServiceUnavailable, "unavailable")
}

func TestRetryAfterRelayedOnlyWhenNumeric(t *testing.T) {
	up := newUpstream(t, 429, `{"error":"x","code":"locked_out"}`)
	up.header = map[string]string{"Retry-After": "Wed, 21 Oct 2026 07:28:00 GMT"}
	p := newProxy(t, up)
	w := do(p.Audit, http.MethodGet, "/api/audit", "", withToken())
	if w.Code != 429 || w.Header().Get("Retry-After") != "" {
		t.Fatalf("status=%d retry-after=%q", w.Code, w.Header().Get("Retry-After"))
	}
}

// ---------------------------------------------------------------------------
// whoami
// ---------------------------------------------------------------------------

func TestWhoami_ReturnsOnlyName(t *testing.T) {
	up := newUpstream(t, 200, `{"admin_id":"secret-admin-id","name":"Wael El Saeed"}`)
	p := newProxy(t, up)
	w := do(p.Whoami, http.MethodGet, "/api/whoami", "", withToken())
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	m := decode(t, w)
	if len(m) != 1 || m["name"] != "Wael El Saeed" {
		t.Fatalf("response = %v, want only name", m)
	}
	if strings.Contains(w.Body.String(), "secret-admin-id") {
		t.Fatal("admin_id leaked")
	}
}

func TestWhoami_UnusableUpstreamNameIs503(t *testing.T) {
	for _, body := range []string{`{"admin_id":"a"}`, `{"name":""}`, `{"name":"   "}`, `{"name":42}`, `[]`} {
		t.Run(body, func(t *testing.T) {
			up := newUpstream(t, 200, body)
			p := newProxy(t, up)
			w := do(p.Whoami, http.MethodGet, "/api/whoami", "", withToken())
			assertSafeError(t, w, http.StatusServiceUnavailable, "unavailable")
		})
	}
}

func TestWhoami_Upstream401AndLockoutAreRelayed(t *testing.T) {
	up := newUpstream(t, 401, `{"error":"unauthorized","code":"unauthorized"}`)
	p := newProxy(t, up)
	if w := do(p.Whoami, http.MethodGet, "/api/whoami", "", withToken()); w.Code != 401 {
		t.Fatalf("status = %d", w.Code)
	}
	up.set(429, `{"error":"too many attempts, retry later","code":"locked_out"}`)
	if w := do(p.Whoami, http.MethodGet, "/api/whoami", "", withToken()); w.Code != 429 {
		t.Fatalf("status = %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// accounts list
// ---------------------------------------------------------------------------

func TestAccounts_InvalidQueryIs400WithoutUpstreamCall(t *testing.T) {
	long := strings.Repeat("a", maxSearchRunes+1)
	cases := map[string]string{
		"bad status":      "status=admin",
		"upper status":    "status=ACTIVE",
		"page zero":       "page=0",
		"page negative":   "page=-1",
		"page text":       "page=abc",
		"page float":      "page=1.5",
		"page too big":    "page=1000001",
		"limit zero":      "limit=0",
		"limit 101":       "limit=101",
		"limit text":      "limit=x",
		"search too long": "search=" + long,
		"search bad utf8": "search=%ff%fe",
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			up := newUpstream(t, 200, `{}`)
			p := newProxy(t, up)
			w := do(p.Accounts, http.MethodGet, "/api/accounts?"+q, "", withToken())
			assertSafeError(t, w, http.StatusBadRequest, "bad_request")
			if len(up.calls()) != 0 {
				t.Fatal("upstream was called")
			}
		})
	}
}

func TestAccounts_QueryIsRebuiltFromAllowlistedParams(t *testing.T) {
	up := newUpstream(t, 200, `{"items":[]}`)
	p := newProxy(t, up)
	q := "search=%D8%B9%D9%84%D9%8A+%0Aevil%0D&status=suspended&page=3&limit=100&role=admin&$where=1&internal=1&search=second"
	w := do(p.Accounts, http.MethodGet, "/api/accounts?"+q, "", withToken())
	if w.Code != 200 {
		t.Fatalf("status = %d %q", w.Code, w.Body.String())
	}
	got := up.calls()[0].RawQuery
	// Only the four known names, CR/LF replaced by spaces, first search value used.
	want := "limit=100&page=3&search=%D8%B9%D9%84%D9%8A++evil&status=suspended"
	if got != want {
		t.Fatalf("upstream query = %q, want %q", got, want)
	}
}

func TestAccounts_PendingDeletionStatusPassesThrough(t *testing.T) {
	up := newUpstream(t, 200, `{"items":[]}`)
	p := newProxy(t, up)
	w := do(p.Accounts, http.MethodGet, "/api/accounts?status=pending_deletion", "", withToken())
	if w.Code != 200 {
		t.Fatalf("status = %d %q", w.Code, w.Body.String())
	}
	if got := up.calls()[0].RawQuery; got != "status=pending_deletion" {
		t.Fatalf("upstream query = %q, want status=pending_deletion", got)
	}
}

func TestAccounts_EmptyAndWhitespaceFiltersAreOmitted(t *testing.T) {
	up := newUpstream(t, 200, `{"items":[]}`)
	p := newProxy(t, up)
	do(p.Accounts, http.MethodGet, "/api/accounts?search=+++&status=&page=&limit=", "", withToken())
	if got := up.calls()[0].RawQuery; got != "" {
		t.Fatalf("expected empty query, got %q", got)
	}
}

func TestAccounts_SearchBoundaryIs100Characters(t *testing.T) {
	up := newUpstream(t, 200, `{"items":[]}`)
	p := newProxy(t, up)
	// 100 Arabic letters = 200 bytes but 100 characters: allowed.
	ok := strings.Repeat("\u0639", maxSearchRunes)
	w := do(p.Accounts, http.MethodGet, "/api/accounts?search="+ok, "", withToken())
	if w.Code != 200 {
		t.Fatalf("100 characters refused: %d", w.Code)
	}
	w = do(p.Accounts, http.MethodGet, "/api/accounts?search="+ok+"\u0639", "", withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")
}

// ---------------------------------------------------------------------------
// audit list
// ---------------------------------------------------------------------------

func TestAudit_InvalidPagingIs400AndExtraParamsDropped(t *testing.T) {
	for _, q := range []string{"page=0", "limit=0", "limit=101", "page=x", "limit=-3"} {
		t.Run(q, func(t *testing.T) {
			up := newUpstream(t, 200, `{}`)
			p := newProxy(t, up)
			w := do(p.Audit, http.MethodGet, "/api/audit?"+q, "", withToken())
			assertSafeError(t, w, http.StatusBadRequest, "bad_request")
			if len(up.calls()) != 0 {
				t.Fatal("upstream was called")
			}
		})
	}
	up := newUpstream(t, 200, `{"items":[]}`)
	p := newProxy(t, up)
	do(p.Audit, http.MethodGet, "/api/audit?page=2&actor=x&limit=5&search=y", "", withToken())
	if got := up.calls()[0].RawQuery; got != "limit=5&page=2" {
		t.Fatalf("upstream query = %q", got)
	}
}

// ---------------------------------------------------------------------------
// account actions: ids, reasons, bodies
// ---------------------------------------------------------------------------

func TestAccountActions_BadIDIs400WithoutUpstreamCall(t *testing.T) {
	ids := map[string]string{
		"empty":         "",
		"missing":       "MISSING",
		"upper":         strings.ToUpper(testID),
		"short":         "0f8fad5b-d9cb-469f-a165",
		"no dashes":     strings.ReplaceAll(testID, "-", ""),
		"traversal":     "../" + testID,
		"suffix":        testID + "/suspend",
		"query":         testID + "?x=1",
		"fragment":      testID + "#x",
		"trailing nl":   testID + "\n",
		"leading space": " " + testID,
		"non hex":       "zf8fad5b-d9cb-469f-a165-70867728950e",
		"sql":           "' OR 1=1",
		"too long":      testID + testID,
		"percent":       strings.Replace(testID, "-", "%2d", 1),
	}
	actions := []struct {
		name    string
		handler func(*Proxy) http.HandlerFunc
		reason  bool
	}{
		{"suspend", func(p *Proxy) http.HandlerFunc { return p.Suspend }, true},
		{"reactivate", func(p *Proxy) http.HandlerFunc { return p.Reactivate }, false},
		{"delete", func(p *Proxy) http.HandlerFunc { return p.Delete }, true},
	}
	for _, a := range actions {
		for name, id := range ids {
			t.Run(a.name+"/"+name, func(t *testing.T) {
				up := newUpstream(t, 200, `{}`)
				p := newProxy(t, up)
				payload := map[string]any{}
				if name != "missing" {
					payload["id"] = id
				}
				if a.reason {
					payload["reason"] = "valid reason"
				}
				b, _ := json.Marshal(payload)
				w := do(a.handler(p), http.MethodPost, "/api/accounts/x", string(b), withToken())
				assertSafeError(t, w, http.StatusBadRequest, "bad_request")
				if len(up.calls()) != 0 {
					t.Fatal("upstream was called")
				}
			})
		}
	}
}

func TestAccountActions_BadReasonIs400WithoutUpstreamCall(t *testing.T) {
	tooLong := strings.Repeat("a", maxReasonRunes+1)
	cases := map[string]string{
		"missing":      `{"id":"` + testID + `"}`,
		"null":         `{"id":"` + testID + `","reason":null}`,
		"empty":        `{"id":"` + testID + `","reason":""}`,
		"spaces":       `{"id":"` + testID + `","reason":"   "}`,
		"only crlf":    `{"id":"` + testID + `","reason":"\r\n\r\n"}`,
		"only control": `{"id":"` + testID + `","reason":"\u0000\u0007\t"}`,
		"too long":     `{"id":"` + testID + `","reason":"` + tooLong + `"}`,
		"number":       `{"id":"` + testID + `","reason":5}`,
		"object":       `{"id":"` + testID + `","reason":{"a":1}}`,
		"bad utf8":     "{\"id\":\"" + testID + "\",\"reason\":\"\xff\xfe\"}",
	}
	for _, route := range []struct {
		name    string
		handler func(*Proxy) http.HandlerFunc
	}{
		{"suspend", func(p *Proxy) http.HandlerFunc { return p.Suspend }},
		{"delete", func(p *Proxy) http.HandlerFunc { return p.Delete }},
	} {
		for name, body := range cases {
			t.Run(route.name+"/"+name, func(t *testing.T) {
				up := newUpstream(t, 200, `{}`)
				p := newProxy(t, up)
				w := do(route.handler(p), http.MethodPost, "/api/accounts/x", body, withToken())
				assertSafeError(t, w, http.StatusBadRequest, "bad_request")
				if len(up.calls()) != 0 {
					t.Fatal("upstream was called")
				}
			})
		}
	}
}

func TestAccountActions_BadBodiesAre400WithoutUpstreamCall(t *testing.T) {
	big := `{"id":"` + testID + `","reason":"` + strings.Repeat("a", maxRequestBody) + `"}`
	cases := map[string]string{
		"empty":         ``,
		"not json":      `id=1`,
		"array":         `[]`,
		"string":        `"x"`,
		"unknown field": `{"id":"` + testID + `","reason":"r","extra":1}`,
		"role field":    `{"id":"` + testID + `","reason":"r","role":"admin"}`,
		"trailing":      `{"id":"` + testID + `","reason":"r"}{"id":"x"}`,
		"trailing junk": `{"id":"` + testID + `","reason":"r"} x`,
		"huge":          big,
		"truncated":     `{"id":"` + testID + `","reason":`,
		"id number":     `{"id":5,"reason":"r"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			up := newUpstream(t, 200, `{}`)
			p := newProxy(t, up)
			w := do(p.Suspend, http.MethodPost, "/api/accounts/suspend", body, withToken())
			assertSafeError(t, w, http.StatusBadRequest, "bad_request")
			if len(up.calls()) != 0 {
				t.Fatal("upstream was called")
			}
		})
	}
}

func TestSuspend_ReasonIsCleanedBeforeForwarding(t *testing.T) {
	cases := []struct{ name, reasonJSON, want string }{
		{"crlf replaced", `"line one\r\nline two"`, "line one  line two"},
		{"lf replaced", `"a\nb"`, "a b"},
		{"trimmed", `"  padded  "`, "padded"},
		{"control to space", `"a\u0000b\u0007c"`, "a b c"},
		{"tab to space", `"a\tb"`, "a b"},
		{"arabic kept", `"\u0633\u0628\u0627\u0645"`, "\u0633\u0628\u0627\u0645"},
		{"quotes escaped by marshal", `"say \"hi\" <b>x</b>"`, `say "hi" <b>x</b>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			up := newUpstream(t, 200, `{"status":"ok"}`)
			p := newProxy(t, up)
			w := do(p.Suspend, http.MethodPost, "/api/accounts/suspend", `{"id":"`+testID+`","reason":`+c.reasonJSON+`}`, withToken())
			if w.Code != 200 {
				t.Fatalf("status = %d %q", w.Code, w.Body.String())
			}
			var sent struct{ Reason string }
			if err := json.Unmarshal([]byte(up.calls()[0].Body), &sent); err != nil {
				t.Fatalf("upstream body not JSON: %v", err)
			}
			if sent.Reason != c.want {
				t.Fatalf("upstream reason = %q, want %q", sent.Reason, c.want)
			}
			if strings.ContainsAny(sent.Reason, "\r\n") {
				t.Fatal("CR/LF reached the upstream")
			}
			var keys map[string]json.RawMessage
			_ = json.Unmarshal([]byte(up.calls()[0].Body), &keys)
			if len(keys) != 1 {
				t.Fatalf("upstream body must contain only reason, got %v", keys)
			}
		})
	}
}

func TestSuspend_ReasonLengthBoundaryCountsCharacters(t *testing.T) {
	up := newUpstream(t, 200, `{"status":"ok"}`)
	p := newProxy(t, up)
	atLimit := strings.Repeat("\u0633", maxReasonRunes) // 2000 bytes, 1000 characters
	w := do(p.Suspend, http.MethodPost, "/api/accounts/suspend", `{"id":"`+testID+`","reason":"`+atLimit+`"}`, withToken())
	if w.Code != 200 {
		t.Fatalf("1000-character reason refused: %d", w.Code)
	}
	w = do(p.Suspend, http.MethodPost, "/api/accounts/suspend", `{"id":"`+testID+`","reason":"`+atLimit+`x"}`, withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")
}

func TestReactivate_TakesNoReason(t *testing.T) {
	up := newUpstream(t, 200, `{"status":"ok"}`)
	p := newProxy(t, up)
	for _, body := range []string{
		`{"id":"` + testID + `","reason":"why"}`,
		`{"id":"` + testID + `","reason":""}`,
		`{"id":"` + testID + `","reason":null}`,
	} {
		w := do(p.Reactivate, http.MethodPost, "/api/accounts/reactivate", body, withToken())
		assertSafeError(t, w, http.StatusBadRequest, "bad_request")
	}
	if len(up.calls()) != 0 {
		t.Fatal("upstream was called for a body with a reason")
	}
	w := do(p.Reactivate, http.MethodPost, "/api/accounts/reactivate", `{"id":"`+testID+`"}`, withToken())
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if c := up.calls()[0]; c.Body != "" || c.Header.Get("Content-Type") != "" {
		t.Fatalf("reactivate must send no body: body=%q ct=%q", c.Body, c.Header.Get("Content-Type"))
	}
}

func TestDelete_UsesUpstreamDELETEWithReasonBody(t *testing.T) {
	up := newUpstream(t, 200, `{"status":"ok"}`)
	p := newProxy(t, up)
	w := do(p.Delete, http.MethodPost, "/api/accounts/delete", `{"id":"`+testID+`","reason":"abuse"}`, withToken())
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	c := up.calls()[0]
	if c.Method != http.MethodDelete || c.Path != "/internal/admin/accounts/"+testID || c.Body != `{"reason":"abuse"}` {
		t.Fatalf("upstream got %s %s %q", c.Method, c.Path, c.Body)
	}
}

func TestAccountActions_UpstreamConflictIsRelayed(t *testing.T) {
	up := newUpstream(t, 409, `{"error":"account is already suspended","code":"conflict","request_id":"r1"}`)
	p := newProxy(t, up)
	w := do(p.Suspend, http.MethodPost, "/api/accounts/suspend", `{"id":"`+testID+`","reason":"r"}`, withToken())
	if w.Code != 409 || decode(t, w)["code"] != "conflict" {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// client IP
// ---------------------------------------------------------------------------

func TestClientIP(t *testing.T) {
	cases := []struct {
		name    string
		trusted []string
		remote  string
		xff     []string
		want    string
	}{
		{"no trusted proxies ignores xff", nil, "203.0.113.7:1", []string{"1.1.1.1"}, "203.0.113.7"},
		{"untrusted peer ignores forged xff", []string{"172.30.0.10/32"}, "203.0.113.7:1", []string{"1.1.1.1"}, "203.0.113.7"},
		{"trusted peer single entry", []string{"172.30.0.10/32"}, "172.30.0.10:443", []string{"198.51.100.9"}, "198.51.100.9"},
		{"trusted peer, client forged left entry", []string{"172.30.0.10/32"}, "172.30.0.10:443", []string{"6.6.6.6, 198.51.100.9"}, "198.51.100.9"},
		{"chain with trusted hop on the right", []string{"10.0.0.0/8"}, "10.0.0.2:443", []string{"198.51.100.9, 10.0.0.5"}, "198.51.100.9"},
		{"multiple header lines", []string{"10.0.0.0/8"}, "10.0.0.2:443", []string{"6.6.6.6", "198.51.100.9"}, "198.51.100.9"},
		{"trusted peer without xff", []string{"172.30.0.10/32"}, "172.30.0.10:443", nil, "172.30.0.10"},
		{"malformed entry falls back to peer", []string{"172.30.0.10/32"}, "172.30.0.10:443", []string{"198.51.100.9, garbage"}, "172.30.0.10"},
		{"hostname entry falls back to peer", []string{"172.30.0.10/32"}, "172.30.0.10:443", []string{"evil.example"}, "172.30.0.10"},
		{"empty entry falls back to peer", []string{"172.30.0.10/32"}, "172.30.0.10:443", []string{"198.51.100.9,"}, "172.30.0.10"},
		{"all hops trusted gives peer", []string{"10.0.0.0/8"}, "10.0.0.2:443", []string{"10.0.0.7, 10.0.0.9"}, "10.0.0.2"},
		{"ipv6 client", []string{"172.30.0.10/32"}, "172.30.0.10:443", []string{"2001:db8::1"}, "2001:db8::1"},
		{"ipv6 peer", nil, "[2001:db8::7]:5000", nil, "2001:db8::7"},
		{"ipv6 zone dropped", nil, "[fe80::1%eth0]:5000", nil, "fe80::1"},
		{"ipv4-mapped peer is unmapped", nil, "[::ffff:203.0.113.7]:5000", nil, "203.0.113.7"},
		{"ipv4-mapped xff is unmapped", []string{"172.30.0.10/32"}, "172.30.0.10:443", []string{"::ffff:198.51.100.9"}, "198.51.100.9"},
		{"remote without port", nil, "203.0.113.7", nil, "203.0.113.7"},
		{"unparsable remote", nil, "not-an-address", nil, ""},
		{"empty remote", nil, "", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var prefixes []netip.Prefix
			for _, s := range c.trusted {
				prefixes = append(prefixes, netip.MustParsePrefix(s))
			}
			p := &Proxy{trusted: prefixes}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = c.remote
			for _, v := range c.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := p.ClientIP(r); got != c.want {
				t.Fatalf("ClientIP = %q, want %q", got, c.want)
			}
		})
	}
}

func TestUnknownPeerAddressOmitsClientIPHeader(t *testing.T) {
	up := newUpstream(t, 200, `{"name":"n"}`)
	p := newProxy(t, up)
	r := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	r.RemoteAddr = "pipe"
	r.Header.Set(AdminTokenHeader, testToken)
	r.Header.Set(ClientIPHeader, "6.6.6.6")
	w := httptest.NewRecorder()
	p.Whoami(w, r)
	if v := up.calls()[0].Header.Get(ClientIPHeader); v != "" {
		t.Fatalf("client-supplied address leaked when the peer is unknown: %q", v)
	}
}

// ---------------------------------------------------------------------------
// constructor and logging
// ---------------------------------------------------------------------------

func TestNew_FailsFastOnEmptySecretOrURL(t *testing.T) {
	for name, o := range map[string]Options{
		"empty token":       {InternalToken: "", AuthURL: "https://a:9001", AcademyURL: "https://a:9002"},
		"blank token":       {InternalToken: "  ", AuthURL: "https://a:9001", AcademyURL: "https://a:9002"},
		"empty auth url":    {InternalToken: "x", AuthURL: "", AcademyURL: "https://a:9002"},
		"blank auth url":    {InternalToken: "x", AuthURL: "  ", AcademyURL: "https://a:9002"},
		"empty academy url": {InternalToken: "x", AuthURL: "https://a:9001", AcademyURL: ""},
		"blank academy url": {InternalToken: "x", AuthURL: "https://a:9001", AcademyURL: "  "},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(o); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestNew_DefaultsToTenSecondTimeout(t *testing.T) {
	p, err := New(Options{InternalToken: "x", AuthURL: "https://a:9001/", AcademyURL: "https://a:9002/"})
	if err != nil {
		t.Fatal(err)
	}
	if p.timeout != 10*time.Second || p.client.Timeout != 10*time.Second {
		t.Fatalf("timeout = %v / %v, want 10s", p.timeout, p.client.Timeout)
	}
	if p.authURL != "https://a:9001" {
		t.Fatalf("authURL = %q", p.authURL)
	}
	if p.academyURL != "https://a:9002" {
		t.Fatalf("academyURL = %q", p.academyURL)
	}
	if UpstreamTimeout != 10*time.Second {
		t.Fatal("UpstreamTimeout must be 10s")
	}
}

func TestLogs_ContainIDsAndStatusOnly(t *testing.T) {
	var buf bytes.Buffer
	prev := logWriter(&buf)
	defer prev()

	secretReason := "MY-SECRET-REASON-TEXT"
	secretSearch := "needle-search-text"
	up := newUpstream(t, 200, `{"status":"ok","name":"Wael"}`)
	p := newProxy(t, up)

	do(p.Suspend, http.MethodPost, "/api/accounts/suspend", `{"id":"`+testID+`","reason":"`+secretReason+`"}`, withToken())
	do(p.Accounts, http.MethodGet, "/api/accounts?search="+secretSearch, "", withToken())
	do(p.Whoami, http.MethodGet, "/api/whoami", "", withToken())
	up.set(500, `{"error":"db password is hunter2"}`)
	do(p.Audit, http.MethodGet, "/api/audit", "", withToken())
	up.srv.Close()
	do(p.Audit, http.MethodGet, "/api/audit", "", withToken())

	out := buf.String()
	for _, forbidden := range []string{testToken, testInternal, secretReason, secretSearch, "hunter2", "203.0.113.7", "127.0.0.1"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("log contains %q:\n%s", forbidden, out)
		}
	}
	if !strings.Contains(out, "route=accounts.suspend target="+testID+" upstream_status=200") {
		t.Fatalf("expected route, id and status in the log:\n%s", out)
	}
	if !strings.Contains(out, "upstream_status=500") {
		t.Fatalf("expected upstream status in the log:\n%s", out)
	}
	if !strings.Contains(out, "upstream_failure=error") {
		t.Fatalf("expected failure classification in the log:\n%s", out)
	}
}
