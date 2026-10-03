package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func futureExpires() string {
	return time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
}

func createSubject(t *testing.T, s *Server, body map[string]any) SubjectAdminDTO {
	t.Helper()
	rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var dto SubjectAdminDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode subject: %v", err)
	}
	if dto.ID == "" {
		t.Fatalf("expected server-generated id")
	}
	return dto
}

func baseSubjectBody(levelID string) map[string]any {
	term := "first"
	if strings.HasPrefix(levelID, "vocational") {
		term = ""
	}
	return map[string]any{
		"level_id":          levelID,
		"term":              term,
		"title_ar":          "مادة اختبار",
		"description_ar":    "وصف المادة",
		"price":             1800,
		"access_expires_at": futureExpires(),
		"order":             1,
	}
}

func TestAdminSubjects_CreateValidation(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	t.Run("missing_level_id", func(t *testing.T) {
		body := baseSubjectBody("")
		delete(body, "level_id")
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_level_id" {
			t.Fatalf("expected 400 invalid_level_id, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("malformed_level_id", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", baseSubjectBody("bad id!"))
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_level_id" {
			t.Fatalf("expected 400 invalid_level_id, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("unknown_level_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", baseSubjectBody("diploma-missing"))
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "level_not_found" {
			t.Fatalf("expected 404 level_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("missing_title", func(t *testing.T) {
		body := baseSubjectBody("bachelor-y1")
		delete(body, "title_ar")
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_title" {
			t.Fatalf("expected 400 invalid_title, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("empty_and_long_title", func(t *testing.T) {
		for _, title := range []string{"   ", strings.Repeat("أ", 201)} {
			body := baseSubjectBody("bachelor-y1")
			body["title_ar"] = title
			rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
			if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_title" {
				t.Fatalf("title %q: expected 400 invalid_title, got %d %q", title, rec.Code, adminCode(t, rec))
			}
		}
	})

	t.Run("negative_price", func(t *testing.T) {
		body := baseSubjectBody("bachelor-y1")
		body["price"] = -5
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_price" {
			t.Fatalf("expected 400 invalid_price, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("non_integer_price_invalid_json", func(t *testing.T) {
		body := baseSubjectBody("bachelor-y1")
		body["price"] = "1800"
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_json" {
			t.Fatalf("expected 400 invalid_json, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("expires_missing_past_garbage", func(t *testing.T) {
		for _, exp := range []string{"", time.Now().Add(-time.Hour).Format(time.RFC3339), "not-a-date", "2026-13-45"} {
			body := baseSubjectBody("bachelor-y1")
			if exp == "" {
				delete(body, "access_expires_at")
			} else {
				body["access_expires_at"] = exp
			}
			rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
			if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_expires_at" {
				t.Fatalf("expires %q: expected 400 invalid_expires_at, got %d %q", exp, rec.Code, adminCode(t, rec))
			}
		}
	})

	t.Run("invalid_term", func(t *testing.T) {
		body := baseSubjectBody("bachelor-y1")
		body["term"] = "third"
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_term" {
			t.Fatalf("expected 400 invalid_term, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("bachelor_empty_term_rejected", func(t *testing.T) {
		body := baseSubjectBody("bachelor-y1")
		body["term"] = ""
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_term" {
			t.Fatalf("expected 400 invalid_term for bachelor empty term, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("vocational_with_term_rejected_and_empty_accepted", func(t *testing.T) {
		body := baseSubjectBody("vocational")
		body["term"] = "first"
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_term" {
			t.Fatalf("expected 400 invalid_term for vocational with first term, got %d %q", rec.Code, adminCode(t, rec))
		}

		body["term"] = ""
		rec2 := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec2.Code != http.StatusCreated {
			t.Fatalf("expected 201 for vocational empty term, got %d", rec2.Code)
		}
	})

	t.Run("long_description", func(t *testing.T) {
		body := baseSubjectBody("bachelor-y1")
		body["description_ar"] = strings.Repeat("أ", 5001)
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_description" {
			t.Fatalf("expected 400 invalid_description, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("published_true_on_create_409", func(t *testing.T) {
		body := baseSubjectBody("bachelor-y1")
		body["published"] = true
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "subject_has_no_videos" {
			t.Fatalf("expected 409 subject_has_no_videos, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("defaults_and_shape", func(t *testing.T) {
		body := map[string]any{
			"level_id":          "vocational",
			"title_ar":          "مادة الشكل",
			"access_expires_at": futureExpires(),
		}
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
		}
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		for _, k := range []string{"id", "level_id", "term", "title_ar", "title_en", "description_ar", "description_en", "price", "currency", "access_expires_at", "order", "published", "video_count", "created_at", "updated_at"} {
			if _, ok := raw[k]; !ok {
				t.Fatalf("create response missing %q: %s", k, rec.Body.String())
			}
		}
		var dto SubjectAdminDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &dto)
		if dto.Published || dto.Price != 0 || dto.Order != 0 || dto.VideoCount != 0 {
			t.Fatalf("defaults wrong: %+v", dto)
		}
		if dto.Currency != "EGP" {
			t.Fatalf("currency = %q, want EGP", dto.Currency)
		}
	})
}

func TestAdminSubjects_List(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	d1 := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	d2body := baseSubjectBody("bachelor-y2")
	d2body["title_ar"] = "مادة ثانية"
	d2 := createSubject(t, s, d2body)

	t.Run("all_drafts_listed", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var res struct {
			Items []SubjectAdminDTO `json:"items"`
			Total int               `json:"total"`
			Page  int               `json:"page"`
			Limit int               `json:"limit"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if res.Total != 2 || len(res.Items) != 2 {
			t.Fatalf("expected 2 drafts, got total=%d items=%d", res.Total, len(res.Items))
		}
	})

	t.Run("level_filter", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects?level_id=bachelor-y1", nil)
		var res struct {
			Items []SubjectAdminDTO `json:"items"`
			Total int               `json:"total"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if res.Total != 1 || res.Items[0].ID != d1.ID {
			t.Fatalf("level filter wrong: %+v", res)
		}
		_ = d2
	})

	t.Run("published_filter_false", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects?published=false", nil)
		var res struct {
			Total int `json:"total"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if res.Total != 2 {
			t.Fatalf("expected 2 drafts, got %d", res.Total)
		}
	})

	t.Run("published_filter_true_empty", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects?published=true", nil)
		var res struct {
			Total int `json:"total"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if res.Total != 0 {
			t.Fatalf("expected 0 published, got %d", res.Total)
		}
	})

	t.Run("invalid_filters", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects?published=maybe", nil)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_published" {
			t.Fatalf("expected 400 invalid_published, got %d %q", rec.Code, adminCode(t, rec))
		}
		rec2 := doAdminJSON(t, s, http.MethodGet, "/internal/admin/subjects?level_id=bad!", nil)
		if rec2.Code != http.StatusBadRequest || adminCode(t, rec2) != "invalid_level_id" {
			t.Fatalf("expected 400 invalid_level_id, got %d %q", rec2.Code, adminCode(t, rec2))
		}
	})
}

func TestAdminSubjects_Patch(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	dto := createSubject(t, s, baseSubjectBody("bachelor-y1"))

	t.Run("update_fields", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/subjects/"+dto.ID, map[string]any{
			"title_ar": "مادة محدثة", "price": 2500, "order": 3, "term": "second",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got SubjectAdminDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got.TitleAr != "مادة محدثة" || got.Price != 2500 || got.Order != 3 || got.Term != "second" {
			t.Fatalf("patch not applied: %+v", got)
		}
	})

	t.Run("move_level", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/subjects/"+dto.ID, map[string]any{"level_id": "bachelor-y2"})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got SubjectAdminDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got.LevelID != "bachelor-y2" {
			t.Fatalf("level move not applied: %+v", got)
		}
	})

	t.Run("move_to_unknown_level_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/subjects/"+dto.ID, map[string]any{"level_id": "diploma-missing"})
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "level_not_found" {
			t.Fatalf("expected 404 level_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("unknown_subject_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/subjects/subj-missing-1", map[string]any{"title_ar": "x"})
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
			t.Fatalf("expected 404 subject_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("malformed_id_400", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/subjects/bad$id!", map[string]any{"title_ar": "x"})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_subject_id" {
			t.Fatalf("expected 400 invalid_subject_id, got %d %q", rec.Code, adminCode(t, rec))
		}
		rec404 := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/subjects/a/b", map[string]any{"title_ar": "x"})
		if rec404.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for unknown sub-action, got %d", rec404.Code)
		}
	})

	t.Run("publish_via_patch_gated", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/subjects/"+dto.ID, map[string]any{"published": true})
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "subject_has_no_videos" {
			t.Fatalf("expected 409 subject_has_no_videos, got %d %q", rec.Code, adminCode(t, rec))
		}
	})
}

