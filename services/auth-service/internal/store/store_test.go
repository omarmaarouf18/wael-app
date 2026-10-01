package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func runUserStoreSuite(t *testing.T, s Store) {
	ctx := context.Background()

	// 1. Create a user
	u1 := &models.User{
		ID:           "u-1",
		Email:        "a@example.com",
		PasswordHash: "hash1",
		Role:         models.RoleUser,
	}
	if err := s.Create(ctx, u1); err != nil {
		t.Fatalf("Create u1: %v", err)
	}

	// 2. Duplicate email on Create errors
	uDup := &models.User{
		ID:           "u-dup",
		Email:        "a@example.com",
		PasswordHash: "hash-dup",
		Role:         models.RoleUser,
	}
	if err := s.Create(ctx, uDup); err == nil {
		t.Fatal("expected duplicate email error on Create, got nil")
	}

	// 3. FindByEmail on existing record
	gotByMail, err := s.FindByEmail(ctx, "a@example.com")
	if err != nil || gotByMail == nil || gotByMail.ID != "u-1" {
		t.Fatalf("FindByEmail(a@example.com) = %+v, %v", gotByMail, err)
	}

	// 4. FindBy* on a missing record returns (nil, nil)
	missingMail, err := s.FindByEmail(ctx, "missing@example.com")
	if err != nil {
		t.Fatalf("FindByEmail missing: expected nil error, got %v", err)
	}
	if missingMail != nil {
		t.Fatalf("FindByEmail missing: expected nil user, got %+v", missingMail)
	}

	missingID, err := s.FindByID(ctx, "missing-id")
	if err != nil {
		t.Fatalf("FindByID missing: expected nil error, got %v", err)
	}
	if missingID != nil {
		t.Fatalf("FindByID missing: expected nil user, got %+v", missingID)
	}

	// 5. FindByID on existing record
	gotByID, err := s.FindByID(ctx, "u-1")
	if err != nil || gotByID == nil || gotByID.Email != "a@example.com" {
		t.Fatalf("FindByID(u-1) = %+v, %v", gotByID, err)
	}

	// 6. Update user
	gotByID.PasswordHash = "new-hash"
	if err := s.Update(ctx, gotByID); err != nil {
		t.Fatalf("Update u1: %v", err)
	}
	updated, err := s.FindByID(ctx, "u-1")
	if err != nil || updated == nil || updated.PasswordHash != "new-hash" {
		t.Fatalf("FindByID after update = %+v, %v", updated, err)
	}

	// 7. Update on a missing id errors
	missingUpdateUser := &models.User{
		ID:           "non-existent-user-id",
		Email:        "ghost@example.com",
		PasswordHash: "ghost-hash",
		Role:         models.RoleUser,
	}
	if err := s.Update(ctx, missingUpdateUser); err == nil {
		t.Fatal("expected error on Update for missing id, got nil")
	}

	// 8. Duplicate email on Update errors
	u2 := &models.User{
		ID:           "u-2",
		Email:        "b@example.com",
		PasswordHash: "hash2",
		Role:         models.RoleUser,
	}
	if err := s.Create(ctx, u2); err != nil {
		t.Fatalf("Create u2: %v", err)
	}
	u2.Email = "a@example.com" // already taken by u1
	if err := s.Update(ctx, u2); err == nil {
		t.Fatal("expected duplicate email error on Update, got nil")
	}

	// 9. Count
	n, err := s.Count(ctx)
	if err != nil || n != 2 {
		t.Fatalf("Count: expected 2, got %d (err: %v)", n, err)
	}

	// 10. EffectiveStatus on a legacy doc without status
	uLegacy := &models.User{
		ID:           "u-legacy",
		Email:        "legacy@example.com",
		PasswordHash: "hash-legacy",
		Role:         models.RoleUser,
		// Status is deliberately omitted (empty)
	}
	if err := s.Create(ctx, uLegacy); err != nil {
		t.Fatalf("Create uLegacy: %v", err)
	}
	gotLegacy, err := s.FindByID(ctx, "u-legacy")
	if err != nil || gotLegacy == nil {
		t.Fatalf("FindByID uLegacy: %v", err)
	}
	if gotLegacy.Status != "" {
		t.Fatalf("expected empty status on legacy doc, got %q", gotLegacy.Status)
	}
	if gotLegacy.EffectiveStatus() != models.StatusActive {
		t.Fatalf("expected EffectiveStatus active for legacy doc, got %q", gotLegacy.EffectiveStatus())
	}
	// Transition legacy doc (empty status) from active to suspended via SetStatus
	legacySuspendAt := time.Now().Truncate(time.Millisecond)
	if err := s.SetStatus(ctx, "u-legacy", string(models.StatusActive), string(models.StatusSuspended), "legacy migration suspend", legacySuspendAt); err != nil {
		t.Fatalf("SetStatus legacy from active to suspended: %v", err)
	}
	afterLegacySuspend, err := s.FindByID(ctx, "u-legacy")
	if err != nil || afterLegacySuspend == nil {
		t.Fatalf("FindByID after legacy suspend: %v", err)
	}
	if afterLegacySuspend.EffectiveStatus() != models.StatusSuspended {
		t.Fatalf("expected suspended, got %q", afterLegacySuspend.EffectiveStatus())
	}
	if afterLegacySuspend.StatusReason != "legacy migration suspend" {
		t.Fatalf("expected reason %q, got %q", "legacy migration suspend", afterLegacySuspend.StatusReason)
	}
	if !afterLegacySuspend.SuspendedAt.Equal(legacySuspendAt) {
		t.Fatalf("expected SuspendedAt %v, got %v", legacySuspendAt, afterLegacySuspend.SuspendedAt)
	}

	// 11. Stale-copy test (finding P-1: Update must never overwrite status fields)
	uStale := &models.User{
		ID:           "u-stale",
		Email:        "stale@example.com",
		PasswordHash: "stale-pass-1",
		Role:         models.RoleUser,
		Status:       models.StatusActive,
	}
	if err := s.Create(ctx, uStale); err != nil {
		t.Fatalf("Create uStale: %v", err)
	}
	staleCopy, err := s.FindByID(ctx, "u-stale")
	if err != nil || staleCopy == nil {
		t.Fatalf("FindByID stale: %v", err)
	}
	// Admin suspends the user
	suspendTime := time.Now().Truncate(time.Millisecond)
	if err := s.SetStatus(ctx, "u-stale", string(models.StatusActive), string(models.StatusSuspended), "fraud investigation", suspendTime); err != nil {
		t.Fatalf("SetStatus suspend: %v", err)
	}
	// Verify user is suspended in store
	suspendedUser, err := s.FindByID(ctx, "u-stale")
	if err != nil || suspendedUser.EffectiveStatus() != models.StatusSuspended {
		t.Fatalf("expected suspended user, got %+v (err: %v)", suspendedUser, err)
	}
	// Handler with stale copy (where Status is still active) calls Update
	staleCopy.PasswordHash = "new-stale-pass-2"
	if err := s.Update(ctx, staleCopy); err != nil {
		t.Fatalf("Update stale copy: %v", err)
	}
	// Status in store must REMAIN suspended (P-1 fix)
	afterStaleUpdate, err := s.FindByID(ctx, "u-stale")
	if err != nil || afterStaleUpdate == nil {
		t.Fatalf("FindByID after stale update: %v", err)
	}
	if afterStaleUpdate.PasswordHash != "new-stale-pass-2" {
		t.Fatalf("expected password_hash updated to new-stale-pass-2, got %q", afterStaleUpdate.PasswordHash)
	}
	if afterStaleUpdate.EffectiveStatus() != models.StatusSuspended {
		t.Fatalf("P-1 lost update bug! Status was overwritten to %q, expected suspended", afterStaleUpdate.EffectiveStatus())
	}
	if afterStaleUpdate.StatusReason != "fraud investigation" {
		t.Fatalf("expected StatusReason %q, got %q", "fraud investigation", afterStaleUpdate.StatusReason)
	}
	if !afterStaleUpdate.SuspendedAt.Equal(suspendTime) {
		t.Fatalf("expected SuspendedAt %v, got %v", suspendTime, afterStaleUpdate.SuspendedAt)
	}

	// 12. SetStatus CAS conflict
	// Current status is suspended; attempting to transition from active to deleted must fail
	conflictErr := s.SetStatus(ctx, "u-stale", string(models.StatusActive), string(models.StatusDeleted), "cannot delete active", time.Now())
	if conflictErr == nil {
		t.Fatal("expected error on SetStatus CAS conflict, got nil")
	}
	if !errors.Is(conflictErr, ErrStatusConflict) {
		t.Fatalf("expected ErrStatusConflict, got %v", conflictErr)
	}
	// Verify status remains suspended
	verifyConflict, err := s.FindByID(ctx, "u-stale")
	if err != nil || verifyConflict.EffectiveStatus() != models.StatusSuspended {
		t.Fatalf("expected user to remain suspended after conflict, got %+v (err: %v)", verifyConflict, err)
	}

	// 13. SetStatus on missing id
	missingIDErr := s.SetStatus(ctx, "non-existent-user-id", string(models.StatusActive), string(models.StatusSuspended), "test", time.Now())
	if missingIDErr == nil {
		t.Fatal("expected error on SetStatus for missing id, got nil")
	}
	if errors.Is(missingIDErr, ErrStatusConflict) {
		t.Fatal("expected non-conflict error for missing id, got ErrStatusConflict")
	}
	if !errors.Is(missingIDErr, ErrUserNotFound) && missingIDErr.Error() != "store: user not found" {
		t.Fatalf("expected user not found error for missing id, got %v", missingIDErr)
	}

	// 14. Additional transitions: reactivate (suspended -> active) and delete (active -> deleted)
	reactivateTime := time.Now().Truncate(time.Millisecond)
	if err := s.SetStatus(ctx, "u-stale", string(models.StatusSuspended), string(models.StatusActive), "reinstated", reactivateTime); err != nil {
		t.Fatalf("SetStatus reactivate: %v", err)
	}
	reactivatedUser, err := s.FindByID(ctx, "u-stale")
	if err != nil || reactivatedUser.EffectiveStatus() != models.StatusActive {
		t.Fatalf("expected active user, got %+v", reactivatedUser)
	}
	if !reactivatedUser.ReactivatedAt.Equal(reactivateTime) {
		t.Fatalf("expected ReactivatedAt %v, got %v", reactivateTime, reactivatedUser.ReactivatedAt)
	}

	deleteTime := time.Now().Truncate(time.Millisecond)
	if err := s.SetStatus(ctx, "u-stale", string(models.StatusActive), string(models.StatusDeleted), "account deleted", deleteTime); err != nil {
		t.Fatalf("SetStatus delete: %v", err)
	}
	deletedUser, err := s.FindByID(ctx, "u-stale")
	if err != nil || deletedUser.EffectiveStatus() != models.StatusDeleted {
		t.Fatalf("expected deleted user, got %+v", deletedUser)
	}
	if !deletedUser.DeletedAt.Equal(deleteTime) {
		t.Fatalf("expected DeletedAt %v, got %v", deleteTime, deletedUser.DeletedAt)
	}
}

