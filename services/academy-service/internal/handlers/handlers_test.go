package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/limiter"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
)

func init() {
	jwtutil.Init("test-jwt-secret")
}

func newTestServer(exposePrice bool) *Server {
	st := store.NewMemoryStore()
	_ = st.SeedLevels(context.Background())
	return New(st, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", exposePrice, "+201000000000")
}

func makeStudentToken(t *testing.T, userID string) string {
	t.Helper()
	tok, err := jwtutil.GenerateToken(userID, "user", userID+"@example.com")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	return tok
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %q, want ok", body["status"])
	}
}

func TestGatewayAuth(t *testing.T) {
	s := newTestServer(false)
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	authed := s.GatewayAuth(nextHandler)

	t.Run("health_bypasses_gateway_secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("missing_gateway_secret_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("wrong_gateway_secret_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		req.Header.Set("X-Gateway-Secret", "wrong-secret")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("valid_gateway_secret_allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}

func TestInternalTokenAuth(t *testing.T) {
	s := newTestServer(false)
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	authed := s.InternalTokenAuth(nextHandler)

	t.Run("missing_token_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/admin/test", nil)
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("wrong_token_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/admin/test", nil)
		req.Header.Set("X-Internal-Token", "wrong-token")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("valid_token_allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/admin/test", nil)
		req.Header.Set("X-Internal-Token", "test-internal-token")
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}

func TestAdminHandler_UnknownPaths404(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	handler := s.AdminHandler()

	req := httptest.NewRequest(http.MethodGet, "/internal/admin/anything", nil)
	req.Header.Set("X-Internal-Token", "test-internal-token")
	req.Header.Set("X-Admin-Token", "some-admin-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown admin path, got %d", rec.Code)
	}
}

func TestRouteIsolation(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	adminHandler := s.AdminHandler()

	for _, path := range []string{"/health", "/academy/levels", "/academy/subjects"} {
		t.Run("admin_rejects_"+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Internal-Token", "test-internal-token")
			req.Header.Set("X-Admin-Token", "some-admin-token")
			rec := httptest.NewRecorder()
			adminHandler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected admin listener to return 404 for %s, got %d", path, rec.Code)
			}
		})
	}
}

func TestGetLevels(t *testing.T) {
	s := newTestServer(false)
	publicHandler := s.PublicHandler()
	tok := makeStudentToken(t, "student-123")

	t.Run("missing_gateway_secret_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("missing_bearer_token_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("invalid_bearer_token_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer invalid-jwt-token")
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("non_get_method_refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	getLevels := func(t *testing.T, s *Server) (models.LevelsResponseDTO, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		raw := rec.Body.String()
		var resp models.LevelsResponseDTO
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("decode JSON: %v", err)
		}
		return resp, raw
	}
	keysOf := func(levels []models.LevelDTO) []string {
		out := make([]string, len(levels))
		for i, l := range levels {
			out[i] = l.Key
		}
		return out
	}
	sameKeys := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	t.Run("empty_catalog_returns_three_study_types_with_4_0_1_levels", func(t *testing.T) {
		fresh := newTestServer(false)
		resp, raw := getLevels(t, fresh)

		if len(resp.StudyTypes) != 3 {
			t.Fatalf("expected 3 study types, got %d", len(resp.StudyTypes))
		}
		wantOrder := []string{models.StudyTypeBachelor, models.StudyTypeDiploma, models.StudyTypeVocational}
		wantLevels := []int{4, 0, 1}
		for i, st := range resp.StudyTypes {
			if st.Key != wantOrder[i] {
				t.Errorf("study type[%d] = %q, want %q", i, st.Key, wantOrder[i])
			}
			if len(st.Levels) != wantLevels[i] {
				t.Errorf("study type %q has %d levels, want %d", st.Key, len(st.Levels), wantLevels[i])
			}
			if st.Title.Ar == "" || st.Title.En == "" {
				t.Errorf("study type %q has an empty title: %+v", st.Key, st.Title)
			}
		}
		if !sameKeys(keysOf(resp.StudyTypes[0].Levels), []string{"bachelor-y1", "bachelor-y2", "bachelor-y3", "bachelor-y4"}) {
			t.Errorf("bachelor levels = %v", keysOf(resp.StudyTypes[0].Levels))
		}
		if !sameKeys(keysOf(resp.StudyTypes[2].Levels), []string{"vocational"}) {
			t.Errorf("vocational levels = %v", keysOf(resp.StudyTypes[2].Levels))
		}
		if len(resp.Levels) != 5 {
			t.Errorf("flat levels = %d, want 5", len(resp.Levels))
		}
		// The empty diploma list is a JSON array, never null.
		if !strings.Contains(raw, `"key":"diploma"`) || strings.Contains(raw, `"levels":null`) {
			t.Errorf("diploma must be present with an empty array, never null: %s", raw)
		}
		if !strings.Contains(raw, `"levels":[]`) {
			t.Errorf("expected an empty diploma levels array in: %s", raw)
		}
	})

	t.Run("a_diploma_without_subjects_is_listed", func(t *testing.T) {
		fresh := newTestServer(false)
		st := fresh.Store.(*store.MemoryStore)
		st.PutLevel(models.Level{Key: "diploma-criminal-law", StudyType: models.StudyTypeDiploma, TitleAr: "دبلومة القانون الجنائي", TitleEn: "Criminal Law Diploma", Position: 6, Published: true})

		resp, _ := getLevels(t, fresh)
		diploma := resp.StudyTypes[1]
		if diploma.Key != models.StudyTypeDiploma || len(diploma.Levels) != 1 {
			t.Fatalf("diploma study type = %+v", diploma)
		}
		if diploma.Levels[0].Key != "diploma-criminal-law" || diploma.Levels[0].Title.En != "Criminal Law Diploma" {
			t.Errorf("diploma level = %+v", diploma.Levels[0])
		}
		// Tree order in the flat list: bachelor, diploma, vocational, even
		// though the diploma's position is after the vocational level's.
		want := []string{"bachelor-y1", "bachelor-y2", "bachelor-y3", "bachelor-y4", "diploma-criminal-law", "vocational"}
		if !sameKeys(keysOf(resp.Levels), want) {
			t.Errorf("flat levels = %v, want %v", keysOf(resp.Levels), want)
		}
	})

	t.Run("diplomas_are_ordered_by_position_then_key", func(t *testing.T) {
		fresh := newTestServer(false)
		st := fresh.Store.(*store.MemoryStore)
		st.PutLevel(models.Level{Key: "diploma-b", StudyType: models.StudyTypeDiploma, TitleAr: "ب", TitleEn: "B", Position: 7, Published: true})
		st.PutLevel(models.Level{Key: "diploma-c", StudyType: models.StudyTypeDiploma, TitleAr: "ج", TitleEn: "C", Position: 6, Published: true})
		st.PutLevel(models.Level{Key: "diploma-a", StudyType: models.StudyTypeDiploma, TitleAr: "أ", TitleEn: "A", Position: 7, Published: true})

		resp, _ := getLevels(t, fresh)
		got := keysOf(resp.StudyTypes[1].Levels)
		if !sameKeys(got, []string{"diploma-c", "diploma-a", "diploma-b"}) {
			t.Errorf("diploma order = %v", got)
		}
	})

	t.Run("a_level_with_an_unknown_study_type_comes_after_the_fixed_three", func(t *testing.T) {
		fresh := newTestServer(false)
		fresh.Store.(*store.MemoryStore).PutLevel(models.Level{Key: "odd-1", StudyType: "legacy", TitleAr: "قديم", TitleEn: "Legacy", Position: 9, Published: true})

		resp, _ := getLevels(t, fresh)
		if len(resp.StudyTypes) != 4 || resp.StudyTypes[3].Key != "legacy" {
			t.Fatalf("study types = %+v", resp.StudyTypes)
		}
		for i, key := range []string{"bachelor", "diploma", "vocational"} {
			if resp.StudyTypes[i].Key != key {
				t.Errorf("study type[%d] = %q, want %q", i, resp.StudyTypes[i].Key, key)
			}
		}
	})

	t.Run("an_unpublished_subject_is_hidden_but_its_level_stays_listed", func(t *testing.T) {
		draft := &models.Subject{
			ID:              "subj-draft-1",
			LevelKey:        "bachelor-y1",
			Term:            "first",
			TitleAr:         "مادة مسودة",
			TitleEn:         "Draft Subject",
			Status:          models.StatusDraft,
			AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		_ = s.Store.CreateSubject(context.Background(), draft)

		resp, _ := getLevels(t, s)
		if len(resp.Levels) != 5 || len(resp.StudyTypes) != 3 {
			t.Fatalf("levels = %d, study types = %d; want 5 and 3", len(resp.Levels), len(resp.StudyTypes))
		}

		// The subject list and the detail still hide it.
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects?level=bachelor-y1", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)
		var list models.SubjectListResponseDTO
		if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
			t.Fatalf("decode subjects: %v", err)
		}
		if len(list.Items) != 0 || list.Total != 0 {
			t.Fatalf("a draft subject must not be listed: %+v", list)
		}
		req = httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-draft-1", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec = httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("a draft subject's detail must be 404, got %d", rec.Code)
		}
	})

	t.Run("a_published_subject_is_listed_and_the_level_dto_shape_holds", func(t *testing.T) {
		published := &models.Subject{
			ID:              "subj-pub-1",
			LevelKey:        "bachelor-y1",
			Term:            "first",
			TitleAr:         "المدخل للعلوم القانونية",
			TitleEn:         "Intro to Legal Science",
			Status:          models.StatusPublished,
			AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		_ = s.Store.CreateSubject(context.Background(), published)

		resp, _ := getLevels(t, s)
		// Publishing a subject adds nothing to the axes: still 5 levels.
		if len(resp.Levels) != 5 {
			t.Fatalf("expected 5 levels, got %d", len(resp.Levels))
		}

		lvl := resp.Levels[0]
		if lvl.Key != "bachelor-y1" {
			t.Errorf("level key = %q, want bachelor-y1", lvl.Key)
		}
		if lvl.StudyType != models.StudyTypeBachelor {
			t.Errorf("study_type = %q, want bachelor", lvl.StudyType)
		}
		if lvl.Title.Ar != "الفرقة الأولى" {
			t.Errorf("title.ar = %q, want الفرقة الأولى", lvl.Title.Ar)
		}
		if lvl.Title.En != "Year 1" {
			t.Errorf("title.en = %q, want Year 1", lvl.Title.En)
		}
		if lvl.Position != 1 {
			t.Errorf("position = %d, want 1", lvl.Position)
		}
		if len(resp.StudyTypes) != 3 || resp.StudyTypes[0].Key != models.StudyTypeBachelor {
			t.Fatalf("study types = %+v", resp.StudyTypes)
		}
	})
}

