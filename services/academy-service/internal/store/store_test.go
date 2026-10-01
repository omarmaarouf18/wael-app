package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func runStoreSuite(t *testing.T, s Store) {
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := s.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	// 1. Seed levels idempotently: first run
	if err := s.SeedLevels(ctx); err != nil {
		t.Fatalf("SeedLevels 1 failed: %v", err)
	}

	allLevels, err := s.ListLevels(ctx, false)
	if err != nil {
		t.Fatalf("ListLevels all failed: %v", err)
	}
	if len(allLevels) != 5 {
		t.Fatalf("expected 5 seeded levels, got %d", len(allLevels))
	}

	expectedKeys := []string{"bachelor-y1", "bachelor-y2", "bachelor-y3", "bachelor-y4", "vocational"}
	for i, k := range expectedKeys {
		if allLevels[i].Key != k {
			t.Errorf("level[%d] key = %q, want %q", i, allLevels[i].Key, k)
		}
		if allLevels[i].Position != i+1 {
			t.Errorf("level[%d] position = %d, want %d", i, allLevels[i].Position, i+1)
		}
	}

	// 2. Running seed a second time changes nothing
	if err := s.SeedLevels(ctx); err != nil {
		t.Fatalf("SeedLevels 2 failed: %v", err)
	}
	allLevelsSecond, err := s.ListLevels(ctx, false)
	if err != nil {
		t.Fatalf("ListLevels after second seed failed: %v", err)
	}
	if len(allLevelsSecond) != 5 {
		t.Fatalf("expected still 5 levels after second seed, got %d", len(allLevelsSecond))
	}

	// 3. Levels with no published subjects are hidden
	publishedOnly, err := s.ListLevels(ctx, true)
	if err != nil {
		t.Fatalf("ListLevels publishedOnly failed: %v", err)
	}
	if len(publishedOnly) != 0 {
		t.Fatalf("expected 0 levels when no published subjects exist, got %d", len(publishedOnly))
	}

	// 4. Draft subject does not reveal level
	draftSubj := &models.Subject{
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
	if err := s.CreateSubject(ctx, draftSubj); err != nil {
		t.Fatalf("CreateSubject draft failed: %v", err)
	}

	publishedOnly, err = s.ListLevels(ctx, true)
	if err != nil {
		t.Fatalf("ListLevels after draft subject failed: %v", err)
	}
	if len(publishedOnly) != 0 {
		t.Fatalf("expected 0 levels with only draft subject, got %d", len(publishedOnly))
	}

	// 5. Published subject reveals the level
	pubSubj := &models.Subject{
		ID:              "subj-pub-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة منشورة",
		TitleEn:         "Published Subject",
		Status:          models.StatusPublished,
		Price:           1200,
		AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.CreateSubject(ctx, pubSubj); err != nil {
		t.Fatalf("CreateSubject published failed: %v", err)
	}

	publishedOnly, err = s.ListLevels(ctx, true)
	if err != nil {
		t.Fatalf("ListLevels after published subject failed: %v", err)
	}
	if len(publishedOnly) != 1 {
		t.Fatalf("expected 1 level with published subject, got %d", len(publishedOnly))
	}
	if publishedOnly[0].Key != "bachelor-y1" {
		t.Errorf("published level key = %q, want bachelor-y1", publishedOnly[0].Key)
	}

	// 6. ListSubjects queries
	subjs, total, err := s.ListSubjects(ctx, SubjectFilter{LevelKey: "bachelor-y1", Status: models.StatusPublished})
	if err != nil {
		t.Fatalf("ListSubjects failed: %v", err)
	}
	if total != 1 || len(subjs) != 1 {
		t.Fatalf("ListSubjects total=%d items=%d, want 1", total, len(subjs))
	}

	// 7. GetSubjectByID
	found, err := s.GetSubjectByID(ctx, "subj-pub-1")
	if err != nil {
		t.Fatalf("GetSubjectByID failed: %v", err)
	}
	if found == nil || found.TitleAr != "مادة منشورة" {
		t.Fatalf("GetSubjectByID mismatch: %+v", found)
	}

	notFound, err := s.GetSubjectByID(ctx, "non-existent")
	if err != nil {
		t.Fatalf("GetSubjectByID non-existent failed: %v", err)
	}
	if notFound != nil {
		t.Fatalf("expected nil for non-existent subject, got %+v", notFound)
	}

	// 8. Videos: create and list
	vPub := &models.Video{
		ID:             "v-1",
		SubjectID:      "subj-pub-1",
		Position:       1,
		TitleAr:        "فيديو 1",
		YouTubeVideoID: "yt_123456789",
		Published:      true,
		CreatedAt:      time.Now(),
	}
	vDraft := &models.Video{
		ID:             "v-2",
		SubjectID:      "subj-pub-1",
		Position:       2,
		TitleAr:        "فيديو غير منشور",
		YouTubeVideoID: "yt_unpub_456",
		Published:      false,
		CreatedAt:      time.Now(),
	}
	if err := s.CreateVideo(ctx, vPub); err != nil {
		t.Fatalf("CreateVideo pub failed: %v", err)
	}
	if err := s.CreateVideo(ctx, vDraft); err != nil {
		t.Fatalf("CreateVideo draft failed: %v", err)
	}

	vidsPub, err := s.ListVideosBySubject(ctx, "subj-pub-1", true)
	if err != nil {
		t.Fatalf("ListVideosBySubject published failed: %v", err)
	}
	if len(vidsPub) != 1 || vidsPub[0].ID != "v-1" {
		t.Fatalf("expected 1 published video, got %+v", vidsPub)
	}

	vidsAll, err := s.ListVideosBySubject(ctx, "subj-pub-1", false)
	if err != nil {
		t.Fatalf("ListVideosBySubject all failed: %v", err)
	}
	if len(vidsAll) != 2 {
		t.Fatalf("expected 2 videos total, got %d", len(vidsAll))
	}

	// 9. Files: create and list
	f1 := &models.SubjectFile{
		ID:         "f-1",
		SubjectID:  "subj-pub-1",
		Kind:       "book",
		TitleAr:    "كتاب 1",
		SizeBytes:  5000,
		StorageKey: "uuid-storage-1",
		CreatedAt:  time.Now(),
	}
	f2 := &models.SubjectFile{
		ID:         "f-2",
		SubjectID:  "subj-pub-1",
		Kind:       "note",
		TitleAr:    "مذكرة 1",
		SizeBytes:  2000,
		StorageKey: "uuid-storage-2",
		CreatedAt:  time.Now(),
	}
	if err := s.CreateFile(ctx, f1); err != nil {
		t.Fatalf("CreateFile 1 failed: %v", err)
	}
	if err := s.CreateFile(ctx, f2); err != nil {
		t.Fatalf("CreateFile 2 failed: %v", err)
	}

	files, err := s.ListFilesBySubject(ctx, "subj-pub-1")
	if err != nil {
		t.Fatalf("ListFilesBySubject failed: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	// 10. Counts verification
	counts, err := s.GetSubjectCounts(ctx, "subj-pub-1")
	if err != nil {
		t.Fatalf("GetSubjectCounts failed: %v", err)
	}
	if counts.Videos != 1 || counts.Books != 1 || counts.Notes != 1 {
		t.Fatalf("counts mismatch: got videos=%d books=%d notes=%d, want 1/1/1", counts.Videos, counts.Books, counts.Notes)
	}
}

func TestMemoryStore(t *testing.T) {
	s := NewMemoryStore()
	defer func() { _ = s.Close(context.Background()) }()
	runStoreSuite(t, s)
}

func TestMongoStore(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_academy_store")
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

	runStoreSuite(t, s)
}
