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
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

func TestSubject_EffectivePriceVisible(t *testing.T) {
	cases := []struct {
		name             string
		visibility       string
		globalShowPrices bool
		want             bool
	}{
		{"follow_global with global on", models.PriceVisibilityFollowGlobal, true, true},
		{"follow_global with global off", models.PriceVisibilityFollowGlobal, false, false},
		{"empty visibility with global on", "", true, true},
		{"empty visibility with global off", "", false, false},
		{"show with global on", models.PriceVisibilityShow, true, true},
		{"show with global off", models.PriceVisibilityShow, false, true},
		{"hide with global on", models.PriceVisibilityHide, true, false},
		{"hide with global off", models.PriceVisibilityHide, false, false},
		{"unknown visibility with global on", "unknown", true, true},
		{"unknown visibility with global off", "unknown", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &models.Subject{PriceVisibility: tc.visibility}
			got := s.EffectivePriceVisible(tc.globalShowPrices)
			if got != tc.want {
				t.Errorf("EffectivePriceVisible(%v) = %v, want %v", tc.globalShowPrices, got, tc.want)
			}
		})
	}
}

func setupPriceVisibilityTestServer(t *testing.T, globalShowPrices bool) (*Server, string) {
	t.Helper()
	st := store.NewMemoryStore()
	_ = st.SeedLevels(context.Background())
	_ = st.SaveAppSettings(context.Background(), &models.AppSettings{ShowPrices: globalShowPrices})

	s := New(st, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", true, "+201000000000")

	// Create published level
	st.PutLevel(models.Level{
		Key:       "bachelor-y1",
		StudyType: "bachelor",
		Position:  1,
		Published: true,
	})

	// Create 3 subjects: follow_global, show, hide
	expires := time.Now().Add(24 * time.Hour)
	subjFollow := &models.Subject{
		ID:              "subj-follow",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة تتبع العام",
		Price:           100,
		PriceVisibility: models.PriceVisibilityFollowGlobal,
		Status:          models.StatusPublished,
		AccessExpiresAt: expires,
	}
	subjShow := &models.Subject{
		ID:              "subj-show",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة إظهار دائم",
		Price:           200,
		PriceVisibility: models.PriceVisibilityShow,
		Status:          models.StatusPublished,
		AccessExpiresAt: expires,
	}
	subjHide := &models.Subject{
		ID:              "subj-hide",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة إخفاء دائم",
		Price:           300,
		PriceVisibility: models.PriceVisibilityHide,
		Status:          models.StatusPublished,
		AccessExpiresAt: expires,
	}

	_ = st.CreateSubject(context.Background(), subjFollow)
	_ = st.CreateSubject(context.Background(), subjShow)
	_ = st.CreateSubject(context.Background(), subjHide)

	tok, _ := jwtutil.GenerateToken("student-1", "user", "student1@example.com")
	return s, tok
}

func TestPriceVisibility_AppStudentEndpoints_SixCombinations(t *testing.T) {
	// The 3 visibility modes × 2 global settings = 6 cases for list and detail
	tests := []struct {
		globalShowPrices bool
		subjectID        string
		wantPriceInList  bool
		wantPrice        int
	}{
		// Global ON
		{globalShowPrices: true, subjectID: "subj-follow", wantPriceInList: true, wantPrice: 100},
		{globalShowPrices: true, subjectID: "subj-show", wantPriceInList: true, wantPrice: 200},
		{globalShowPrices: true, subjectID: "subj-hide", wantPriceInList: false, wantPrice: 0},

		// Global OFF
		{globalShowPrices: false, subjectID: "subj-follow", wantPriceInList: false, wantPrice: 0},
		{globalShowPrices: false, subjectID: "subj-show", wantPriceInList: true, wantPrice: 200},
		{globalShowPrices: false, subjectID: "subj-hide", wantPriceInList: false, wantPrice: 0},
	}

	for _, tc := range tests {
		name := tc.subjectID
		if tc.globalShowPrices {
			name += "_global_on"
		} else {
			name += "_global_off"
		}

		t.Run(name, func(t *testing.T) {
			s, tok := setupPriceVisibilityTestServer(t, tc.globalShowPrices)
			handler := s.PublicHandler()

			// 1. Check List endpoint
			reqList := httptest.NewRequest(http.MethodGet, "/academy/subjects?level=bachelor-y1", nil)
			reqList.Header.Set("X-Gateway-Secret", "test-gateway-secret")
			reqList.Header.Set("Authorization", "Bearer "+tok)
			recList := httptest.NewRecorder()
			handler.ServeHTTP(recList, reqList)

			if recList.Code != http.StatusOK {
				t.Fatalf("list status = %d: %s", recList.Code, recList.Body.String())
			}

			var listResp models.SubjectListResponseDTO
			if err := json.Unmarshal(recList.Body.Bytes(), &listResp); err != nil {
				t.Fatalf("unmarshal list: %v", err)
			}

			var foundItem *models.SubjectListItemDTO
			for i := range listResp.Items {
				if listResp.Items[i].ID == tc.subjectID {
					foundItem = &listResp.Items[i]
					break
				}
			}
			if foundItem == nil {
				t.Fatalf("subject %s not found in list response", tc.subjectID)
			}

			if tc.wantPriceInList {
				if foundItem.Price == nil {
					t.Fatalf("expected price for %s in list, got nil", tc.subjectID)
				}
				if *foundItem.Price != tc.wantPrice {
					t.Errorf("list price = %d, want %d", *foundItem.Price, tc.wantPrice)
				}
				if foundItem.Currency != "EGP" {
					t.Errorf("list currency = %q, want EGP", foundItem.Currency)
				}
			} else {
				if foundItem.Price != nil {
					t.Errorf("expected no price for %s in list, got %d", tc.subjectID, *foundItem.Price)
				}
				if foundItem.Currency != "" {
					t.Errorf("expected empty currency for %s in list, got %q", tc.subjectID, foundItem.Currency)
				}
			}

			// 2. Check Detail endpoint
			reqDetail := httptest.NewRequest(http.MethodGet, "/academy/subjects/"+tc.subjectID, nil)
			reqDetail.Header.Set("X-Gateway-Secret", "test-gateway-secret")
			reqDetail.Header.Set("Authorization", "Bearer "+tok)
			recDetail := httptest.NewRecorder()
			handler.ServeHTTP(recDetail, reqDetail)

			if recDetail.Code != http.StatusOK {
				t.Fatalf("detail status = %d: %s", recDetail.Code, recDetail.Body.String())
			}

			var detailResp models.SubjectDetailDTO
			if err := json.Unmarshal(recDetail.Body.Bytes(), &detailResp); err != nil {
				t.Fatalf("unmarshal detail: %v", err)
			}

			if tc.wantPriceInList {
				if detailResp.Price == nil {
					t.Fatalf("expected price for %s in detail, got nil", tc.subjectID)
				}
				if *detailResp.Price != tc.wantPrice {
					t.Errorf("detail price = %d, want %d", *detailResp.Price, tc.wantPrice)
				}
				if detailResp.Currency != "EGP" {
					t.Errorf("detail currency = %q, want EGP", detailResp.Currency)
				}
			} else {
				if detailResp.Price != nil {
					t.Errorf("expected no price for %s in detail, got %d", tc.subjectID, *detailResp.Price)
				}
				if detailResp.Currency != "" {
					t.Errorf("expected empty currency for %s in detail, got %q", tc.subjectID, detailResp.Currency)
				}
			}
		})
	}
}

func TestAdminSubjects_PriceVisibilityField(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	// 1. Create with explicit show
	expires := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	rec := doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", map[string]any{
		"level_id":          "bachelor-y1",
		"title_ar":          "مادة اختبار الظهور",
		"price":             500,
		"price_visibility":  "show",
		"access_expires_at": expires,
		"term":              "first",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with show status = %d: %s", rec.Code, rec.Body.String())
	}
	var created1 SubjectAdminDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &created1)
	if created1.PriceVisibility != "show" {
		t.Errorf("created1 price_visibility = %q, want show", created1.PriceVisibility)
	}

	// 2. Create with default (omitted price_visibility) -> defaults to follow_global
	rec = doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", map[string]any{
		"level_id":          "bachelor-y1",
		"title_ar":          "مادة اختبار الافتراضي",
		"price":             300,
		"access_expires_at": expires,
		"term":              "first",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with default status = %d: %s", rec.Code, rec.Body.String())
	}
	var created2 SubjectAdminDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &created2)
	if created2.PriceVisibility != "follow_global" {
		t.Errorf("created2 price_visibility = %q, want follow_global", created2.PriceVisibility)
	}

	// 3. Create with invalid price_visibility -> 400
	rec = doAdminJSON(t, s, http.MethodPost, "/internal/admin/subjects", map[string]any{
		"level_id":          "bachelor-y1",
		"title_ar":          "مادة خاطئة",
		"price_visibility":  "invalid_value",
		"access_expires_at": expires,
		"term":              "first",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("create with invalid price_visibility status = %d, want 400", rec.Code)
	}

	// 4. Patch created2 to hide
	rec = doAdminJSON(t, s, http.MethodPatch, "/internal/admin/subjects/"+created2.ID, map[string]any{
		"price_visibility": "hide",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d: %s", rec.Code, rec.Body.String())
	}
	var patched SubjectAdminDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &patched)
	if patched.PriceVisibility != "hide" {
		t.Errorf("patched price_visibility = %q, want hide", patched.PriceVisibility)
	}

	// 5. Patch with invalid price_visibility -> 400
	rec = doAdminJSON(t, s, http.MethodPatch, "/internal/admin/subjects/"+created2.ID, map[string]any{
		"price_visibility": "invalid_value",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("patch with invalid price_visibility status = %d, want 400", rec.Code)
	}
}
