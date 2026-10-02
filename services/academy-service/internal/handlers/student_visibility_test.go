package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func studentGet(t *testing.T, s *Server, tok, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	return rec
}

func studentPlay(t *testing.T, s *Server, tok, vid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/academy/videos/"+vid+"/play", nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	return rec
}

func listIDs(t *testing.T, rec *httptest.ResponseRecorder) (map[string]models.SubjectListItemDTO, int) {
	t.Helper()
	var res models.SubjectListResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	out := make(map[string]models.SubjectListItemDTO, len(res.Items))
	for _, it := range res.Items {
		out[it.ID] = it
	}
	return out, res.Total
}

// Owners keep seeing an unpublished subject in their owned list/detail and
// /play keeps working; non-owners lose all three.
func TestStudentVisibility_OwnedUnpublished(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	v1 := createVideo(t, s, subj.ID, baseVideoBody())

	ownerTok := makeStudentToken(t, "user-vis-owner")
	strangerTok := makeStudentToken(t, "user-vis-stranger")
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-vis-owner", SubjectID: subj.ID}); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/publish", nil); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d", rec.Code)
	}

	// Sanity while published: both see it, only the owner plays.
	if items, _ := listIDs(t, studentGet(t, s, ownerTok, "/academy/subjects")); !items[subj.ID].Owned {
		t.Fatalf("owner must see owned=true while published")
	}
	if items, _ := listIDs(t, studentGet(t, s, strangerTok, "/academy/subjects")); items[subj.ID].Owned {
		t.Fatalf("stranger must not own")
	}

	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/unpublish", nil); rec.Code != http.StatusOK {
		t.Fatalf("unpublish: %d", rec.Code)
	}

	items, total := listIDs(t, studentGet(t, s, ownerTok, "/academy/subjects"))
	if total != 1 || !items[subj.ID].Owned {
		t.Fatalf("owner list must keep the unpublished subject owned=true, total=%d", total)
	}
	if rec := studentGet(t, s, ownerTok, "/academy/subjects/"+subj.ID); rec.Code != http.StatusOK {
		t.Fatalf("owner detail must be 200 after unpublish, got %d", rec.Code)
	} else if strings.Contains(rec.Body.String(), "youtube_video_id") {
		t.Fatalf("LEAK: owner unpublished detail contains youtube_video_id")
	}
	if rec := studentPlay(t, s, ownerTok, v1.ID); rec.Code != http.StatusOK {
		t.Fatalf("owner /play must be 200 after unpublish, got %d", rec.Code)
	}

	sItems, sTotal := listIDs(t, studentGet(t, s, strangerTok, "/academy/subjects"))
	if sTotal != 0 || len(sItems) != 0 {
		t.Fatalf("stranger list must exclude the unpublished subject, total=%d", sTotal)
	}
	if rec := studentGet(t, s, strangerTok, "/academy/subjects/"+subj.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("stranger detail must be 404, got %d", rec.Code)
	}
	if rec := studentPlay(t, s, strangerTok, v1.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("stranger /play must be 404, got %d", rec.Code)
	}
}

// Once the entitlement expires, the former owner is treated as a non-owner.
func TestStudentVisibility_EntitlementExpiry(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	body := baseSubjectBody("bachelor-y1")
	// Short expiry (RFC3339 has second precision, so 2 s is the practical
	// minimum): the grant copies it, then it lapses mid-test.
	body["access_expires_at"] = time.Now().Add(2 * time.Second).Format(time.RFC3339)
	subj := createSubject(t, s, body)
	v1 := createVideo(t, s, subj.ID, baseVideoBody())
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/publish", nil); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d", rec.Code)
	}
	ownerTok := makeStudentToken(t, "user-exp-owner")
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-exp-owner", SubjectID: subj.ID}); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/unpublish", nil); rec.Code != http.StatusOK {
		t.Fatalf("unpublish: %d", rec.Code)
	}

	if rec := studentGet(t, s, ownerTok, "/academy/subjects/"+subj.ID); rec.Code != http.StatusOK {
		t.Fatalf("owner detail must be 200 before expiry, got %d", rec.Code)
	}
	time.Sleep(2500 * time.Millisecond)

	if items, total := listIDs(t, studentGet(t, s, ownerTok, "/academy/subjects")); total != 0 || len(items) != 0 {
		t.Fatalf("expired owner list must exclude the subject, total=%d", total)
	}
	if rec := studentGet(t, s, ownerTok, "/academy/subjects/"+subj.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("expired owner detail must be 404, got %d", rec.Code)
	}
	if rec := studentPlay(t, s, ownerTok, v1.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("expired owner /play must be 404, got %d", rec.Code)
	}
}

