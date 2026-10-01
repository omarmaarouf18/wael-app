package store

import (
	"context"
	"os"
	"testing"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMemoryStore_SeedLevels_Idempotent(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()

	// Run once
	if err := st.SeedLevels(ctx); err != nil {
		t.Fatalf("first seed failed: %v", err)
	}

	levels, err := st.ListLevels(ctx, false)
	if err != nil {
		t.Fatalf("list levels failed: %v", err)
	}
	if len(levels) != 5 {
		t.Fatalf("expected 5 seeded levels, got %d", len(levels))
	}

	// Run twice - changes nothing
	if err := st.SeedLevels(ctx); err != nil {
		t.Fatalf("second seed failed: %v", err)
	}

	levels2, err := st.ListLevels(ctx, false)
	if err != nil {
		t.Fatalf("list levels after second seed failed: %v", err)
	}
	if len(levels2) != 5 {
		t.Fatalf("expected 5 seeded levels after second seed, got %d", len(levels2))
	}

	// Verify order by position
	for i := 0; i < len(levels2)-1; i++ {
		if levels2[i].Position >= levels2[i+1].Position {
			t.Errorf("levels not sorted by position: %d >= %d", levels2[i].Position, levels2[i+1].Position)
		}
	}
}

func TestMemoryStore_ListLevels_PublishedFilter(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()
	_ = st.SeedLevels(ctx)

	// Initially, with onlyWithPublished=true, 0 levels returned
	published, err := st.ListLevels(ctx, true)
	if err != nil {
		t.Fatalf("list published levels: %v", err)
	}
	if len(published) != 0 {
		t.Fatalf("expected 0 published levels initially, got %d", len(published))
	}

	// Mark bachelor-y1 as published
	st.SetLevelPublished("bachelor-y1", true)

	published, err = st.ListLevels(ctx, true)
	if err != nil {
		t.Fatalf("list published levels after publish: %v", err)
	}
	if len(published) != 1 || published[0].Key != "bachelor-y1" {
		t.Fatalf("expected only bachelor-y1 published, got: %v", published)
	}
}

func TestMongoStore_Integration(t *testing.T) {
	if os.Getenv("REQUIRE_DB") != "true" {
		t.Skip("skipping MongoDB integration test: REQUIRE_DB != true")
	}

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://root:c1e0856070f52fe6f0f8746662a64d57e9d3133e081df682@localhost:27019/admin"
	}

	ctx := context.Background()
	dbName := "academy_test_phase22"
	st, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}
	defer func() {
		_ = st.db.Drop(ctx)
		_ = st.Close(ctx)
	}()

	t.Run("seed_levels_idempotent", func(t *testing.T) {
		// First seed
		if err := st.SeedLevels(ctx); err != nil {
			t.Fatalf("first seed failed: %v", err)
		}

		levels, err := st.ListLevels(ctx, false)
		if err != nil {
			t.Fatalf("list levels failed: %v", err)
		}
		if len(levels) != 5 {
			t.Fatalf("expected 5 seeded levels, got %d", len(levels))
		}

		// Second seed
		if err := st.SeedLevels(ctx); err != nil {
			t.Fatalf("second seed failed: %v", err)
		}

		levels2, err := st.ListLevels(ctx, false)
		if err != nil {
			t.Fatalf("list levels after second seed failed: %v", err)
		}
		if len(levels2) != 5 {
			t.Fatalf("expected 5 seeded levels, got %d", len(levels2))
		}
	})

	t.Run("unique_level_key_index", func(t *testing.T) {
		// Attempt to insert duplicate key into levels collection
		dup := models.Level{
			Key:       "bachelor-y1",
			StudyType: models.StudyTypeBachelor,
			TitleAr:   "مكرر",
			TitleEn:   "Duplicate",
			Position:  99,
		}
		_, err := st.db.Collection("levels").InsertOne(ctx, dup)
		if err == nil {
			t.Fatalf("expected duplicate key error for level key 'bachelor-y1', got nil")
		}
	})

	t.Run("list_levels_published_filter", func(t *testing.T) {
		// Before inserting any subjects, onlyWithPublished=true returns 0
		published, err := st.ListLevels(ctx, true)
		if err != nil {
			t.Fatalf("list published levels: %v", err)
		}
		if len(published) != 0 {
			t.Fatalf("expected 0 published levels before subjects, got %d", len(published))
		}

		// Insert a draft subject for bachelor-y1: should NOT reveal bachelor-y1
		_, err = st.db.Collection("subjects").InsertOne(ctx, bson.M{
			"level_key": "bachelor-y1",
			"status":    "draft",
		})
		if err != nil {
			t.Fatalf("insert draft subject: %v", err)
		}

		published, err = st.ListLevels(ctx, true)
		if err != nil {
			t.Fatalf("list published levels with draft subject: %v", err)
		}
		if len(published) != 0 {
			t.Fatalf("expected 0 published levels with only draft subject, got %d", len(published))
		}

		// Insert a published subject for bachelor-y2: should reveal bachelor-y2
		_, err = st.db.Collection("subjects").InsertOne(ctx, bson.M{
			"level_key": "bachelor-y2",
			"status":    "published",
		})
		if err != nil {
			t.Fatalf("insert published subject: %v", err)
		}

		published, err = st.ListLevels(ctx, true)
		if err != nil {
			t.Fatalf("list published levels with published subject: %v", err)
		}
		if len(published) != 1 || published[0].Key != "bachelor-y2" {
			t.Fatalf("expected 1 published level (bachelor-y2), got: %v", published)
		}
	})
}
