package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func doAdminJSON(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Internal-Token", "test-internal-token")
	req.Header.Set("X-Admin-Token", "test-admin-token")
	rec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(rec, req)
	return rec
}

func createDiploma(t *testing.T, s *Server, body map[string]any) LevelAdminDTO {
	t.Helper()
	rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/levels", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var dto LevelAdminDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode level: %v", err)
	}
	if !strings.HasPrefix(dto.Key, "diploma-") {
		t.Fatalf("expected server-generated diploma- key, got %q", dto.Key)
	}
	return dto
}

func adminCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	code, _ := body["code"].(string)
	return code
}

func TestAdminLevels_AuthWiring(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	req := httptest.NewRequest(http.MethodGet, "/internal/admin/levels", nil)
	rec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without tokens, got %d", rec.Code)
	}

	recPost := doAdminJSON(t, s, http.MethodPost, "/internal/admin/levels", map[string]any{
		"study_type": "diploma", "name_ar": "دبلومة", "order": 6,
	})
	if recPost.Code != http.StatusCreated {
		t.Fatalf("expected 201 with tokens, got %d (%s)", recPost.Code, recPost.Body.String())
	}
}

func TestAdminLevels_CreateValidation(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	for _, tc := range []struct {
		name  string
		study string
	}{
		{"bachelor_rejected", "bachelor"},
		{"vocational_rejected", "vocational"},
		{"empty_rejected", ""},
		{"case_sensitive", "Diploma"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/levels", map[string]any{
				"study_type": tc.study, "name_ar": "دبلومة", "order": 6,
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if adminCode(t, rec) != "invalid_study_type" {
				t.Fatalf("expected code invalid_study_type, got %q", adminCode(t, rec))
			}
		})
	}

	t.Run("empty_name_rejected", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/levels", map[string]any{
			"study_type": "diploma", "name_ar": "   ", "order": 6,
		})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_name" {
			t.Fatalf("expected 400 invalid_name, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("long_name_rejected", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/levels", map[string]any{
			"study_type": "diploma", "name_ar": strings.Repeat("أ", 201), "order": 6,
		})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_name" {
			t.Fatalf("expected 400 invalid_name, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("crlf_removed_from_name", func(t *testing.T) {
		dto := createDiploma(t, s, map[string]any{
			"study_type": "diploma", "name_ar": "دبلومة\r\nالقانون", "order": 6,
		})
		if strings.Contains(dto.NameAr, "\r") || strings.Contains(dto.NameAr, "\n") {
			t.Fatalf("CR/LF not removed: %q", dto.NameAr)
		}
		if dto.NameAr != "دبلومةالقانون" {
			t.Fatalf("name = %q", dto.NameAr)
		}
	})

	t.Run("defaults_and_shape", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/levels", map[string]any{
			"study_type": "diploma", "name_ar": "دبلومة القانون الجنائي",
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, k := range []string{"key", "study_type", "name_ar", "name_en", "order", "published", "subject_count", "published_subject_count"} {
			if _, ok := raw[k]; !ok {
				t.Fatalf("create response missing %q: %s", k, rec.Body.String())
			}
		}
		var dto LevelAdminDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &dto)
		if dto.Published {
			t.Errorf("published default must be false")
		}
		if dto.Order != 0 {
			t.Errorf("order default must be 0, got %d", dto.Order)
		}
		if dto.SubjectCount != 0 || dto.PublishedSubjectCount != 0 {
			t.Errorf("new diploma counts must be 0, got %+v", dto)
		}
	})

	t.Run("malformed_json_400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/admin/levels", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", "test-internal-token")
		req.Header.Set("X-Admin-Token", "test-admin-token")
		rec := httptest.NewRecorder()
		s.AdminHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_json" {
			t.Fatalf("expected 400 invalid_json, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("method_not_allowed", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/levels", nil)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}

func TestAdminLevels_Patch(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	dto := createDiploma(t, s, map[string]any{
		"study_type": "diploma", "name_ar": "دبلومة أولى", "order": 6,
	})

	t.Run("rename_reorder_publish", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/levels/"+dto.Key, map[string]any{
			"name_ar": "دبلومة القانون الجنائي", "order": 7, "published": true,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got LevelAdminDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got.NameAr != "دبلومة القانون الجنائي" || got.Order != 7 || !got.Published {
			t.Fatalf("patch not applied: %+v", got)
		}
	})

	t.Run("unknown_id_404", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/levels/diploma-missing", map[string]any{"name_ar": "x"})
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "level_not_found" {
			t.Fatalf("expected 404 level_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("malformed_id_400", func(t *testing.T) {
		for _, path := range []string{"/internal/admin/levels/a/b", "/internal/admin/levels/bad$id!"} {
			rec := doAdminJSON(t, s, http.MethodPatch, path, map[string]any{"name_ar": "x"})
			if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_level_id" {
				t.Fatalf("path %s: expected 400 invalid_level_id, got %d %q", path, rec.Code, adminCode(t, rec))
			}
		}
	})

	t.Run("study_type_change_rejected", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/levels/"+dto.Key, map[string]any{"study_type": "bachelor"})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_study_type" {
			t.Fatalf("expected 400 invalid_study_type, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("invalid_name_rejected", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/levels/"+dto.Key, map[string]any{"name_ar": ""})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_name" {
			t.Fatalf("expected 400 invalid_name, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("seeded_level_rename_reorder", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/levels/bachelor-y1", map[string]any{
			"name_ar": "الفرقة الأولى (محدث)", "order": 1,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 renaming seeded level, got %d (%s)", rec.Code, rec.Body.String())
		}
	})
}

