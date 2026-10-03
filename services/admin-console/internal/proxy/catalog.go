// Catalog proxy routes (SPEC 6.3 part 1): diplomas/levels, subjects and videos
// over the academy-service admin API (Phase 4.2-4.4, ADR-0012).
//
// The guarantees match the 6.1 account routes in proxy.go: X-Admin-Token is
// required (401 with no upstream call), input is validated locally (400 with
// no upstream call), the upstream request is built from scratch against
// ACADEMY_ADMIN_URL over mTLS, and upstream failures become a safe 503.
// Upstream 4xx answers are relayed with their status and JSON body (error
// code included) so the page can map the code to its own text.
//
// Validation mirrors the academy limits (admin_validate.go, admin_levels.go,
// admin_subjects.go, admin_videos.go): only the fields the upstream accepts
// are forwarded; client headers never are.
package proxy

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// Academy ids: level keys ("diploma-<hex>", seeded fixed keys) and
	// subject/video UUIDs all match [A-Za-z0-9_-]{1,100} (admin_validate.go).
	catalogIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

	catalogTerms = map[string]bool{"": true, "first": true, "second": true}

	catalogPublishedFilters = map[string]bool{"": true, "true": true, "1": true, "false": true, "0": true}

	catalogStudyTypes = map[string]bool{"bachelor": true, "diploma": true, "vocational": true}
)

const (
	maxCatalogNameRunes  = 200
	maxCatalogDescRunes  = 5000
	maxVideoIDs          = 1000
	maxYouTubeInputBytes = 4096
)

// validCatalogID reports whether s is a well-formed level, subject or video
// id. Checked before any upstream call; malformed ids return 400.
func validCatalogID(s string) bool {
	return catalogIDPattern.MatchString(s)
}

// cleanCatalogName mirrors cleanAdminName: trim, drop CR/LF, 1-200 runes.
func cleanCatalogName(raw string) (string, bool) {
	s := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), "\r", ""), "\n", "")
	if s == "" {
		return "", false
	}
	if utf8.RuneCountInString(s) > maxCatalogNameRunes {
		return "", false
	}
	return s, true
}

// cleanCatalogText mirrors cleanAdminText: trim, CR/LF become spaces, at most
// maxRunes (empty is allowed; the caller decides whether empty is valid).
func cleanCatalogText(raw string, maxRunes int) (string, bool) {
	s := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), "\r", " "), "\n", " ")
	if utf8.RuneCountInString(s) > maxRunes {
		return "", false
	}
	return s, true
}

// validCatalogExpiresAt mirrors parseAccessExpiresAt without the futureness
// check: the timestamp must be RFC3339. Whether it still lies in the future
// when the academy reads it is the academy's call (relayed as 400
// invalid_expires_at).
func validCatalogExpiresAt(raw string) bool {
	_, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	return err == nil
}

// decodeCatalogBody reads a size-capped, strict JSON object into dst.
// Invalid UTF-8, unknown fields and trailing data are refused.
func decodeCatalogBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeStrict(w, r, dst)
}

// ---------------------------------------------------------------------------
// Levels
// ---------------------------------------------------------------------------

// LevelsList handles GET /api/levels -> GET academy /internal/admin/levels.
func (p *Proxy) LevelsList(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "levels.list", method: http.MethodGet, path: "/internal/admin/levels",
	})
}

// levelWriteRequest is the body of POST /api/levels/create and
// POST /api/levels/update. Every field except the id is optional on update.
type levelWriteRequest struct {
	ID        string  `json:"id"`
	StudyType *string `json:"study_type"`
	NameAr    *string `json:"name_ar"`
	NameEn    *string `json:"name_en"`
	Order     *int    `json:"order"`
	Published *bool   `json:"published"`
}

// LevelsCreate handles POST /api/levels/create -> POST academy
// /internal/admin/levels. Only diplomas can be created (study_type must be
// "diploma"); the key is server-generated and published defaults to false.
func (p *Proxy) LevelsCreate(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in levelWriteRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if in.ID != "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, ok := buildLevelBody(in, true)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "levels.create", method: http.MethodPost, path: "/internal/admin/levels", body: body,
	})
}

// LevelsUpdate handles POST /api/levels/update {id, ...} -> PATCH academy
// /internal/admin/levels/{id}. Seeded levels accept name/order/published;
// study_type is immutable upstream.
func (p *Proxy) LevelsUpdate(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in levelWriteRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if !validCatalogID(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, ok := buildLevelBody(in, false)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "levels.update", method: http.MethodPatch,
		path: "/internal/admin/levels/" + in.ID, body: body, targetID: in.ID,
	})
}

