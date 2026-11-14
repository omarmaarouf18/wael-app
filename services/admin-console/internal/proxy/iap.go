// iOS in-app purchase package routes (issue #56, ADR-0015) over the academy
// admin API added in #53 (docs/contracts/iap-packages-api.md section 5).
//
// Same guarantees as the catalog routes in catalog.go: X-Admin-Token is
// required (401 with no upstream call), input is validated locally (400 with
// no upstream call), the upstream request is built from scratch against
// ACADEMY_ADMIN_URL over mTLS, bodies are capped at 1 MiB, upstream failures
// become a safe 503 and upstream 4xx answers are relayed with their JSON body
// ({"error","code","request_id"}) so the page can map the code to its text.
//
// Routes follow the console's allowlist style (exact paths, id in the body):
//
//	GET  /api/iap/packages          -> GET    /internal/admin/iap/packages
//	POST /api/iap/packages/create   -> POST   /internal/admin/iap/packages
//	POST /api/iap/packages/update   -> PATCH  /internal/admin/iap/packages/{id}
//	POST /api/iap/packages/delete   -> DELETE /internal/admin/iap/packages/{id}
//	GET  /api/iap/purchases         -> GET    /internal/admin/iap/purchases
//	GET  /api/iap/coverage          -> GET    /internal/admin/iap/coverage
//
// Validation mirrors admin_iap.go: product_id pattern, titles 1-80 runes,
// descriptions at most 1000 runes, at most 100 unique well-formed subject ids,
// available_until RFC3339 or null. product_id is accepted on create only; an
// update carrying it is refused locally (strict decode, unknown field).
package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// iapProductIDPattern is the academy pattern (admin_iap.go). Package ids are
// "pkg_" plus hex and match catalogIDPattern.
var iapProductIDPattern = regexp.MustCompile(`^com\.wael\.app\.pkg\.[a-z0-9_.]+$`)

const (
	maxIAPTitleRunes = 80
	maxIAPDescRunes  = 1000
	maxIAPSubjects   = 100
	maxIAPProductID  = 200
)

// iapPackageWrite is the body of POST /api/iap/packages/create and
// POST /api/iap/packages/update. AvailableUntil stays raw so an update can
// tell "absent" (keep) from null (clear).
type iapPackageWrite struct {
	TitleAr        *string         `json:"title_ar"`
	TitleEn        *string         `json:"title_en"`
	DescriptionAr  *string         `json:"description_ar"`
	DescriptionEn  *string         `json:"description_en"`
	SubjectIDs     *[]string       `json:"subject_ids"`
	AvailableUntil json.RawMessage `json:"available_until"`
	Published      *bool           `json:"published"`
	Position       *int            `json:"position"`
}

type iapPackageCreate struct {
	ProductID string `json:"product_id"`
	iapPackageWrite
}

type iapPackageUpdate struct {
	ID string `json:"id"`
	iapPackageWrite
}

// cleanIAPTitle mirrors the academy: trim, drop CR/LF, 1-80 runes.
func cleanIAPTitle(raw string) (string, bool) {
	s := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), "\r", ""), "\n", "")
	if s == "" || utf8.RuneCountInString(s) > maxIAPTitleRunes {
		return "", false
	}
	return s, true
}

// validIAPProductID reports whether s is an App Store product id the academy
// accepts. The length cap only bounds the regexp input.
func validIAPProductID(s string) bool {
	return len(s) <= maxIAPProductID && iapProductIDPattern.MatchString(s)
}

