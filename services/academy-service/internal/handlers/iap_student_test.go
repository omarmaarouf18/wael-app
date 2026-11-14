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

func studentIAPGet(t *testing.T, s *Server, tok, gwSecret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/academy/iap/packages", nil)
	if gwSecret != "" {
		req.Header.Set("X-Gateway-Secret", gwSecret)
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	return rec
}

func TestStudentIAP_DeterministicAppAccountToken(t *testing.T) {
	// Standard test vector checked against RFC 4122 UUID v5 with namespace 8e77e759-b33e-4ba9-8283-ac53aca1f8c8
	got := models.DeterministicAppAccountToken("usr_abc123")
	want := "a62e0d69-e2d5-550c-bddf-de079100d93d"
	if got != want {
		t.Fatalf("DeterministicAppAccountToken got %s, want %s", got, want)
	}
}

func TestStudentIAP_Auth(t *testing.T) {
	s := newTestServer(false)
	studentTok := makeStudentToken(t, "usr-stud-1")
	webTok := makeWebStudentToken(t, "usr-stud-web")

	t.Run("Missing_Gateway_Secret_401", func(t *testing.T) {
		rec := studentIAPGet(t, s, studentTok, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("Wrong_Gateway_Secret_401", func(t *testing.T) {
		rec := studentIAPGet(t, s, studentTok, "wrong-gw-secret")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("Missing_Student_Token_401", func(t *testing.T) {
		rec := studentIAPGet(t, s, "", "test-gateway-secret")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("Web_Token_Forbidden_403_AppClientRequired", func(t *testing.T) {
		rec := studentIAPGet(t, s, webTok, "test-gateway-secret")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d (%s)", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["code"] != "app_client_required" {
			t.Errorf("expected code app_client_required, got %v", body["code"])
		}
	})

	t.Run("Valid_Student_Token_200", func(t *testing.T) {
		rec := studentIAPGet(t, s, studentTok, "test-gateway-secret")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
	})
}

func TestStudentIAP_Packages_Response(t *testing.T) {
	s := newTestServer(false)
	ctx := context.Background()
	studentID := "usr-stud-packages-1"
	studentTok := makeStudentToken(t, studentID)
	future := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)

	s1 := createIAPTestSubject(t, s, "subj-stud-1", "bachelor-y1", models.StatusPublished, future)
	s2 := createIAPTestSubject(t, s, "subj-stud-2", "bachelor-y1", models.StatusPublished, future)
	s3 := createIAPTestSubject(t, s, "subj-stud-3", "bachelor-y1", models.StatusPublished, future)

	// Grant s1 to student
	ent := &models.Entitlement{
		ID:        "ent-stud-1",
		UserID:    studentID,
		SubjectID: s1.ID,
		Source:    models.EntitlementSourceAdminGrant,
		GrantedAt: time.Now().UTC(),
		ExpiresAt: future,
		Active:    true,
	}
	if err := s.Store.Grant(ctx, ent); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	availFuture := time.Now().UTC().Add(60 * 24 * time.Hour).Truncate(time.Second)
	availPast := time.Now().UTC().Add(-60 * 24 * time.Hour).Truncate(time.Second)

	// Package 1: published, position 2, title_ar "باقة 2"
	pkg1 := &models.IAPPackage{
		ID:             "pkg-stud-1",
		ProductID:      "com.wael.app.pkg.year2",
		TitleAr:        "الفرقة الثانية",
		TitleEn:        "Year 2",
		DescriptionAr:  "وصف 2",
		DescriptionEn:  "Desc 2",
		SubjectIDs:     []string{s3.ID},
		AvailableUntil: &availFuture,
		Published:      true,
		Position:       2,
	}
	if err := s.Store.CreateIAPPackage(ctx, pkg1); err != nil {
		t.Fatalf("CreateIAPPackage pkg1: %v", err)
	}

	// Package 2: published, position 1, title_ar "الفرقة الأولى"
	pkg2 := &models.IAPPackage{
		ID:             "pkg-stud-2",
		ProductID:      "com.wael.app.pkg.year1",
		TitleAr:        "الفرقة الأولى",
		TitleEn:        "Year 1",
		DescriptionAr:  "وصف 1",
		DescriptionEn:  "Desc 1",
		SubjectIDs:     []string{s1.ID, s2.ID},
		AvailableUntil: &availFuture,
		Published:      true,
		Position:       1,
	}
	if err := s.Store.CreateIAPPackage(ctx, pkg2); err != nil {
		t.Fatalf("CreateIAPPackage pkg2: %v", err)
	}

	// Package 3: unpublished -> must NOT be in student response
	pkg3 := &models.IAPPackage{
		ID:        "pkg-stud-unpub",
		ProductID: "com.wael.app.pkg.year3",
		TitleAr:   "الفرقة الثالثة",
		TitleEn:   "Year 3",
		Published: false,
		Position:  0,
	}
	if err := s.Store.CreateIAPPackage(ctx, pkg3); err != nil {
		t.Fatalf("CreateIAPPackage pkg3: %v", err)
	}

	// Package 4: expired available_until -> on_sale must be false
	pkg4 := &models.IAPPackage{
		ID:             "pkg-stud-past",
		ProductID:      "com.wael.app.pkg.year4",
		TitleAr:        "الفرقة الرابعة",
		TitleEn:        "Year 4",
		SubjectIDs:     []string{s3.ID},
		AvailableUntil: &availPast,
		Published:      true,
		Position:       3,
	}
	if err := s.Store.CreateIAPPackage(ctx, pkg4); err != nil {
		t.Fatalf("CreateIAPPackage pkg4: %v", err)
	}

	rec := studentIAPGet(t, s, studentTok, "test-gateway-secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var resp IAPPackagesResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// Check app_account_token
	expectedToken := models.DeterministicAppAccountToken(studentID)
	if resp.AppAccountToken != expectedToken {
		t.Errorf("app_account_token = %s, want %s", resp.AppAccountToken, expectedToken)
	}

	// Only 3 published packages returned
	if len(resp.Packages) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(resp.Packages))
	}

	// Ordered by position: pkg2 (pos 1), pkg1 (pos 2), pkg4 (pos 3)
	if resp.Packages[0].ID != pkg2.ID {
		t.Errorf("first package = %s, want %s", resp.Packages[0].ID, pkg2.ID)
	}
	if resp.Packages[1].ID != pkg1.ID {
		t.Errorf("second package = %s, want %s", resp.Packages[1].ID, pkg1.ID)
	}
	if resp.Packages[2].ID != pkg4.ID {
		t.Errorf("third package = %s, want %s", resp.Packages[2].ID, pkg4.ID)
	}

	// Check on_sale
	if !resp.Packages[0].OnSale {
		t.Errorf("pkg2 should be on_sale=true")
	}
	if !resp.Packages[1].OnSale {
		t.Errorf("pkg1 should be on_sale=true")
	}
	if resp.Packages[2].OnSale {
		t.Errorf("pkg4 with past available_until should be on_sale=false")
	}

	// Check owned flag on pkg2 subjects: s1 owned=true, s2 owned=false
	p2Subjs := resp.Packages[0].Subjects
	if len(p2Subjs) != 2 {
		t.Fatalf("pkg2 subjects len = %d, want 2", len(p2Subjs))
	}
	if p2Subjs[0].ID != s1.ID || !p2Subjs[0].Owned {
		t.Errorf("p2Subjs[0] = %+v, want %s owned=true", p2Subjs[0], s1.ID)
	}
	if p2Subjs[1].ID != s2.ID || p2Subjs[1].Owned {
		t.Errorf("p2Subjs[1] = %+v, want %s owned=false", p2Subjs[1], s2.ID)
	}
}

func TestStudentIAP_StaleSubjectSkipping(t *testing.T) {
	s := newTestServer(false)
	ctx := context.Background()
	studentTok := makeStudentToken(t, "usr-stud-stale")
	future := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	past := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)

	// 1. Valid active subject
	sActive := createIAPTestSubject(t, s, "subj-stud-active", "bachelor-y1", models.StatusPublished, future)
	// 2. Draft subject
	sDraft := createIAPTestSubject(t, s, "subj-stud-draft", "bachelor-y1", models.StatusDraft, future)
	// 3. Expired subject
	sExpired := createIAPTestSubject(t, s, "subj-stud-expired", "bachelor-y1", models.StatusPublished, past)
	// 4. Hidden subject under unpublished level
	unpubLevel := &models.Level{
		Key:       "diploma-unpub-stud",
		StudyType: models.StudyTypeDiploma,
		TitleAr:   "دبلوم غير منشور",
		Published: false,
	}
	if err := s.Store.CreateLevel(ctx, unpubLevel); err != nil {
		t.Fatalf("CreateLevel: %v", err)
	}
	sHidden := createIAPTestSubject(t, s, "subj-stud-hidden", "diploma-unpub-stud", models.StatusPublished, future)

	// Package with active + 4 stale subjects
	pkg := &models.IAPPackage{
		ID:        "pkg-stud-stale-1",
		ProductID: "com.wael.app.pkg.stale1",
		TitleAr:   "باقة تحتوي مواد ملغاة",
		TitleEn:   "Stale Subjects Package",
		SubjectIDs: []string{
			sActive.ID,
			sDraft.ID,
			sExpired.ID,
			sHidden.ID,
			"subj-stud-nonexistent-999",
		},
		Published: true,
		Position:  1,
	}
	if err := s.Store.CreateIAPPackage(ctx, pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	// Package with ONLY stale subjects -> len(subjects)==0 and on_sale==false
	pkgEmpty := &models.IAPPackage{
		ID:        "pkg-stud-stale-empty",
		ProductID: "com.wael.app.pkg.stale2",
		TitleAr:   "باقة فارغة بعد التنقية",
		TitleEn:   "Empty After Filter",
		SubjectIDs: []string{
			sDraft.ID,
			sExpired.ID,
		},
		Published: true,
		Position:  2,
	}
	if err := s.Store.CreateIAPPackage(ctx, pkgEmpty); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	rec := studentIAPGet(t, s, studentTok, "test-gateway-secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var resp IAPPackagesResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(resp.Packages))
	}

	// Package 1: only sActive kept
	p1 := resp.Packages[0]
	if len(p1.Subjects) != 1 || p1.Subjects[0].ID != sActive.ID {
		t.Errorf("p1 subjects: expected only [%s], got %+v", sActive.ID, p1.Subjects)
	}
	if !p1.OnSale {
		t.Errorf("p1 should be on_sale=true")
	}

	// Package 2: 0 subjects kept -> on_sale=false
	p2 := resp.Packages[1]
	if len(p2.Subjects) != 0 {
		t.Errorf("p2 subjects: expected 0, got %d", len(p2.Subjects))
	}
	if p2.OnSale {
		t.Errorf("p2 with 0 subjects must be on_sale=false")
	}
}

// TestStudentIAP_NoPriceOrContentIDs ensures the GET /academy/iap/packages response
// never carries a price, currency, or video/content storage identifier.
func TestStudentIAP_NoPriceOrContentIDs(t *testing.T) {
	s := newTestServer(true) // Even if exposePrice=true on server!
	studentTok := makeStudentToken(t, "usr-stud-noprice")
	future := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)

	s1 := createIAPTestSubject(t, s, "subj-stud-pricecheck", "bachelor-y1", models.StatusPublished, future)

	pkg := &models.IAPPackage{
		ID:         "pkg-stud-noprice",
		ProductID:  "com.wael.app.pkg.noprice",
		TitleAr:    "باقة فحص الأسعار",
		TitleEn:    "Price Check Package",
		SubjectIDs: []string{s1.ID},
		Published:  true,
		Position:   1,
	}
	if err := s.Store.CreateIAPPackage(context.Background(), pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	rec := studentIAPGet(t, s, studentTok, "test-gateway-secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	bodyStr := rec.Body.String()
	forbiddenKeys := []string{
		`"price"`,
		`"currency"`,
		`"video_id"`,
		`"youtube_video_id"`,
		`"content_id"`,
		`"storage_key"`,
	}
	for _, fk := range forbiddenKeys {
		if strings.Contains(bodyStr, fk) {
			t.Errorf("response body unexpectedly contains %s: %s", fk, bodyStr)
		}
	}
}