// buildLevelBody validates the write fields and returns the upstream body
// with only the fields the academy accepts. On create, study_type must be
// "diploma" and name_ar is required.
func buildLevelBody(in levelWriteRequest, create bool) ([]byte, bool) {
	out := map[string]any{}
	if create {
		if in.StudyType == nil || strings.TrimSpace(*in.StudyType) != "diploma" {
			return nil, false
		}
		out["study_type"] = "diploma"
		if in.NameAr == nil {
			return nil, false
		}
	} else if in.StudyType != nil {
		studyType := strings.TrimSpace(*in.StudyType)
		if studyType == "" || !catalogStudyTypes[studyType] {
			return nil, false
		}
		out["study_type"] = studyType
	}
	if in.NameAr != nil {
		nameAr, ok := cleanCatalogName(*in.NameAr)
		if !ok {
			return nil, false
		}
		out["name_ar"] = nameAr
	} else if create {
		return nil, false
	}
	if in.NameEn != nil {
		if strings.TrimSpace(*in.NameEn) == "" {
			if !create {
				out["name_en"] = ""
			}
		} else {
			nameEn, ok := cleanCatalogName(*in.NameEn)
			if !ok {
				return nil, false
			}
			out["name_en"] = nameEn
		}
	}
	if in.Order != nil {
		out["order"] = *in.Order
	}
	if in.Published != nil {
		out["published"] = *in.Published
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return b, true
}

// LevelsDelete handles POST /api/levels/delete {id} -> DELETE academy
// /internal/admin/levels/{id}. Only an empty diploma can be deleted upstream.
func (p *Proxy) LevelsDelete(w http.ResponseWriter, r *http.Request) {
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
		route: "levels.delete", method: http.MethodDelete,
		path: "/internal/admin/levels/" + in.ID, targetID: in.ID,
	})
}

// catalogIDBody is the body of the id-only actions (level delete, subject
// publish/unpublish).
type catalogIDBody struct {
	ID string `json:"id"`
}

// ---------------------------------------------------------------------------
// Subjects
// ---------------------------------------------------------------------------

// SubjectsList handles GET /api/subjects?level_id&published&page&limit ->
// GET academy /internal/admin/subjects. Only the four known parameters are
// read, each validated.
func (p *Proxy) SubjectsList(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	in := r.URL.Query()
	q := url.Values{}
	if levelID := strings.TrimSpace(in.Get("level_id")); levelID != "" {
		if !validCatalogID(levelID) {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		q.Set("level_id", levelID)
	}
	if raw := strings.TrimSpace(in.Get("published")); raw != "" {
		if !catalogPublishedFilters[strings.ToLower(raw)] {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		q.Set("published", raw)
	}
	if !copyBoundedInt(q, in, "page", 1, maxPage) || !copyBoundedInt(q, in, "limit", 1, maxLimit) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "subjects.list", method: http.MethodGet, path: "/internal/admin/subjects", query: q,
	})
}

// subjectWriteRequest is the body of POST /api/subjects/create and
// POST /api/subjects/update. Every field except the id is optional on
// update; on create level_id, title_ar and access_expires_at are required.
type subjectWriteRequest struct {
	ID              string  `json:"id"`
	LevelID         *string `json:"level_id"`
	TitleAr         *string `json:"title_ar"`
	TitleEn         *string `json:"title_en"`
	DescriptionAr   *string `json:"description_ar"`
	DescriptionEn   *string `json:"description_en"`
	Term            *string `json:"term"`
	Price           *int    `json:"price"`
	AccessExpiresAt *string `json:"access_expires_at"`
	Order           *int    `json:"order"`
	Published       *bool   `json:"published"`
}

// SubjectsCreate handles POST /api/subjects/create -> POST academy
// /internal/admin/subjects.
func (p *Proxy) SubjectsCreate(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in subjectWriteRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if in.ID != "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, ok := buildSubjectBody(in, true)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "subjects.create", method: http.MethodPost, path: "/internal/admin/subjects", body: body,
	})
}

// SubjectsUpdate handles POST /api/subjects/update {id, ...} -> PATCH
// academy /internal/admin/subjects/{id}. Moving a subject to another level
// is level_id in the same dialog.
func (p *Proxy) SubjectsUpdate(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in subjectWriteRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if !validCatalogID(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, ok := buildSubjectBody(in, false)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "subjects.update", method: http.MethodPatch,
		path: "/internal/admin/subjects/" + in.ID, body: body, targetID: in.ID,
	})
}

