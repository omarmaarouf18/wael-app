package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func TestStudentIAP_Mongo(t *testing.T) {
	mongoURI := requireMongoURI(t)
	dbName := randomMongoDatabase("test_academy_student_iap")
	s, _, ms := newAdminMongoTestServer(t, mongoURI, dbName)
	ctx := context.Background()

	future := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	past := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)

	studentID := "usr-mongo-stud-1"
	studentTok := makeStudentToken(t, studentID)

	// Create subjects on Mongo: 1 active, 1 draft, 1 expired
	sActive := &models.Subject{
		ID:              "subj-m-stud-active",
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
		ID:              "subj-m-stud-draft",
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
		ID:              "subj-m-stud-expired",
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

	// Grant sActive to student
	ent := &models.Entitlement{
		ID:        "ent-m-stud-1",
		UserID:    studentID,
		SubjectID: sActive.ID,
		Source:    models.EntitlementSourceAdminGrant,
		GrantedAt: time.Now().UTC(),
		ExpiresAt: future,
		Active:    true,
	}
	if err := ms.Grant(ctx, ent); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	// Create package with active + stale subjects
	pkg := &models.IAPPackage{
		ID:        "pkg-m-stud-1",
		ProductID: "com.wael.app.pkg.year1",
		TitleAr:   "الفرقة الأولى",
		TitleEn:   "Year 1",
		SubjectIDs: []string{
			sActive.ID,
			sDraft.ID,
			sExpired.ID,
			"subj-m-nonexistent",
		},
		Published: true,
		Position:  1,
	}
	if err := ms.CreateIAPPackage(ctx, pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	t.Run("GetIAPPackages_On_Mongo_Filters_Stale_And_Sets_Owned", func(t *testing.T) {
		rec := studentIAPGet(t, s, studentTok, "test-gateway-secret")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}

		var resp IAPPackagesResponseDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		wantToken := models.DeterministicAppAccountToken(studentID)
		if resp.AppAccountToken != wantToken {
			t.Errorf("app_account_token = %s, want %s", resp.AppAccountToken, wantToken)
		}

		if len(resp.Packages) != 1 {
			t.Fatalf("expected 1 package, got %d", len(resp.Packages))
		}

		gotPkg := resp.Packages[0]
		if len(gotPkg.Subjects) != 1 || gotPkg.Subjects[0].ID != sActive.ID {
			t.Fatalf("subjects: expected only active subject [%s], got %+v", sActive.ID, gotPkg.Subjects)
		}
		if !gotPkg.Subjects[0].Owned {
			t.Errorf("active subject owned should be true")
		}
		if !gotPkg.OnSale {
			t.Errorf("package should be on_sale=true")
		}
	})

	t.Run("GetIAPPackages_On_Mongo_No_Price_Or_Video_IDs", func(t *testing.T) {
		rec := studentIAPGet(t, s, studentTok, "test-gateway-secret")
		bodyStr := rec.Body.String()
		for _, fk := range []string{`"price"`, `"currency"`, `"video_id"`, `"youtube_video_id"`, `"storage_key"`, `"content_id"`} {
			if strings.Contains(bodyStr, fk) {
				t.Errorf("response contains forbidden field %s", fk)
			}
		}
	})
}
