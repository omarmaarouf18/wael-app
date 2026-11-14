package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func runIAPStoreSuite(t *testing.T, s Store) {
	ctx := context.Background()

	t.Run("Package_CRUD_And_NotFound", func(t *testing.T) {
		until := time.Now().UTC().Add(365 * 24 * time.Hour).Truncate(time.Second)
		pkg := &models.IAPPackage{
			ID:             "iap-pkg-crud",
			ProductID:      "com.wael.app.pkg.year1",
			TitleAr:        "حزمة السنة الأولى",
			TitleEn:        "First Year Package",
			DescriptionAr:  "وصف الحزمة",
			DescriptionEn:  "Package Description",
			SubjectIDs:     []string{"sub-crud-1", "sub-crud-2"},
			AvailableUntil: &until,
			Published:      true,
			Position:       5,
		}

		if err := s.CreateIAPPackage(ctx, pkg); err != nil {
			t.Fatalf("CreateIAPPackage failed: %v", err)
		}

		got, err := s.GetIAPPackage(ctx, "iap-pkg-crud")
		if err != nil {
			t.Fatalf("GetIAPPackage failed: %v", err)
		}
		if got == nil {
			t.Fatal("GetIAPPackage returned nil, expected package")
		}
		if got.ID != pkg.ID || got.ProductID != pkg.ProductID || got.TitleAr != pkg.TitleAr ||
			got.TitleEn != pkg.TitleEn || got.DescriptionAr != pkg.DescriptionAr ||
			got.DescriptionEn != pkg.DescriptionEn || got.Position != pkg.Position || !got.Published {
			t.Errorf("GetIAPPackage got %+v, want %+v", got, pkg)
		}
		if len(got.SubjectIDs) != 2 || got.SubjectIDs[0] != "sub-crud-1" || got.SubjectIDs[1] != "sub-crud-2" {
			t.Errorf("SubjectIDs mismatch: got %v", got.SubjectIDs)
		}
		if !got.HasSubject("sub-crud-1") || got.HasSubject("sub-crud-nonexistent") {
			t.Errorf("HasSubject returned unexpected results")
		}

		gotByProd, err := s.GetIAPPackageByProductID(ctx, "com.wael.app.pkg.year1")
		if err != nil {
			t.Fatalf("GetIAPPackageByProductID failed: %v", err)
		}
		if gotByProd == nil || gotByProd.ID != pkg.ID {
			t.Fatalf("GetIAPPackageByProductID got %+v, want ID %s", gotByProd, pkg.ID)
		}

		// Non-existent lookups
		missing, err := s.GetIAPPackage(ctx, "missing-pkg-id")
		if err != nil {
			t.Fatalf("GetIAPPackage missing returned error: %v", err)
		}
		if missing != nil {
			t.Errorf("expected nil for missing package, got %+v", missing)
		}

		missingProd, err := s.GetIAPPackageByProductID(ctx, "missing-prod-id")
		if err != nil {
			t.Fatalf("GetIAPPackageByProductID missing returned error: %v", err)
		}
		if missingProd != nil {
			t.Errorf("expected nil for missing product ID, got %+v", missingProd)
		}

		// Update mutable fields (attempting to alter ProductID must not succeed)
		updatePkg := &models.IAPPackage{
			ID:            "iap-pkg-crud",
			ProductID:     "com.tampered.product.id",
			TitleAr:       "حزمة السنة الأولى معدلة",
			TitleEn:       "First Year Package Updated",
			DescriptionAr: "وصف معدل",
			DescriptionEn: "Updated Description",
			SubjectIDs:    []string{"sub-crud-1"},
			Published:     false,
			Position:      15,
		}
		if err := s.UpdateIAPPackage(ctx, updatePkg); err != nil {
			t.Fatalf("UpdateIAPPackage failed: %v", err)
		}

		updated, err := s.GetIAPPackage(ctx, "iap-pkg-crud")
		if err != nil {
			t.Fatalf("GetIAPPackage after update failed: %v", err)
		}
		if updated.TitleAr != "حزمة السنة الأولى معدلة" || updated.Position != 15 || updated.Published != false {
			t.Errorf("updated fields mismatch: %+v", updated)
		}
		if len(updated.SubjectIDs) != 1 || updated.SubjectIDs[0] != "sub-crud-1" {
			t.Errorf("updated SubjectIDs mismatch: %v", updated.SubjectIDs)
		}
		// ProductID MUST remain immutable
		if updated.ProductID != "com.wael.app.pkg.year1" {
			t.Errorf("ProductID was mutated! got %s, want com.wael.app.pkg.year1", updated.ProductID)
		}
		tamperedLookup, err := s.GetIAPPackageByProductID(ctx, "com.tampered.product.id")
		if err != nil || tamperedLookup != nil {
			t.Errorf("lookup by tampered product ID returned: %+v, %v", tamperedLookup, err)
		}

		// Update non-existent package
		err = s.UpdateIAPPackage(ctx, &models.IAPPackage{ID: "missing-pkg-id"})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("UpdateIAPPackage missing package: got err %v, want ErrNotFound", err)
		}

		// Delete package without purchases
		tempPkg := &models.IAPPackage{
			ID:        "iap-pkg-temp-del",
			ProductID: "com.wael.app.pkg.temp",
			TitleAr:   "حزمة مؤقتة",
			TitleEn:   "Temp Package",
		}
		if err := s.CreateIAPPackage(ctx, tempPkg); err != nil {
			t.Fatalf("CreateIAPPackage temp failed: %v", err)
		}
		if err := s.DeleteIAPPackage(ctx, "iap-pkg-temp-del"); err != nil {
			t.Fatalf("DeleteIAPPackage temp failed: %v", err)
		}
		afterDel, err := s.GetIAPPackage(ctx, "iap-pkg-temp-del")
		if err != nil || afterDel != nil {
			t.Errorf("expected deleted package to be nil, got %+v, %v", afterDel, err)
		}

		// Delete non-existent
		err = s.DeleteIAPPackage(ctx, "iap-pkg-temp-del")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteIAPPackage non-existent: got err %v, want ErrNotFound", err)
		}
	})

	t.Run("Unique_ProductID", func(t *testing.T) {
		pkgOrig := &models.IAPPackage{
			ID:        "iap-pkg-orig",
			ProductID: "com.wael.app.pkg.uniq.test",
			TitleAr:   "حزمة فريدة",
			TitleEn:   "Unique Package",
		}
		if err := s.CreateIAPPackage(ctx, pkgOrig); err != nil {
			t.Fatalf("CreateIAPPackage orig failed: %v", err)
		}

		pkgDup := &models.IAPPackage{
			ID:        "iap-pkg-dup",
			ProductID: "com.wael.app.pkg.uniq.test", // same ProductID
			TitleAr:   "حزمة مكررة",
			TitleEn:   "Duplicate Package",
		}
		err := s.CreateIAPPackage(ctx, pkgDup)
		if !errors.Is(err, ErrDuplicate) {
			t.Fatalf("CreateIAPPackage duplicate ProductID: got err %v, want ErrDuplicate", err)
		}
	})

	t.Run("Insert_Purchase_And_Unique_TransactionID", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		pur := &models.IAPPurchase{
			ID:                    "iap-pur-1",
			TransactionID:         "apple-tx-10001",
			OriginalTransactionID: "apple-tx-10001",
			UserID:                "user-test-1",
			PackageID:             "iap-pkg-crud",
			ProductID:             "com.wael.app.pkg.year1",
			Environment:           models.IAPEnvironmentProduction,
			PurchaseDate:          now,
			GrantedSubjectIDs:     []string{"sub-crud-1"},
			Skipped:               []models.IAPSkipped{{SubjectID: "sub-crud-2", Reason: models.IAPSkipReasonAlreadyOwned}},
			CreatedAt:             now,
		}

		if err := s.InsertIAPPurchase(ctx, pur); err != nil {
			t.Fatalf("InsertIAPPurchase failed: %v", err)
		}

		gotPur, err := s.GetIAPPurchaseByTransactionID(ctx, "apple-tx-10001")
		if err != nil {
			t.Fatalf("GetIAPPurchaseByTransactionID failed: %v", err)
		}
		if gotPur == nil {
			t.Fatal("expected purchase, got nil")
		}
		if gotPur.ID != pur.ID || gotPur.TransactionID != pur.TransactionID || gotPur.UserID != pur.UserID ||
			gotPur.PackageID != pur.PackageID || gotPur.ProductID != pur.ProductID || gotPur.Environment != pur.Environment {
			t.Errorf("GetIAPPurchaseByTransactionID mismatch: got %+v, want %+v", gotPur, pur)
		}
		if len(gotPur.GrantedSubjectIDs) != 1 || gotPur.GrantedSubjectIDs[0] != "sub-crud-1" {
			t.Errorf("GrantedSubjectIDs mismatch: %v", gotPur.GrantedSubjectIDs)
		}
		if len(gotPur.Skipped) != 1 || gotPur.Skipped[0].SubjectID != "sub-crud-2" || gotPur.Skipped[0].Reason != models.IAPSkipReasonAlreadyOwned {
			t.Errorf("Skipped mismatch: %+v", gotPur.Skipped)
		}

		// Duplicate transaction ID must fail with ErrDuplicate
		purDup := &models.IAPPurchase{
			ID:                    "iap-pur-dup",
			TransactionID:         "apple-tx-10001", // duplicate
			OriginalTransactionID: "apple-tx-10001",
			UserID:                "user-test-2",
			PackageID:             "iap-pkg-crud",
			ProductID:             "com.wael.app.pkg.year1",
			Environment:           models.IAPEnvironmentSandbox,
			PurchaseDate:          now,
		}
		err = s.InsertIAPPurchase(ctx, purDup)
		if !errors.Is(err, ErrDuplicate) {
			t.Fatalf("InsertIAPPurchase duplicate transaction_id: got err %v, want ErrDuplicate", err)
		}

		// Non-existent transaction ID
		missingPur, err := s.GetIAPPurchaseByTransactionID(ctx, "missing-tx-id")
		if err != nil {
			t.Fatalf("GetIAPPurchaseByTransactionID missing returned error: %v", err)
		}
		if missingPur != nil {
			t.Errorf("expected nil for missing transaction ID, got %+v", missingPur)
		}
	})

	t.Run("Delete_Blocked_By_Purchase", func(t *testing.T) {
		// "iap-pkg-crud" has "apple-tx-10001" referencing it
		err := s.DeleteIAPPackage(ctx, "iap-pkg-crud")
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("DeleteIAPPackage with purchases: got err %v, want ErrConflict", err)
		}

		// Package must still exist
		pkg, err := s.GetIAPPackage(ctx, "iap-pkg-crud")
		if err != nil || pkg == nil {
			t.Fatalf("package should still exist after rejected delete: got %+v, %v", pkg, err)
		}

		// Count purchases
		count, err := s.CountIAPPurchasesByPackage(ctx, "iap-pkg-crud")
		if err != nil {
			t.Fatalf("CountIAPPurchasesByPackage failed: %v", err)
		}
		if count != 1 {
			t.Errorf("CountIAPPurchasesByPackage got %d, want 1", count)
		}

		zeroCount, err := s.CountIAPPurchasesByPackage(ctx, "non-existent-pkg")
		if err != nil {
			t.Fatalf("CountIAPPurchasesByPackage non-existent failed: %v", err)
		}
		if zeroCount != 0 {
			t.Errorf("CountIAPPurchasesByPackage non-existent got %d, want 0", zeroCount)
		}
	})

	t.Run("ListIAPPurchases_Filters_And_Limit", func(t *testing.T) {
		pkg2 := &models.IAPPackage{
			ID:        "iap-pkg-filter-2",
			ProductID: "com.wael.app.pkg.year2",
			TitleAr:   "حزمة ثانية",
			TitleEn:   "Second Package",
		}
		if err := s.CreateIAPPackage(ctx, pkg2); err != nil {
			t.Fatalf("CreateIAPPackage pkg2 failed: %v", err)
		}

		t1 := time.Now().UTC().Add(10 * time.Second).Truncate(time.Second)
		t2 := time.Now().UTC().Add(20 * time.Second).Truncate(time.Second)

		pur2 := &models.IAPPurchase{
			ID:            "iap-pur-2",
			TransactionID: "apple-tx-20002",
			UserID:        "user-test-2",
			PackageID:     "iap-pkg-crud",
			ProductID:     "com.wael.app.pkg.year1",
			Environment:   models.IAPEnvironmentProduction,
			PurchaseDate:  t1,
			CreatedAt:     t1,
		}
		pur3 := &models.IAPPurchase{
			ID:            "iap-pur-3",
			TransactionID: "apple-tx-30003",
			UserID:        "user-test-1",
			PackageID:     "iap-pkg-filter-2",
			ProductID:     "com.wael.app.pkg.year2",
			Environment:   models.IAPEnvironmentProduction,
			PurchaseDate:  t2,
			CreatedAt:     t2,
		}
		if err := s.InsertIAPPurchase(ctx, pur2); err != nil {
			t.Fatalf("Insert pur2 failed: %v", err)
		}
		if err := s.InsertIAPPurchase(ctx, pur3); err != nil {
			t.Fatalf("Insert pur3 failed: %v", err)
		}

		// Filter by UserID = "user-test-1": should return pur3 then pur1 (pur1 created earlier)
		byUser, err := s.ListIAPPurchases(ctx, models.IAPPurchaseFilter{UserID: "user-test-1"}, 10)
		if err != nil {
			t.Fatalf("ListIAPPurchases by user failed: %v", err)
		}
		if len(byUser) != 2 {
			t.Fatalf("expected 2 purchases for user-test-1, got %d", len(byUser))
		}
		if byUser[0].ID != "iap-pur-3" || byUser[1].ID != "iap-pur-1" {
			t.Errorf("unexpected ordering by user: got [%s, %s], want [iap-pur-3, iap-pur-1]", byUser[0].ID, byUser[1].ID)
		}

		// Filter by PackageID = "iap-pkg-crud": should return pur2 then pur1
		byPkg, err := s.ListIAPPurchases(ctx, models.IAPPurchaseFilter{PackageID: "iap-pkg-crud"}, 10)
		if err != nil {
			t.Fatalf("ListIAPPurchases by package failed: %v", err)
		}
		if len(byPkg) != 2 {
			t.Fatalf("expected 2 purchases for iap-pkg-crud, got %d", len(byPkg))
		}
		if byPkg[0].ID != "iap-pur-2" || byPkg[1].ID != "iap-pur-1" {
			t.Errorf("unexpected ordering by pkg: got [%s, %s], want [iap-pur-2, iap-pur-1]", byPkg[0].ID, byPkg[1].ID)
		}

		// Filter by both UserID and PackageID: only pur1 matches
		both, err := s.ListIAPPurchases(ctx, models.IAPPurchaseFilter{UserID: "user-test-1", PackageID: "iap-pkg-crud"}, 10)
		if err != nil {
			t.Fatalf("ListIAPPurchases with both filters failed: %v", err)
		}
		if len(both) != 1 || both[0].ID != "iap-pur-1" {
			t.Errorf("expected only iap-pur-1, got %v", both)
		}

		// Limit test: limit = 2 should return at most 2 items
		limited, err := s.ListIAPPurchases(ctx, models.IAPPurchaseFilter{}, 2)
		if err != nil {
			t.Fatalf("ListIAPPurchases limited failed: %v", err)
		}
		if len(limited) != 2 {
			t.Fatalf("expected 2 purchases with limit=2, got %d", len(limited))
		}
		// First item should be the newest overall (pur3)
		if limited[0].ID != "iap-pur-3" {
			t.Errorf("expected newest purchase pur-3 first, got %s", limited[0].ID)
		}
	})

	t.Run("ListIAPPackages_Ordering_And_PublishedOnly", func(t *testing.T) {
		packages := []*models.IAPPackage{
			{
				ID:        "pkg-ord-a",
				ProductID: "com.wael.app.pkg.ord.a",
				TitleAr:   "باء",
				TitleEn:   "B",
				Position:  2,
				Published: true,
			},
			{
				ID:        "pkg-ord-b",
				ProductID: "com.wael.app.pkg.ord.b",
				TitleAr:   "ياء",
				TitleEn:   "Y",
				Position:  1,
				Published: true,
			},
			{
				ID:        "pkg-ord-c",
				ProductID: "com.wael.app.pkg.ord.c",
				TitleAr:   "ألف",
				TitleEn:   "A",
				Position:  1,
				Published: true,
			},
			{
				ID:        "pkg-ord-d",
				ProductID: "com.wael.app.pkg.ord.d",
				TitleAr:   "تاء غير منشور",
				TitleEn:   "T Unpublished",
				Position:  0,
				Published: false,
			},
		}

		for _, p := range packages {
			if err := s.CreateIAPPackage(ctx, p); err != nil {
				t.Fatalf("CreateIAPPackage %s failed: %v", p.ID, err)
			}
		}

		// publishedOnly = false: all packages, ordered by position ASC, then title_ar ASC
		all, err := s.ListIAPPackages(ctx, false)
		if err != nil {
			t.Fatalf("ListIAPPackages(false) failed: %v", err)
		}

		// Extract index of each test package in the list
		idxMap := make(map[string]int)
		for i, p := range all {
			idxMap[p.ID] = i
		}

		for _, id := range []string{"pkg-ord-a", "pkg-ord-b", "pkg-ord-c", "pkg-ord-d"} {
			if _, ok := idxMap[id]; !ok {
				t.Fatalf("missing package %s in ListIAPPackages(false)", id)
			}
		}

		// Position 0 (pkg-ord-d) must precede Position 1 (pkg-ord-c and pkg-ord-b)
		if idxMap["pkg-ord-d"] > idxMap["pkg-ord-c"] {
			t.Errorf("pos 0 (pkg-ord-d) must come before pos 1 (pkg-ord-c)")
		}
		// Position 1: "ألف" (pkg-ord-c) must precede "ياء" (pkg-ord-b)
		if idxMap["pkg-ord-c"] > idxMap["pkg-ord-b"] {
			t.Errorf("title_ar 'ألف' (pkg-ord-c) must come before 'ياء' (pkg-ord-b)")
		}
		// Position 1 (pkg-ord-b) must precede Position 2 (pkg-ord-a)
		if idxMap["pkg-ord-b"] > idxMap["pkg-ord-a"] {
			t.Errorf("pos 1 (pkg-ord-b) must come before pos 2 (pkg-ord-a)")
		}

		// publishedOnly = true: pkg-ord-d (unpublished) must NOT be returned
		pubOnly, err := s.ListIAPPackages(ctx, true)
		if err != nil {
			t.Fatalf("ListIAPPackages(true) failed: %v", err)
		}
		for _, p := range pubOnly {
			if p.ID == "pkg-ord-d" {
				t.Errorf("unpublished package pkg-ord-d found in publishedOnly=true list")
			}
			if !p.Published {
				t.Errorf("found unpublished package %s in publishedOnly=true list", p.ID)
			}
		}

		// Verify relative order of c, b, a in published list
		pubIdxMap := make(map[string]int)
		for i, p := range pubOnly {
			pubIdxMap[p.ID] = i
		}
		if pubIdxMap["pkg-ord-c"] > pubIdxMap["pkg-ord-b"] || pubIdxMap["pkg-ord-b"] > pubIdxMap["pkg-ord-a"] {
			t.Errorf("publishedOnly ordering violated: c=%d, b=%d, a=%d",
				pubIdxMap["pkg-ord-c"], pubIdxMap["pkg-ord-b"], pubIdxMap["pkg-ord-a"])
		}
	})

	t.Run("RemoveSubjectFromAllPackages_Direct", func(t *testing.T) {
		p1 := &models.IAPPackage{
			ID:         "pkg-rem-1",
			ProductID:  "com.wael.app.pkg.rem.1",
			TitleAr:    "حزمة حذف 1",
			SubjectIDs: []string{"sub-target", "sub-keep-1", "sub-keep-2"},
		}
		p2 := &models.IAPPackage{
			ID:         "pkg-rem-2",
			ProductID:  "com.wael.app.pkg.rem.2",
			TitleAr:    "حزمة حذف 2",
			SubjectIDs: []string{"sub-target", "sub-keep-3"},
		}
		p3 := &models.IAPPackage{
			ID:         "pkg-rem-3",
			ProductID:  "com.wael.app.pkg.rem.3",
			TitleAr:    "حزمة حذف 3",
			SubjectIDs: []string{"sub-other"},
		}
		for _, p := range []*models.IAPPackage{p1, p2, p3} {
			if err := s.CreateIAPPackage(ctx, p); err != nil {
				t.Fatalf("CreateIAPPackage %s failed: %v", p.ID, err)
			}
		}

		if err := s.RemoveSubjectFromAllPackages(ctx, "sub-target"); err != nil {
			t.Fatalf("RemoveSubjectFromAllPackages failed: %v", err)
		}

		got1, _ := s.GetIAPPackage(ctx, "pkg-rem-1")
		if got1 == nil || len(got1.SubjectIDs) != 2 || got1.HasSubject("sub-target") {
			t.Errorf("pkg-rem-1 still contains sub-target: %v", got1.SubjectIDs)
		}
		got2, _ := s.GetIAPPackage(ctx, "pkg-rem-2")
		if got2 == nil || len(got2.SubjectIDs) != 1 || got2.HasSubject("sub-target") {
			t.Errorf("pkg-rem-2 still contains sub-target: %v", got2.SubjectIDs)
		}
		got3, _ := s.GetIAPPackage(ctx, "pkg-rem-3")
		if got3 == nil || len(got3.SubjectIDs) != 1 || got3.SubjectIDs[0] != "sub-other" {
			t.Errorf("pkg-rem-3 modified unexpectedly: %v", got3.SubjectIDs)
		}
	})

	t.Run("DeleteDraftSubject_RemovesSubjectFromPackages", func(t *testing.T) {
		subjID := "draft-sub-for-iap-clean"
		now := time.Now().UTC().Truncate(time.Millisecond)
		draftSubj := &models.Subject{
			ID:              subjID,
			LevelKey:        "bachelor-y1",
			Term:            "first",
			TitleAr:         "مادة للتنظيف",
			Price:           100,
			Status:          models.StatusDraft,
			AccessExpiresAt: now.Add(30 * 24 * time.Hour),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := s.CreateSubject(ctx, draftSubj); err != nil {
			t.Fatalf("CreateSubject draft failed: %v", err)
		}

		pkgCleanup := &models.IAPPackage{
			ID:         "pkg-draft-clean",
			ProductID:  "com.wael.app.pkg.draft.clean",
			TitleAr:    "حزمة تنظيف مسودة",
			SubjectIDs: []string{subjID, "sub-survivor"},
		}
		if err := s.CreateIAPPackage(ctx, pkgCleanup); err != nil {
			t.Fatalf("CreateIAPPackage pkgCleanup failed: %v", err)
		}

		// Hard delete draft subject
		_, err := s.DeleteDraftSubject(ctx, subjID)
		if err != nil {
			t.Fatalf("DeleteDraftSubject failed: %v", err)
		}

		// Verify subject is gone
		deletedSubj, err := s.GetSubjectByID(ctx, subjID)
		if err != nil || deletedSubj != nil {
			t.Fatalf("deleted subject still found: %+v, %v", deletedSubj, err)
		}

		// Verify package no longer contains subjID
		pkgAfter, err := s.GetIAPPackage(ctx, "pkg-draft-clean")
		if err != nil || pkgAfter == nil {
			t.Fatalf("package lookup failed after draft delete: %+v, %v", pkgAfter, err)
		}
		if pkgAfter.HasSubject(subjID) {
			t.Errorf("package still contains deleted subject %s: %v", subjID, pkgAfter.SubjectIDs)
		}
		if len(pkgAfter.SubjectIDs) != 1 || pkgAfter.SubjectIDs[0] != "sub-survivor" {
			t.Errorf("package SubjectIDs unexpected: %v", pkgAfter.SubjectIDs)
		}
	})

	t.Run("Concurrency_Safety", func(t *testing.T) {
		const numWorkers = 10
		const numIterations = 50
		var wg sync.WaitGroup

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for i := 0; i < numIterations; i++ {
					_, _ = s.ListIAPPackages(ctx, false)
					_, _ = s.ListIAPPackages(ctx, true)
					_, _ = s.GetIAPPackage(ctx, "iap-pkg-crud")
					_, _ = s.GetIAPPackageByProductID(ctx, "com.wael.app.pkg.year1")
					_, _ = s.CountIAPPurchasesByPackage(ctx, "iap-pkg-crud")
					_, _ = s.GetIAPPurchaseByTransactionID(ctx, "apple-tx-10001")
					_, _ = s.ListIAPPurchases(ctx, models.IAPPurchaseFilter{PackageID: "iap-pkg-crud"}, 5)
				}
			}(w)
		}
		wg.Wait()
	})
}

func TestMemoryStore_IAP(t *testing.T) {
	s := NewMemoryStore()
	defer func() { _ = s.Close(context.Background()) }()
	if err := s.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := s.SeedLevels(context.Background()); err != nil {
		t.Fatalf("SeedLevels: %v", err)
	}
	runIAPStoreSuite(t, s)
}

func TestMongoStore_IAP(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_academy_iap_store")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = s.Database().Drop(dropCtx)
		_ = s.Close(dropCtx)
	})

	if err := s.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := s.SeedLevels(ctx); err != nil {
		t.Fatalf("SeedLevels: %v", err)
	}

	runIAPStoreSuite(t, s)
}
