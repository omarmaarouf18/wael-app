// Package server wires the admin-console HTTP surface: the explicit API
// allowlist, the embedded static pages and the security headers that apply to
// every response.
package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/omarmaarouf18/wael-app/admin-console/internal/proxy"
)

// ContentSecurityPolicy is sent on every response. No inline script or style,
// no third-party origin, no framing.
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; " +
	"base-uri 'none'; form-action 'self'"

// APIRoutes lists every route that reaches an upstream, in registration
// order. It is the single allowlist; tests assert it never includes /internal/.
func APIRoutes() []string {
	return []string{
		"/api/whoami",
		"/api/accounts",
		"/api/accounts/suspend",
		"/api/accounts/reactivate",
		"/api/accounts/delete",
		"/api/audit",
		"/api/levels",
		"/api/levels/create",
		"/api/levels/update",
		"/api/levels/delete",
		"/api/subjects",
		"/api/subjects/create",
		"/api/subjects/update",
		"/api/subjects/publish",
		"/api/subjects/unpublish",
		"/api/videos",
		"/api/videos/create",
		"/api/videos/update",
		"/api/videos/reorder",
		"/api/videos/delete",
	}
}

// New builds the handler. assets must contain index.html, style.css and js/*.js.
func New(p *proxy.Proxy, assets fs.FS) (http.Handler, error) {
	static, err := loadStatic(assets)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	handlers := map[string]http.HandlerFunc{
		"/api/whoami":              p.Whoami,
		"/api/accounts":            p.Accounts,
		"/api/accounts/suspend":    p.Suspend,
		"/api/accounts/reactivate": p.Reactivate,
		"/api/accounts/delete":     p.Delete,
		"/api/audit":               p.Audit,
		"/api/levels":              p.LevelsList,
		"/api/levels/create":       p.LevelsCreate,
		"/api/levels/update":       p.LevelsUpdate,
		"/api/levels/delete":       p.LevelsDelete,
		"/api/subjects":            p.SubjectsList,
		"/api/subjects/create":     p.SubjectsCreate,
		"/api/subjects/update":     p.SubjectsUpdate,
		"/api/subjects/publish":    p.SubjectsPublish,
		"/api/subjects/unpublish":  p.SubjectsUnpublish,
		"/api/videos":              p.VideosList,
		"/api/videos/create":       p.VideosCreate,
		"/api/videos/update":       p.VideosUpdate,
		"/api/videos/reorder":      p.VideosReorder,
		"/api/videos/delete":       p.VideosDelete,
	}
	for _, route := range APIRoutes() {
		mux.HandleFunc(route, handlers[route])
	}
	mux.HandleFunc("/healthz", healthz)
	mux.Handle("/", static)
	return SecurityHeaders(mux), nil
}

// SecurityHeaders sets the response headers every reply needs, including
// errors and 404s. API responses are never cached.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", ContentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

// staticFile is one embedded file, read once at startup.
type staticFile struct {
	contentType string
	body        []byte
}

// staticHandler serves only the files found in the embedded tree, under their
// own names, plus "/" for index.html. There is no directory listing and no
// fallthrough to the filesystem.
type staticHandler map[string]staticFile

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
}

func loadStatic(assets fs.FS) (staticHandler, error) {
	files := staticHandler{}
	err := fs.WalkDir(assets, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ct, ok := contentTypes[path.Ext(name)]
		if !ok {
			return nil
		}
		b, err := fs.ReadFile(assets, name)
		if err != nil {
			return err
		}
		files["/"+name] = staticFile{contentType: ct, body: b}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, ok := files["/index.html"]; !ok {
		return nil, fs.ErrNotExist
	}
	return files, nil
}

func (s staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path
	if name == "/" {
		name = "/index.html"
	}
	f, ok := s[name]
	if !ok {
		proxy.WriteNotFound(w)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", f.contentType)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(f.body)
}
