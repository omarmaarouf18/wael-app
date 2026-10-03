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
	"github.com/omarmaarouf18/wael-app/academy-service/internal/notify"
)

// forbiddenStoreWords is the list of payment terms forbidden in student notifications
// and student-facing surfaces to maintain App Store and Play Store safety.
var forbiddenStoreWords = []string{
	"دفع",
	"سعر",
	"ج.م",
	"جنيه",
	"شراء",
	"payment",
	"price",
	"pay",
	"purchase",
}

// forbiddenStudentFields is the list of financial, internal administrative,
// and revocation metadata that must never appear in student DTOs or student error bodies.
var forbiddenStudentFields = []string{
	"price_at_grant",
	"payment",
	"granted_by",
	"decided_by",
	"revoke_reason",
}

func assertNoForbiddenStoreWords(t *testing.T, contextName string, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, word := range forbiddenStoreWords {
		if strings.Contains(lower, strings.ToLower(word)) {
			t.Errorf("[%s] contains forbidden store word %q: %s", contextName, word, text)
		}
	}
}

func assertNoForbiddenStudentFields(t *testing.T, contextName string, jsonBytes []byte) {
	t.Helper()
	s := strings.ToLower(string(jsonBytes))
	for _, field := range forbiddenStudentFields {
		if strings.Contains(s, field) {
			t.Errorf("[%s] contains forbidden student field %q:\n%s", contextName, field, string(jsonBytes))
		}
	}
}

// TestStoreSafety_Notifications renders SubjectActivated, SubjectRejected (with neutral reason),
// and SubjectRevoked (plus grant notification, which uses SubjectActivated) in Arabic and English,
// asserting none of the forbidden payment terms appear in any notification payload field.
func TestStoreSafety_Notifications(t *testing.T) {
	var capturedPayloads []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]string
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Fatalf("failed to decode push payload: %v", err)
		}
		capturedPayloads = append(capturedPayloads, p)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	token := "test-internal-token"
	userID := "student-safety-user"

	// 1. SubjectActivated in ar and en
	err := notify.SubjectActivated(ctx, server.URL, token, userID, "لغة عربية", "Arabic Language")
	if err != nil {
		t.Fatalf("SubjectActivated failed: %v", err)
	}

	// 2. SubjectRejected with a neutral reason in ar and en
	err = notify.SubjectRejected(ctx, server.URL, token, userID, "أصول فقه", "Islamic Jurisprudence", "بيانات غير مطابقة للطلب")
	if err != nil {
		t.Fatalf("SubjectRejected ar failed: %v", err)
	}
	err = notify.SubjectRejected(ctx, server.URL, token, userID, "أصول فقه", "Islamic Jurisprudence", "Incomplete request information")
	if err != nil {
		t.Fatalf("SubjectRejected en failed: %v", err)
	}

	// 3. SubjectRevoked in ar and en
	err = notify.SubjectRevoked(ctx, server.URL, token, userID, "تفسير القرآن", "Quran Interpretation")
	if err != nil {
		t.Fatalf("SubjectRevoked failed: %v", err)
	}

	// 4. Grant notification (uses SubjectActivated per admin_entitlements.go)
	err = notify.SubjectActivated(ctx, server.URL, token, userID, "حديث شريف", "Prophetic Traditions")
	if err != nil {
		t.Fatalf("Grant notification failed: %v", err)
	}

	if len(capturedPayloads) != 5 {
		t.Fatalf("expected 5 captured notification payloads, got %d", len(capturedPayloads))
	}

	fields := []string{"title", "title_ar", "body", "body_ar"}
	for i, payload := range capturedPayloads {
		for _, f := range fields {
			val := payload[f]
			assertNoForbiddenStoreWords(t, "notification payload "+f+" index "+string(rune('0'+i)), val)
		}
	}
}

