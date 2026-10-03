// Requests and Entitlements proxy routes (SPEC Phase 4.5/4.6, SPEC 6.3 part 2).
//
// The guarantees match the catalog and account routes: X-Admin-Token is
// required (401 with no upstream call), input is validated locally (400 with
// no upstream call), the upstream request is built from scratch against
// ACADEMY_ADMIN_URL over mTLS, and upstream failures become a safe 503.
// Upstream 4xx answers are relayed with their status and JSON body (error
// code included) so the page can map the code to its own text.
package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

var requestStatusFilters = map[string]bool{"pending": true, "accepted": true, "rejected": true}

// RequestsList handles GET /api/requests?status&subject_id&page&limit ->
// GET academy /internal/admin/requests.
func (p *Proxy) RequestsList(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	in := r.URL.Query()
	q := url.Values{}

	if status := strings.TrimSpace(in.Get("status")); status != "" {
		if !requestStatusFilters[status] {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		q.Set("status", status)
	}
	if subjectID := strings.TrimSpace(in.Get("subject_id")); subjectID != "" {
		if !validCatalogID(subjectID) {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		q.Set("subject_id", subjectID)
	}
	if !copyBoundedInt(q, in, "page", 1, maxPage) || !copyBoundedInt(q, in, "limit", 1, maxLimit) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "requests.list", method: http.MethodGet, path: "/internal/admin/requests", query: q,
	})
}

// requestAcceptRequest is the body of POST /api/requests/accept {id}.
type requestAcceptRequest struct {
	ID string `json:"id"`
}

// RequestsAccept handles POST /api/requests/accept {id} ->
// POST academy /internal/admin/requests/{id}/accept.
func (p *Proxy) RequestsAccept(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in requestAcceptRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if !validCatalogID(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "requests.accept", method: http.MethodPost,
		path: "/internal/admin/requests/" + in.ID + "/accept", targetID: in.ID,
	})
}

// reasonActionRequest is the body of actions taking an id and a mandatory reason.
type reasonActionRequest struct {
	ID     string          `json:"id"`
	Reason json.RawMessage `json:"reason"`
}

func parseReason(rawMsg json.RawMessage) (string, bool) {
	raw := bytes.TrimSpace(rawMsg)
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return CleanReason(s)
}

// RequestsReject handles POST /api/requests/reject {id, reason} ->
// POST academy /internal/admin/requests/{id}/reject {reason}.
func (p *Proxy) RequestsReject(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in reasonActionRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if !validCatalogID(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	reason, ok := parseReason(in.Reason)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, err := json.Marshal(struct {
		Reason string `json:"reason"`
	}{reason})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "requests.reject", method: http.MethodPost,
		path: "/internal/admin/requests/" + in.ID + "/reject", body: body, targetID: in.ID,
	})
}

// EntitlementsList handles GET /api/entitlements?user_id= ->
// GET academy /internal/admin/entitlements?user_id=.
func (p *Proxy) EntitlementsList(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if !idPattern.MatchString(userID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	q := url.Values{}
	q.Set("user_id", userID)
	p.relayAcademy(w, r, token, upstreamCall{
		route: "entitlements.list", method: http.MethodGet,
		path: "/internal/admin/entitlements", query: q, targetID: userID,
	})
}

// entitlementGrantRequest is the body of POST /api/entitlements/grant.
type entitlementGrantRequest struct {
	UserID    string `json:"user_id"`
	SubjectID string `json:"subject_id"`
}

// EntitlementsGrant handles POST /api/entitlements/grant {user_id, subject_id} ->
// POST academy /internal/admin/entitlements {user_id, subject_id}.
func (p *Proxy) EntitlementsGrant(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in entitlementGrantRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	in.UserID = strings.TrimSpace(in.UserID)
	in.SubjectID = strings.TrimSpace(in.SubjectID)
	if !idPattern.MatchString(in.UserID) || !validCatalogID(in.SubjectID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, err := json.Marshal(in)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "entitlements.grant", method: http.MethodPost,
		path: "/internal/admin/entitlements", body: body, targetID: in.SubjectID,
	})
}

// EntitlementsRevoke handles POST /api/entitlements/revoke {id, reason} ->
// DELETE academy /internal/admin/entitlements/{id} {reason}.
func (p *Proxy) EntitlementsRevoke(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in reasonActionRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if !validCatalogID(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	reason, ok := parseReason(in.Reason)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, err := json.Marshal(struct {
		Reason string `json:"reason"`
	}{reason})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "entitlements.revoke", method: http.MethodDelete,
		path: "/internal/admin/entitlements/" + in.ID, body: body, targetID: in.ID,
	})
}