// TestLevels_ContractShape pins the JSON shape of GET /academy/levels. The
// contract suite (tests/contracts) runs it by name.
func TestLevels_ContractShape(t *testing.T) {
	s := newTestServer(false)
	tok := makeStudentToken(t, "student-123")
	req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertKeys := func(what string, raw map[string]json.RawMessage, want ...string) {
		t.Helper()
		if len(raw) != len(want) {
			t.Errorf("%s has keys %v, want exactly %v", what, mapKeys(raw), want)
		}
		for _, k := range want {
			if _, ok := raw[k]; !ok {
				t.Errorf("%s is missing %q", what, k)
			}
		}
	}
	assertKeys("response", body, "levels", "study_types")

	var studyTypes []map[string]json.RawMessage
	if err := json.Unmarshal(body["study_types"], &studyTypes); err != nil {
		t.Fatalf("study_types: %v", err)
	}
	if len(studyTypes) != 3 {
		t.Fatalf("study_types = %d, want 3", len(studyTypes))
	}
	for _, st := range studyTypes {
		assertKeys("study type", st, "key", "title", "levels")
		var title map[string]json.RawMessage
		_ = json.Unmarshal(st["title"], &title)
		assertKeys("study type title", title, "ar", "en")
		var levels []map[string]json.RawMessage
		if err := json.Unmarshal(st["levels"], &levels); err != nil || levels == nil {
			t.Fatalf("study type levels must be a JSON array, never null: %s", st["levels"])
		}
		for _, l := range levels {
			assertKeys("level", l, "key", "study_type", "title", "position")
		}
	}
	var flat []map[string]json.RawMessage
	if err := json.Unmarshal(body["levels"], &flat); err != nil || len(flat) != 5 {
		t.Fatalf("flat levels = %d (%v), want 5", len(flat), err)
	}
	// Never any subject, count or price information on the axes.
	for _, banned := range []string{"price", "count", "subject", "owned"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), banned) {
			t.Errorf("levels response must not carry %q: %s", banned, rec.Body.String())
		}
	}
}

func mapKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestSubjectsAndVideosReadEndpoints(t *testing.T) {
	ctx := context.Background()
	tok := makeStudentToken(t, "student-read-test")
	secretVideoID := "dQw4w9WgXcQ"
	secretUnpubVideoID := "unpub_yt_id_123"

	setupData := func(s *Server) {
		// 1. Published Subject in bachelor-y1, first term
		s1 := &models.Subject{
			ID:              "subj-1",
			LevelKey:        "bachelor-y1",
			Term:            "first",
			TitleAr:         "المدخل للعلوم القانونية",
			TitleEn:         "Legal Science",
			DescriptionAr:   "شرح نظرية الحق والقانون",
			DescriptionEn:   "Theory of law and rights",
			Price:           1800,
			Status:          models.StatusPublished,
			AccessExpiresAt: time.Now().Add(60 * 24 * time.Hour),
			CreatedAt:       time.Now().Add(-10 * time.Minute),
			UpdatedAt:       time.Now(),
		}
		_ = s.Store.CreateSubject(ctx, s1)

		// 2. Published Subject in bachelor-y1, second term
		s2 := &models.Subject{
			ID:              "subj-2",
			LevelKey:        "bachelor-y1",
			Term:            "second",
			TitleAr:         "القانون الدستوري",
			TitleEn:         "Constitutional Law",
			DescriptionAr:   "دراسة الدستور والنظام السياسي",
			DescriptionEn:   "Constitutional principles",
			Price:           1500,
			Status:          models.StatusPublished,
			AccessExpiresAt: time.Now().Add(60 * 24 * time.Hour),
			CreatedAt:       time.Now().Add(-5 * time.Minute),
			UpdatedAt:       time.Now(),
		}
		_ = s.Store.CreateSubject(ctx, s2)

		// 3. Draft Subject (must never appear to students)
		sDraft := &models.Subject{
			ID:              "subj-draft",
			LevelKey:        "bachelor-y1",
			Term:            "first",
			TitleAr:         "مادة مسودة سرية",
			TitleEn:         "Secret Draft",
			Price:           2000,
			Status:          models.StatusDraft,
			AccessExpiresAt: time.Now().Add(60 * 24 * time.Hour),
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		_ = s.Store.CreateSubject(ctx, sDraft)

		// 4. Videos on subj-1: 1 published, 1 unpublished
		v1 := &models.Video{
			ID:             "vid-1",
			SubjectID:      "subj-1",
			Position:       1,
			TitleAr:        "المحاضرة الأولى",
			TitleEn:        "Lecture 1",
			DescriptionAr:  "مقدمة عامة",
			DescriptionEn:  "General intro",
			YouTubeVideoID: secretVideoID,
			Published:      true,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		_ = s.Store.CreateVideo(ctx, v1)

		vUnpub := &models.Video{
			ID:             "vid-unpub",
			SubjectID:      "subj-1",
			Position:       2,
			TitleAr:        "محاضرة غير منشورة",
			TitleEn:        "Unpublished Lecture",
			YouTubeVideoID: secretUnpubVideoID,
			Published:      false,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		_ = s.Store.CreateVideo(ctx, vUnpub)

		// 5. Files on subj-1: 1 book, 1 note
		f1 := &models.SubjectFile{
			ID:         "file-1",
			SubjectID:  "subj-1",
			Kind:       "book",
			TitleAr:    "كتاب المدخل",
			TitleEn:    "Legal Science Book",
			SizeBytes:  1048576,
			StorageKey: "secret-storage-uuid-1",
			CreatedAt:  time.Now(),
		}
		_ = s.Store.CreateFile(ctx, f1)

		f2 := &models.SubjectFile{
			ID:         "file-2",
			SubjectID:  "subj-1",
			Kind:       "note",
			TitleAr:    "مذكرة مراجعة",
			TitleEn:    "Revision Note",
			SizeBytes:  524288,
			StorageKey: "secret-storage-uuid-2",
			CreatedAt:  time.Now(),
		}
		_ = s.Store.CreateFile(ctx, f2)
	}

	t.Run("list_subjects_filtering_and_counts", func(t *testing.T) {
		s := newTestServer(false)
		setupData(s)
		handler := s.PublicHandler()

		// Request all published subjects for bachelor-y1
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects?level=bachelor-y1", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var resp models.SubjectListResponseDTO
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}

		// Only the 2 published subjects appear; draft is excluded
		if len(resp.Items) != 2 {
			t.Fatalf("expected 2 published items, got %d", len(resp.Items))
		}
		if resp.Total != 2 {
			t.Fatalf("expected total 2, got %d", resp.Total)
		}

		// Check subj-1 counts: 1 published video (not 2!), 1 book, 1 note
		s1Item := resp.Items[0]
		if s1Item.ID != "subj-1" {
			t.Errorf("expected subj-1 first, got %q", s1Item.ID)
		}
		if s1Item.Counts.Videos != 1 {
			t.Errorf("counts.videos = %d, want 1 (unpublished excluded)", s1Item.Counts.Videos)
		}
		if s1Item.Counts.Books != 1 {
			t.Errorf("counts.books = %d, want 1", s1Item.Counts.Books)
		}
		if s1Item.Counts.Notes != 1 {
			t.Errorf("counts.notes = %d, want 1", s1Item.Counts.Notes)
		}
		if s1Item.Owned != false {
			t.Errorf("owned = %v, want false", s1Item.Owned)
		}

		// Term filter test: term=second returns only subj-2
		req2 := httptest.NewRequest(http.MethodGet, "/academy/subjects?level=bachelor-y1&term=second", nil)
		req2.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req2.Header.Set("Authorization", "Bearer "+tok)
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)

		var resp2 models.SubjectListResponseDTO
		_ = json.NewDecoder(rec2.Body).Decode(&resp2)
		if len(resp2.Items) != 1 || resp2.Items[0].ID != "subj-2" {
			t.Fatalf("term=second filter failed: %+v", resp2.Items)
		}
	})

	t.Run("pagination_capping", func(t *testing.T) {
		s := newTestServer(false)
		setupData(s)
		handler := s.PublicHandler()

		// Limit cap at 100
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects?limit=500", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var resp models.SubjectListResponseDTO
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp.Limit != 100 {
			t.Errorf("expected limit capped at 100, got %d", resp.Limit)
		}

		// Invalid/negative limit defaults to 20
		reqNeg := httptest.NewRequest(http.MethodGet, "/academy/subjects?limit=-5", nil)
		reqNeg.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqNeg.Header.Set("Authorization", "Bearer "+tok)
		recNeg := httptest.NewRecorder()
		handler.ServeHTTP(recNeg, reqNeg)

		var respNeg models.SubjectListResponseDTO
		_ = json.NewDecoder(recNeg.Body).Decode(&respNeg)
		if respNeg.Limit != 20 {
			t.Errorf("expected negative limit to default to 20, got %d", respNeg.Limit)
		}
	})

	t.Run("subject_detail_and_unpublished_filtering", func(t *testing.T) {
		s := newTestServer(false)
		setupData(s)
		handler := s.PublicHandler()

		// 1. Published subject detail returns 200
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-1", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var detail models.SubjectDetailDTO
		if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		if detail.ID != "subj-1" {
			t.Errorf("detail.id = %q, want subj-1", detail.ID)
		}
		// Only 1 published video in detail; unpublished video omitted
		if len(detail.Videos) != 1 {
			t.Fatalf("expected 1 published video in detail, got %d", len(detail.Videos))
		}
		if detail.Videos[0].ID != "vid-1" {
			t.Errorf("video id = %q, want vid-1", detail.Videos[0].ID)
		}
		if len(detail.Files) != 2 {
			t.Fatalf("expected 2 files in detail, got %d", len(detail.Files))
		}

		// 2. Draft subject detail returns 404
		reqDraft := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-draft", nil)
		reqDraft.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqDraft.Header.Set("Authorization", "Bearer "+tok)
		recDraft := httptest.NewRecorder()
		handler.ServeHTTP(recDraft, reqDraft)

		if recDraft.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for draft subject detail, got %d", recDraft.Code)
		}

		// 3. Unknown subject detail returns 404
		reqUnknown := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-unknown-999", nil)
		reqUnknown.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqUnknown.Header.Set("Authorization", "Bearer "+tok)
		recUnknown := httptest.NewRecorder()
		handler.ServeHTTP(recUnknown, reqUnknown)

		if recUnknown.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for unknown subject detail, got %d", recUnknown.Code)
		}
	})

	t.Run("price_visibility_EXPOSE_PRICE_TO_STUDENTS", func(t *testing.T) {
		// When EXPOSE_PRICE_TO_STUDENTS = false (default)
		sHidden := newTestServer(false)
		setupData(sHidden)
		hHidden := sHidden.PublicHandler()

		reqList := httptest.NewRequest(http.MethodGet, "/academy/subjects?level=bachelor-y1", nil)
		reqList.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqList.Header.Set("Authorization", "Bearer "+tok)
		recList := httptest.NewRecorder()
		hHidden.ServeHTTP(recList, reqList)

		jsonBytesList := recList.Body.Bytes()
		if strings.Contains(string(jsonBytesList), `"price"`) || strings.Contains(string(jsonBytesList), `"currency"`) {
			t.Errorf("expected price and currency to be HIDDEN from list when exposePrice=false: %s", string(jsonBytesList))
		}

		reqDetail := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-1", nil)
		reqDetail.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqDetail.Header.Set("Authorization", "Bearer "+tok)
		recDetail := httptest.NewRecorder()
		hHidden.ServeHTTP(recDetail, reqDetail)

		jsonBytesDetail := recDetail.Body.Bytes()
		if strings.Contains(string(jsonBytesDetail), `"price"`) || strings.Contains(string(jsonBytesDetail), `"currency"`) {
			t.Errorf("expected price and currency to be HIDDEN from detail when exposePrice=false: %s", string(jsonBytesDetail))
		}

		// When EXPOSE_PRICE_TO_STUDENTS = true
		sExposed := newTestServer(true)
		setupData(sExposed)
		hExposed := sExposed.PublicHandler()

		recListExp := httptest.NewRecorder()
		hExposed.ServeHTTP(recListExp, reqList)
		if !strings.Contains(recListExp.Body.String(), `"price":1800`) || !strings.Contains(recListExp.Body.String(), `"currency":"EGP"`) {
			t.Errorf("expected price=1800 and currency=EGP when exposePrice=true: %s", recListExp.Body.String())
		}

		recDetailExp := httptest.NewRecorder()
		hExposed.ServeHTTP(recDetailExp, reqDetail)
		if !strings.Contains(recDetailExp.Body.String(), `"price":1800`) || !strings.Contains(recDetailExp.Body.String(), `"currency":"EGP"`) {
			t.Errorf("expected price=1800 and currency=EGP in detail when exposePrice=true: %s", recDetailExp.Body.String())
		}
	})

	t.Run("strict_leak_test_youtube_video_id_and_seeded_ids_never_in_student_json", func(t *testing.T) {
		s := newTestServer(true)
		setupData(s)
		handler := s.PublicHandler()

		// 1. List endpoint output
		reqList := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		reqList.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqList.Header.Set("Authorization", "Bearer "+tok)
		recList := httptest.NewRecorder()
		handler.ServeHTTP(recList, reqList)

		listJSON := recList.Body.String()
		if strings.Contains(listJSON, "youtube_video_id") {
			t.Fatalf("LEAK in list: contains 'youtube_video_id': %s", listJSON)
		}
		if strings.Contains(listJSON, secretVideoID) {
			t.Fatalf("LEAK in list: contains secret video id %q", secretVideoID)
		}
		if strings.Contains(listJSON, secretUnpubVideoID) {
			t.Fatalf("LEAK in list: contains unpub video id %q", secretUnpubVideoID)
		}

		// 2. Detail endpoint output
		reqDetail := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-1", nil)
		reqDetail.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqDetail.Header.Set("Authorization", "Bearer "+tok)
		recDetail := httptest.NewRecorder()
		handler.ServeHTTP(recDetail, reqDetail)

		detailJSON := recDetail.Body.String()
		if strings.Contains(detailJSON, "youtube_video_id") {
			t.Fatalf("LEAK in detail: contains 'youtube_video_id': %s", detailJSON)
		}
		if strings.Contains(detailJSON, secretVideoID) {
			t.Fatalf("LEAK in detail: contains secret video id %q", secretVideoID)
		}
		if strings.Contains(detailJSON, secretUnpubVideoID) {
			t.Fatalf("LEAK in detail: contains unpub video id %q", secretUnpubVideoID)
		}

		// 3. Direct DTO marshaling of every student DTO
		rawVideo := models.Video{
			ID:             "v-test",
			SubjectID:      "s-test",
			Position:       1,
			TitleAr:        "عنوان",
			TitleEn:        "Title",
			YouTubeVideoID: "REAL_YT_ID_11",
			Published:      true,
		}
		videoDTO := rawVideo.ToDTO(false)
		videoBytes, err := json.Marshal(videoDTO)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(videoBytes), "youtube_video_id") || strings.Contains(string(videoBytes), "REAL_YT_ID_11") {
			t.Fatalf("LEAK: VideoMetadataDTO marshaled with video ID: %s", string(videoBytes))
		}

		// 4. File DTO marshaling never leaks storage_key
		rawFile := models.SubjectFile{
			ID:         "f-test",
			SubjectID:  "s-test",
			Kind:       "book",
			StorageKey: "SUPER_SECRET_STORAGE_KEY",
			SizeBytes:  100,
		}
		fileDTO := rawFile.ToDTO()
		fileBytes, err := json.Marshal(fileDTO)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(fileBytes), "storage_key") || strings.Contains(string(fileBytes), "SUPER_SECRET_STORAGE_KEY") {
			t.Fatalf("LEAK: FileMetadataDTO marshaled with storage_key: %s", string(fileBytes))
		}

		// 5. SubjectDetailDTO marshaling test
		rawSubj := models.Subject{
			ID:              "s-detail-test",
			LevelKey:        "bachelor-y1",
			Term:            "first",
			TitleAr:         "مادة",
			Status:          models.StatusPublished,
			AccessExpiresAt: time.Now(),
		}
		detailDTO := rawSubj.ToDetailDTO(models.SubjectCountsDTO{}, []models.VideoMetadataDTO{videoDTO}, []models.FileMetadataDTO{fileDTO}, false, false)
		detailBytes, err := json.Marshal(detailDTO)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"youtube_video_id", "REAL_YT_ID_11", "storage_key", "SUPER_SECRET_STORAGE_KEY"} {
			if strings.Contains(string(detailBytes), forbidden) {
				t.Fatalf("LEAK: SubjectDetailDTO JSON contains forbidden token %q: %s", forbidden, string(detailBytes))
			}
		}
	})
}