// buildSubjectBody validates the write fields and returns the upstream body
// with only the fields the academy accepts.
func buildSubjectBody(in subjectWriteRequest, create bool) ([]byte, bool) {
	out := map[string]any{}
	if in.LevelID != nil {
		levelID := strings.TrimSpace(*in.LevelID)
		if !validCatalogID(levelID) {
			return nil, false
		}
		out["level_id"] = levelID
	} else if create {
		return nil, false
	}
	if in.TitleAr != nil {
		titleAr, ok := cleanCatalogName(*in.TitleAr)
		if !ok {
			return nil, false
		}
		out["title_ar"] = titleAr
	} else if create {
		return nil, false
	}
	if in.TitleEn != nil {
		if strings.TrimSpace(*in.TitleEn) == "" {
			if !create {
				out["title_en"] = ""
			}
		} else {
			titleEn, ok := cleanCatalogName(*in.TitleEn)
			if !ok {
				return nil, false
			}
			out["title_en"] = titleEn
		}
	}
	for _, field := range []struct {
		raw *string
		key string
	}{
		{in.DescriptionAr, "description_ar"},
		{in.DescriptionEn, "description_en"},
	} {
		if field.raw != nil {
			text, ok := cleanCatalogText(*field.raw, maxCatalogDescRunes)
			if !ok {
				return nil, false
			}
			out[field.key] = text
		}
	}
	if in.Term != nil {
		term := strings.TrimSpace(*in.Term)
		if !catalogTerms[term] {
			return nil, false
		}
		out["term"] = term
	}
	if in.Price != nil {
		if *in.Price < 0 {
			return nil, false
		}
		out["price"] = *in.Price
	}
	if in.AccessExpiresAt != nil {
		expires := strings.TrimSpace(*in.AccessExpiresAt)
		if !validCatalogExpiresAt(expires) {
			return nil, false
		}
		out["access_expires_at"] = expires
	} else if create {
		return nil, false
	}
	if in.Order != nil {
		out["order"] = *in.Order
	}
	if in.Published != nil {
		out["published"] = *in.Published
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return b, true
}

// SubjectsPublish handles POST /api/subjects/publish {id} -> POST academy
// .../subjects/{id}/publish. Publishing needs at least one video upstream.
func (p *Proxy) SubjectsPublish(w http.ResponseWriter, r *http.Request) {
	p.subjectStatus(w, r, "subjects.publish", "/publish")
}

// SubjectsUnpublish handles POST /api/subjects/unpublish {id} -> POST
// academy .../subjects/{id}/unpublish.
func (p *Proxy) SubjectsUnpublish(w http.ResponseWriter, r *http.Request) {
	p.subjectStatus(w, r, "subjects.unpublish", "/unpublish")
}

func (p *Proxy) subjectStatus(w http.ResponseWriter, r *http.Request, route, suffix string) {
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
		route: route, method: http.MethodPost,
		path: "/internal/admin/subjects/" + in.ID + suffix, targetID: in.ID,
	})
}

// ---------------------------------------------------------------------------
// Videos
// ---------------------------------------------------------------------------

// VideosList handles GET /api/videos?subject_id -> GET academy
// .../subjects/{id}/videos.
func (p *Proxy) VideosList(w http.ResponseWriter, r *http.Request) {
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
		route: "videos.list", method: http.MethodGet,
		path: "/internal/admin/subjects/" + subjectID + "/videos", targetID: subjectID,
	})
}

// videoCreateRequest is the body of POST /api/videos/create.
type videoCreateRequest struct {
	SubjectID       string `json:"subject_id"`
	TitleAr         string `json:"title_ar"`
	YouTube         string `json:"youtube"`
	Order           *int   `json:"order"`
	DurationSeconds *int   `json:"duration_seconds"`
}

// VideosCreate handles POST /api/videos/create {subject_id, ...} -> POST
// academy .../subjects/{id}/videos.
func (p *Proxy) VideosCreate(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in videoCreateRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if !validCatalogID(strings.TrimSpace(in.SubjectID)) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	subjectID := strings.TrimSpace(in.SubjectID)
	body, ok := buildVideoBody(in.TitleAr, in.YouTube, in.Order, in.DurationSeconds)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "videos.create", method: http.MethodPost,
		path: "/internal/admin/subjects/" + subjectID + "/videos", body: body, targetID: subjectID,
	})
}