func TestMemoryStore_CRUD(t *testing.T) {
	s := NewMemoryStore()
	runUserStoreSuite(t, s)
}

func TestMongoStore_CRUD(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_auth_store")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = s.coll.Database().Drop(dropCtx)
	})

	runUserStoreSuite(t, s)
}

func TestMongoStore_RawLegacyDocWithoutStatusField(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_legacy_store")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = s.coll.Database().Drop(dropCtx)
	})

	// Raw insert without status field in BSON
	_, err = s.coll.InsertOne(ctx, bson.M{
		"_id":           "raw-legacy-id",
		"email":         "rawlegacy@example.com",
		"password_hash": "hash",
		"role":          "user",
		"created_at":    time.Now(),
		"updated_at":    time.Now(),
	})
	if err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	u, err := s.FindByID(ctx, "raw-legacy-id")
	if err != nil || u == nil {
		t.Fatalf("FindByID raw legacy: %v", err)
	}
	if u.EffectiveStatus() != models.StatusActive {
		t.Fatalf("expected EffectiveStatus active, got %q", u.EffectiveStatus())
	}

	// CAS from active to suspended on document where status field does not exist in DB
	suspendTime := time.Now().Truncate(time.Millisecond)
	if err := s.SetStatus(ctx, "raw-legacy-id", string(models.StatusActive), string(models.StatusSuspended), "raw legacy test", suspendTime); err != nil {
		t.Fatalf("SetStatus on raw legacy doc: %v", err)
	}

	uAfter, err := s.FindByID(ctx, "raw-legacy-id")
	if err != nil || uAfter == nil {
		t.Fatalf("FindByID after suspend: %v", err)
	}
	if uAfter.EffectiveStatus() != models.StatusSuspended {
		t.Fatalf("expected suspended, got %q", uAfter.EffectiveStatus())
	}
}