func TestOwnership_ListAndDetail(t *testing.T) {
	s := newTestServer(false)
	h := s.PublicHandler()
	ctx := context.Background()

	// Create a published subject
	subj := &models.Subject{
		ID:              "subj-own-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة الملكية",
		TitleEn:         "Ownership Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(1 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, subj); err != nil {
		t.Fatalf("CreateSubject failed: %v", err)
	}

	user1Token := makeStudentToken(t, "user-own-1")
	user2Token := makeStudentToken(t, "user-own-2")

	// 1. Initial state: neither user owns it
	t.Run("initial_not_owned", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+user1Token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var listResp models.SubjectListResponseDTO
		if err := json.NewDecoder(rec.Body).Decode(&listResp); err != nil {
			t.Fatal(err)
		}
		if len(listResp.Items) != 1 || listResp.Items[0].Owned {
			t.Fatalf("expected 1 item with owned=false, got %+v", listResp.Items)
		}

		// Detail
		reqD := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-own-1", nil)
		reqD.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqD.Header.Set("Authorization", "Bearer "+user1Token)
		recD := httptest.NewRecorder()
		h.ServeHTTP(recD, reqD)

		if recD.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", recD.Code)
		}
		var detailResp models.SubjectDetailDTO
		if err := json.NewDecoder(recD.Body).Decode(&detailResp); err != nil {
			t.Fatal(err)
		}
		if detailResp.Owned {
			t.Fatalf("expected detail owned=false, got true")
		}
	})

	// 2. Grant entitlement to user 1
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-own-1", SubjectID: "subj-own-1"}); err != nil {
		t.Fatalf("Grant failed: %v", err)
	}

	t.Run("user1_owned_user2_not_owned", func(t *testing.T) {
		// User 1 list
		req1 := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		req1.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req1.Header.Set("Authorization", "Bearer "+user1Token)
		rec1 := httptest.NewRecorder()
		h.ServeHTTP(rec1, req1)

		var listResp1 models.SubjectListResponseDTO
		_ = json.NewDecoder(rec1.Body).Decode(&listResp1)
		if len(listResp1.Items) != 1 || !listResp1.Items[0].Owned {
			t.Fatalf("expected user 1 list owned=true, got %+v", listResp1.Items)
		}

		// User 1 detail
		reqD1 := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-own-1", nil)
		reqD1.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqD1.Header.Set("Authorization", "Bearer "+user1Token)
		recD1 := httptest.NewRecorder()
		h.ServeHTTP(recD1, reqD1)

		var detailResp1 models.SubjectDetailDTO
		_ = json.NewDecoder(recD1.Body).Decode(&detailResp1)
		if !detailResp1.Owned {
			t.Fatalf("expected user 1 detail owned=true, got false")
		}

		// User 2 list (must NOT be owned)
		req2 := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		req2.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req2.Header.Set("Authorization", "Bearer "+user2Token)
		rec2 := httptest.NewRecorder()
		h.ServeHTTP(rec2, req2)

		var listResp2 models.SubjectListResponseDTO
		_ = json.NewDecoder(rec2.Body).Decode(&listResp2)
		if len(listResp2.Items) != 1 || listResp2.Items[0].Owned {
			t.Fatalf("expected user 2 list owned=false, got %+v", listResp2.Items)
		}

		// User 2 detail (must NOT be owned)
		reqD2 := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-own-1", nil)
		reqD2.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqD2.Header.Set("Authorization", "Bearer "+user2Token)
		recD2 := httptest.NewRecorder()
		h.ServeHTTP(recD2, reqD2)

		var detailResp2 models.SubjectDetailDTO
		_ = json.NewDecoder(recD2.Body).Decode(&detailResp2)
		if detailResp2.Owned {
			t.Fatalf("expected user 2 detail owned=false, got true")
		}
	})
}