// videoUpdateRequest is the body of POST /api/videos/update.
type videoUpdateRequest struct {
	ID              string  `json:"id"`
	TitleAr         *string `json:"title_ar"`
	YouTube         *string `json:"youtube"`
	Order           *int    `json:"order"`
	DurationSeconds *int    `json:"duration_seconds"`
}

// VideosUpdate handles POST /api/videos/update {id, ...} -> PATCH academy
// /internal/admin/videos/{id}.
func (p *Proxy) VideosUpdate(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in videoUpdateRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if !validCatalogID(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	out := map[string]any{}
	if in.TitleAr != nil {
		titleAr, ok := cleanCatalogName(*in.TitleAr)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		out["title_ar"] = titleAr
	}
	if in.YouTube != nil {
		youtube := strings.TrimSpace(*in.YouTube)
		if youtube == "" || len(youtube) > maxYouTubeInputBytes {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		out["youtube"] = youtube
	}
	if in.Order != nil {
		out["order"] = *in.Order
	}
	if in.DurationSeconds != nil {
		if *in.DurationSeconds < 0 {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		out["duration_seconds"] = *in.DurationSeconds
	}
	body, err := json.Marshal(out)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "videos.update", method: http.MethodPatch,
		path: "/internal/admin/videos/" + in.ID, body: body, targetID: in.ID,
	})
}

// buildVideoBody validates a video create and returns the upstream body.
func buildVideoBody(titleAr, youtube string, order, durationSeconds *int) ([]byte, bool) {
	title, ok := cleanCatalogName(titleAr)
	if !ok {
		return nil, false
	}
	tube := strings.TrimSpace(youtube)
	if tube == "" || len(tube) > maxYouTubeInputBytes {
		return nil, false
	}
	out := map[string]any{"title_ar": title, "youtube": tube}
	if order != nil {
		out["order"] = *order
	}
	if durationSeconds != nil {
		if *durationSeconds < 0 {
			return nil, false
		}
		out["duration_seconds"] = *durationSeconds
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return b, true
}

// videoReorderRequest is the body of POST /api/videos/reorder.
type videoReorderRequest struct {
	SubjectID string   `json:"subject_id"`
	VideoIDs  []string `json:"video_ids"`
}

// VideosReorder handles POST /api/videos/reorder {subject_id, video_ids} ->
// POST academy .../subjects/{id}/videos/reorder. The list must hold every
// video of the subject exactly once upstream; locally the ids must be
// well-formed, unique and bounded.
func (p *Proxy) VideosReorder(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in videoReorderRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	subjectID := strings.TrimSpace(in.SubjectID)
	if !validCatalogID(subjectID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if in.VideoIDs == nil || len(in.VideoIDs) > maxVideoIDs {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	seen := make(map[string]bool, len(in.VideoIDs))
	for _, id := range in.VideoIDs {
		if !validCatalogID(id) || seen[id] {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		seen[id] = true
	}
	body, err := json.Marshal(map[string]any{"video_ids": in.VideoIDs})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "videos.reorder", method: http.MethodPost,
		path: "/internal/admin/subjects/" + subjectID + "/videos/reorder", body: body, targetID: subjectID,
	})
}

// videoDeleteRequest is the body of POST /api/videos/delete.
type videoDeleteRequest struct {
	ID    string `json:"id"`
	Force *bool  `json:"force"`
}

// VideosDelete handles POST /api/videos/delete {id, force} -> DELETE academy
// /internal/admin/videos/{id}[?force=true]. ?force=true is sent only when
// force is true: it also unpublishes the subject upstream.
func (p *Proxy) VideosDelete(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in videoDeleteRequest
	if !decodeCatalogBody(w, r, &in) {
		return
	}
	if !validCatalogID(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	path := "/internal/admin/videos/" + in.ID
	if in.Force != nil && *in.Force {
		path += "?force=true"
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "videos.delete", method: http.MethodDelete, path: path, targetID: in.ID,
	})
}

// ---------------------------------------------------------------------------
// Academy upstream
// ---------------------------------------------------------------------------

// relayAcademy performs the call against ACADEMY_ADMIN_URL and relays a
// usable answer to the browser.
func (p *Proxy) relayAcademy(w http.ResponseWriter, r *http.Request, token string, c upstreamCall) {
	c.academy = true
	status, body, ok := p.call(w, r, token, c)
	if !ok {
		return
	}
	relay(w, status, body)
}
