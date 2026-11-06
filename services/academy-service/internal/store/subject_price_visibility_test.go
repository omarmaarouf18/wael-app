package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
)

func TestMigrateSubjectPriceVisibility_MemoryStore(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	// Create subject without price_visibility (simulating pre-migration data)
	s1 := &models.Subject{
		ID:              "subj-legacy-1",
		LevelKey:        "bachelor-y1",
		TitleAr:         "مادة قديمة 1",
		Price:           100,
		PriceVisibility: "",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(24 * time.Hour),
	}
	s2 := &models.Subject{
		ID:              "subj-explicit-show",
		LevelKey:        "bachelor-y1",
		TitleAr:         "مادة ظاهرة",
		Price:           200,
		PriceVisibility: models.PriceVisibilityShow,
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(24 * time.Hour),
	}
	s3 := &models.Subject{
		ID:              "subj-explicit-hide",
		LevelKey:        "bachelor-y1",
		TitleAr:         "مادة مخفية",
		Price:           300,
		PriceVisibility: models.PriceVisibilityHide,
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(24 * time.Hour),
	}

	// In memory store, CreateSubject defaults empty to follow_global,
	// but let's test MigrateSubjectPriceVisibility directly:
	if err := st.CreateSubject(ctx, s1); err != nil {
		t.Fatalf("CreateSubject s1: %v", err)
	}
	if err := st.CreateSubject(ctx, s2); err != nil {
		t.Fatalf("CreateSubject s2: %v", err)
	}
	if err := st.CreateSubject(ctx, s3); err != nil {
		t.Fatalf("CreateSubject s3: %v", err)
	}

	// Verify s1 got defaulted to follow_global
	got1, err := st.GetSubjectByID(ctx, "subj-legacy-1")
	if err != nil || got1 == nil {
		t.Fatalf("GetSubjectByID s1: %v", err)
	}
	if got1.PriceVisibility != models.PriceVisibilityFollowGlobal {
		t.Errorf("s1 PriceVisibility = %q, want %q", got1.PriceVisibility, models.PriceVisibilityFollowGlobal)
	}

	// Verify s2 kept show
	got2, err := st.GetSubjectByID(ctx, "subj-explicit-show")
	if err != nil || got2 == nil {
		t.Fatalf("GetSubjectByID s2: %v", err)
	}
	if got2.PriceVisibility != models.PriceVisibilityShow {
		t.Errorf("s2 PriceVisibility = %q, want %q", got2.PriceVisibility, models.PriceVisibilityShow)
	}

	// Verify s3 kept hide
	got3, err := st.GetSubjectByID(ctx, "subj-explicit-hide")
	if err != nil || got3 == nil {
		t.Fatalf("GetSubjectByID s3: %v", err)
	}
	if got3.PriceVisibility != models.PriceVisibilityHide {
		t.Errorf("s3 PriceVisibility = %q, want %q", got3.PriceVisibility, models.PriceVisibilityHide)
	}

	// Run migration
	if err := st.MigrateSubjectPriceVisibility(ctx); err != nil {
		t.Fatalf("MigrateSubjectPriceVisibility: %v", err)
	}
}