// A published subject under an unpublished level is hidden from non-owners
// but stays available to owners; publishing the level opens it to everyone.
func TestStudentVisibility_UnpublishedLevel(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()

	dip := createDiploma(t, s, map[string]any{"study_type": "diploma", "name_ar": "دبلومة مخفية", "order": 6})
	subj := createSubject(t, s, baseSubjectBody(dip.Key))
	v1 := createVideo(t, s, subj.ID, baseVideoBody())
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/publish", nil); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d", rec.Code)
	}

	ownerTok := makeStudentToken(t, "user-lvl-owner")
	strangerTok := makeStudentToken(t, "user-lvl-stranger")
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-lvl-owner", SubjectID: subj.ID}); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	// Stranger: hidden everywhere.
	if items, total := listIDs(t, studentGet(t, s, strangerTok, "/academy/subjects")); total != 0 || len(items) != 0 {
		t.Fatalf("stranger catalog must hide the subject under an unpublished level, total=%d", total)
	}
	if items, total := listIDs(t, studentGet(t, s, strangerTok, "/academy/subjects?level="+dip.Key)); total != 0 || len(items) != 0 {
		t.Fatalf("stranger level filter must be empty, total=%d", total)
	}
	if rec := studentGet(t, s, strangerTok, "/academy/subjects/"+subj.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("stranger detail must be 404, got %d", rec.Code)
	}
	if rec := studentPlay(t, s, strangerTok, v1.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("stranger /play must be 404, got %d", rec.Code)
	}

	// Owner: full access despite the unpublished level.
	if items, _ := listIDs(t, studentGet(t, s, ownerTok, "/academy/subjects")); !items[subj.ID].Owned {
		t.Fatalf("owner list must contain the subject owned=true")
	}
	if rec := studentGet(t, s, ownerTok, "/academy/subjects/"+subj.ID); rec.Code != http.StatusOK {
		t.Fatalf("owner detail must be 200, got %d", rec.Code)
	}
	if rec := studentPlay(t, s, ownerTok, v1.ID); rec.Code != http.StatusOK {
		t.Fatalf("owner /play must be 200, got %d", rec.Code)
	}

	// Publishing the level opens the catalog to everyone (play still gated).
	if rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/levels/"+dip.Key, map[string]any{"published": true}); rec.Code != http.StatusOK {
		t.Fatalf("publish level: %d", rec.Code)
	}
	if items, total := listIDs(t, studentGet(t, s, strangerTok, "/academy/subjects")); total != 1 || items[subj.ID].ID == "" {
		t.Fatalf("stranger must see the subject after level publish, total=%d", total)
	}
	if rec := studentGet(t, s, strangerTok, "/academy/subjects/"+subj.ID); rec.Code != http.StatusOK {
		t.Fatalf("stranger detail must be 200 after level publish, got %d", rec.Code)
	}
	if rec := studentPlay(t, s, strangerTok, v1.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("stranger /play must stay 404 without entitlement, got %d", rec.Code)
	}
}

// Owned drafts paginate together with the catalog: totals include them and
// they surface with owned=true.
func TestStudentVisibility_OwnedDraftPagination(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()

	p1body := baseSubjectBody("bachelor-y1")
	p1body["title_ar"] = "مادة أولى"
	p1 := createSubject(t, s, p1body)
	p2body := baseSubjectBody("bachelor-y1")
	p2body["title_ar"] = "مادة ثانية"
	p2 := createSubject(t, s, p2body)
	draftBody := baseSubjectBody("bachelor-y1")
	draftBody["title_ar"] = "مادة مسودة مملوكة"
	draft := createSubject(t, s, draftBody)

	ownerTok := makeStudentToken(t, "user-page-owner")
	for _, id := range []string{p1.ID, p2.ID, draft.ID} {
		if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-page-owner", SubjectID: id}); err != nil {
			t.Fatalf("Grant %s: %v", id, err)
		}
	}

	rec := studentGet(t, s, ownerTok, "/academy/subjects?limit=1&page=1")
	_, total := listIDs(t, rec)
	if total != 3 {
		t.Fatalf("total must include the owned draft, got %d", total)
	}
	var res models.SubjectListResponseDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if len(res.Items) != 1 {
		t.Fatalf("page 1 must hold 1 item, got %d", len(res.Items))
	}
	// Walk all pages: every item owned, draft present exactly once.
	seen := map[string]int{}
	for page := 1; page <= 3; page++ {
		rec := studentGet(t, s, ownerTok, "/academy/subjects?limit=1&page="+strconv.Itoa(page))
		var r models.SubjectListResponseDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &r)
		for _, it := range r.Items {
			seen[it.ID]++
			if !it.Owned {
				t.Fatalf("page %d item %s must be owned=true", page, it.ID)
			}
		}
	}
	for _, id := range []string{p1.ID, p2.ID, draft.ID} {
		if seen[id] != 1 {
			t.Fatalf("subject %s seen %dx across pages, want once (seen=%v)", id, seen[id], seen)
		}
	}
}