func TestR2Gating_SubjectDetailAndLeakTests(t *testing.T) {
	s := newTestServer(false)
	h := s.PublicHandler()
	ctx := context.Background()

	const (
		videoIDPos1        = "yt_gate_pos1_111"
		videoIDPos2        = "yt_gate_pos2_222"
		videoIDUnpublished = "yt_gate_unpub_333"
		videoIDExpiredSubj = "yt_gate_exp_4444"
	)

	// Subject 1: published with videos
	subj1 := &models.Subject{
		ID:              "subj-gate-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة البوابة",
		TitleEn:         "Gated Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, subj1); err != nil {
		t.Fatalf("CreateSubject subj1: %v", err)
	}

	// Video 1: published, position 2
	v1 := &models.Video{
		ID:             "vid-pos2",
		SubjectID:      "subj-gate-1",
		Position:       2,
		TitleAr:        "فيديو 2",
		TitleEn:        "Video 2",
		YouTubeVideoID: videoIDPos2,
		Published:      true,
		CreatedAt:      time.Now(),
	}
	// Video 2: published, position 1
	v2 := &models.Video{
		ID:             "vid-pos1",
		SubjectID:      "subj-gate-1",
		Position:       1,
		TitleAr:        "فيديو 1",
		TitleEn:        "Video 1",
		YouTubeVideoID: videoIDPos1,
		Published:      true,
		CreatedAt:      time.Now(),
	}
	// Video 3: UNPUBLISHED, position 3
	v3 := &models.Video{
		ID:             "vid-unpub",
		SubjectID:      "subj-gate-1",
		Position:       3,
		TitleAr:        "فيديو غير منشور",
		TitleEn:        "Unpublished Video",
		YouTubeVideoID: videoIDUnpublished,
		Published:      false,
		CreatedAt:      time.Now(),
	}
	for _, v := range []*models.Video{v1, v2, v3} {
		if err := s.Store.CreateVideo(ctx, v); err != nil {
			t.Fatalf("CreateVideo: %v", err)
		}
	}

	// Subject 2: short expiry for owned-but-expired test (expires_at = now - 1s after waiting)
	subjExpired := &models.Subject{
		ID:              "subj-gate-exp",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة منتهية للبوابة",
		TitleEn:         "Expired Gated Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(50 * time.Millisecond),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, subjExpired); err != nil {
		t.Fatalf("CreateSubject subjExpired: %v", err)
	}
	vExp := &models.Video{
		ID:             "vid-exp",
		SubjectID:      "subj-gate-exp",
		Position:       1,
		TitleAr:        "فيديو منتهي",
		TitleEn:        "Expired Video",
		YouTubeVideoID: videoIDExpiredSubj,
		Published:      true,
		CreatedAt:      time.Now(),
	}
	if err := s.Store.CreateVideo(ctx, vExp); err != nil {
		t.Fatalf("CreateVideo vExp: %v", err)
	}

	ownerToken := makeStudentToken(t, "user-gate-owner")
	nonOwnerToken := makeStudentToken(t, "user-gate-nonowner")
	otherUserToken := makeStudentToken(t, "user-gate-other")
	expiredUserToken := makeStudentToken(t, "user-gate-expired")

	// Grant subj-gate-1 to owner
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-gate-owner", SubjectID: "subj-gate-1"}); err != nil {
		t.Fatalf("Grant to owner: %v", err)
	}

	// Grant subj-gate-exp to expiredUser before it expires
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-gate-expired", SubjectID: "subj-gate-exp"}); err != nil {
		t.Fatalf("Grant to expiredUser: %v", err)
	}

	// Wait for subj-gate-exp entitlement to expire
	time.Sleep(70 * time.Millisecond)

	// 1. Positive test: owned -> playable=true in position order, no YouTube IDs in detail
	t.Run("positive_owned_playable_flags_present_in_position_order", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-gate-1", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		rawBody := rec.Body.String()

		var detail models.SubjectDetailDTO
		if err := json.Unmarshal([]byte(rawBody), &detail); err != nil {
			t.Fatalf("unmarshal detail: %v", err)
		}
		if !detail.Owned {
			t.Fatalf("expected owned=true")
		}
		if len(detail.Videos) != 2 {
			t.Fatalf("expected 2 published videos, got %d", len(detail.Videos))
		}
		if detail.Videos[0].Position != 1 || !detail.Videos[0].Playable {
			t.Errorf("video[0] position=%d playable=%v, want pos=1 playable=true", detail.Videos[0].Position, detail.Videos[0].Playable)
		}
		if detail.Videos[1].Position != 2 || !detail.Videos[1].Playable {
			t.Errorf("video[1] position=%d playable=%v, want pos=2 playable=true", detail.Videos[1].Position, detail.Videos[1].Playable)
		}
		// Confirm IDs do NOT appear in subject detail raw JSON (video IDs only at play time)
		if strings.Contains(rawBody, videoIDPos1) || strings.Contains(rawBody, videoIDPos2) {
			t.Errorf("LEAK: raw JSON contains video IDs: %s", rawBody)
		}
		if strings.Contains(rawBody, "youtube_video_id") {
			t.Errorf("LEAK: raw JSON contains youtube_video_id: %s", rawBody)
		}
		// Confirm unpublished video ID does not appear
		if strings.Contains(rawBody, videoIDUnpublished) {
			t.Errorf("LEAK: raw JSON contains unpublished video ID %q: %s", videoIDUnpublished, rawBody)
		}
	})

	// 2. Leak test: not owned
	t.Run("leak_not_owned", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-gate-1", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+nonOwnerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		rawBody := rec.Body.String()
		for _, forbidden := range []string{"youtube_video_id", videoIDPos1, videoIDPos2, videoIDUnpublished} {
			if strings.Contains(rawBody, forbidden) {
				t.Fatalf("LEAK in not_owned: body contains forbidden %q: %s", forbidden, rawBody)
			}
		}
	})

	// 3. Leak test: owned-but-expired (expires_at = now - 1s)
	t.Run("leak_owned_but_expired", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-gate-exp", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+expiredUserToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		rawBody := rec.Body.String()
		for _, forbidden := range []string{"youtube_video_id", videoIDExpiredSubj} {
			if strings.Contains(rawBody, forbidden) {
				t.Fatalf("LEAK in owned_but_expired: body contains forbidden %q: %s", forbidden, rawBody)
			}
		}
	})

	// 4. Leak test: owned by ANOTHER user
	t.Run("leak_owned_by_another_user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-gate-1", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+otherUserToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		rawBody := rec.Body.String()
		for _, forbidden := range []string{"youtube_video_id", videoIDPos1, videoIDPos2, videoIDUnpublished} {
			if strings.Contains(rawBody, forbidden) {
				t.Fatalf("LEAK in owned_by_another_user: body contains forbidden %q: %s", forbidden, rawBody)
			}
		}
	})

	// 5. Leak test: unpublished video inside an owned subject
	t.Run("leak_unpublished_video_inside_owned_subject", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-gate-1", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		rawBody := rec.Body.String()
		if strings.Contains(rawBody, videoIDUnpublished) {
			t.Fatalf("LEAK: owned subject detail contains unpublished video ID %q: %s", videoIDUnpublished, rawBody)
		}
	})

	// 6. Leak test: list endpoints while owned
	t.Run("leak_list_endpoints_while_owned", func(t *testing.T) {
		// /academy/subjects
		reqList := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		reqList.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqList.Header.Set("Authorization", "Bearer "+ownerToken)
		recList := httptest.NewRecorder()
		h.ServeHTTP(recList, reqList)

		if recList.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", recList.Code)
		}
		rawListBody := recList.Body.String()
		for _, forbidden := range []string{"youtube_video_id", videoIDPos1, videoIDPos2, videoIDUnpublished, videoIDExpiredSubj} {
			if strings.Contains(rawListBody, forbidden) {
				t.Fatalf("LEAK in /academy/subjects while owned: body contains %q: %s", forbidden, rawListBody)
			}
		}

		// /academy/levels
		reqLevels := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		reqLevels.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqLevels.Header.Set("Authorization", "Bearer "+ownerToken)
		recLevels := httptest.NewRecorder()
		h.ServeHTTP(recLevels, reqLevels)

		if recLevels.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", recLevels.Code)
		}
		rawLevelsBody := recLevels.Body.String()
		for _, forbidden := range []string{"youtube_video_id", videoIDPos1, videoIDPos2, videoIDUnpublished, videoIDExpiredSubj} {
			if strings.Contains(rawLevelsBody, forbidden) {
				t.Fatalf("LEAK in /academy/levels while owned: body contains %q: %s", forbidden, rawLevelsBody)
			}
		}
	})

	// 7. Leak test: error bodies (404/401) while owned
	t.Run("leak_error_bodies_while_owned", func(t *testing.T) {
		// 404 for unknown subject
		req404 := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-non-existent", nil)
		req404.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req404.Header.Set("Authorization", "Bearer "+ownerToken)
		rec404 := httptest.NewRecorder()
		h.ServeHTTP(rec404, req404)

		if rec404.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec404.Code)
		}
		raw404 := rec404.Body.String()
		for _, forbidden := range []string{"youtube_video_id", videoIDPos1, videoIDPos2, videoIDUnpublished} {
			if strings.Contains(raw404, forbidden) {
				t.Fatalf("LEAK in 404 body: contains %q: %s", forbidden, raw404)
			}
		}

		// 401 for invalid token
		req401 := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-gate-1", nil)
		req401.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req401.Header.Set("Authorization", "Bearer invalid-token-sig")
		rec401 := httptest.NewRecorder()
		h.ServeHTTP(rec401, req401)

		if rec401.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec401.Code)
		}
		raw401 := rec401.Body.String()
		for _, forbidden := range []string{"youtube_video_id", videoIDPos1, videoIDPos2, videoIDUnpublished} {
			if strings.Contains(raw401, forbidden) {
				t.Fatalf("LEAK in 401 body: contains %q: %s", forbidden, raw401)
			}
		}
	})

	// 8. Log check: run handler with captured logger and assert no video ID appears in log output
	t.Run("log_check_no_video_id_in_logs", func(t *testing.T) {
		var logBuf bytes.Buffer
		origOutput := log.Writer()
		log.SetOutput(&logBuf)
		defer log.SetOutput(origOutput)

		// Execute 200 owned detail request
		reqDetail := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-gate-1", nil)
		reqDetail.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqDetail.Header.Set("Authorization", "Bearer "+ownerToken)
		recDetail := httptest.NewRecorder()
		h.ServeHTTP(recDetail, reqDetail)
		if recDetail.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", recDetail.Code)
		}

		// Execute not-owned detail request
		reqUnowned := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-gate-1", nil)
		reqUnowned.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqUnowned.Header.Set("Authorization", "Bearer "+nonOwnerToken)
		recUnowned := httptest.NewRecorder()
		h.ServeHTTP(recUnowned, reqUnowned)
		if recUnowned.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", recUnowned.Code)
		}

		// Execute 404 request
		req404 := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-not-found", nil)
		req404.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req404.Header.Set("Authorization", "Bearer "+ownerToken)
		rec404 := httptest.NewRecorder()
		h.ServeHTTP(rec404, req404)

		// Execute list request
		reqList := httptest.NewRequest(http.MethodGet, "/academy/subjects", nil)
		reqList.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqList.Header.Set("Authorization", "Bearer "+ownerToken)
		recList := httptest.NewRecorder()
		h.ServeHTTP(recList, reqList)

		capturedLogs := logBuf.String()
		for _, forbidden := range []string{videoIDPos1, videoIDPos2, videoIDUnpublished, videoIDExpiredSubj} {
			if strings.Contains(capturedLogs, forbidden) {
				t.Fatalf("LEAK in server logs: log output contains video ID %q:\n%s", forbidden, capturedLogs)
			}
		}
	})
}

