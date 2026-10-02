package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

	// GetVideoByID: found published, found draft, not found
	gotV1, err := s.GetVideoByID(ctx, "v-1")
	if err != nil {
		t.Fatalf("GetVideoByID v-1 failed: %v", err)
	}
	if gotV1 == nil || gotV1.ID != "v-1" || gotV1.YouTubeVideoID != "yt_123456789" {
		t.Fatalf("GetVideoByID v-1 unexpected: %+v", gotV1)
	}
	gotVDraft, err := s.GetVideoByID(ctx, "v-2")
	if err != nil {
		t.Fatalf("GetVideoByID v-2 failed: %v", err)
	}
	if gotVDraft == nil || gotVDraft.ID != "v-2" || gotVDraft.Published {
		t.Fatalf("GetVideoByID v-2 unexpected: %+v", gotVDraft)
	}
	gotMissing, err := s.GetVideoByID(ctx, "v-nonexistent")
	if err != nil {
		t.Fatalf("GetVideoByID nonexistent failed: %v", err)
	}
	if gotMissing != nil {
		t.Fatalf("expected nil for nonexistent video, got %+v", gotMissing)
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

	// 11. Grant validation & D20: refuses when subject access has expired
	expiredSubj := &models.Subject{
		ID:              "subj-expired-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة منتهية",
		TitleEn:         "Expired Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(-1 * time.Hour), // in the past
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.CreateSubject(ctx, expiredSubj); err != nil {
		t.Fatalf("CreateSubject expired failed: %v", err)
	}

	// Grant on non-existent subject
	err = s.Grant(ctx, &models.Entitlement{UserID: "u-1", SubjectID: "non-existent"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent subject, got %v", err)
	}

	// Grant on expired subject returns ErrSubjectExpired (D20)
	err = s.Grant(ctx, &models.Entitlement{UserID: "u-1", SubjectID: "subj-expired-1"})
	if !errors.Is(err, ErrSubjectExpired) {
		t.Fatalf("expected ErrSubjectExpired for expired subject, got %v", err)
	}

	// 12. Valid Grant, D21 copy expiry, HasActiveEntitlement, duplicate refusal
	ent := &models.Entitlement{
		UserID:    "u-grant-1",
		SubjectID: "subj-pub-1",
	}
	if err := s.Grant(ctx, ent); err != nil {
		t.Fatalf("Grant failed: %v", err)
	}
	if !ent.Active {
		t.Errorf("expected granted entitlement to be active")
	}
	if !ent.ExpiresAt.Truncate(time.Millisecond).Equal(pubSubj.AccessExpiresAt.Truncate(time.Millisecond)) {
		t.Errorf("expected ExpiresAt=%v copied from subject (D21), got %v", pubSubj.AccessExpiresAt, ent.ExpiresAt)
	}

	// HasActiveEntitlement check
	hasActive, err := s.HasActiveEntitlement(ctx, "u-grant-1", "subj-pub-1")
	if err != nil {
		t.Fatalf("HasActiveEntitlement failed: %v", err)
	}
	if !hasActive {
		t.Errorf("expected user u-grant-1 to own subj-pub-1")
	}

	// Another user does not own it
	hasActiveOther, err := s.HasActiveEntitlement(ctx, "u-other", "subj-pub-1")
	if err != nil {
		t.Fatalf("HasActiveEntitlement other failed: %v", err)
	}
	if hasActiveOther {
		t.Errorf("expected user u-other NOT to own subj-pub-1")
	}

	// GetActiveEntitlementSubjectIDs
	ownedMap, err := s.GetActiveEntitlementSubjectIDs(ctx, "u-grant-1")
	if err != nil {
		t.Fatalf("GetActiveEntitlementSubjectIDs failed: %v", err)
	}
	if !ownedMap["subj-pub-1"] {
		t.Errorf("expected subj-pub-1 in ownedMap")
	}

	// GetActiveEntitlement
	activeEnt, err := s.GetActiveEntitlement(ctx, "u-grant-1", "subj-pub-1")
	if err != nil {
		t.Fatalf("GetActiveEntitlement failed: %v", err)
	}
	if activeEnt == nil || activeEnt.SubjectID != "subj-pub-1" {
		t.Fatalf("expected active entitlement for subj-pub-1, got %v", activeEnt)
	}
	if !activeEnt.ExpiresAt.Truncate(time.Millisecond).Equal(pubSubj.AccessExpiresAt.Truncate(time.Millisecond)) {
		t.Errorf("expected activeEnt.ExpiresAt = %v, got %v", pubSubj.AccessExpiresAt, activeEnt.ExpiresAt)
	}

	activeEntNone, err := s.GetActiveEntitlement(ctx, "u-other", "subj-pub-1")
	if err != nil {
		t.Fatalf("GetActiveEntitlement for u-other failed: %v", err)
	}
	if activeEntNone != nil {
		t.Fatalf("expected nil for u-other, got %v", activeEntNone)
	}

	// GetActiveEntitlements
	activeMap, err := s.GetActiveEntitlements(ctx, "u-grant-1")
	if err != nil {
		t.Fatalf("GetActiveEntitlements failed: %v", err)
	}
	if activeMap["subj-pub-1"] == nil {
		t.Errorf("expected subj-pub-1 in activeMap")
	}

	// Duplicate grant while active returns ErrDuplicate
	err = s.Grant(ctx, &models.Entitlement{UserID: "u-grant-1", SubjectID: "subj-pub-1"})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate on second active grant, got %v", err)
	}

	// 13. Re-purchase after expiry (decision 18/19)
	subjRepurchase := &models.Subject{
		ID:              "subj-repurchase-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة إعادة شراء",
		TitleEn:         "Repurchase Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(50 * time.Millisecond),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.CreateSubject(ctx, subjRepurchase); err != nil {
		t.Fatalf("CreateSubject repurchase failed: %v", err)
	}

	// Grant initial subscription
	if err := s.Grant(ctx, &models.Entitlement{UserID: "u-repurchase-1", SubjectID: "subj-repurchase-1"}); err != nil {
		t.Fatalf("Grant repurchase initial failed: %v", err)
	}

	// Wait for expiry
	time.Sleep(70 * time.Millisecond)

	// Ownership should now be false
	hasActiveAfterExpire, err := s.HasActiveEntitlement(ctx, "u-repurchase-1", "subj-repurchase-1")
	if err != nil {
		t.Fatalf("HasActiveEntitlement after expire failed: %v", err)
	}
	if hasActiveAfterExpire {
		t.Errorf("expected ownership to be false after expiry")
	}

	// Subject must have its date updated by admin before granting again (D20)
	subjRepurchase.AccessExpiresAt = time.Now().Add(30 * 24 * time.Hour)
	if err := s.UpdateSubject(ctx, subjRepurchase); err != nil {
		t.Fatalf("UpdateSubject new date failed: %v", err)
	}

	// Student purchases again -> Grant succeeds
	if err := s.Grant(ctx, &models.Entitlement{UserID: "u-repurchase-1", SubjectID: "subj-repurchase-1"}); err != nil {
		t.Fatalf("Grant repurchase second failed: %v", err)
	}

	// Ownership is active again
	hasActiveRepurchased, err := s.HasActiveEntitlement(ctx, "u-repurchase-1", "subj-repurchase-1")
	if err != nil {
		t.Fatalf("HasActiveEntitlement after repurchase failed: %v", err)
	}
	if !hasActiveRepurchased {
		t.Errorf("expected ownership to be true after repurchase")
	}

	// History rows are kept (decision 19): 2 entitlements in history, only 1 active
	history, err := s.ListEntitlementsByUser(ctx, "u-repurchase-1")
	if err != nil {
		t.Fatalf("ListEntitlementsByUser failed: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entitlement rows, got %d", len(history))
	}
	activeCount := 0
	for _, h := range history {
		if h.Active {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Errorf("expected exactly 1 active entitlement, got %d", activeCount)
	}

	// 14. Concurrency: 20 goroutines granting the exact same (user, subject) pair
	subjConc := &models.Subject{
		ID:              "subj-concurrent-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة التزامن",
		TitleEn:         "Concurrent Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.CreateSubject(ctx, subjConc); err != nil {
		t.Fatalf("CreateSubject concurrent failed: %v", err)
	}

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var duplicateCount atomic.Int32
	const concRoutines = 20

	for i := 0; i < concRoutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gErr := s.Grant(ctx, &models.Entitlement{
				UserID:    "u-concurrent-user",
				SubjectID: "subj-concurrent-1",
			})
			if gErr == nil {
				successCount.Add(1)
			} else if errors.Is(gErr, ErrDuplicate) {
				duplicateCount.Add(1)
			} else {
				t.Errorf("unexpected error in concurrent Grant: %v", gErr)
			}
		}()
	}
	wg.Wait()

	if successCount.Load() != 1 {
		t.Fatalf("expected exactly 1 successful Grant, got %d", successCount.Load())
	}
	if duplicateCount.Load() != concRoutines-1 {
		t.Fatalf("expected %d duplicate errors, got %d", concRoutines-1, duplicateCount.Load())
	}

	// Assert exactly 1 row exists
	concEnts, err := s.ListEntitlementsByUser(ctx, "u-concurrent-user")
	if err != nil {
		t.Fatalf("ListEntitlementsByUser conc failed: %v", err)
	}
	if len(concEnts) != 1 {
		t.Fatalf("expected exactly 1 entitlement in store after concurrency test, got %d", len(concEnts))
	}
	if !concEnts[0].Active {
		t.Errorf("expected the single entitlement to be active")
	}

	// 10. Purchase Requests: CreateOrGetPendingRequest idempotency and concurrency (Phase 3.3, R5)
	const prUser = "u-pr-student-1"
	const prSubj = "subj-concurrent-1"

	// Concurrency test: 20 goroutines calling CreateOrGetPendingRequest
	var prWg sync.WaitGroup
	prIDs := make([]string, 20)
	var prCallSuccessCount atomic.Int32

	for i := 0; i < 20; i++ {
		prWg.Add(1)
		idx := i
		go func() {
			defer prWg.Done()
			req, _, err := s.CreateOrGetPendingRequest(ctx, &models.PurchaseRequest{
				UserID:    prUser,
				SubjectID: prSubj,
			})
			if err != nil {
				t.Errorf("goroutine %d CreateOrGetPendingRequest failed: %v", idx, err)
				return
			}
			prIDs[idx] = req.ID
			prCallSuccessCount.Add(1)
		}()
	}
	prWg.Wait()

	if prCallSuccessCount.Load() != 20 {
		t.Fatalf("expected all 20 calls to succeed, got %d", prCallSuccessCount.Load())
	}
	// All callers get the same id
	expectedPRID := prIDs[0]
	if expectedPRID == "" {
		t.Fatal("expected non-empty PR ID")
	}
	for i, id := range prIDs {
		if id != expectedPRID {
			t.Errorf("caller %d got id %q, want %q", i, id, expectedPRID)
		}
	}

	// Exactly one pending row exists in store
	userReqs, err := s.ListRequestsByUser(ctx, prUser)
	if err != nil {
		t.Fatalf("ListRequestsByUser failed: %v", err)
	}
	if len(userReqs) != 1 {
		t.Fatalf("expected exactly 1 purchase request in store, got %d", len(userReqs))
	}
	if userReqs[0].ID != expectedPRID {
		t.Errorf("stored request ID = %q, want %q", userReqs[0].ID, expectedPRID)
	}
	if userReqs[0].Status != models.RequestStatusPending {
		t.Errorf("stored request status = %q, want %q", userReqs[0].Status, models.RequestStatusPending)
	}

	// Idempotent second call returns existing request (same id, created=false)
	secondReq, created, err := s.CreateOrGetPendingRequest(ctx, &models.PurchaseRequest{
		UserID:    prUser,
		SubjectID: prSubj,
	})
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if created {
		t.Errorf("expected created = false on second call")
	}
	if secondReq.ID != expectedPRID {
		t.Errorf("second call got ID = %q, want %q", secondReq.ID, expectedPRID)
	}

	// GetPendingRequest returns the pending request
	pending, err := s.GetPendingRequest(ctx, prUser, prSubj)
	if err != nil {
		t.Fatalf("GetPendingRequest failed: %v", err)
	}
	if pending == nil || pending.ID != expectedPRID {
		t.Fatalf("GetPendingRequest = %v, want ID %q", pending, expectedPRID)
	}

	// GetPendingRequest for nonexistent pair returns nil, nil
	nonePending, err := s.GetPendingRequest(ctx, "nonexistent-user", prSubj)
	if err != nil {
		t.Fatalf("GetPendingRequest nonexistent failed: %v", err)
	}
	if nonePending != nil {
		t.Errorf("expected nil for nonexistent user, got %v", nonePending)
	}

	// Test RecordVideoPlay and queries (Phase 3.5 fix)
	playUser := "play-user-1"
	playVid := "play-vid-1"
	playSubj := "play-subj-1"
	playNow := time.Now().UTC().Truncate(time.Millisecond)

	err = s.RecordVideoPlay(ctx, &models.VideoPlay{
		UserID:    playUser,
		VideoID:   playVid,
		SubjectID: playSubj,
		PlayedAt:  playNow,
	})
	if err != nil {
		t.Fatalf("RecordVideoPlay failed: %v", err)
	}

	// Record second play for same video by another user
	err = s.RecordVideoPlay(ctx, &models.VideoPlay{
		UserID:    "play-user-2",
		VideoID:   playVid,
		SubjectID: playSubj,
		PlayedAt:  playNow.Add(1 * time.Minute),
	})
	if err != nil {
		t.Fatalf("second RecordVideoPlay failed: %v", err)
	}

	// List by video
	byVid, err := s.ListVideoPlaysByVideo(ctx, playVid)
	if err != nil {
		t.Fatalf("ListVideoPlaysByVideo failed: %v", err)
	}
	if len(byVid) != 2 {
		t.Fatalf("expected 2 plays for video, got %d", len(byVid))
	}

	// List by user
	byUser, err := s.ListVideoPlaysByUser(ctx, playUser)
	if err != nil {
		t.Fatalf("ListVideoPlaysByUser failed: %v", err)
	}
	if len(byUser) != 1 {
		t.Fatalf("expected 1 play for user, got %d", len(byUser))
	}
	if byUser[0].VideoID != playVid || byUser[0].SubjectID != playSubj {
		t.Errorf("unexpected play record content: %+v", byUser[0])
	}

	// Admin audit log (Phase 4.1): write three entries, read newest first.
	auditBase := time.Now().UTC()
	auditEntries := []*models.AuditLog{
		{ActorID: "adm-1", ActorName: "Op", Action: "level_create", TargetType: "level", TargetID: "diploma-x", CreatedAt: auditBase.Add(-2 * time.Hour)},
		{ActorID: "adm-1", ActorName: "Op", Action: "subject_publish", TargetType: "subject", TargetID: "subj-1", CreatedAt: auditBase.Add(-1 * time.Hour)},
		{ActorID: "adm-1", ActorName: "Op", Action: "video_delete", TargetType: "video", TargetID: "vid-1", CreatedAt: auditBase},
	}
	for i, e := range auditEntries {
		if err := s.CreateAuditLog(ctx, e); err != nil {
			t.Fatalf("CreateAuditLog %d failed: %v", i, err)
		}
		if e.ID == "" {
			t.Fatalf("CreateAuditLog %d did not assign an id", i)
		}
		if e.CreatedAt.IsZero() {
			t.Fatalf("CreateAuditLog %d did not assign created_at", i)
		}
	}

	logs, total, err := s.ListAuditLogs(ctx, 1, 2)
	if err != nil {
		t.Fatalf("ListAuditLogs failed: %v", err)
	}
	if total != 3 || len(logs) != 2 {
		t.Fatalf("expected total=3 items=2, got total=%d items=%d", total, len(logs))
	}
	if logs[0].Action != "video_delete" || logs[1].Action != "subject_publish" {
		t.Fatalf("expected newest-first order, got %s then %s", logs[0].Action, logs[1].Action)
	}

	logsPage2, _, err := s.ListAuditLogs(ctx, 2, 2)
	if err != nil {
		t.Fatalf("ListAuditLogs page 2 failed: %v", err)
	}
	if len(logsPage2) != 1 || logsPage2[0].Action != "level_create" {
		t.Fatalf("expected page 2 to hold level_create, got %+v", logsPage2)
	}

	// Level admin CRUD (Phase 4.2).
	dip := &models.Level{
		Key:       "diploma-test-1",
		StudyType: models.StudyTypeDiploma,
		TitleAr:   "دبلومة اختبار",
		Position:  6,
		Published: false,
	}
	if err := s.CreateLevel(ctx, dip); err != nil {
		t.Fatalf("CreateLevel failed: %v", err)
	}
	if err := s.CreateLevel(ctx, dip); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate on second CreateLevel, got %v", err)
	}
	gotDip, err := s.GetLevelByKey(ctx, "diploma-test-1")
	if err != nil || gotDip == nil {
		t.Fatalf("GetLevelByKey failed: %v", err)
	}
	if gotDip.Published {
		t.Fatalf("expected new diploma unpublished, got %+v", gotDip)
	}
	missingLvl, err := s.GetLevelByKey(ctx, "diploma-missing")
	if err != nil || missingLvl != nil {
		t.Fatalf("expected nil for missing level, got %+v err=%v", missingLvl, err)
	}

	gotDip.TitleAr = "دبلومة محدثة"
	gotDip.Published = true
	if err := s.UpdateLevel(ctx, gotDip); err != nil {
		t.Fatalf("UpdateLevel failed: %v", err)
	}
	afterUpdate, err := s.GetLevelByKey(ctx, "diploma-test-1")
	if err != nil || afterUpdate == nil {
		t.Fatalf("GetLevelByKey after update failed: %v", err)
	}
	if afterUpdate.TitleAr != "دبلومة محدثة" || !afterUpdate.Published {
		t.Fatalf("update not applied: %+v", afterUpdate)
	}
	if err := s.UpdateLevel(ctx, &models.Level{Key: "diploma-missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating missing level, got %v", err)
	}

	if n, err := s.CountSubjectsByLevel(ctx, "diploma-test-1", ""); err != nil || n != 0 {
		t.Fatalf("expected 0 subjects in new diploma, got n=%d err=%v", n, err)
	}
	dipSubj := &models.Subject{
		ID:              "subj-dip-count-1",
		LevelKey:        "diploma-test-1",
		Term:            "first",
		TitleAr:         "مادة دبلومة",
		Status:          models.StatusDraft,
		AccessExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := s.CreateSubject(ctx, dipSubj); err != nil {
		t.Fatalf("CreateSubject in diploma failed: %v", err)
	}
	if n, err := s.CountSubjectsByLevel(ctx, "diploma-test-1", ""); err != nil || n != 1 {
		t.Fatalf("expected 1 subject in diploma, got n=%d err=%v", n, err)
	}
	if n, err := s.CountSubjectsByLevel(ctx, "diploma-test-1", models.StatusPublished); err != nil || n != 0 {
		t.Fatalf("expected 0 published subjects in diploma, got n=%d err=%v", n, err)
	}

	if err := s.DeleteLevel(ctx, "diploma-test-1"); err != nil {
		t.Fatalf("DeleteLevel failed: %v", err)
	}
	if err := s.DeleteLevel(ctx, "diploma-test-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting missing level, got %v", err)
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
