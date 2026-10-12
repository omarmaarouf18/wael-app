// Broadcast proxy routes (SPEC Phase 7, ADR-0013): the Notifications tab
// composes a send, previews the recipient count, lists history and retries
// failed targets through the notification-service admin listener.
package proxy

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// broadcastCreateRequest is the browser body for POST /api/broadcasts/create.
// Audience is "all" or "subject:<id>"; texts follow the server contract.
type broadcastCreateRequest struct {
	Audience string `json:"audience"`
	TitleAr  string `json:"title_ar"`
	TitleEn  string `json:"title_en"`
	BodyAr   string `json:"body_ar"`
	BodyEn   string `json:"body_en"`
}

// validBroadcastAudience accepts "all" or "subject:<id>" with a well-formed
// id (letters, digits, _ and -, 1-100). Existence is checked upstream.
func validBroadcastAudience(audience string) bool {
	audience = strings.TrimSpace(audience)
	if audience == "all" {
		return true
	}
	id, found := strings.CutPrefix(audience, "subject:")
	if !found || id == "" || len(id) > 100 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// validBroadcastID reports whether s is a well-formed broadcast id
// (server-issued UUID). Checked before any upstream call so the id can be
// placed in the upstream path.
func validBroadcastID(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// BroadcastsPreview handles POST /api/broadcasts/preview {audience} ->
// POST notification /internal/admin/broadcasts/preview. A dry run: nothing
// is stored or sent.
func (p *Proxy) BroadcastsPreview(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in struct {
		Audience string `json:"audience"`
	}
	if !decodeStrict(w, r, &in) {
		return
	}
	if !validBroadcastAudience(in.Audience) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, err := json.Marshal(map[string]string{"audience": strings.TrimSpace(in.Audience)})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	p.relayNotif(w, r, token, upstreamCall{
		route: "broadcasts.preview", method: http.MethodPost, path: "/internal/admin/broadcasts/preview", body: body,
	})
}

// BroadcastsCreate handles POST /api/broadcasts/create ->
// POST notification /internal/admin/broadcasts. The body is rebuilt from
// validated fields only.
func (p *Proxy) BroadcastsCreate(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in broadcastCreateRequest
	if !decodeStrict(w, r, &in) {
		return
	}
	if !validBroadcastAudience(in.Audience) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, err := json.Marshal(map[string]string{
		"audience": strings.TrimSpace(in.Audience),
		"title_ar": in.TitleAr,
		"title_en": in.TitleEn,
		"body_ar":  in.BodyAr,
		"body_en":  in.BodyEn,
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	p.relayNotif(w, r, token, upstreamCall{
		route: "broadcasts.create", method: http.MethodPost, path: "/internal/admin/broadcasts", body: body,
	})
}

// BroadcastsList handles GET /api/broadcasts?page=&limit= ->
// GET notification /internal/admin/broadcasts.
func (p *Proxy) BroadcastsList(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	in := r.URL.Query()
	q := url.Values{}
	if !copyBoundedInt(q, in, "page", 1, maxPage) || !copyBoundedInt(q, in, "limit", 1, maxLimit) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayNotif(w, r, token, upstreamCall{
		route: "broadcasts.list", method: http.MethodGet, path: "/internal/admin/broadcasts", query: q,
	})
}

// BroadcastsRetry handles POST /api/broadcasts/retry {id} ->
// POST notification /internal/admin/broadcasts/{id}/retry.
func (p *Proxy) BroadcastsRetry(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if !decodeStrict(w, r, &in) {
		return
	}
	if !validBroadcastID(strings.TrimSpace(in.ID)) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	id := strings.TrimSpace(in.ID)
	p.relayNotif(w, r, token, upstreamCall{
		route: "broadcasts.retry", method: http.MethodPost, path: "/internal/admin/broadcasts/" + id + "/retry", targetID: id,
	})
}

// relayNotif performs the call against NOTIFICATION_ADMIN_URL and relays a
// usable answer to the browser.
func (p *Proxy) relayNotif(w http.ResponseWriter, r *http.Request, token string, c upstreamCall) {
	c.notif = true
	status, body, ok := p.call(w, r, token, c)
	if !ok {
		return
	}
	relay(w, status, body)
}
