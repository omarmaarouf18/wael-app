package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
)

func newAdminMongoTestServer(t *testing.T, mongoURI, dbName string) (*Server, *httptest.Server, *store.MongoStore) {
	t.Helper()
	vsrv := fakeVerifyServer(t, okVerify)
	t.Cleanup(vsrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ms, err := store.NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}

	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = ms.Database().Drop(dropCtx)
		_ = ms.Close(dropCtx)
	})

	if err := ms.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes failed: %v", err)
	}
	if err := ms.SeedLevels(ctx); err != nil {
		t.Fatalf("SeedLevels failed: %v", err)
	}

	s := New(ms, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", false, "+201000000000")
	s.AuthAdminURL = vsrv.URL
	s.VerifyClient = vsrv.Client()
	return s, vsrv, ms
}

func TestAdminIAP_Mongo(t *testing.T) {
	mongoURI := requireMongoURI(t)
	dbName := randomMongoDatabase("test_academy_admin_iap")
	s, _, ms := newAdminMongoTestServer(t, mongoURI, dbName)
	ctx := context.Background()

	future := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	past := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)

	// Create test subjects
	sActive := &models.Subject{
		ID:              "subj-m-active",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة نشطة",
		TitleEn:         "Active Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: future,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := ms.CreateSubject(ctx, sActive); err != nil {
		t.Fatalf("CreateSubject active: %v", err)
	}

	sDraft := &models.Subject{
		ID:              "subj-m-draft",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة مسودة",
		TitleEn:         "Draft Subject",
		Status:          models.StatusDraft,
		AccessExpiresAt: future,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := ms.CreateSubject(ctx, sDraft); err != nil {
		t.Fatalf("CreateSubject draft: %v", err)
	}

	sExpired := &models.Subject{
		ID:              "subj-m-expired",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة منتهية",
		TitleEn:         "Expired Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: past,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := ms.CreateSubject(ctx, sExpired); err != nil {
		t.Fatalf("CreateSubject expired: %v", err)
	}

	t.Run("Create_And_Read_With_Stale_Subject_Skipping", func(t *testing.T) {
		// Create package directly in store with active, draft, expired, and non-existent IDs
		stalePkg := &models.IAPPackage{
			ID:        "pkg-m-stale",
			ProductID: "com.wael.app.pkg.mongostale",
			TitleAr:   "باقة مونجو للمواد القديمة",
			TitleEn:   "Mongo Stale Package",
			SubjectIDs: []string{
				sActive.ID,
				sDraft.ID,
				sExpired.ID,
				"subj-m-deleted-nonexistent",
			},
			Published: true,
			Position:  1,
		}
		if err := ms.CreateIAPPackage(ctx, stalePkg); err != nil {
			t.Fatalf("CreateIAPPackage: %v", err)
		}

		// Read package by ID via Admin API
		recGet := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/packages/"+stalePkg.ID, nil)
		if recGet.Code != http.StatusOK {
			t.Fatalf("GET /internal/admin/iap/packages/{id} failed: %d (%s)", recGet.Code, recGet.Body.String())
		}
		var gotPkg IAPPackageAdminDTO
		if err := json.Unmarshal(recGet.Body.Bytes(), &gotPkg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(gotPkg.SubjectIDs) != 1 || gotPkg.SubjectIDs[0] != sActive.ID {
			t.Errorf("expected only active subject [%s], got %v", sActive.ID, gotPkg.SubjectIDs)
		}

		// Read package list via Admin API
		recList := doAdminJSON(t, s, http.MethodGet, "/internal/admin/iap/packages", nil)
		if recList.Code != http.StatusOK {
			t.Fatalf("GET /internal/admin/iap/packages failed: %d (%s)", recList.Code, recList.Body.String())
		}
		var listRes struct {
			Packages []IAPPackageAdminDTO `json:"packages"`
		}
		if err := json.Unmarshal(recList.Body.Bytes(), &listRes); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		found := false
		for _, p := range listRes.Packages {
			if p.ID == stalePkg.ID {
				found = true
				if len(p.SubjectIDs) != 1 || p.SubjectIDs[0] != sActive.ID {
					t.Errorf("list package: expected only active subject [%s], got %v", sActive.ID, p.SubjectIDs)
				}
			}
		}
		if !found {
			t.Errorf("package %s not found in list", stalePkg.ID)
		}
	})

	t.Run("Create_Via_API_And_Patch_Immutable_ProductID", func(t *testing.T) {
		body := map[string]any{
			"product_id":  "com.wael.app.pkg.mongocrud",
			"title_ar":    "باقة تجريبية",
			"title_en":    "Test Package",
			"subject_ids": []string{sActive.ID},
			"published":   true,
		}
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create failed: %d (%s)", rec.Code, rec.Body.String())
		}
		var p IAPPackageAdminDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &p)

		// Duplicate product_id -> 409
		recDup := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", body)
		if recDup.Code != http.StatusConflict || adminCode(t, recDup) != "duplicate_product_id" {
			t.Errorf("expected 409 duplicate_product_id, got %d %s", recDup.Code, adminCode(t, recDup))
		}

		// Patch with product_id -> 400 product_id_immutable
		recPatch := doAdminJSON(t, s, http.MethodPatch, "/internal/admin/iap/packages/"+p.ID, map[string]any{
			"product_id": "com.wael.app.pkg.mongocrud",
		})
		if recPatch.Code != http.StatusBadRequest || adminCode(t, recPatch) != "product_id_immutable" {
			t.Errorf("expected 400 product_id_immutable, got %d %s", recPatch.Code, adminCode(t, recPatch))
		}
	})

	t.Run("Delete_Blocked_By_Purchase_On_Mongo", func(t *testing.T) {
		body := map[string]any{
			"product_id": "com.wael.app.pkg.mongodel",
			"title_ar":   "باقة حذف",
			"title_en":   "Delete Package",
		}
		rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/iap/packages", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create failed: %d", rec.Code)
		}
		var p IAPPackageAdminDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &p)

		// Insert purchase referencing this package
		pur := &models.IAPPurchase{
			ID:            "pur-m-del",
			TransactionID: "tx-m-del-1",
			UserID:        "user-m-1",
			PackageID:     p.ID,
			ProductID:     p.ProductID,
			Environment:   models.IAPEnvironmentProduction,
			PurchaseDate:  time.Now().UTC(),
			CreatedAt:     time.Now().UTC(),
		}
		if err := ms.InsertIAPPurchase(ctx, pur); err != nil {
			t.Fatalf("InsertIAPPurchase: %v", err)
		}

		// Delete should fail with 409
		recDel := doAdminJSON(t, s, http.MethodDelete, "/internal/admin/iap/packages/"+p.ID, nil)
		if recDel.Code != http.StatusConflict || adminCode(t, recDel) != "package_has_purchases" {
			t.Errorf("expected 409 package_has_purchases, got %d %s", recDel.Code, adminCode(t, recDel))
		}
	})

	t.Run("Coverage_On_Mongo", func(t *testing.T) {
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

		// sActive is covered by pkg-m-stale and com.wael.app.pkg.mongocrud
		// sDraft and sExpired are ignored from coverage
		for _, id := range covRes.UncoveredSubjectIDs {
			if id == sActive.ID {
				t.Errorf("active subject %s should be covered", sActive.ID)
			}
			if id == sDraft.ID || id == sExpired.ID {
				t.Errorf("draft or expired subject %s should not appear in coverage", id)
			}
		}
	})
}