// TestStoreSafety_StudentDTOs asserts that no student DTO contains price_at_grant,
// payment, granted_by, decided_by, or revoke_reason when marshaled to JSON.
func TestStoreSafety_StudentDTOs(t *testing.T) {
	now := time.Now().UTC()

	levelDTO := models.LevelDTO{
		Key:       "bachelor-y1",
		StudyType: "bachelor",
		Title:     models.LocalizedText{Ar: "الفرقة الأولى", En: "First Year"},
		Position:  1,
	}
	data, _ := json.Marshal(levelDTO)
	assertNoForbiddenStudentFields(t, "LevelDTO", data)

	studyTypeDTO := models.StudyTypeDTO{
		Key:    "bachelor",
		Title:  models.LocalizedText{Ar: "بكالوريوس", En: "Bachelor"},
		Levels: []models.LevelDTO{levelDTO},
	}
	data, _ = json.Marshal(studyTypeDTO)
	assertNoForbiddenStudentFields(t, "StudyTypeDTO", data)

	levelsResp := models.LevelsResponseDTO{
		Levels:     []models.LevelDTO{levelDTO},
		StudyTypes: []models.StudyTypeDTO{studyTypeDTO},
	}
	data, _ = json.Marshal(levelsResp)
	assertNoForbiddenStudentFields(t, "LevelsResponseDTO", data)

	itemDTO := models.SubjectListItemDTO{
		ID:              "subj-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		Title:           models.LocalizedText{Ar: "مادة تجريبية", En: "Test Subject"},
		Description:     models.LocalizedText{Ar: "وصف", En: "Description"},
		AccessExpiresAt: now.Add(24 * time.Hour),
		Counts: models.SubjectCountsDTO{
			Videos: 5,
			Books:  1,
			Notes:  2,
		},
		Owned: true,
	}
	data, _ = json.Marshal(itemDTO)
	assertNoForbiddenStudentFields(t, "SubjectListItemDTO", data)

	listResp := models.SubjectListResponseDTO{
		Items: []models.SubjectListItemDTO{itemDTO},
		Total: 1,
		Page:  1,
		Limit: 20,
	}
	data, _ = json.Marshal(listResp)
	assertNoForbiddenStudentFields(t, "SubjectListResponseDTO", data)

	videoMeta := models.VideoMetadataDTO{
		ID:          "vid-1",
		Position:    1,
		Title:       models.LocalizedText{Ar: "درس أول", En: "Lesson 1"},
		Description: models.LocalizedText{Ar: "وصف", En: "Description"},
		Playable:    true,
	}
	data, _ = json.Marshal(videoMeta)
	assertNoForbiddenStudentFields(t, "VideoMetadataDTO", data)

	fileMeta := models.FileMetadataDTO{
		ID:        "file-1",
		Kind:      "book",
		Title:     models.LocalizedText{Ar: "كتاب", En: "Book"},
		SizeBytes: 1048576,
	}
	data, _ = json.Marshal(fileMeta)
	assertNoForbiddenStudentFields(t, "FileMetadataDTO", data)

	subjectReq := &models.SubjectRequestDTO{
		Status: models.RequestStatusPending,
	}
	data, _ = json.Marshal(subjectReq)
	assertNoForbiddenStudentFields(t, "SubjectRequestDTO", data)

	detailDTO := models.SubjectDetailDTO{
		ID:              "subj-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		Title:           models.LocalizedText{Ar: "مادة تجريبية", En: "Test Subject"},
		Description:     models.LocalizedText{Ar: "وصف", En: "Description"},
		AccessExpiresAt: now.Add(24 * time.Hour),
		Counts: models.SubjectCountsDTO{
			Videos: 1,
			Books:  1,
		},
		Videos:  []models.VideoMetadataDTO{videoMeta},
		Files:   []models.FileMetadataDTO{fileMeta},
		Owned:   false,
		Request: subjectReq,
	}
	data, _ = json.Marshal(detailDTO)
	assertNoForbiddenStudentFields(t, "SubjectDetailDTO", data)

	playResp := models.VideoPlayResponseDTO{
		VideoID:        "vid-1",
		YouTubeVideoID: "dQw4w9WgXcQ",
	}
	data, _ = json.Marshal(playResp)
	assertNoForbiddenStudentFields(t, "VideoPlayResponseDTO", data)

	accessReqResp := models.AccessRequestResponseDTO{
		ID:          "req-1",
		SubjectID:   "subj-1",
		Status:      "pending",
		CreatedAt:   now,
		WhatsAppURL: "https://wa.me/201000000000",
	}
	data, _ = json.Marshal(accessReqResp)
	assertNoForbiddenStudentFields(t, "AccessRequestResponseDTO", data)

	myEntsResp := map[string]any{
		"subject_ids": []string{"subj-1", "subj-2"},
	}
	data, _ = json.Marshal(myEntsResp)
	assertNoForbiddenStudentFields(t, "MyEntitlementsDTO", data)
}

