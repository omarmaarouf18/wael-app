package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func createVideo(t *testing.T, s *Server, subjectID string, body map[string]any) models.VideoAdminDTO {
	t.Helper()
	rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subjectID+"/videos", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var dto models.VideoAdminDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode video: %v", err)
	}
	if dto.ID == "" {
		t.Fatalf("expected server-generated id")
	}
	return dto
}

func baseVideoBody() map[string]any {
	return map[string]any{
		"title_ar": "المحاضرة الأولى",
		"youtube":  "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	}
}

func TestAdminVideos_AuthWiring(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	req := httptest.NewRequest(http.MethodGet, "/internal/admin/subjects/subj-1/videos", nil)
	rec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without tokens, got %d", rec.Code)
	}
}

func TestAdminVideos_CreateValidation(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))

	t.Run("bad_youtube_rejected", func(t *testing.T) {
		for _, yt := range []string{"", "short", "https://vimeo.com/12345678901", "https://www.youtube.com/watch", "not a url"} {
			body := baseVideoBody()
			body["youtube"] = yt
			rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/videos", body)
			if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_youtube_id" {
				t.Fatalf("youtube %q: expected 400 invalid_youtube_id, got %d %q", yt, rec.Code, adminCode(t, rec))
			}
		}
	})

	t.Run("empty_title_rejected", func(t *testing.T) {
		body := baseVideoBody()
		body["title_ar"] = "  "
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/videos", body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_title" {
			t.Fatalf("expected 400 invalid_title, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("negative_duration_rejected", func(t *testing.T) {
		body := baseVideoBody()
		body["duration_seconds"] = -10
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/videos", body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_duration_seconds" {
			t.Fatalf("expected 400 invalid_duration_seconds, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("unknown_subject_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/subj-missing-1/videos", baseVideoBody())
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
			t.Fatalf("expected 404 subject_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("malformed_subject_id_400", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/bad$id!/videos", baseVideoBody())
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_subject_id" {
			t.Fatalf("expected 400 invalid_subject_id, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("defaults_shape_and_id_storage", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/videos", map[string]any{
			"title_ar": "محاضرة", "youtube": "https://youtu.be/AAAAAAAAAAA",
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
		}
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		for _, k := range []string{"id", "subject_id", "title_ar", "youtube_video_id", "order", "duration_seconds", "published", "created_at", "updated_at"} {
			if _, ok := raw[k]; !ok {
				t.Fatalf("create response missing %q: %s", k, rec.Body.String())
			}
		}
		var dto models.VideoAdminDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &dto)
		if dto.YouTubeVideoID != "AAAAAAAAAAA" {
			t.Fatalf("stored id = %q, want only the validated id", dto.YouTubeVideoID)
		}
		if dto.Order != 1 || dto.DurationSeconds != 0 || !dto.Published {
			t.Fatalf("defaults wrong: %+v", dto)
		}

		// Second video appends after the first.
		v2 := createVideo(t, s, subj.ID, baseVideoBody())
		if v2.Order != 2 {
			t.Fatalf("second video order = %d, want 2", v2.Order)
		}
	})
}

func TestAdminVideos_ListAndPatch(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	v1 := createVideo(t, s, subj.ID, baseVideoBody())

	t.Run("list_includes_youtube_id", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects/"+subj.ID+"/videos", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var res struct {
			Videos []models.VideoAdminDTO `json:"videos"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if len(res.Videos) != 1 || res.Videos[0].YouTubeVideoID != "dQw4w9WgXcQ" {
			t.Fatalf("admin list must include the YouTube id: %+v", res)
		}
	})

	t.Run("patch_fields", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/videos/"+v1.ID, map[string]any{
			"title_ar": "محاضرة محدثة", "youtube": "BBBBBBBBBBB", "order": 5, "duration_seconds": 3600,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got models.VideoAdminDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got.TitleAr != "محاضرة محدثة" || got.YouTubeVideoID != "BBBBBBBBBBB" || got.Order != 5 || got.DurationSeconds != 3600 {
			t.Fatalf("patch not applied: %+v", got)
		}
	})

	t.Run("patch_bad_youtube_400", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/videos/"+v1.ID, map[string]any{"youtube": "nope"})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_youtube_id" {
			t.Fatalf("expected 400 invalid_youtube_id, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("patch_unknown_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/videos/vid-missing-1", map[string]any{"title_ar": "x"})
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "video_not_found" {
			t.Fatalf("expected 404 video_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("patch_malformed_400", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/videos/bad$id!", map[string]any{"title_ar": "x"})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_video_id" {
			t.Fatalf("expected 400 invalid_video_id, got %d %q", rec.Code, adminCode(t, rec))
		}
	})
}

func TestAdminVideos_Reorder(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	v1 := createVideo(t, s, subj.ID, baseVideoBody())
	v2body := baseVideoBody()
	v2body["youtube"] = "BBBBBBBBBBB"
	v2 := createVideo(t, s, subj.ID, v2body)
	v3body := baseVideoBody()
	v3body["youtube"] = "CCCCCCCCCCC"
	v3 := createVideo(t, s, subj.ID, v3body)

	otherSubj := createSubject(t, s, baseSubjectBody("bachelor-y2"))
	foreign := createVideo(t, s, otherSubj.ID, baseVideoBody())

	reorder := func(ids []string) *httptest.ResponseRecorder {
		return doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/videos/reorder", map[string]any{"video_ids": ids})
	}

	t.Run("missing_extra_foreign_duplicate_rejected", func(t *testing.T) {
		cases := [][]string{
			{v1.ID, v2.ID},                 // missing v3
			{v1.ID, v2.ID, v3.ID, "xxxxx"}, // extra (well-formed but unknown)
			{v1.ID, v2.ID, foreign.ID},     // foreign
			{v1.ID, v1.ID, v3.ID},          // duplicate
		}
		for i, ids := range cases {
			rec := reorder(ids)
			if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_video_order" {
				t.Fatalf("case %d: expected 400 invalid_video_order, got %d %q", i, rec.Code, adminCode(t, rec))
			}
		}
	})

	t.Run("happy_path_reorders", func(t *testing.T) {
		rec := reorder([]string{v3.ID, v1.ID, v2.ID})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var res struct {
			Videos []models.VideoAdminDTO `json:"videos"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if len(res.Videos) != 3 || res.Videos[0].ID != v3.ID || res.Videos[1].ID != v1.ID || res.Videos[2].ID != v2.ID {
			t.Fatalf("order wrong: %+v", res.Videos)
		}
		for i, v := range res.Videos {
			if v.Order != i+1 {
				t.Fatalf("video %s order = %d, want %d", v.ID, v.Order, i+1)
			}
		}
	})

	t.Run("unknown_subject_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/subj-missing-1/videos/reorder", map[string]any{"video_ids": []string{}})
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
			t.Fatalf("expected 404 subject_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})
}

func TestAdminVideos_Delete(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	v1 := createVideo(t, s, subj.ID, baseVideoBody())
	v2body := baseVideoBody()
	v2body["youtube"] = "BBBBBBBBBBB"
	v2 := createVideo(t, s, subj.ID, v2body)

	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/publish", nil); rec.Code != http.StatusOK {
		t.Fatalf("publish setup: %d", rec.Code)
	}

	owner, other := "user-del-owner", "user-del-other"
	ownerTok, otherTok := makeStudentToken(t, owner), makeStudentToken(t, other)
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: owner, SubjectID: subj.ID}); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	play := func(tok, vid string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/"+vid+"/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(rec, req)
		return rec
	}
	detailHas := func(vid string) bool {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/"+subj.ID, nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerTok)
		rec := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(rec, req)
		return strings.Contains(rec.Body.String(), vid)
	}

	t.Run("delete_hides_from_students_and_play", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/videos/"+v1.ID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := play(ownerTok, v1.ID); got.Code != http.StatusNotFound {
			t.Fatalf("deleted video /play must be 404, got %d", got.Code)
		}
		if detailHas(v1.ID) {
			t.Fatalf("deleted video must be hidden from student detail")
		}
		if got := play(ownerTok, v2.ID); got.Code != http.StatusOK {
			t.Fatalf("remaining video /play must be 200, got %d", got.Code)
		}
		// Subject stays published: one video remains.
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/"+subj.ID, nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+otherTok)
		recS := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(recS, req)
		if recS.Code != http.StatusOK {
			t.Fatalf("subject must stay published, got %d", recS.Code)
		}
	})

	t.Run("last_video_of_published_needs_force", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/videos/"+v2.ID, nil)
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "last_video_of_published_subject" {
			t.Fatalf("expected 409 last_video_of_published_subject, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("force_unpublishes_subject", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/videos/"+v2.ID+"?force=true", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := play(ownerTok, v2.ID); got.Code != http.StatusNotFound {
			t.Fatalf("force-deleted video /play must be 404, got %d", got.Code)
		}
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/"+subj.ID, nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerTok)
		recS := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(recS, req)
		if recS.Code != http.StatusNotFound {
			t.Fatalf("subject must be unpublished after force delete, got %d", recS.Code)
		}
	})

	t.Run("delete_missing_and_deleted_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/videos/vid-missing-1", nil)
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "video_not_found" {
			t.Fatalf("expected 404 video_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
		recAgain := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/videos/"+v1.ID, nil)
		if recAgain.Code != http.StatusNotFound {
			t.Fatalf("re-deleting must be 404, got %d", recAgain.Code)
		}
	})
}