func TestPurchaseRequests_StudentEndpoint(t *testing.T) {
	s := newTestServer(false)
	ctx := context.Background()

	// 1. Published subject with future expiry
	subjActive := &models.Subject{
		ID:              "subj-pr-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة نشطة",
		TitleEn:         "Active Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	_ = s.Store.CreateSubject(ctx, subjActive)

	// 2. Expired subject (access_expires_at in past)
	subjExpired := &models.Subject{
		ID:              "subj-pr-expired",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة منتهية",
		TitleEn:         "Expired Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(-1 * time.Hour),
		CreatedAt:       time.Now().Add(-48 * time.Hour),
		UpdatedAt:       time.Now().Add(-48 * time.Hour),
	}
	_ = s.Store.CreateSubject(ctx, subjExpired)

	// 3. Unpublished subject
	subjDraft := &models.Subject{
		ID:              "subj-pr-draft",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة مسودة",
		TitleEn:         "Draft Subject",
		Status:          models.StatusDraft,
		AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	_ = s.Store.CreateSubject(ctx, subjDraft)

	// 4. Owned subject for user-owned
	subjOwned := &models.Subject{
		ID:              "subj-pr-owned",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة مملوكة",
		TitleEn:         "Owned Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	_ = s.Store.CreateSubject(ctx, subjOwned)
	_ = s.Store.Grant(ctx, &models.Entitlement{
		UserID:    "user-pr-owner",
		SubjectID: "subj-pr-owned",
	})

	h := s.PublicHandler()
	studentToken := makeStudentToken(t, "user-pr-student")
	ownerToken := makeStudentToken(t, "user-pr-owner")

	t.Run("valid_first_request_returns_200_and_DTO", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-pr-1/access-request", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+studentToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: body=%s", rec.Code, rec.Body.String())
		}

		var resp models.AccessRequestResponseDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json unmarshal failed: %v", err)
		}
		if resp.ID == "" {
			t.Error("expected non-empty request id")
		}
		if resp.SubjectID != "subj-pr-1" {
			t.Errorf("subject_id = %q, want subj-pr-1", resp.SubjectID)
		}
		if resp.Status != models.RequestStatusPending {
			t.Errorf("status = %q, want pending", resp.Status)
		}
		if resp.CreatedAt.IsZero() {
			t.Error("expected non-zero created_at")
		}
		if resp.WhatsAppURL != "https://wa.me/201000000000" {
			t.Errorf("whatsapp_url = %q, want https://wa.me/201000000000", resp.WhatsAppURL)
		}

		// Leak check: no admin fields in response body
		bodyStr := rec.Body.String()
		for _, forbidden := range []string{"decided_by", "decided_at", "reject_reason", "price", "payment"} {
			if strings.Contains(bodyStr, forbidden) {
				t.Fatalf("LEAK: access-request response body contains forbidden field %q: %s", forbidden, bodyStr)
			}
		}
	})

	t.Run("idempotent_second_request_returns_same_id_200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-pr-1/access-request", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+studentToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: body=%s", rec.Code, rec.Body.String())
		}

		var resp models.AccessRequestResponseDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json unmarshal failed: %v", err)
		}

		// Check against store
		pr, err := s.Store.GetPendingRequest(ctx, "user-pr-student", "subj-pr-1")
		if err != nil {
			t.Fatalf("GetPendingRequest failed: %v", err)
		}
		if pr == nil {
			t.Fatal("expected pending request in store")
		}
		if resp.ID != pr.ID {
			t.Errorf("second call ID = %q, want %q", resp.ID, pr.ID)
		}
	})

	t.Run("refuse_409_when_already_owned_R1", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-pr-owned/access-request", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict when already owned, got %d: body=%s", rec.Code, rec.Body.String())
		}

		var safeErr handlerutil.SafeError
		if err := json.Unmarshal(rec.Body.Bytes(), &safeErr); err != nil {
			t.Fatalf("json unmarshal failed: %v", err)
		}
		if safeErr.Code != handlerutil.ErrCodeConflict {
			t.Errorf("expected code conflict, got %q", safeErr.Code)
		}
	})

	t.Run("refuse_409_when_subject_expired_D20", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-pr-expired/access-request", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+studentToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict for expired subject, got %d: body=%s", rec.Code, rec.Body.String())
		}

		var safeErr handlerutil.SafeError
		if err := json.Unmarshal(rec.Body.Bytes(), &safeErr); err != nil {
			t.Fatalf("json unmarshal failed: %v", err)
		}
		if safeErr.Code != handlerutil.ErrCodeConflict {
			t.Errorf("expected code conflict, got %q", safeErr.Code)
		}
	})

	t.Run("refuse_404_for_unpublished_or_unknown_subject", func(t *testing.T) {
		for _, path := range []string{
			"/academy/subjects/subj-pr-draft/access-request",
			"/academy/subjects/subj-non-existent-999/access-request",
		} {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
			req.Header.Set("Authorization", "Bearer "+studentToken)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: expected 404, got %d", path, rec.Code)
			}
		}
	})

	t.Run("auth_checks_missing_secret_or_token", func(t *testing.T) {
		// Missing gateway secret
		req1 := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-pr-1/access-request", nil)
		req1.Header.Set("Authorization", "Bearer "+studentToken)
		rec1 := httptest.NewRecorder()
		h.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusUnauthorized {
			t.Errorf("missing secret: expected 401, got %d", rec1.Code)
		}

		// Missing bearer token
		req2 := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-pr-1/access-request", nil)
		req2.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		rec2 := httptest.NewRecorder()
		h.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusUnauthorized {
			t.Errorf("missing token: expected 401, got %d", rec2.Code)
		}
	})

	t.Run("method_not_allowed_for_non_post", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-pr-1/access-request", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+studentToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET access-request: expected 405, got %d", rec.Code)
		}
	})
}

func TestPurchaseRequests_SubjectDetailPendingStatus(t *testing.T) {
	s := newTestServer(false)
	ctx := context.Background()

	subj := &models.Subject{
		ID:              "subj-detail-req-test",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة طلب",
		TitleEn:         "Request Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	_ = s.Store.CreateSubject(ctx, subj)

	h := s.PublicHandler()
	studentA := makeStudentToken(t, "user-detail-student-a")
	studentB := makeStudentToken(t, "user-detail-student-b")

	// 1. Before requesting: detail for student A has no request field
	reqA1 := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-detail-req-test", nil)
	reqA1.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqA1.Header.Set("Authorization", "Bearer "+studentA)
	recA1 := httptest.NewRecorder()
	h.ServeHTTP(recA1, reqA1)
	if recA1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recA1.Code)
	}
	var detailA1 models.SubjectDetailDTO
	_ = json.Unmarshal(recA1.Body.Bytes(), &detailA1)
	if detailA1.Request != nil {
		t.Errorf("expected request == nil before access request, got %v", detailA1.Request)
	}
	if strings.Contains(recA1.Body.String(), `"request"`) {
		t.Errorf("expected request key absent from JSON, got %s", recA1.Body.String())
	}

	// 2. Student A submits access request
	postReq := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-detail-req-test/access-request", nil)
	postReq.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	postReq.Header.Set("Authorization", "Bearer "+studentA)
	postRec := httptest.NewRecorder()
	h.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", postRec.Code)
	}

	// 3. Detail for student A now has request: {"status": "pending"}
	reqA2 := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-detail-req-test", nil)
	reqA2.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqA2.Header.Set("Authorization", "Bearer "+studentA)
	recA2 := httptest.NewRecorder()
	h.ServeHTTP(recA2, reqA2)
	var detailA2 models.SubjectDetailDTO
	_ = json.Unmarshal(recA2.Body.Bytes(), &detailA2)
	if detailA2.Request == nil {
		t.Fatal("expected request != nil after access request")
	}
	if detailA2.Request.Status != models.RequestStatusPending {
		t.Errorf("request.status = %q, want pending", detailA2.Request.Status)
	}
	if !strings.Contains(recA2.Body.String(), `"request":{"status":"pending"}`) {
		t.Errorf("expected JSON to contain request pending, got %s", recA2.Body.String())
	}

	// 4. Detail for student B still has no request field
	reqB := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-detail-req-test", nil)
	reqB.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqB.Header.Set("Authorization", "Bearer "+studentB)
	recB := httptest.NewRecorder()
	h.ServeHTTP(recB, reqB)
	var detailB models.SubjectDetailDTO
	_ = json.Unmarshal(recB.Body.Bytes(), &detailB)
	if detailB.Request != nil {
		t.Errorf("expected student B request == nil, got %v", detailB.Request)
	}

	// 5. When subject becomes owned, request is absent
	_ = s.Store.Grant(ctx, &models.Entitlement{
		UserID:    "user-detail-student-a",
		SubjectID: "subj-detail-req-test",
	})
	reqA3 := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-detail-req-test", nil)
	reqA3.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqA3.Header.Set("Authorization", "Bearer "+studentA)
	recA3 := httptest.NewRecorder()
	h.ServeHTTP(recA3, reqA3)
	var detailA3 models.SubjectDetailDTO
	_ = json.Unmarshal(recA3.Body.Bytes(), &detailA3)
	if !detailA3.Owned {
		t.Fatal("expected owned == true")
	}
	if detailA3.Request != nil {
		t.Errorf("expected request == nil when owned, got %v", detailA3.Request)
	}
}

func TestPurchaseRequests_HandlerConcurrency(t *testing.T) {
	s := newTestServer(false)
	ctx := context.Background()

	subj := &models.Subject{
		ID:              "subj-conc-handler-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة التزامن هاندلر",
		TitleEn:         "Handler Concurrency Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	_ = s.Store.CreateSubject(ctx, subj)

	h := s.PublicHandler()
	studentToken := makeStudentToken(t, "user-conc-handler-student")

	var wg sync.WaitGroup
	const routines = 20
	resps := make([]models.AccessRequestResponseDTO, routines)
	codes := make([]int, routines)

	for i := 0; i < routines; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-conc-handler-1/access-request", nil)
			req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
			req.Header.Set("Authorization", "Bearer "+studentToken)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			codes[idx] = rec.Code
			if rec.Code == http.StatusOK {
				_ = json.Unmarshal(rec.Body.Bytes(), &resps[idx])
			}
		}()
	}
	wg.Wait()

	// All 20 goroutines must get 200 OK
	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("routine %d got code %d, want 200", i, code)
		}
	}

	// All 20 callers must receive the exact same request ID
	expectedID := resps[0].ID
	if expectedID == "" {
		t.Fatal("expected non-empty request ID")
	}
	for i, resp := range resps {
		if resp.ID != expectedID {
			t.Errorf("caller %d got ID %q, want %q", i, resp.ID, expectedID)
		}
		if resp.Status != models.RequestStatusPending {
			t.Errorf("caller %d got status %q, want pending", i, resp.Status)
		}
	}

	// Exactly one pending row exists in store
	userReqs, err := s.Store.ListRequestsByUser(ctx, "user-conc-handler-student")
	if err != nil {
		t.Fatalf("ListRequestsByUser failed: %v", err)
	}
	if len(userReqs) != 1 {
		t.Fatalf("expected exactly 1 stored request, got %d", len(userReqs))
	}
	if userReqs[0].ID != expectedID {
		t.Errorf("stored ID %q != expected %q", userReqs[0].ID, expectedID)
	}
}

func setupTestLimiter(t *testing.T, read, play, download, write int) (*miniredis.Miniredis, limiter.TierLimiter) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("failed to connect to miniredis: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, limiter.NewRedisTierLimiter(rdb, read, play, download, write)
}

func TestRateLimitTiers_ReadTierExhaustion(t *testing.T) {
	s := newTestServer(false)
	_, tl := setupTestLimiter(t, 30, 20, 10, 5)
	s.Limiter = tl

	h := s.PublicHandler()
	const userID = "student-read-rl"
	tok := makeStudentToken(t, userID)

	for i := 1; i <= 30; i++ {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d", i, rec.Code)
		}
	}

	// 31st call must be rate limited (429)
	req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("call 31: expected 429, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	retryAfterStr := rec.Header().Get("Retry-After")
	if retryAfterStr == "" {
		t.Fatal("expected Retry-After header on 429")
	}
	retryAfter, err := strconv.Atoi(retryAfterStr)
	if err != nil || retryAfter <= 0 {
		t.Fatalf("invalid Retry-After value: %q", retryAfterStr)
	}

	var errBody map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("invalid json response body: %v", err)
	}
	if errBody["code"] != "rate_limited" {
		t.Errorf("expected code=rate_limited, got %v", errBody["code"])
	}
}

