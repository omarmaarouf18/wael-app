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
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

func init() {
	jwtutil.Init("test-jwt-secret")
}

func newTestServer(exposePrice bool) *Server {
	st := store.NewMemoryStore()
	_ = st.SeedLevels(context.Background())
	return New(st, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", exposePrice)
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

func TestAdminHandler_EmptyInPhase2(t *testing.T) {
	s := newTestServer(false)
	handler := s.AdminHandler()

	req := httptest.NewRequest(http.MethodGet, "/internal/admin/anything", nil)
	req.Header.Set("X-Internal-Token", "test-internal-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for empty admin surface in Phase 2, got %d", rec.Code)
	}
}

func TestRouteIsolation(t *testing.T) {
	s := newTestServer(false)
	adminHandler := s.AdminHandler()

	for _, path := range []string{"/health", "/academy/levels", "/academy/subjects"} {
		t.Run("admin_rejects_"+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Internal-Token", "test-internal-token")
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

	t.Run("empty_levels_when_no_published_subjects", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var resp models.LevelsResponseDTO
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode JSON: %v", err)
		}
		if len(resp.Levels) != 0 {
			t.Fatalf("expected 0 levels when no published subjects, got %d", len(resp.Levels))
		}
	})

	t.Run("draft_subject_does_not_reveal_level", func(t *testing.T) {
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

		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var resp models.LevelsResponseDTO
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode JSON: %v", err)
		}
		if len(resp.Levels) != 0 {
			t.Fatalf("expected 0 levels with draft subject, got %d", len(resp.Levels))
		}
	})

	t.Run("published_subject_reveals_level_with_DTO_shape", func(t *testing.T) {
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

		req := httptest.NewRequest(http.MethodGet, "/academy/levels", nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		publicHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var resp models.LevelsResponseDTO
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode JSON: %v", err)
		}
		if len(resp.Levels) != 1 {
			t.Fatalf("expected 1 level, got %d", len(resp.Levels))
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

		// Also check tree representation
		if len(resp.StudyTypes) != 1 {
			t.Fatalf("expected 1 study type, got %d", len(resp.StudyTypes))
		}
		st := resp.StudyTypes[0]
		if st.Key != models.StudyTypeBachelor {
			t.Errorf("study type key = %q, want bachelor", st.Key)
		}
		if len(st.Levels) != 1 || st.Levels[0].Key != "bachelor-y1" {
			t.Errorf("study type levels mismatch: %+v", st.Levels)
		}
	})
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
		videoDTO := rawVideo.ToDTO()
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