func TestAdminVideos_StudentGatingAndLeaks(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()
	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	secretVisible := "dQw4w9WgXcQ"
	secretDeleted := "D3l3t3dV1d0"
	vVis := createVideo(t, s, subj.ID, baseVideoBody())
	vDelBody := baseVideoBody()
	vDelBody["youtube"] = secretDeleted
	vDel := createVideo(t, s, subj.ID, vDelBody)
	_ = vVis

	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/publish", nil); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d", rec.Code)
	}
	owner := "user-gate-owner"
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: owner, SubjectID: subj.ID}); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/videos/"+vDel.ID, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}

	ownerTok := makeStudentToken(t, owner)
	otherTok := makeStudentToken(t, "user-gate-stranger")
	studentGet := func(tok, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(rec, req)
		return rec
	}

	// Detail for the owner: no youtube ids anywhere, deleted video absent.
	detail := studentGet(ownerTok, "/academy/subjects/"+subj.ID)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail: %d", detail.Code)
	}
	for _, forbidden := range []string{"youtube_video_id", secretVisible, secretDeleted} {
		if strings.Contains(detail.Body.String(), forbidden) {
			t.Fatalf("LEAK in owned detail: contains %q", forbidden)
		}
	}
	if strings.Contains(detail.Body.String(), vDel.ID) {
		t.Fatalf("deleted video id must be absent from student detail")
	}

	// List: same guarantees.
	list := studentGet(otherTok, "/academy/subjects")
	for _, forbidden := range []string{"youtube_video_id", secretVisible, secretDeleted} {
		if strings.Contains(list.Body.String(), forbidden) {
			t.Fatalf("LEAK in list: contains %q", forbidden)
		}
	}

	// /play: owner gets the id for the live video only.
	playReq := func(tok, vid string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/"+vid+"/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(rec, req)
		return rec
	}
	if rec := playReq(ownerTok, vDel.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted /play must be 404 even for owner, got %d", rec.Code)
	}
	if rec := playReq(otherTok, vDel.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted /play must be 404 for stranger, got %d", rec.Code)
	}
	recPlay := playReq(ownerTok, vVis.ID)
	if recPlay.Code != http.StatusOK {
		t.Fatalf("live /play for owner must be 200, got %d", recPlay.Code)
	}
	if !strings.Contains(recPlay.Body.String(), secretVisible) {
		t.Fatalf("/play must release the id to the entitled owner: %s", recPlay.Body.String())
	}
	if rec := playReq(otherTok, vVis.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("unowned /play must be 404, got %d", rec.Code)
	}
}