func TestRateLimitTiers_PlayTierExhaustion(t *testing.T) {
	s := newTestServer(false)
	_, tl := setupTestLimiter(t, 120, 2, 10, 5) // play limit set to 2 for fast test
	s.Limiter = tl

	ctx := context.Background()
	subj := &models.Subject{
		ID:              "subj-rl-play",
		LevelKey:        "bachelor-y1",
		TitleAr:         "مدخل",
		Price:           100,
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(48 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, subj); err != nil {
		t.Fatal(err)
	}
	vPublished := &models.Video{
		ID:             "vid-rl-play",
		SubjectID:      "subj-rl-play",
		TitleAr:        "درس",
		Published:      true,
		YouTubeVideoID: "12345678901",
	}
	if err := s.Store.CreateVideo(ctx, vPublished); err != nil {
		t.Fatal(err)
	}
	const userID = "student-play-rl"
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: userID, SubjectID: "subj-rl-play"}); err != nil {
		t.Fatal(err)
	}

	h := s.PublicHandler()
	tok := makeStudentToken(t, userID)

	for i := 1; i <= 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-rl-play/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d (body: %s)", i, rec.Code, rec.Body.String())
		}
	}

	// 3rd call must be rate limited (429)
	req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-rl-play/play", nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("call 3: expected 429, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429")
	}
}

func TestRateLimitTiers_WriteTierExhaustion(t *testing.T) {
	s := newTestServer(false)
	_, tl := setupTestLimiter(t, 30, 20, 10, 5)
	s.Limiter = tl

	ctx := context.Background()
	subj := &models.Subject{
		ID:              "subj-rl-write",
		LevelKey:        "bachelor-y1",
		TitleAr:         "مدخل قانون",
		Price:           100,
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(48 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, subj); err != nil {
		t.Fatalf("CreateSubject failed: %v", err)
	}

	h := s.PublicHandler()
	const userID = "student-write-rl"
	tok := makeStudentToken(t, userID)

	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-rl-write/access-request", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d (body: %s)", i, rec.Code, rec.Body.String())
		}
	}

	// 6th call must be rate limited (429)
	req := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-rl-write/access-request", nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("call 6: expected 429, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	retryAfterStr := rec.Header().Get("Retry-After")
	if retryAfterStr == "" {
		t.Fatal("expected Retry-After header on 429")
	}
	retryAfter, err := strconv.Atoi(retryAfterStr)
	if err != nil || retryAfter <= 0 {
		t.Fatalf("invalid Retry-After value: %q", retryAfterStr)
	}
}

func TestRateLimitTiers_DownloadTier(t *testing.T) {
	s := newTestServer(false)
	_, tl := setupTestLimiter(t, 30, 20, 10, 5)
	s.Limiter = tl

	const userID = "student-download-rl"
	tok := makeStudentToken(t, userID)

	dummyDownloadHandler := s.EnforceTier(limiter.TierDownload, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pdf-content"))
	})

	for i := 1; i <= 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/academy/subjects/s1/files/f1/download", nil)
		claims, _ := jwtutil.ValidateToken(tok)
		req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
		rec := httptest.NewRecorder()

		dummyDownloadHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d", i, rec.Code)
		}
	}

	// 11th call must be rate limited (429)
	req := httptest.NewRequest(http.MethodGet, "/academy/subjects/s1/files/f1/download", nil)
	claims, _ := jwtutil.ValidateToken(tok)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()

	dummyDownloadHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("call 11: expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429")
	}
}

func TestRateLimitTiers_PerUserIsolation(t *testing.T) {
	s := newTestServer(false)
	_, tl := setupTestLimiter(t, 30, 20, 10, 5)
	s.Limiter = tl

	ctx := context.Background()
	subj := &models.Subject{
		ID:              "subj-rl-iso",
		LevelKey:        "bachelor-y1",
		TitleAr:         "مدخل",
		Price:           100,
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(48 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	_ = s.Store.CreateSubject(ctx, subj)

	h := s.PublicHandler()
	tokA := makeStudentToken(t, "user-rl-a")
	tokB := makeStudentToken(t, "user-rl-b")

	// Exhaust User A's write limit (5 calls)
	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-rl-iso/access-request", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tokA)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("user A call %d: expected 200, got %d", i, rec.Code)
		}
	}

	// User A call 6 is 429
	reqA := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-rl-iso/access-request", nil)
	reqA.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqA.Header.Set("Authorization", "Bearer "+tokA)
	recA := httptest.NewRecorder()
	h.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusTooManyRequests {
		t.Fatalf("user A call 6: expected 429, got %d", recA.Code)
	}

	// User B is unaffected
	reqB := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-rl-iso/access-request", nil)
	reqB.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqB.Header.Set("Authorization", "Bearer "+tokB)
	recB := httptest.NewRecorder()
	h.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("user B call 1: expected 200, got %d", recB.Code)
	}

	// User A reading is unaffected (Read tier is separate)
	reqARead := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
	reqARead.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqARead.Header.Set("Authorization", "Bearer "+tokA)
	recARead := httptest.NewRecorder()
	h.ServeHTTP(recARead, reqARead)
	if recARead.Code != http.StatusOK {
		t.Fatalf("user A read call: expected 200, got %d", recARead.Code)
	}
}

func TestRateLimitTiers_FailClosed_RedisDown(t *testing.T) {
	s := newTestServer(false)
	mr, tl := setupTestLimiter(t, 30, 20, 10, 5)
	s.Limiter = tl

	h := s.PublicHandler()
	tok := makeStudentToken(t, "user-redis-down")

	// Close redis
	mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 fail-closed when Redis is down, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "" {
		t.Fatal("Retry-After must NOT be set on 503 backend failure")
	}

	var errBody map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("invalid json body: %v", err)
	}
	if errBody["code"] != handlerutil.ErrCodeUnavailable {
		t.Errorf("expected code=%s, got %v", handlerutil.ErrCodeUnavailable, errBody["code"])
	}
}

func TestRateLimitTiers_FailClosed_NilLimiterOutsideDev(t *testing.T) {
	s := newTestServer(false)
	s.AppEnv = "production"
	s.Limiter = nil

	h := s.PublicHandler()
	tok := makeStudentToken(t, "user-nil-limiter")

	req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 fail-closed when Limiter is nil in production, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "" {
		t.Fatal("Retry-After must NOT be set on 503 backend failure")
	}

	var errBody map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("invalid json body: %v", err)
	}
	if errBody["code"] != handlerutil.ErrCodeUnavailable {
		t.Errorf("expected code=%s, got %v", handlerutil.ErrCodeUnavailable, errBody["code"])
	}
}

func TestRateLimitTiers_NoClaimsFailClosed(t *testing.T) {
	s := newTestServer(false)
	_, tl := setupTestLimiter(t, 30, 20, 10, 5)
	s.Limiter = tl

	handler := s.EnforceTier(limiter.TierRead, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/dummy", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when claims are missing, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "" {
		t.Fatal("Retry-After must NOT be set on 503 backend failure")
	}

	var errBody map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("invalid json body: %v", err)
	}
	if errBody["code"] != handlerutil.ErrCodeUnavailable {
		t.Errorf("expected code=%s, got %v", handlerutil.ErrCodeUnavailable, errBody["code"])
	}
}

