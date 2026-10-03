package server

import (
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/omarmaarouf18/wael-app/admin-console/internal/proxy"
	"github.com/omarmaarouf18/wael-app/admin-console/web"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

// harness wires the real handler to the real embedded assets and a counting
// fake upstream.
type harness struct {
	h     http.Handler
	calls *atomic.Int64
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	calls := &atomic.Int64{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Wael","items":[],"total":0}`))
	}))
	t.Cleanup(up.Close)
	p, err := proxy.New(proxy.Options{InternalToken: "internal", AuthURL: up.URL, AcademyURL: up.URL, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(p, web.Files)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &harness{h: h, calls: calls}
}

func (hh *harness) get(path string, headers ...string) *httptest.ResponseRecorder {
	return hh.do(http.MethodGet, path, headers...)
}

func (hh *harness) do(method, path string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = "203.0.113.7:4000"
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	hh.h.ServeHTTP(w, r)
	return w
}

func TestContentSecurityPolicyIsExactlyTheSpecifiedDirectives(t *testing.T) {
	want := "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
		"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	if ContentSecurityPolicy != want {
		t.Fatalf("CSP = %q", ContentSecurityPolicy)
	}
	for _, bad := range []string{"unsafe-inline", "unsafe-eval", "http:", "https:", "*"} {
		if strings.Contains(ContentSecurityPolicy, bad) {
			t.Fatalf("CSP contains %q", bad)
		}
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	hh := newHarness(t)
	cases := []struct {
		name, method, path string
		headers            []string
		status             int
	}{
		{"index", "GET", "/", nil, 200},
		{"index.html", "GET", "/index.html", nil, 200},
		{"css", "GET", "/style.css", nil, 200},
		{"js", "GET", "/js/app.js", nil, 200},
		{"healthz", "GET", "/healthz", nil, 200},
		{"unknown path", "GET", "/nope", nil, 404},
		{"unknown api path", "GET", "/api/unknown", nil, 404},
		{"api without token", "GET", "/api/whoami", nil, 401},
		{"api wrong method", "POST", "/api/whoami", []string{"X-Admin-Token", "t"}, 405},
		{"api ok", "GET", "/api/whoami", []string{"X-Admin-Token", "t"}, 200},
		{"static wrong method", "POST", "/", nil, 405},
		{"head", "HEAD", "/", nil, 200},
		{"options", "OPTIONS", "/api/accounts", nil, 405},
		{"internal path", "GET", "/internal/admin/verify", nil, 404},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := hh.do(c.method, c.path, c.headers...)
			if w.Code != c.status {
				t.Fatalf("status = %d, want %d", w.Code, c.status)
			}
			want := map[string]string{
				"Content-Security-Policy":      ContentSecurityPolicy,
				"X-Content-Type-Options":       "nosniff",
				"Referrer-Policy":              "no-referrer",
				"X-Frame-Options":              "DENY",
				"Cross-Origin-Opener-Policy":   "same-origin",
				"Cross-Origin-Resource-Policy": "same-origin",
			}
			for k, v := range want {
				if got := w.Header().Get(k); got != v {
					t.Fatalf("%s = %q, want %q", k, got, v)
				}
			}
			if got := w.Header().Get("Permissions-Policy"); got == "" {
				t.Fatal("Permissions-Policy missing")
			}
		})
	}
}

func TestCacheControl(t *testing.T) {
	hh := newHarness(t)
	for _, path := range []string{"/api/whoami", "/api/accounts", "/api/audit", "/api/unknown", "/api/accounts/suspend", "/healthz"} {
		if got := hh.get(path).Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s Cache-Control = %q, want no-store", path, got)
		}
	}
	if got := hh.get("/api/whoami", "X-Admin-Token", "t").Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("authenticated API Cache-Control = %q", got)
	}
	if got := hh.get("/style.css").Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("static Cache-Control = %q, want no-cache", got)
	}
}

func TestNoThirdPartyOriginsInResponses(t *testing.T) {
	hh := newHarness(t)
	for _, path := range []string{"/", "/style.css", "/js/app.js"} {
		body := hh.get(path).Body.String()
		for _, bad := range []string{"http://", "https://", "//cdn", "@import", "url(http"} {
			if strings.Contains(body, bad) {
				t.Fatalf("%s references %q", path, bad)
			}
		}
	}
}

func TestEveryAllowlistedRouteIsRegistered(t *testing.T) {
	hh := newHarness(t)
	for _, route := range APIRoutes() {
		w := hh.get(route) // no token: a registered route answers 401 or 405, never 404
		if w.Code == http.StatusNotFound {
			t.Fatalf("route %s is not registered", route)
		}
		if w.Code != http.StatusUnauthorized && w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("route %s answered %d without a token", route, w.Code)
		}
	}
	if got := len(APIRoutes()); got != 26 {
		t.Fatalf("expected exactly 26 API routes, got %d", got)
	}
}