// buildIAPPackageBody validates the shared write fields into out. On create
// both titles are required.
func buildIAPPackageBody(in iapPackageWrite, create bool, out map[string]any) bool {
	for _, field := range []struct {
		raw *string
		key string
	}{
		{in.TitleAr, "title_ar"},
		{in.TitleEn, "title_en"},
	} {
		if field.raw == nil {
			if create {
				return false
			}
			continue
		}
		title, ok := cleanIAPTitle(*field.raw)
		if !ok {
			return false
		}
		out[field.key] = title
	}
	for _, field := range []struct {
		raw *string
		key string
	}{
		{in.DescriptionAr, "description_ar"},
		{in.DescriptionEn, "description_en"},
	} {
		if field.raw != nil {
			text, ok := cleanCatalogText(*field.raw, maxIAPDescRunes)
			if !ok {
				return false
			}
			out[field.key] = text
		}
	}
	if in.SubjectIDs != nil {
		ids := *in.SubjectIDs
		if ids == nil || len(ids) > maxIAPSubjects {
			return false
		}
		seen := make(map[string]bool, len(ids))
		for _, id := range ids {
			if !validCatalogID(id) || seen[id] {
				return false
			}
			seen[id] = true
		}
		out["subject_ids"] = ids
	}
	if in.AvailableUntil != nil {
		raw := bytes.TrimSpace(in.AvailableUntil)
		if bytes.Equal(raw, []byte("null")) {
			out["available_until"] = nil
		} else {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return false
			}
			ts, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
			if err != nil {
				return false
			}
			out["available_until"] = ts.UTC().Format(time.RFC3339)
		}
	}
	if in.Published != nil {
		out["published"] = *in.Published
	}
	if in.Position != nil {
		out["position"] = *in.Position
	}
	return true
}

// IAPPackagesList handles GET /api/iap/packages -> GET academy
// /internal/admin/iap/packages. No query is forwarded.
func (p *Proxy) IAPPackagesList(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "iap.packages.list", method: http.MethodGet, path: "/internal/admin/iap/packages",
	})
}

// IAPPackagesCreate handles POST /api/iap/packages/create -> POST academy
// /internal/admin/iap/packages.
func (p *Proxy) IAPPackagesCreate(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in iapPackageCreate
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	productID := strings.TrimSpace(in.ProductID)
	if !validIAPProductID(productID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	out := map[string]any{"product_id": productID}
	if !buildIAPPackageBody(in.iapPackageWrite, true, out) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, err := json.Marshal(out)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "iap.packages.create", method: http.MethodPost, path: "/internal/admin/iap/packages", body: body,
	})
}

// IAPPackagesUpdate handles POST /api/iap/packages/update {id, ...} -> PATCH
// academy /internal/admin/iap/packages/{id}. product_id is not a field here:
// it is immutable upstream and refused locally.
func (p *Proxy) IAPPackagesUpdate(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in iapPackageUpdate
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if !validCatalogID(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	out := map[string]any{}
	if !buildIAPPackageBody(in.iapPackageWrite, false, out) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, err := json.Marshal(out)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "iap.packages.update", method: http.MethodPatch,
		path: "/internal/admin/iap/packages/" + in.ID, body: body, targetID: in.ID,
	})
}

// IAPPackagesDelete handles POST /api/iap/packages/delete {id} -> DELETE
// academy /internal/admin/iap/packages/{id}. A package with purchases is
// refused upstream (409 package_has_purchases), relayed to the page.
func (p *Proxy) IAPPackagesDelete(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in catalogIDBody
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if !validCatalogID(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "iap.packages.delete", method: http.MethodDelete,
		path: "/internal/admin/iap/packages/" + in.ID, targetID: in.ID,
	})
}

// IAPPurchases handles GET /api/iap/purchases?package_id&user_id&limit ->
// GET academy /internal/admin/iap/purchases. Only the three known
// parameters are read, each validated.
func (p *Proxy) IAPPurchases(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	in := r.URL.Query()
	q := url.Values{}
	if packageID := strings.TrimSpace(in.Get("package_id")); packageID != "" {
		if !validCatalogID(packageID) {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		q.Set("package_id", packageID)
	}
	if userID := strings.TrimSpace(in.Get("user_id")); userID != "" {
		if !idPattern.MatchString(userID) {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		q.Set("user_id", userID)
	}
	if !copyBoundedInt(q, in, "limit", 1, maxLimit) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "iap.purchases", method: http.MethodGet, path: "/internal/admin/iap/purchases", query: q,
	})
}

// IAPCoverage handles GET /api/iap/coverage -> GET academy
// /internal/admin/iap/coverage (published subjects in no published package).
func (p *Proxy) IAPCoverage(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "iap.coverage", method: http.MethodGet, path: "/internal/admin/iap/coverage",
	})
}