func TestPlayVideoEndpoint(t *testing.T) {
	s := newTestServer(false)
	h := s.PublicHandler()
	ctx := context.Background()

	const (
		ytID1 = "dQw4w9WgXcQ"
		ytID2 = "jNQXAC9IVRw"
	)

	// Published subject
	subj := &models.Subject{
		ID:              "subj-play-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة التشغيل",
		TitleEn:         "Playback Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, subj); err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	// Draft subject
	draftSubj := &models.Subject{
		ID:              "subj-draft-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة مسودة",
		TitleEn:         "Draft Subject",
		Status:          models.StatusDraft,
		AccessExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, draftSubj); err != nil {
		t.Fatalf("CreateSubject draft: %v", err)
	}

	// Expired subject
	expiredSubj := &models.Subject{
		ID:              "subj-expired-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة منتهية",
		TitleEn:         "Expired Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(20 * time.Millisecond),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, expiredSubj); err != nil {
		t.Fatalf("CreateSubject expired: %v", err)
	}

	// Videos
	vPublished := &models.Video{
		ID:             "vid-pub-1",
		SubjectID:      "subj-play-1",
		Position:       1,
		TitleAr:        "فيديو 1",
		TitleEn:        "Video 1",
		YouTubeVideoID: ytID1,
		Published:      true,
		CreatedAt:      time.Now(),
	}
	vUnpublished := &models.Video{
		ID:             "vid-unpub-1",
		SubjectID:      "subj-play-1",
		Position:       2,
		TitleAr:        "فيديو غير منشور",
		TitleEn:        "Unpublished Video",
		YouTubeVideoID: ytID2,
		Published:      false,
		CreatedAt:      time.Now(),
	}
	vInDraft := &models.Video{
		ID:             "vid-draft-1",
		SubjectID:      "subj-draft-1",
		Position:       1,
		TitleAr:        "فيديو مادة مسودة",
		TitleEn:        "Draft Subject Video",
		YouTubeVideoID: ytID1,
		Published:      true,
		CreatedAt:      time.Now(),
	}
	vInExpired := &models.Video{
		ID:             "vid-exp-1",
		SubjectID:      "subj-expired-1",
		Position:       1,
		TitleAr:        "فيديو مادة منتهية",
		TitleEn:        "Expired Video",
		YouTubeVideoID: ytID1,
		Published:      true,
		CreatedAt:      time.Now(),
	}
	for _, v := range []*models.Video{vPublished, vUnpublished, vInDraft, vInExpired} {
		if err := s.Store.CreateVideo(ctx, v); err != nil {
			t.Fatalf("CreateVideo: %v", err)
		}
	}

	ownerToken := makeStudentToken(t, "user-play-owner")
	nonOwnerToken := makeStudentToken(t, "user-play-nonowner")
	expiredUserToken := makeStudentToken(t, "user-play-exp")

	// Grant subj-play-1 to owner
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-play-owner", SubjectID: "subj-play-1"}); err != nil {
		t.Fatalf("Grant to owner: %v", err)
	}
	// Grant subj-draft-1 to owner
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-play-owner", SubjectID: "subj-draft-1"}); err != nil {
		t.Fatalf("Grant to owner draft: %v", err)
	}
	// Grant subj-expired-1 to expiredUser
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-play-exp", SubjectID: "subj-expired-1"}); err != nil {
		t.Fatalf("Grant to expired: %v", err)
	}

	// Wait for expired subject to pass
	time.Sleep(40 * time.Millisecond)

	// 1. Success: owner plays published video
	t.Run("owner_play_published_video_200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-pub-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("expected Cache-Control: private, no-store on 200, got %q", rec.Header().Get("Cache-Control"))
		}

		var resp models.VideoPlayResponseDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.VideoID != "vid-pub-1" {
			t.Errorf("expected video_id vid-pub-1, got %q", resp.VideoID)
		}
		if resp.YouTubeVideoID != ytID1 {
			t.Errorf("expected youtube_video_id %q, got %q", ytID1, resp.YouTubeVideoID)
		}

		// Verify video_plays append-only log in store
		plays, err := s.Store.ListVideoPlaysByVideo(ctx, "vid-pub-1")
		if err != nil {
			t.Fatalf("ListVideoPlaysByVideo failed: %v", err)
		}
		if len(plays) != 1 {
			t.Fatalf("expected exactly 1 video_plays record, got %d", len(plays))
		}
		if plays[0].UserID != "user-play-owner" || plays[0].VideoID != "vid-pub-1" || plays[0].SubjectID != "subj-play-1" {
			t.Errorf("unexpected play record fields: %+v", plays[0])
		}
		if plays[0].PlayedAt.IsZero() {
			t.Error("expected non-zero played_at in video_plays record")
		}
	})

	// 2. Refusal: non-owner gets generic 404
	t.Run("non_owner_play_published_video_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-pub-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+nonOwnerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("expected Cache-Control: private, no-store on 404, got %q", rec.Header().Get("Cache-Control"))
		}
		if strings.Contains(rec.Body.String(), ytID1) {
			t.Fatalf("LEAK: 404 body contains youtube id: %s", rec.Body.String())
		}
	})

	// 3. Refusal: unpublished video returns generic 404 even to owner
	t.Run("owner_play_unpublished_video_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-unpub-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("expected Cache-Control: private, no-store on 404, got %q", rec.Header().Get("Cache-Control"))
		}
		if strings.Contains(rec.Body.String(), ytID2) {
			t.Fatalf("LEAK: 404 body contains unpublished youtube id: %s", rec.Body.String())
		}
	})

	// 4. Refusal: video in draft subject returns generic 404 even if entitled
	t.Run("owner_play_video_in_draft_subject_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-draft-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("expected Cache-Control: private, no-store on 404, got %q", rec.Header().Get("Cache-Control"))
		}
	})

	// 5. Refusal: expired subject access returns generic 404
	t.Run("expired_user_play_video_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-exp-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+expiredUserToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("expected Cache-Control: private, no-store on 404, got %q", rec.Header().Get("Cache-Control"))
		}
	})

	// 6. Refusal: non-existent video ID returns generic 404
	t.Run("play_nonexistent_video_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/does-not-exist/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("expected Cache-Control: private, no-store on 404, got %q", rec.Header().Get("Cache-Control"))
		}
	})

	// 7. Refusal: published video with empty youtube_video_id -> playable=false in detail and 404 on /play
	t.Run("published_video_empty_youtube_id_playable_false_and_play_404", func(t *testing.T) {
		vEmpty := &models.Video{
			ID:             "vid-empty-yt",
			SubjectID:      "subj-play-1",
			Position:       10,
			TitleAr:        "فيديو بدون يوتيوب",
			TitleEn:        "No YT Video",
			YouTubeVideoID: "",
			Published:      true,
		}
		if err := s.Store.CreateVideo(ctx, vEmpty); err != nil {
			t.Fatal(err)
		}

		// Subject detail: playable must be false even though owned=true and published=true
		getReq := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-play-1", nil)
		getReq.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		getReq.Header.Set("Authorization", "Bearer "+ownerToken)
		getRec := httptest.NewRecorder()
		h.ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusOK {
			t.Fatalf("expected 200 from get subject, got %d", getRec.Code)
		}
		var detail models.SubjectDetailDTO
		if err := json.Unmarshal(getRec.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		var foundEmpty *models.VideoMetadataDTO
		for _, v := range detail.Videos {
			if v.ID == "vid-empty-yt" {
				vCopy := v
				foundEmpty = &vCopy
				break
			}
		}
		if foundEmpty == nil {
			t.Fatal("expected vid-empty-yt in subject detail videos")
		}
		if foundEmpty.Playable {
			t.Errorf("expected playable=false for video with empty youtube_video_id, got true")
		}

		// /play request: must return 404 with Cache-Control: private, no-store
		playReq := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-empty-yt/play", nil)
		playReq.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		playReq.Header.Set("Authorization", "Bearer "+ownerToken)
		playRec := httptest.NewRecorder()
		h.ServeHTTP(playRec, playReq)
		if playRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", playRec.Code, playRec.Body.String())
		}
		if playRec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("expected Cache-Control: private, no-store, got %q", playRec.Header().Get("Cache-Control"))
		}
	})

	// 8. Resilience: video_plays write failure is logged with IDs only and NEVER blocks playback
	t.Run("video_play_log_failure_never_blocks_playback", func(t *testing.T) {
		sFailLog := newTestServer(false)
		sFailLog.Store = &errPlayLogStore{Store: s.Store, recordErr: errors.New("disk full write error")}
		hFailLog := sFailLog.PublicHandler()

		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-pub-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		hFailLog.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 even when log fails, got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("expected Cache-Control: private, no-store, got %q", rec.Header().Get("Cache-Control"))
		}
		var resp models.VideoPlayResponseDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.VideoID != "vid-pub-1" || resp.YouTubeVideoID != ytID1 {
			t.Errorf("unexpected play response: %+v", resp)
		}
	})

	// 9. Captured-logger test: /play (200 and 404) never logs the youtube id
	t.Run("captured_logger_never_logs_youtube_id", func(t *testing.T) {
		canaryYT := "YT_CANARY_SECRET_ID_7777"
		canaryVideo := &models.Video{
			ID:             "vid-canary-1",
			SubjectID:      "subj-play-1",
			Position:       20,
			TitleAr:        "فيديو كناري",
			TitleEn:        "Canary Video",
			YouTubeVideoID: canaryYT,
			Published:      true,
		}
		if err := s.Store.CreateVideo(ctx, canaryVideo); err != nil {
			t.Fatal(err)
		}

		var logBuf bytes.Buffer
		origOutput := log.Writer()
		log.SetOutput(&logBuf)
		defer log.SetOutput(origOutput)

		// 1. Success 200 call
		req200 := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-canary-1/play", nil)
		req200.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req200.Header.Set("Authorization", "Bearer "+ownerToken)
		rec200 := httptest.NewRecorder()
		h.ServeHTTP(rec200, req200)
		if rec200.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec200.Code, rec200.Body.String())
		}

		// 2. Refusal 404 call (non-owner)
		req404 := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-canary-1/play", nil)
		req404.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req404.Header.Set("Authorization", "Bearer "+nonOwnerToken)
		rec404 := httptest.NewRecorder()
		h.ServeHTTP(rec404, req404)
		if rec404.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec404.Code, rec404.Body.String())
		}

		// 3. Log failure path (error logging)
		sFailLog := newTestServer(false)
		sFailLog.Store = &errPlayLogStore{Store: s.Store, recordErr: errors.New("simulated log db error")}
		hFailLog := sFailLog.PublicHandler()

		reqErr := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-canary-1/play", nil)
		reqErr.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		reqErr.Header.Set("Authorization", "Bearer "+ownerToken)
		recErr := httptest.NewRecorder()
		hFailLog.ServeHTTP(recErr, reqErr)
		if recErr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", recErr.Code, recErr.Body.String())
		}

		capturedLogs := logBuf.String()
		if strings.Contains(capturedLogs, canaryYT) {
			t.Fatalf("SECURITY VIOLATION: captured logger contains youtube id %q:\n%s", canaryYT, capturedLogs)
		}
	})

	// 10. Method refusal: GET on /play returns 405 Method Not Allowed
	t.Run("get_play_405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/videos/vid-pub-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// 11. Auth refusal: missing token returns 401
	t.Run("missing_auth_token_401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-pub-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// 12. Auth refusal: missing gateway secret returns 401
	t.Run("missing_gateway_secret_401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-pub-1/play", nil)
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// 13. Rate limiting tier: Play tier (60/min) enforced
	t.Run("rate_limit_play_tier_enforced", func(t *testing.T) {
		mr, tl := setupTestLimiter(t, 30, 2, 10, 5) // set play limit to 2 for fast test
		defer mr.Close()
		sLimit := newTestServer(false)
		sLimit.Limiter = tl
		hLimit := sLimit.PublicHandler()

		rateUserToken := makeStudentToken(t, "user-play-rate")
		if err := sLimit.Store.CreateSubject(ctx, subj); err != nil {
			t.Fatal(err)
		}
		if err := sLimit.Store.CreateVideo(ctx, vPublished); err != nil {
			t.Fatal(err)
		}
		if err := sLimit.Store.Grant(ctx, &models.Entitlement{UserID: "user-play-rate", SubjectID: "subj-play-1"}); err != nil {
			t.Fatal(err)
		}

		// 1st request -> 200
		req1 := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-pub-1/play", nil)
		req1.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req1.Header.Set("Authorization", "Bearer "+rateUserToken)
		rec1 := httptest.NewRecorder()
		hLimit.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("call 1: expected 200, got %d: %s", rec1.Code, rec1.Body.String())
		}

		// 2nd request -> 200
		req2 := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-pub-1/play", nil)
		req2.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req2.Header.Set("Authorization", "Bearer "+rateUserToken)
		rec2 := httptest.NewRecorder()
		hLimit.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("call 2: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
		}

		// 3rd request -> 429 Too Many Requests
		req3 := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-pub-1/play", nil)
		req3.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req3.Header.Set("Authorization", "Bearer "+rateUserToken)
		rec3 := httptest.NewRecorder()
		hLimit.ServeHTTP(rec3, req3)
		if rec3.Code != http.StatusTooManyRequests {
			t.Fatalf("call 3: expected 429, got %d: %s", rec3.Code, rec3.Body.String())
		}
		if rec3.Header().Get("Retry-After") == "" {
			t.Error("expected Retry-After header on 429")
		}
	})
}

type errPlayLogStore struct {
	store.Store
	recordErr error
}

func (m *errPlayLogStore) RecordVideoPlay(ctx context.Context, play *models.VideoPlay) error {
	if m.recordErr != nil {
		return m.recordErr
	}
	return m.Store.RecordVideoPlay(ctx, play)
}

type errVideoStore struct {
	store.Store
	getVideoErr       error
	getSubjectErr     error
	hasEntitlementErr error
}

func (m *errVideoStore) GetVideoByID(ctx context.Context, id string) (*models.Video, error) {
	if m.getVideoErr != nil {
		return nil, m.getVideoErr
	}
	return m.Store.GetVideoByID(ctx, id)
}

func (m *errVideoStore) GetSubjectByID(ctx context.Context, id string) (*models.Subject, error) {
	if m.getSubjectErr != nil {
		return nil, m.getSubjectErr
	}
	return m.Store.GetSubjectByID(ctx, id)
}

func (m *errVideoStore) HasActiveEntitlement(ctx context.Context, userID, subjectID string) (bool, error) {
	if m.hasEntitlementErr != nil {
		return false, m.hasEntitlementErr
	}
	return m.Store.HasActiveEntitlement(ctx, userID, subjectID)
}