func TestNoRouteServesInternalPaths(t *testing.T) {
	for _, route := range APIRoutes() {
		if strings.Contains(route, "internal") {
			t.Fatalf("allowlisted route %q mentions internal", route)
		}
		if !strings.HasPrefix(route, "/api/") {
			t.Fatalf("allowlisted route %q is outside /api/", route)
		}
	}
	hh := newHarness(t)
	paths := []string{
		"/internal", "/internal/", "/internal/admin/verify", "/internal/admin/accounts",
		"/internal/admin/accounts/0f8fad5b-d9cb-469f-a165-70867728950e/suspend",
		"/internal/admin/audit-log", "//internal/admin/verify", "/api/internal/admin/verify",
		"/api/../internal/admin/verify", "/%69nternal/admin/verify", "/static/../internal/admin/verify",
		"/js/../internal/admin/verify", "/INTERNAL/admin/verify",
	}
	for _, method := range []string{"GET", "POST", "DELETE"} {
		for _, path := range paths {
			w := hh.do(method, path, "X-Admin-Token", "t", "X-Internal-Token", "attacker")
			// A redirect to the cleaned path is fine, as long as the cleaned path is also refused.
			if w.Code >= 200 && w.Code < 300 {
				t.Fatalf("%s %s answered %d", method, path, w.Code)
			}
			if w.Code >= 300 && w.Code < 400 {
				loc := w.Header().Get("Location")
				if w2 := hh.do(method, loc, "X-Admin-Token", "t"); w2.Code >= 200 && w2.Code < 300 {
					t.Fatalf("%s %s redirected to %s which answered %d", method, path, loc, w2.Code)
				}
			}
		}
	}
	if hh.calls.Load() != 0 {
		t.Fatalf("an /internal path reached the upstream (%d calls)", hh.calls.Load())
	}
}

func TestNearMissAPIPathsAre404(t *testing.T) {
	hh := newHarness(t)
	for _, path := range []string{
		"/api", "/api/", "/api/accounts/", "/api/accounts/suspend/", "/api/accounts/x",
		"/api/accounts/suspend/extra", "/api/whoami/", "/api/audit/", "/api/Accounts", "/api/accounts%2fsuspend",
		"/api/levels/", "/api/levels/create/extra", "/api/subjects/x", "/api/subjects/publish/",
		"/api/videos/", "/api/videos/reorder/extra", "/api/Levels", "/api/videos%2fcreate",
	} {
		w := hh.do("POST", path, "X-Admin-Token", "t")
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s answered %d, want 404", path, w.Code)
		}
	}
	if hh.calls.Load() != 0 {
		t.Fatal("a near-miss path reached the upstream")
	}
}

func TestStaticServesOnlyEmbeddedFiles(t *testing.T) {
	hh := newHarness(t)

	for path, ct := range map[string]string{
		"/":               "text/html; charset=utf-8",
		"/index.html":     "text/html; charset=utf-8",
		"/style.css":      "text/css; charset=utf-8",
		"/js/app.js":      "text/javascript; charset=utf-8",
		"/js/api.js":      "text/javascript; charset=utf-8",
		"/js/auth.js":     "text/javascript; charset=utf-8",
		"/js/i18n.js":     "text/javascript; charset=utf-8",
		"/js/accounts.js": "text/javascript; charset=utf-8",
		"/js/audit.js":    "text/javascript; charset=utf-8",
	} {
		w := hh.get(path)
		if w.Code != 200 || w.Header().Get("Content-Type") != ct || w.Body.Len() == 0 {
			t.Fatalf("%s: status=%d type=%q len=%d", path, w.Code, w.Header().Get("Content-Type"), w.Body.Len())
		}
	}
	for _, path := range []string{
		"/js", "/js/", "/web/embed.go", "/embed.go", "/test/api.test.mjs", "/js/nope.js",
		"/.git/config", "/go.mod", "/js/../index.html.bak", "/index.html/", "/%2e%2e/etc/passwd",
	} {
		w := hh.get(path)
		if w.Code == 200 {
			t.Fatalf("%s was served", path)
		}
		if w.Code >= 300 && w.Code < 400 {
			// ServeMux redirects unclean paths; the cleaned path must be refused too.
			if w2 := hh.get(w.Header().Get("Location")); w2.Code == 200 {
				t.Fatalf("%s redirected to a served file", path)
			}
		}
	}
	if w := hh.do("HEAD", "/style.css"); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("HEAD: status=%d len=%d", w.Code, w.Body.Len())
	}
}

func TestEmbeddedFilesDoNotIncludeTests(t *testing.T) {
	if _, err := fs.Stat(web.Files, "test"); err == nil {
		t.Fatal("web/test must not be embedded")
	}
	err := fs.WalkDir(web.Files, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(name, ".test.") || strings.HasSuffix(name, ".go") {
			t.Fatalf("unexpected embedded file %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHealthz(t *testing.T) {
	hh := newHarness(t)
	w := hh.get("/healthz")
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if w := hh.do("POST", "/healthz"); w.Code != 405 {
		t.Fatalf("POST /healthz = %d", w.Code)
	}
	if hh.calls.Load() != 0 {
		t.Fatal("healthz must not call the upstream")
	}
}

func TestNewRequiresIndexAndIgnoresUnknownExtensions(t *testing.T) {
	p, err := proxy.New(proxy.Options{InternalToken: "x", AuthURL: "http://127.0.0.1:1", AcademyURL: "http://127.0.0.1:2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(p, fstest.MapFS{"style.css": {Data: []byte("a{}")}}); err == nil {
		t.Fatal("expected an error without index.html")
	}
	h, err := New(p, fstest.MapFS{
		"index.html":  {Data: []byte("<html></html>")},
		"secret.txt":  {Data: []byte("nope")},
		"js/a.js":     {Data: []byte("export {}")},
		"js/a.js.map": {Data: []byte("{}")},
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{"/secret.txt": 404, "/js/a.js.map": 404, "/js/a.js": 200} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s = %d, want %d", path, w.Code, want)
		}
	}
}
