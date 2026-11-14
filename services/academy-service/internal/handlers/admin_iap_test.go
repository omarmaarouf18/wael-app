package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func createIAPTestSubject(t *testing.T, s *Server, id, levelKey, status string, expires time.Time) *models.Subject {
	t.Helper()
	subj := &models.Subject{
		ID:              id,
		LevelKey:        levelKey,
		Term:            "first",
		TitleAr:         "مادة " + id,
		TitleEn:         "Subject " + id,
		Status:          status,
		Price:           1000,
		AccessExpiresAt: expires,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := s.Store.CreateSubject(context.Background(), subj); err != nil {
		t.Fatalf("CreateSubject failed: %v", err)
	}
	return subj
}

func TestAdminIAP_Auth(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	t.Run("missing_internal_token_401", func(t *testing.T) {
		rec := doAdmin(t, s, http.MethodGet, "/internal/admin/iap/packages", "", "test-admin-token", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("wrong_internal_token_401", func(t *testing.T) {
		rec := doAdmin(t, s, http.MethodGet, "/internal/admin/iap/packages", "wrong-internal-tok", "test-admin-token", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing_admin_token_401", func(t *testing.T) {
		rec := doAdmin(t, s, http.MethodGet, "/internal/admin/iap/packages", "test-internal-token", "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid_tokens_200", func(t *testing.T) {
		rec := doAdmin(t, s, http.MethodGet, "/internal/admin/iap/packages", "test-internal-token", "test-admin-token", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
	})
}

func TestAdminIAP_Packages_CRUD(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().UTC().Add(30 * 24 * time.Hour)
	createIAPTestSubject(t, s, "subj-iap-1", "bachelor-y1", models.StatusPublished, future)
	createIAPTestSubject(t, s, "subj-iap-2", "bachelor-y1", models.StatusPublished, future)

	var createdPkg IAPPackageAdminDTO

	t.Run("Create_Success", func(t *testing.T) {
		avail := time.Now().UTC().Add(90 * 24 * time.Hour).Truncate(time.Second)
		body := map[string]any{
			"product_id":      "com.wael.app.pkg.year1",
			"title_ar":        "الفرقة الأولى",
			"title_en":        "Year 1",
			"description_ar":  "وصف باقة الفرقة الأولى",
			"description_en":  "Year 1 Package Description",
			"subject_ids":     []string{"subj-iap-1", "subj-iap-2"},
			"available_until": avail.Format(time.RFC3339),
			"published":       false,
			"position":        1,
		}
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &createdPkg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if createdPkg.ID == "" || !strings.HasPrefix(createdPkg.ID, "pkg_") {
			t.Errorf("expected package ID starting with pkg_, got %q", createdPkg.ID)
		}
		if createdPkg.ProductID != "com.wael.app.pkg.year1" {
			t.Errorf("product_id = %q, want com.wael.app.pkg.year1", createdPkg.ProductID)
		}
		if len(createdPkg.SubjectIDs) != 2 {
			t.Errorf("subject_ids len = %d, want 2", len(createdPkg.SubjectIDs))
		}
		if createdPkg.Position != 1 || createdPkg.Published {
			t.Errorf("position/published mismatch: %+v", createdPkg)
		}
	})

	t.Run("Get_Success", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/packages/"+createdPkg.ID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got IAPPackageAdminDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.ID != createdPkg.ID || got.ProductID != createdPkg.ProductID {
			t.Errorf("got %+v, want %+v", got, createdPkg)
		}
	})

	t.Run("Get_NotFound", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/packages/pkg_missing_999", nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
		}
		if adminCode(t, rec) != "not_found" {
			t.Errorf("expected not_found, got %s", adminCode(t, rec))
		}
	})

	t.Run("Get_InvalidID", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/packages/bad@id!", nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
		}
		if adminCode(t, rec) != "invalid_package_id" {
			t.Errorf("expected invalid_package_id, got %s", adminCode(t, rec))
		}
	})

	t.Run("List_Success", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/packages", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var res struct {
			Packages []IAPPackageAdminDTO `json:"packages"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(res.Packages) != 1 {
			t.Fatalf("expected 1 package, got %d", len(res.Packages))
		}
		if res.Packages[0].ID != createdPkg.ID {
			t.Errorf("list package ID = %s, want %s", res.Packages[0].ID, createdPkg.ID)
		}
	})

	t.Run("Patch_ImmutableProductID_Fails", func(t *testing.T) {
		patchBody := map[string]any{
			"product_id": "com.wael.app.pkg.year1",
		}
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/iap/packages/"+createdPkg.ID, patchBody)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
		}
		if adminCode(t, rec) != "product_id_immutable" {
			t.Errorf("expected product_id_immutable, got %s", adminCode(t, rec))
		}
	})

	t.Run("Patch_Fields_Success", func(t *testing.T) {
		patchBody := map[string]any{
			"title_ar":        "الفرقة الأولى معدل",
			"published":       true,
			"position":        10,
			"subject_ids":     []string{"subj-iap-1"},
			"available_until": nil,
		}
		rec := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/iap/packages/"+createdPkg.ID, patchBody)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var updated IAPPackageAdminDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if updated.TitleAr != "الفرقة الأولى معدل" || !updated.Published || updated.Position != 10 {
			t.Errorf("patch fields mismatch: %+v", updated)
		}
		if len(updated.SubjectIDs) != 1 || updated.SubjectIDs[0] != "subj-iap-1" {
			t.Errorf("patch subjects mismatch: %v", updated.SubjectIDs)
		}
		if updated.AvailableUntil != nil {
			t.Errorf("expected available_until nil, got %v", updated.AvailableUntil)
		}
	})

	t.Run("Delete_BlockedByPurchases", func(t *testing.T) {
		// Insert a purchase referencing this package
		pur := &models.IAPPurchase{
			ID:            "pur-del-test",
			TransactionID: "tx-del-test",
			UserID:        "usr-del-1",
			PackageID:     createdPkg.ID,
			ProductID:     createdPkg.ProductID,
			Environment:   models.IAPEnvironmentProduction,
			PurchaseDate:  time.Now().UTC(),
			CreatedAt:     time.Now().UTC(),
		}
		if err := s.Store.InsertIAPPurchase(context.Background(), pur); err != nil {
			t.Fatalf("InsertIAPPurchase: %v", err)
		}

		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/iap/packages/"+createdPkg.ID, nil)
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
		}
		if adminCode(t, rec) != "package_has_purchases" {
			t.Errorf("expected package_has_purchases, got %s", adminCode(t, rec))
		}
	})

	t.Run("Delete_SuccessWithoutPurchases", func(t *testing.T) {
		// Create another package without purchases
		body := map[string]any{
			"product_id": "com.wael.app.pkg.year2",
			"title_ar":   "الفرقة الثانية",
			"title_en":   "Year 2",
		}
		recCreate := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", body)
		if recCreate.Code != http.StatusCreated {
			t.Fatalf("create failed: %d", recCreate.Code)
		}
		var p2 IAPPackageAdminDTO
		_ = json.Unmarshal(recCreate.Body.Bytes(), &p2)

		rec := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/iap/packages/"+p2.ID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}

		recGet := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/packages/"+p2.ID, nil)
		if recGet.Code != http.StatusNotFound {
			t.Errorf("expected 404 after delete, got %d", recGet.Code)
		}
	})
}

func TestAdminIAP_Validation(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().UTC().Add(30 * 24 * time.Hour)
	createIAPTestSubject(t, s, "subj-v-1", "bachelor-y1", models.StatusPublished, future)

	t.Run("Invalid_ProductID_Regex", func(t *testing.T) {
		badIDs := []string{
			"com.wael.app.pkg",           // missing suffix
			"com.wael.app.pkg.",          // empty suffix
			"com.wael.app.pkg.Year1",     // uppercase
			"com.wael.app.pkg.year 1",    // space
			"com.other.app.pkg.year1",    // wrong prefix
			"com.wael.app.pkg.year1@bad", // special char
		}
		for _, bid := range badIDs {
			body := map[string]any{
				"product_id": bid,
				"title_ar":   "عنوان",
				"title_en":   "Title",
			}
			rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("product_id %q: expected 400, got %d", bid, rec.Code)
			}
			if adminCode(t, rec) != "invalid_product_id" {
				t.Errorf("product_id %q: expected invalid_product_id, got %s", bid, adminCode(t, rec))
			}
		}
	})

	t.Run("Invalid_Titles", func(t *testing.T) {
		// Missing title_ar
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
			"product_id": "com.wael.app.pkg.v1",
			"title_ar":   "",
			"title_en":   "Valid",
		})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_title" {
			t.Errorf("empty title_ar: expected 400 invalid_title, got %d %s", rec.Code, adminCode(t, rec))
		}

		// Over 80 runes title_ar
		rec = doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
			"product_id": "com.wael.app.pkg.v1",
			"title_ar":   strings.Repeat("ع", 81),
			"title_en":   "Valid",
		})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_title" {
			t.Errorf("long title_ar: expected 400 invalid_title, got %d %s", rec.Code, adminCode(t, rec))
		}

		// Missing title_en
		rec = doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
			"product_id": "com.wael.app.pkg.v1",
			"title_ar":   "صحيح",
			"title_en":   "",
		})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_title" {
			t.Errorf("empty title_en: expected 400 invalid_title, got %d %s", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("Invalid_Descriptions", func(t *testing.T) {
		longDesc := strings.Repeat("x", 1001)
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
			"product_id":     "com.wael.app.pkg.v2",
			"title_ar":       "عنوان",
			"title_en":       "Title",
			"description_ar": longDesc,
		})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_description" {
			t.Errorf("long desc_ar: expected 400 invalid_description, got %d %s", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("Unknown_Subject_ID", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
			"product_id":  "com.wael.app.pkg.v3",
			"title_ar":    "عنوان",
			"title_en":    "Title",
			"subject_ids": []string{"subj-v-1", "subj-nonexistent-999"},
		})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "unknown_subject" {
			t.Errorf("unknown subject: expected 400 unknown_subject, got %d %s", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("Duplicate_Subject_ID", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
			"product_id":  "com.wael.app.pkg.v4",
			"title_ar":    "عنوان",
			"title_en":    "Title",
			"subject_ids": []string{"subj-v-1", "subj-v-1"},
		})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_subject_ids" {
			t.Errorf("duplicate subject: expected 400 invalid_subject_ids, got %d %s", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("Too_Many_Subjects", func(t *testing.T) {
		subjs := make([]string, 101)
		for i := 0; i < 101; i++ {
			subjs[i] = fmt.Sprintf("sub-%d", i)
		}
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
			"product_id":  "com.wael.app.pkg.v5",
			"title_ar":    "عنوان",
			"title_en":    "Title",
			"subject_ids": subjs,
		})
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_subject_ids" {
			t.Errorf("too many subjects: expected 400 invalid_subject_ids, got %d %s", rec.Code, adminCode(t, rec))
		}
	})

	t.Run("Disallow_Unknown_Fields", func(t *testing.T) {
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
			"product_id": "com.wael.app.pkg.v6",
			"title_ar":   "عنوان",
			"title_en":   "Title",
			"price":      500, // unknown field
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("unknown field: expected 400, got %d", rec.Code)
		}
	})

	t.Run("Duplicate_ProductID", func(t *testing.T) {
		body := map[string]any{
			"product_id": "com.wael.app.pkg.uniq",
			"title_ar":   "عنوان",
			"title_en":   "Title",
		}
		rec1 := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", body)
		if rec1.Code != http.StatusCreated {
			t.Fatalf("first create failed: %d", rec1.Code)
		}
		rec2 := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", body)
		if rec2.Code != http.StatusConflict || adminCode(t, rec2) != "duplicate_product_id" {
			t.Errorf("duplicate product_id: expected 409 duplicate_product_id, got %d %s", rec2.Code, adminCode(t, rec2))
		}
	})
}

func TestAdminIAP_Audit(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	// 1. Create
	recCreate := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
		"product_id": "com.wael.app.pkg.aud",
		"title_ar":   "باقة تدقيق",
		"title_en":   "Audit Package",
	})
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", recCreate.Code)
	}
	var pkg IAPPackageAdminDTO
	_ = json.Unmarshal(recCreate.Body.Bytes(), &pkg)

	// 2. Patch
	recPatch := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/iap/packages/"+pkg.ID, map[string]any{
		"title_ar": "باقة تدقيق معدلة",
	})
	if recPatch.Code != http.StatusOK {
		t.Fatalf("patch failed: %d", recPatch.Code)
	}

	// 3. Delete
	recDel := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/iap/packages/"+pkg.ID, nil)
	if recDel.Code != http.StatusOK {
		t.Fatalf("delete failed: %d", recDel.Code)
	}

	// Verify audit logs
	recAudit := doAdminJSON(t, s, http.MethodGet, "/internal/admin/audit-log", nil)
	if recAudit.Code != http.StatusOK {
		t.Fatalf("audit-log failed: %d", recAudit.Code)
	}
	var auditRes struct {
		Items []models.AuditLog `json:"items"`
	}
	_ = json.Unmarshal(recAudit.Body.Bytes(), &auditRes)

	actions := make(map[string]bool)
	for _, a := range auditRes.Items {
		if a.TargetID == pkg.ID && a.TargetType == "iap_package" {
			actions[a.Action] = true
		}
	}
	for _, expectedAction := range []string{"iap_package_create", "iap_package_update", "iap_package_delete"} {
		if !actions[expectedAction] {
			t.Errorf("missing audit log action %s for package %s", expectedAction, pkg.ID)
		}
	}
}

func TestAdminIAP_Coverage(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().UTC().Add(30 * 24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)

	// Create an unpublished level
	unpubLevel := &models.Level{
		Key:       "diploma-unpub-cov",
		StudyType: models.StudyTypeDiploma,
		TitleAr:   "دبلوم غير منشور",
		TitleEn:   "Unpublished Diploma",
		Published: false,
	}
	if err := s.Store.CreateLevel(context.Background(), unpubLevel); err != nil {
		t.Fatalf("CreateLevel: %v", err)
	}

	// Subjects:
	// 1. Covered: published, in published package
	sCovered := createIAPTestSubject(t, s, "subj-cov-1", "bachelor-y1", models.StatusPublished, future)
	// 2. Uncovered: published, in NO published package
	sUncovered := createIAPTestSubject(t, s, "subj-cov-2", "bachelor-y1", models.StatusPublished, future)
	// 3. Draft: draft status -> ignored in coverage
	_ = createIAPTestSubject(t, s, "subj-cov-draft", "bachelor-y1", models.StatusDraft, future)
	// 4. Expired: past access_expires_at -> ignored in coverage
	_ = createIAPTestSubject(t, s, "subj-cov-expired", "bachelor-y1", models.StatusPublished, past)
	// 5. Hidden: under unpublished level -> ignored in coverage
	_ = createIAPTestSubject(t, s, "subj-cov-hidden", "diploma-unpub-cov", models.StatusPublished, future)
	// 6. In unpublished package only -> still uncovered!
	sUnpubPkgSubj := createIAPTestSubject(t, s, "subj-cov-unpubpkg", "bachelor-y1", models.StatusPublished, future)

	// Create published package containing sCovered
	rec1 := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
		"product_id":  "com.wael.app.pkg.cov1",
		"title_ar":    "باقة منشورة",
		"title_en":    "Published Package",
		"subject_ids": []string{sCovered.ID},
		"published":   true,
	})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("create pkg1 failed: %d", rec1.Code)
	}

	// Create unpublished package containing sUnpubPkgSubj
	rec2 := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
		"product_id":  "com.wael.app.pkg.cov2",
		"title_ar":    "باقة مسودة",
		"title_en":    "Draft Package",
		"subject_ids": []string{sUnpubPkgSubj.ID},
		"published":   false,
	})
	if rec2.Code != http.StatusCreated {
		t.Fatalf("create pkg2 failed: %d", rec2.Code)
	}

	recCov := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/coverage", nil)
	if recCov.Code != http.StatusOK {
		t.Fatalf("coverage failed: %d (%s)", recCov.Code, recCov.Body.String())
	}
	var covRes struct {
		UncoveredSubjectIDs []string `json:"uncovered_subject_ids"`
	}
	if err := json.Unmarshal(recCov.Body.Bytes(), &covRes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	uncoveredMap := make(map[string]bool)
	for _, id := range covRes.UncoveredSubjectIDs {
		uncoveredMap[id] = true
	}

	if uncoveredMap[sCovered.ID] {
		t.Errorf("covered subject %s appeared in uncovered list", sCovered.ID)
	}
	if !uncoveredMap[sUncovered.ID] {
		t.Errorf("uncovered subject %s missing from uncovered list", sUncovered.ID)
	}
	if !uncoveredMap[sUnpubPkgSubj.ID] {
		t.Errorf("subject in unpublished package %s missing from uncovered list", sUnpubPkgSubj.ID)
	}
	if uncoveredMap["subj-cov-draft"] {
		t.Errorf("draft subject appeared in coverage list")
	}
	if uncoveredMap["subj-cov-expired"] {
		t.Errorf("expired subject appeared in coverage list")
	}
	if uncoveredMap["subj-cov-hidden"] {
		t.Errorf("hidden subject appeared in coverage list")
	}
}

func TestAdminIAP_PurchasesFilter(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	now := time.Now().UTC()

	p1 := &models.IAPPurchase{
		ID:            "pur-f-1",
		TransactionID: "tx-f-1",
		UserID:        "user-f-1",
		PackageID:     "pkg-f-1",
		ProductID:     "com.wael.app.pkg.f1",
		CreatedAt:     now.Add(1 * time.Second),
	}
	p2 := &models.IAPPurchase{
		ID:            "pur-f-2",
		TransactionID: "tx-f-2",
		UserID:        "user-f-1",
		PackageID:     "pkg-f-2",
		ProductID:     "com.wael.app.pkg.f2",
		CreatedAt:     now.Add(2 * time.Second),
	}
	p3 := &models.IAPPurchase{
		ID:            "pur-f-3",
		TransactionID: "tx-f-3",
		UserID:        "user-f-2",
		PackageID:     "pkg-f-1",
		ProductID:     "com.wael.app.pkg.f1",
		CreatedAt:     now.Add(3 * time.Second),
	}

	for _, p := range []*models.IAPPurchase{p1, p2, p3} {
		if err := s.Store.InsertIAPPurchase(context.Background(), p); err != nil {
			t.Fatalf("InsertIAPPurchase: %v", err)
		}
	}

	// Filter by user_id
	recUser := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/purchases?user_id=user-f-1", nil)
	if recUser.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recUser.Code)
	}
	var resUser struct {
		Purchases []models.IAPPurchase `json:"purchases"`
	}
	_ = json.Unmarshal(recUser.Body.Bytes(), &resUser)
	if len(resUser.Purchases) != 2 {
		t.Errorf("filter user_id: expected 2 purchases, got %d", len(resUser.Purchases))
	}

	// Filter by package_id
	recPkg := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/purchases?package_id=pkg-f-2", nil)
	var resPkg struct {
		Purchases []models.IAPPurchase `json:"purchases"`
	}
	_ = json.Unmarshal(recPkg.Body.Bytes(), &resPkg)
	if len(resPkg.Purchases) != 1 || resPkg.Purchases[0].ID != "pur-f-2" {
		t.Errorf("filter package_id: expected [pur-f-2], got %+v", resPkg.Purchases)
	}

	// Limit
	recLim := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/purchases?limit=1", nil)
	var resLim struct {
		Purchases []models.IAPPurchase `json:"purchases"`
	}
	_ = json.Unmarshal(recLim.Body.Bytes(), &resLim)
	if len(resLim.Purchases) != 1 {
		t.Errorf("limit 1: expected 1 purchase, got %d", len(resLim.Purchases))
	}
}

// TestAdminIAP_StaleSubjectSkipping tests the review fix:
// "Every read path (#53 coverage/list, #54 packages) must skip subject ids that no
// longer exist or are draft/hidden/expired, because RemoveSubjectFromAllPackages
// only logs on failure. Add a test with a stale subject id in a package."
// Amended 2026-11-14 (owner fix, W5): the admin GET and list keep draft,
// expired and hidden-level subjects, because the console saves the list it
// reads; only deleted subject ids are omitted. Coverage and the student read
// still skip every stale id.
func TestAdminIAP_StaleSubjectSkipping(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().UTC().Add(30 * 24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)

	// 1. Valid active subject
	sActive := createIAPTestSubject(t, s, "subj-stale-active", "bachelor-y1", models.StatusPublished, future)

	// 2. Draft subject
	sDraft := createIAPTestSubject(t, s, "subj-stale-draft", "bachelor-y1", models.StatusDraft, future)

	// 3. Expired subject
	sExpired := createIAPTestSubject(t, s, "subj-stale-expired", "bachelor-y1", models.StatusPublished, past)

	// 4. Hidden subject under unpublished level
	unpubLevel := &models.Level{
		Key:       "diploma-stale-unpub",
		StudyType: models.StudyTypeDiploma,
		TitleAr:   "دبلوم غير منشور",
		Published: false,
	}
	if err := s.Store.CreateLevel(context.Background(), unpubLevel); err != nil {
		t.Fatalf("CreateLevel: %v", err)
	}
	sHidden := createIAPTestSubject(t, s, "subj-stale-hidden", "diploma-stale-unpub", models.StatusPublished, future)

	// 5. Non-existent subject ID (simulates subject deleted where RemoveSubjectFromAllPackages failed)
	sNonExistentID := "subj-stale-deleted-999"

	// Create package in store with all 5 subject IDs directly
	pkg := &models.IAPPackage{
		ID:        "pkg-stale-test",
		ProductID: "com.wael.app.pkg.stale",
		TitleAr:   "باقة تحتوي مواد ملغاة",
		TitleEn:   "Stale Subjects Package",
		SubjectIDs: []string{
			sActive.ID,
			sDraft.ID,
			sExpired.ID,
			sHidden.ID,
			sNonExistentID,
		},
		Published: true,
		Position:  1,
	}
	if err := s.Store.CreateIAPPackage(context.Background(), pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	// Test 1: GET /internal/admin/iap/packages/{id} keeps existing IDs, omits deleted
	recGet := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/packages/"+pkg.ID, nil)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET package failed: %d (%s)", recGet.Code, recGet.Body.String())
	}
	var gotPkg IAPPackageAdminDTO
	if err := json.Unmarshal(recGet.Body.Bytes(), &gotPkg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantKept := []string{sActive.ID, sDraft.ID, sExpired.ID, sHidden.ID}
	if !slices.Equal(gotPkg.SubjectIDs, wantKept) {
		t.Errorf("GET package: expected existing subjects %v (deleted omitted), got %v", wantKept, gotPkg.SubjectIDs)
	}

	// Test 2: GET /internal/admin/iap/packages list does the same
	recList := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/packages", nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("list packages failed: %d", recList.Code)
	}
	var listRes struct {
		Packages []IAPPackageAdminDTO `json:"packages"`
	}
	if err := json.Unmarshal(recList.Body.Bytes(), &listRes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := false
	for _, p := range listRes.Packages {
		if p.ID == pkg.ID {
			found = true
			if !slices.Equal(p.SubjectIDs, wantKept) {
				t.Errorf("List package: expected existing subjects %v (deleted omitted), got %v", wantKept, p.SubjectIDs)
			}
		}
	}
	if !found {
		t.Errorf("package %s not found in list", pkg.ID)
	}
}

// TestAdminIAP_EditKeepsDraftAndExpiredSubjects is the console round trip
// (owner fix 2026-11-14, W5): the console reads a package, changes one field
// and PATCHes back the subject_ids it read. Draft and expired subjects the
// admin picked must survive that save.
func TestAdminIAP_EditKeepsDraftAndExpiredSubjects(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().UTC().Add(30 * 24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)

	sActive := createIAPTestSubject(t, s, "subj-keep-active", "bachelor-y1", models.StatusPublished, future)
	sDraft := createIAPTestSubject(t, s, "subj-keep-draft", "bachelor-y1", models.StatusDraft, future)
	sExpired := createIAPTestSubject(t, s, "subj-keep-expired", "bachelor-y1", models.StatusPublished, past)
	want := []string{sActive.ID, sDraft.ID, sExpired.ID}

	recCreate := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", map[string]any{
		"product_id":  "com.wael.app.pkg.keep",
		"title_ar":    "باقة",
		"title_en":    "Keep",
		"subject_ids": want,
		"published":   true,
	})
	if recCreate.Code != http.StatusCreated && recCreate.Code != http.StatusOK {
		t.Fatalf("create: %d (%s)", recCreate.Code, recCreate.Body.String())
	}
	var created IAPPackageAdminDTO
	if err := json.Unmarshal(recCreate.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	if !slices.Equal(created.SubjectIDs, want) {
		t.Fatalf("create response: expected %v, got %v", want, created.SubjectIDs)
	}

	recGet := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/packages/"+created.ID, nil)
	if recGet.Code != http.StatusOK {
		t.Fatalf("get: %d (%s)", recGet.Code, recGet.Body.String())
	}
	var read IAPPackageAdminDTO
	if err := json.Unmarshal(recGet.Body.Bytes(), &read); err != nil {
		t.Fatalf("unmarshal get: %v", err)
	}

	recPatch := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/iap/packages/"+created.ID, map[string]any{
		"title_en":    "Keep edited",
		"subject_ids": read.SubjectIDs,
	})
	if recPatch.Code != http.StatusOK {
		t.Fatalf("patch: %d (%s)", recPatch.Code, recPatch.Body.String())
	}

	stored, err := s.Store.GetIAPPackage(context.Background(), created.ID)
	if err != nil || stored == nil {
		t.Fatalf("GetIAPPackage: %v", err)
	}
	if !slices.Equal(stored.SubjectIDs, want) {
		t.Errorf("stored after edit: expected %v, got %v", want, stored.SubjectIDs)
	}

	// Coverage still ignores draft and expired subjects.
	recCov := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/coverage", nil)
	if recCov.Code != http.StatusOK {
		t.Fatalf("coverage: %d", recCov.Code)
	}
	if strings.Contains(recCov.Body.String(), sDraft.ID) || strings.Contains(recCov.Body.String(), sExpired.ID) {
		t.Errorf("coverage must not list draft or expired subjects: %s", recCov.Body.String())
	}
}