func TestPlayVideo_StoreErrorsFailClosed(t *testing.T) {
	ctx := context.Background()
	baseStore := store.NewMemoryStore()
	subj := &models.Subject{
		ID:              "subj-err-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(24 * time.Hour),
	}
	_ = baseStore.CreateSubject(ctx, subj)
	vid := &models.Video{
		ID:             "vid-err-1",
		SubjectID:      "subj-err-1",
		Published:      true,
		YouTubeVideoID: "12345678901",
	}
	_ = baseStore.CreateVideo(ctx, vid)
	_ = baseStore.Grant(ctx, &models.Entitlement{UserID: "u-err-1", SubjectID: "subj-err-1"})
	tok := makeStudentToken(t, "u-err-1")

	t.Run("get_video_error_503", func(t *testing.T) {
		s := newTestServer(false)
		s.Store = &errVideoStore{Store: baseStore, getVideoErr: errors.New("db error")}
		h := s.PublicHandler()

		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-err-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("get_subject_error_503", func(t *testing.T) {
		s := newTestServer(false)
		s.Store = &errVideoStore{Store: baseStore, getSubjectErr: errors.New("db error")}
		h := s.PublicHandler()

		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-err-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("has_entitlement_error_503", func(t *testing.T) {
		s := newTestServer(false)
		s.Store = &errVideoStore{Store: baseStore, hasEntitlementErr: errors.New("db error")}
		h := s.PublicHandler()

		req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-err-1/play", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestSubjectDetail_AccessExpiresAt_EntitlementVersusSubject(t *testing.T) {
	s := newTestServer(false)
	h := s.PublicHandler()
	ctx := context.Background()

	// 1. Admin creates subject with access_expires_at = T1
	t1 := time.Now().Add(10 * 24 * time.Hour).Truncate(time.Second)
	subj := &models.Subject{
		ID:              "subj-exp-test-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة اختبار الصلاحية",
		TitleEn:         "Expiry Test Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: t1,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, subj); err != nil {
		t.Fatalf("CreateSubject failed: %v", err)
	}

	// 2. Grant entitlement to student A (copies T1 per D21)
	const userA = "student-exp-a"
	if err := s.Store.Grant(ctx, &models.Entitlement{
		UserID:    userA,
		SubjectID: "subj-exp-test-1",
	}); err != nil {
		t.Fatalf("Grant failed: %v", err)
	}

	// 3. Admin updates subject access_expires_at to T2 (e.g. 30 days in future)
	t2 := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	subj.AccessExpiresAt = t2
	subj.UpdatedAt = time.Now()
	if err := s.Store.UpdateSubject(ctx, subj); err != nil {
		t.Fatalf("UpdateSubject failed: %v", err)
	}

	// 4. Student A (OWNED) requests detail: access_expires_at MUST be T1 (entitlement date)
	tokA := makeStudentToken(t, userA)
	reqA := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-exp-test-1", nil)
	reqA.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqA.Header.Set("Authorization", "Bearer "+tokA)
	recA := httptest.NewRecorder()
	h.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("student A call: expected 200, got %d: %s", recA.Code, recA.Body.String())
	}

	var detailA models.SubjectDetailDTO
	if err := json.Unmarshal(recA.Body.Bytes(), &detailA); err != nil {
		t.Fatalf("failed to decode student A response: %v", err)
	}
	if !detailA.Owned {
		t.Fatalf("expected student A to own subject")
	}
	if !detailA.AccessExpiresAt.Equal(t1) {
		t.Errorf("student A access_expires_at = %v, want %v (student's entitlement date)", detailA.AccessExpiresAt, t1)
	}
	if detailA.AccessExpiresAt.Equal(t2) {
		t.Errorf("student A access_expires_at should NOT match updated subject date %v", t2)
	}

	// 5. Student B (UNOWNED) requests detail: access_expires_at MUST be T2 (subject's current date)
	const userB = "student-exp-b"
	tokB := makeStudentToken(t, userB)
	reqB := httptest.NewRequest(http.MethodGet, "/academy/subjects/subj-exp-test-1", nil)
	reqB.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqB.Header.Set("Authorization", "Bearer "+tokB)
	recB := httptest.NewRecorder()
	h.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("student B call: expected 200, got %d: %s", recB.Code, recB.Body.String())
	}

	var detailB models.SubjectDetailDTO
	if err := json.Unmarshal(recB.Body.Bytes(), &detailB); err != nil {
		t.Fatalf("failed to decode student B response: %v", err)
	}
	if detailB.Owned {
		t.Fatalf("expected student B NOT to own subject")
	}
	if !detailB.AccessExpiresAt.Equal(t2) {
		t.Errorf("student B access_expires_at = %v, want %v (subject's updated date)", detailB.AccessExpiresAt, t2)
	}
}

func TestListSubjects_AccessExpiresAt_EntitlementVersusSubject(t *testing.T) {
	s := newTestServer(false)
	h := s.PublicHandler()
	ctx := context.Background()

	// 1. Admin creates subject with access_expires_at = T1
	t1 := time.Now().Add(14 * 24 * time.Hour).Truncate(time.Second)
	subj := &models.Subject{
		ID:              "subj-exp-list-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة قائمة الصلاحية",
		TitleEn:         "Expiry List Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: t1,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, subj); err != nil {
		t.Fatalf("CreateSubject failed: %v", err)
	}

	// 2. Grant entitlement to student A (copies T1 per D21)
	const userA = "student-exp-list-a"
	if err := s.Store.Grant(ctx, &models.Entitlement{
		UserID:    userA,
		SubjectID: "subj-exp-list-1",
	}); err != nil {
		t.Fatalf("Grant failed: %v", err)
	}

	// 3. Admin moves subject date to T2
	t2 := time.Now().Add(45 * 24 * time.Hour).Truncate(time.Second)
	subj.AccessExpiresAt = t2
	subj.UpdatedAt = time.Now()
	if err := s.Store.UpdateSubject(ctx, subj); err != nil {
		t.Fatalf("UpdateSubject failed: %v", err)
	}

	// 4. Student A lists subjects: owned subject has access_expires_at = T1
	tokA := makeStudentToken(t, userA)
	reqA := httptest.NewRequest(http.MethodGet, "/academy/subjects?level=bachelor-y1", nil)
	reqA.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqA.Header.Set("Authorization", "Bearer "+tokA)
	recA := httptest.NewRecorder()
	h.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("student A list: expected 200, got %d: %s", recA.Code, recA.Body.String())
	}

	var listA models.SubjectListResponseDTO
	if err := json.Unmarshal(recA.Body.Bytes(), &listA); err != nil {
		t.Fatalf("failed to decode student A list response: %v", err)
	}
	var foundA *models.SubjectListItemDTO
	for i := range listA.Items {
		if listA.Items[i].ID == "subj-exp-list-1" {
			foundA = &listA.Items[i]
			break
		}
	}
	if foundA == nil {
		t.Fatalf("subject subj-exp-list-1 not found in student A list")
	}
	if !foundA.Owned {
		t.Errorf("expected foundA.Owned = true")
	}
	if !foundA.AccessExpiresAt.Equal(t1) {
		t.Errorf("student A list item access_expires_at = %v, want %v (entitlement date)", foundA.AccessExpiresAt, t1)
	}

	// 5. Student B lists subjects: unowned subject has access_expires_at = T2
	const userB = "student-exp-list-b"
	tokB := makeStudentToken(t, userB)
	reqB := httptest.NewRequest(http.MethodGet, "/academy/subjects?level=bachelor-y1", nil)
	reqB.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqB.Header.Set("Authorization", "Bearer "+tokB)
	recB := httptest.NewRecorder()
	h.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("student B list: expected 200, got %d: %s", recB.Code, recB.Body.String())
	}

	var listB models.SubjectListResponseDTO
	if err := json.Unmarshal(recB.Body.Bytes(), &listB); err != nil {
		t.Fatalf("failed to decode student B list response: %v", err)
	}
	var foundB *models.SubjectListItemDTO
	for i := range listB.Items {
		if listB.Items[i].ID == "subj-exp-list-1" {
			foundB = &listB.Items[i]
			break
		}
	}
	if foundB == nil {
		t.Fatalf("subject subj-exp-list-1 not found in student B list")
	}
	if foundB.Owned {
		t.Errorf("expected foundB.Owned = false")
	}
	if !foundB.AccessExpiresAt.Equal(t2) {
		t.Errorf("student B list item access_expires_at = %v, want %v (subject date)", foundB.AccessExpiresAt, t2)
	}
}

func TestPlayVideo_EndedDeviceSessionRevoked_Refused401(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb, err := ratelimit.NewRedisClient(fmt.Sprintf("redis://%s", mr.Addr()))
	if err != nil {
		t.Fatalf("failed to connect to miniredis: %v", err)
	}
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	s := newTestServer(false)
	h := s.PublicHandler()
	ctx := context.Background()

	subj := &models.Subject{
		ID:              "subj-play-sess",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة الجلسات",
		TitleEn:         "Session Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.Store.CreateSubject(ctx, subj); err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	vid := &models.Video{
		ID:             "vid-play-sess",
		SubjectID:      "subj-play-sess",
		Position:       1,
		TitleAr:        "فيديو الجلسة",
		TitleEn:        "Session Video",
		YouTubeVideoID: "dQw4w9WgXcQ",
		Published:      true,
		CreatedAt:      time.Now(),
	}
	if err := s.Store.CreateVideo(ctx, vid); err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	if err := s.Store.Grant(ctx, &models.Entitlement{UserID: "user-sess-1", SubjectID: "subj-play-sess"}); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	sid := "sid-ended-device-1234"
	token, err := jwtutil.GenerateTokenWithSession("user-sess-1", "user", "user-sess@example.com", sid)
	if err != nil {
		t.Fatalf("GenerateTokenWithSession: %v", err)
	}

	// 1. When session is active (not revoked in Redis), /play succeeds through real StudentAuth middleware
	req := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-play-sess/play", nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for active session, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 2. Revoke session in Redis (simulating 3rd device login ending this device's session)
	if err := jwtutil.RevokeSession(sid); err != nil {
		t.Fatalf("RevokeSession failed: %v", err)
	}

	// 3. jwtutil.ValidateToken rejects the old access token immediately
	claims, valErr := jwtutil.ValidateToken(token)
	if valErr == nil || claims != nil {
		t.Fatalf("expected ValidateToken to fail for revoked sid, got claims=%+v, err=nil", claims)
	}
	if !errors.Is(valErr, jwtutil.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked, got %v", valErr)
	}

	// 4. Academy /play through real StudentAuth middleware immediately returns 401 Unauthorized
	reqRevoked := httptest.NewRequest(http.MethodPost, "/academy/videos/vid-play-sess/play", nil)
	reqRevoked.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	reqRevoked.Header.Set("Authorization", "Bearer "+token)
	recRevoked := httptest.NewRecorder()
	h.ServeHTTP(recRevoked, reqRevoked)
	if recRevoked.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for revoked session, got %d (%s)", recRevoked.Code, recRevoked.Body.String())
	}
}