func TestAdminLevels_Delete(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	t.Run("seeded_never_deleted", func(t *testing.T) {
		for _, id := range []string{"bachelor-y1", "vocational"} {
			rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/levels/"+id, nil)
			if rec.Code != http.StatusConflict || adminCode(t, rec) != "level_not_deletable" {
				t.Fatalf("id %s: expected 409 level_not_deletable, got %d %q", id, rec.Code, adminCode(t, rec))
			}
		}
	})

	t.Run("diploma_with_subjects_409", func(t *testing.T) {
		dto := createDiploma(t, s, map[string]any{"study_type": "diploma", "name_ar": "دبلومة لها مواد"})
		subj := &models.Subject{
			ID: "subj-dip-1", LevelKey: dto.Key, Term: "first",
			TitleAr: "مادة", Status: models.StatusDraft,
			AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
			CreatedAt:       time.Now(), UpdatedAt: time.Now(),
		}
		if err := s.Store.CreateSubject(context.Background(), subj); err != nil {
			t.Fatalf("CreateSubject: %v", err)
		}
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/levels/"+dto.Key, nil)
		if rec.Code != http.StatusConflict || adminCode(t, rec) != "level_has_subjects" {
			t.Fatalf("expected 409 level_has_subjects, got %d %q", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("empty_diploma_deleted", func(t *testing.T) {
		dto := createDiploma(t, s, map[string]any{"study_type": "diploma", "name_ar": "دبلومة للحذف"})
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/levels/"+dto.Key, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		recGet := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/levels/"+dto.Key, map[string]any{"name_ar": "x"})
		if recGet.Code != http.StatusNotFound {
			t.Fatalf("expected deleted diploma to be gone, patch got %d", recGet.Code)
		}
	})

	t.Run("unknown_and_malformed", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/levels/diploma-missing", nil)
		if rec.Code != http.StatusNotFound || adminCode(t, rec) != "level_not_found" {
			t.Fatalf("expected 404 level_not_found, got %d %q", rec.Code, adminCode(t, rec))
		}
		recBad := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/levels/", nil)
		if recBad.Code != http.StatusBadRequest || adminCode(t, recBad) != "invalid_level_id" {
			t.Fatalf("expected 400 invalid_level_id, got %d %q", recBad.Code, adminCode(t, recBad))
		}
	})
}

func TestAdminLevels_ListAndStudentVisibility(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	dto := createDiploma(t, s, map[string]any{"study_type": "diploma", "name_ar": "دبلومة مخفية", "order": 6})

	rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/levels", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var list struct {
		Levels []LevelAdminDTO `json:"levels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Levels) != 6 {
		t.Fatalf("expected 5 seeded + 1 diploma = 6 levels, got %d", len(list.Levels))
	}
	found := false
	for _, l := range list.Levels {
		if l.Key == dto.Key {
			found = true
			if l.Published {
				t.Errorf("new diploma must default to unpublished")
			}
			if l.SubjectCount != 0 || l.PublishedSubjectCount != 0 {
				t.Errorf("new diploma counts must be 0: %+v", l)
			}
		}
	}
	if !found {
		t.Fatalf("admin list must include unpublished diplomas")
	}

	studentHas := func(key string) bool {
		tok := makeStudentToken(t, "student-vis")
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		recS := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(recS, req)
		if recS.Code != http.StatusOK {
			t.Fatalf("student levels: %d", recS.Code)
		}
		return strings.Contains(recS.Body.String(), key)
	}

	if studentHas(dto.Key) {
		t.Fatalf("unpublished diploma must be hidden from students")
	}

	recPub := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/levels/"+dto.Key, map[string]any{"published": true})
	if recPub.Code != http.StatusOK {
		t.Fatalf("publish: %d (%s)", recPub.Code, recPub.Body.String())
	}
	if !studentHas(dto.Key) {
		t.Fatalf("published diploma must be visible to students")
	}
}

func TestAdminLevels_AuditPerMutation(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	ctx := context.Background()

	dto := createDiploma(t, s, map[string]any{"study_type": "diploma", "name_ar": "دبلومة مدققة"})
	rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/levels/"+dto.Key, map[string]any{"order": 9})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d", rec.Code)
	}
	other := createDiploma(t, s, map[string]any{"study_type": "diploma", "name_ar": "دبلومة للحذف"})
	recDel := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/levels/"+other.Key, nil)
	if recDel.Code != http.StatusOK {
		t.Fatalf("delete: %d", recDel.Code)
	}

	logs, total, err := s.Store.ListAuditLogs(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if total != 4 {
		t.Fatalf("expected 4 audit rows (2 creates + 1 update + 1 delete), got %d", total)
	}
	actions := map[string]int{}
	for _, l := range logs {
		actions[l.Action]++
		if l.ActorID != "adm-1" || l.ActorName != "Test Operator" {
			t.Fatalf("audit actor wrong: %+v", l)
		}
		if l.TargetType != "level" {
			t.Fatalf("audit target_type = %q, want level", l.TargetType)
		}
	}
	if actions["level_create"] != 2 || actions["level_update"] != 1 || actions["level_delete"] != 1 {
		t.Fatalf("audit actions = %v", actions)
	}
}