func TestAdminVideos_AuditPerMutation(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()

	subj := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	v1 := createVideo(t, s, subj.ID, baseVideoBody())
	if rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/videos/"+v1.ID, map[string]any{"order": 9}); rec.Code != http.StatusOK {
		t.Fatalf("patch: %d", rec.Code)
	}
	v2body := baseVideoBody()
	v2body["youtube"] = "BBBBBBBBBBB"
	v2 := createVideo(t, s, subj.ID, v2body)
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+subj.ID+"/videos/reorder", map[string]any{"video_ids": []string{v2.ID, v1.ID}}); rec.Code != http.StatusOK {
		t.Fatalf("reorder: %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/videos/"+v1.ID, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}

	logs, total, err := s.Store.ListAuditLogs(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	// subject_create + 2 video_create + video_update + video_reorder + video_delete = 6.
	if total != 6 {
		t.Fatalf("expected 6 audit rows, got %d", total)
	}
	actions := map[string]int{}
	for _, l := range logs {
		actions[l.Action]++
	}
	for action, want := range map[string]int{"subject_create": 1, "video_create": 2, "video_update": 1, "video_reorder": 1, "video_delete": 1} {
		if actions[action] != want {
			t.Fatalf("audit actions = %v, want %s x%d", actions, action, want)
		}
	}
}