func addStoreVideo(t *testing.T, s *Server, subjectID, videoID string) {
	t.Helper()
	v := &models.Video{
		ID:             videoID,
		SubjectID:      subjectID,
		Position:       1,
		TitleAr:        "محاضرة",
		YouTubeVideoID: "dQw4w9WgXcQ",
		Published:      true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := s.Store.CreateVideo(context.Background(), v); err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
}

func TestAdminSubjects_PublishFlow(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	dto := createSubject(t, s, baseSubjectBody("bachelor-y1"))

	t.Run("publish_without_videos_409", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+dto.ID+"/publish", nil)
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "subject_has_no_videos" {
			t.Fatalf("expected 409 subject_has_no_videos, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	addStoreVideo(t, s, dto.ID, "vid-pub-1")

	t.Run("publish_then_visible", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+dto.ID+"/publish", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got SubjectAdminDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if !got.Published || got.VideoCount != 1 {
			t.Fatalf("publish not applied: %+v", got)
		}

		tok := makeStudentToken(t, "student-pub")
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/"+dto.ID, nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		recS := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(recS, req)
		if recS.Code != http.StatusOK {
			t.Fatalf("published subject must be visible to students, got %d", recS.Code)
		}
	})

	t.Run("unpublish_hides", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+dto.ID+"/unpublish", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		tok := makeStudentToken(t, "student-pub")
		for _, path := range []string{"/academy/subjects/" + dto.ID} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
			req.Header.Set("Authorization", "Bearer "+tok)
			recS := httptest.NewRecorder()
			s.PublicHandler().ServeHTTP(recS, req)
			if recS.Code != http.StatusNotFound {
				t.Fatalf("unpublished subject must be 404, got %d", recS.Code)
			}
		}
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		recL := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(recL, req)
		if strings.Contains(recL.Body.String(), dto.ID) {
			t.Fatalf("unpublished subject must not be listed: %s", recL.Body.String())
		}
	})

	t.Run("publish_unknown_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/subj-missing-1/publish", nil)
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
			t.Fatalf("expected 404 subject_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})
}

func TestAdminSubjects_StudentVisibilityAndPrice(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	dto := createSubject(t, s, baseSubjectBody("bachelor-y1"))

	tok := makeStudentToken(t, "student-vis-subj")
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(rec, req)
		return rec
	}

	if rec := get("/academy/subjects/" + dto.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("draft subject detail must be 404, got %d", rec.Code)
	}
	if rec := get("/academy/subjects"); strings.Contains(rec.Body.String(), dto.ID) {
		t.Fatalf("draft subject must not be listed")
	}

	addStoreVideo(t, s, dto.ID, "vid-vis-1")
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+dto.ID+"/publish", nil); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d", rec.Code)
	}

	// Price stays hidden from students while EXPOSE_PRICE_TO_STUDENTS=false.
	if rec := get("/academy/subjects"); strings.Contains(rec.Body.String(), `"price"`) {
		t.Fatalf("price must stay hidden from student list: %s", rec.Body.String())
	}
	if rec := get("/academy/subjects/" + dto.ID); rec.Code != http.StatusOK {
		t.Fatalf("published subject must be visible, got %d", rec.Code)
	} else if strings.Contains(rec.Body.String(), `"price"`) {
		t.Fatalf("price must stay hidden from student detail: %s", rec.Body.String())
	}
}

func TestAdminSubjects_AuditPerMutation(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()

	dto := createSubject(t, s, baseSubjectBody("bachelor-y1"))
	if rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/subjects/"+dto.ID, map[string]any{"order": 2}); rec.Code != http.StatusOK {
		t.Fatalf("patch: %d", rec.Code)
	}
	addStoreVideo(t, s, dto.ID, "vid-audit-1")
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+dto.ID+"/publish", nil); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d", rec.Code)
	}
	if rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects/"+dto.ID+"/unpublish", nil); rec.Code != http.StatusOK {
		t.Fatalf("unpublish: %d", rec.Code)
	}

	logs, total, err := s.Store.ListAuditLogs(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if total != 4 {
		t.Fatalf("expected 4 audit rows, got %d", total)
	}
	actions := map[string]int{}
	for _, l := range logs {
		actions[l.Action]++
		if l.TargetType != "subject" || l.TargetID != dto.ID {
			t.Fatalf("audit target wrong: %+v", l)
		}
	}
	for _, a := range []string{"subject_create", "subject_update", "subject_publish", "subject_unpublish"} {
		if actions[a] != 1 {
			t.Fatalf("audit actions = %v", actions)
		}
	}
}