// TestStoreSafety_StudentEndpointsAndErrors runs live student HTTP requests
// against all student endpoints (success and error paths) and asserts no response
// contains price_at_grant, payment, granted_by, decided_by, or revoke_reason.
func TestStoreSafety_StudentEndpointsAndErrors(t *testing.T) {
	s := newTestServer(false)
	router := s.PublicHandler()

	studentUserID := "safety-student-1"
	token := makeStudentToken(t, studentUserID)

	// Create test published subject and video
	now := time.Now().UTC()
	subj := &models.Subject{
		ID:              "subj-store-safe",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة الاختبار",
		TitleEn:         "Test Subject",
		DescriptionAr:   "وصف المادة",
		DescriptionEn:   "Subject Description",
		Price:           300,
		Status:          models.StatusPublished,
		AccessExpiresAt: now.Add(30 * 24 * time.Hour),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_ = s.Store.CreateSubject(context.Background(), subj)

	vid := &models.Video{
		ID:              "vid-store-safe",
		SubjectID:       subj.ID,
		Position:        1,
		TitleAr:         "فيديو",
		TitleEn:         "Video",
		YouTubeVideoID:  "safeVideo123",
		DurationSeconds: 120,
		Published:       true,
		Deleted:         false,
	}
	_ = s.Store.CreateVideo(context.Background(), vid)

	doStudent := func(method, path string, body string, withAuth bool) *httptest.ResponseRecorder {
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		if withAuth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	testCases := []struct {
		name     string
		method   string
		path     string
		body     string
		withAuth bool
	}{
		{"GET /academy/levels", http.MethodGet, "/academy/levels", "", true},
		{"GET /academy/subjects", http.MethodGet, "/academy/subjects", "", true},
		{"GET /academy/subjects/{id} unowned", http.MethodGet, "/academy/subjects/" + subj.ID, "", true},
		{"POST /academy/subjects/{id}/access-request", http.MethodPost, "/academy/subjects/" + subj.ID + "/access-request", "", true},
		{"GET /academy/subjects/{id} with request", http.MethodGet, "/academy/subjects/" + subj.ID, "", true},
		{"GET /academy/me/entitlements", http.MethodGet, "/academy/me/entitlements", "", true},
		{"POST /academy/videos/{id}/play unowned (404)", http.MethodPost, "/academy/videos/" + vid.ID + "/play", "", true},
		{"401 missing auth", http.MethodGet, "/academy/subjects", "", false},
		{"404 unknown subject", http.MethodGet, "/academy/subjects/unknown-id", "", true},
		{"400 invalid access request", http.MethodPost, "/academy/subjects/invalid-id/access-request", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doStudent(tc.method, tc.path, tc.body, tc.withAuth)
			assertNoForbiddenStudentFields(t, tc.name, rec.Body.Bytes())
		})
	}

	// Now grant entitlement to test owned subject & play response
	_ = s.Store.Grant(context.Background(), &models.Entitlement{
		ID:        "ent-safe-1",
		UserID:    studentUserID,
		SubjectID: subj.ID,
		ExpiresAt: subj.AccessExpiresAt,
		GrantedAt: now,
		Source:    models.EntitlementSourceAdminGrant,
		GrantedBy: "adm-1",
		Active:    true,
	})

	ownedCases := []struct {
		name   string
		method string
		path   string
	}{
		{"GET /academy/subjects/{id} owned", http.MethodGet, "/academy/subjects/" + subj.ID},
		{"POST /academy/videos/{id}/play owned (200)", http.MethodPost, "/academy/videos/" + vid.ID + "/play"},
		{"GET /academy/me/entitlements owned", http.MethodGet, "/academy/me/entitlements"},
	}

	for _, tc := range ownedCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doStudent(tc.method, tc.path, "", true)
			assertNoForbiddenStudentFields(t, tc.name, rec.Body.Bytes())
		})
	}
}
