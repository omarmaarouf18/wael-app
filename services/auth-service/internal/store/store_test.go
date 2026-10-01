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
	if u1.Status != models.StatusActive {
		t.Fatalf("expected Status %q after Create, got %q", models.StatusActive, u1.Status)
	}
	gotCreated, err := s.FindByID(ctx, "u-1")
	if err != nil || gotCreated == nil || gotCreated.Status != models.StatusActive {
		t.Fatalf("expected stored Status %q, got %+v (err: %v)", models.StatusActive, gotCreated, err)
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

	// 10. EffectiveStatus on newly created doc (Create stores active explicitly)
	uLegacy := &models.User{
		ID:           "u-legacy",
		Email:        "legacy@example.com",
		PasswordHash: "hash-legacy",
		Role:         models.RoleUser,
		// Status is omitted (empty)
	}
	if err := s.Create(ctx, uLegacy); err != nil {
		t.Fatalf("Create uLegacy: %v", err)
	}
	gotLegacy, err := s.FindByID(ctx, "u-legacy")
	if err != nil || gotLegacy == nil {
		t.Fatalf("FindByID uLegacy: %v", err)
	}
	if gotLegacy.Status != models.StatusActive {
		t.Fatalf("expected active status on created doc, got %q", gotLegacy.Status)
	}
	if gotLegacy.EffectiveStatus() != models.StatusActive {
		t.Fatalf("expected EffectiveStatus active for doc, got %q", gotLegacy.EffectiveStatus())
	}

	// For MemoryStore, also test an unmigrated legacy doc with raw empty status field
	if memStore, ok := s.(*MemoryStore); ok {
		memStore.mu.Lock()
		uRawLegacy := &models.User{
			ID:           "u-raw-legacy",
			Email:        "rawlegacy-mem@example.com",
			PasswordHash: "hash-raw-legacy",
			Role:         models.RoleUser,
		}
		memStore.byID[uRawLegacy.ID] = uRawLegacy
		memStore.byMail[uRawLegacy.Email] = uRawLegacy
		memStore.mu.Unlock()

		gotRaw, err := s.FindByID(ctx, "u-raw-legacy")
		if err != nil || gotRaw == nil {
			t.Fatalf("FindByID raw legacy: %v", err)
		}
		if gotRaw.Status != "" {
			t.Fatalf("expected empty status on raw legacy doc, got %q", gotRaw.Status)
		}
		if gotRaw.EffectiveStatus() != models.StatusActive {
			t.Fatalf("expected EffectiveStatus active for raw legacy doc, got %q", gotRaw.EffectiveStatus())
		}
		if err := s.SetStatus(ctx, "u-raw-legacy", string(models.StatusActive), string(models.StatusSuspended), "raw legacy suspend", time.Now()); err != nil {
			t.Fatalf("SetStatus raw legacy: %v", err)
		}
	}

	// Transition legacy doc from active to suspended via SetStatus
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
	if !errors.Is(missingIDErr, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound for missing id, got %v", missingIDErr)
	}

	// 14. SetStatus invalid target status returns ErrInvalidStatus in both stores
	invalidTargetErr := s.SetStatus(ctx, "u-stale", string(models.StatusSuspended), "invalid-status-xyz", "test", time.Now())
	if !errors.Is(invalidTargetErr, ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus for invalid status, got %v", invalidTargetErr)
	}

	// 15. Additional transitions: reactivate (suspended -> active) and delete (active -> deleted)
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

	// 16. Phone unique partial index (P-6):
	// - two users with empty phone do not collide
	uEmpty1 := &models.User{
		ID:           "u-empty-1",
		Email:        "empty1@example.com",
		PasswordHash: "h1",
		Role:         models.RoleUser,
		Phone:        "",
	}
	if err := s.Create(ctx, uEmpty1); err != nil {
		t.Fatalf("Create user with empty phone 1: %v", err)
	}
	uEmpty2 := &models.User{
		ID:           "u-empty-2",
		Email:        "empty2@example.com",
		PasswordHash: "h2",
		Role:         models.RoleUser,
		Phone:        "",
	}
	if err := s.Create(ctx, uEmpty2); err != nil {
		t.Fatalf("Create user with empty phone 2: %v", err)
	}

	// - duplicate phone between two active users is refused
	uPhone1 := &models.User{
		ID:           "u-phone-1",
		Email:        "phone1@example.com",
		PasswordHash: "h1",
		Role:         models.RoleUser,
		Phone:        "+201012345678",
	}
	if err := s.Create(ctx, uPhone1); err != nil {
		t.Fatalf("Create user with phone 1: %v", err)
	}
	uPhone2 := &models.User{
		ID:           "u-phone-2",
		Email:        "phone2@example.com",
		PasswordHash: "h2",
		Role:         models.RoleUser,
		Phone:        "+201012345678",
	}
	if err := s.Create(ctx, uPhone2); err == nil {
		t.Fatal("expected duplicate phone error on Create between two active users, got nil")
	}

	// - a deleted user's phone can be reused
	if err := s.SetStatus(ctx, "u-phone-1", string(models.StatusActive), string(models.StatusDeleted), "deleted user", time.Now()); err != nil {
		t.Fatalf("SetStatus delete u-phone-1: %v", err)
	}
	if err := s.Create(ctx, uPhone2); err != nil {
		t.Fatalf("expected creating uPhone2 to succeed after uPhone1 was deleted, got: %v", err)
	}

	// 17. Blocklist
	blocked, err := s.IsBlocked(ctx, "email", "hash-unblocked-identity")
	if err != nil {
		t.Fatalf("IsBlocked unblocked: %v", err)
	}
	if blocked {
		t.Fatal("expected unblocked identity to return false")
	}
	if err := s.AddToBlocklist(ctx, "email", "hash-blocked-identity", "banned for abuse", time.Now()); err != nil {
		t.Fatalf("AddToBlocklist: %v", err)
	}
	blocked, err = s.IsBlocked(ctx, "email", "hash-blocked-identity")
	if err != nil {
		t.Fatalf("IsBlocked after add: %v", err)
	}
	if !blocked {
		t.Fatal("expected blocked identity to return true")
	}
	// Idempotent add: same kind and hash does not error
	if err := s.AddToBlocklist(ctx, "email", "hash-blocked-identity", "duplicate ban", time.Now()); err != nil {
		t.Fatalf("AddToBlocklist duplicate: %v", err)
	}
}

func runAdminStoreSuite(t *testing.T, s Store) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// 1. Create an admin
	a1 := &models.Admin{
		ID:        "adm-1",
		Name:      "Operator Alice",
		TokenHash: "hash-token-1",
		CreatedAt: now,
		ExpiresAt: now.Add(90 * 24 * time.Hour),
	}
	if err := s.CreateAdmin(ctx, a1); err != nil {
		t.Fatalf("CreateAdmin a1: %v", err)
	}

	// 2. Duplicate token_hash errors
	aDupToken := &models.Admin{
		ID:        "adm-2",
		Name:      "Operator Bob",
		TokenHash: "hash-token-1",
		CreatedAt: now,
		ExpiresAt: now.Add(90 * 24 * time.Hour),
	}
	if err := s.CreateAdmin(ctx, aDupToken); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate for duplicate token_hash, got %v", err)
	}

	// 3. Duplicate ID errors
	aDupID := &models.Admin{
		ID:        "adm-1",
		Name:      "Operator Charlie",
		TokenHash: "hash-token-2",
		CreatedAt: now,
		ExpiresAt: now.Add(90 * 24 * time.Hour),
	}
	if err := s.CreateAdmin(ctx, aDupID); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate for duplicate ID, got %v", err)
	}

	// 4. FindAdminByTokenHash
	gotByHash, err := s.FindAdminByTokenHash(ctx, "hash-token-1")
	if err != nil || gotByHash == nil || gotByHash.ID != "adm-1" || gotByHash.Name != "Operator Alice" {
		t.Fatalf("FindAdminByTokenHash(hash-token-1) = %+v, %v", gotByHash, err)
	}
	missingByHash, err := s.FindAdminByTokenHash(ctx, "nonexistent-hash")
	if err != nil || missingByHash != nil {
		t.Fatalf("expected nil, nil for missing token_hash, got %+v, %v", missingByHash, err)
	}

	// 5. FindAdminByID
	gotByID, err := s.FindAdminByID(ctx, "adm-1")
	if err != nil || gotByID == nil || gotByID.Name != "Operator Alice" {
		t.Fatalf("FindAdminByID(adm-1) = %+v, %v", gotByID, err)
	}
	missingByID, err := s.FindAdminByID(ctx, "nonexistent-id")
	if err != nil || missingByID != nil {
		t.Fatalf("expected nil, nil for missing ID, got %+v, %v", missingByID, err)
	}

	// 6. RevokeAdmin
	revokeTime := now.Add(time.Hour)
	if err := s.RevokeAdmin(ctx, "adm-1", revokeTime); err != nil {
		t.Fatalf("RevokeAdmin(adm-1): %v", err)
	}
	revoked, err := s.FindAdminByID(ctx, "adm-1")
	if err != nil || revoked == nil || revoked.RevokedAt.IsZero() {
		t.Fatalf("expected admin to have RevokedAt set, got %+v, %v", revoked, err)
	}
	if revoked.IsActive(now) {
		t.Fatalf("revoked admin must not be active")
	}

	// 7. RevokeAdmin is idempotent
	if err := s.RevokeAdmin(ctx, "adm-1", revokeTime.Add(time.Minute)); err != nil {
		t.Fatalf("second RevokeAdmin call must succeed idempotently: %v", err)
	}

	// 8. RevokeAdmin on missing admin returns ErrAdminNotFound
	if err := s.RevokeAdmin(ctx, "missing-adm", revokeTime); !errors.Is(err, ErrAdminNotFound) {
		t.Fatalf("expected ErrAdminNotFound for missing admin, got %v", err)
	}
}

func runPhase15StoreSuite(t *testing.T, s Store) {
	ctx := context.Background()

	// 1. Audit log create and list (newest first, pagination)
	t0 := time.Now().Add(-2 * time.Hour).Truncate(time.Millisecond)
	t1 := time.Now().Add(-1 * time.Hour).Truncate(time.Millisecond)
	t2 := time.Now().Truncate(time.Millisecond)

	log1 := &models.AuditLog{
		ActorID:    "adm-1",
		ActorName:  "Admin One",
		Action:     "account_suspend",
		TargetType: "user",
		TargetID:   "u-target-1",
		Detail:     "violating terms",
		CreatedAt:  t0,
	}
	log2 := &models.AuditLog{
		ActorID:    "adm-1",
		ActorName:  "Admin One",
		Action:     "account_reactivate",
		TargetType: "user",
		TargetID:   "u-target-1",
		Detail:     "resolved",
		CreatedAt:  t1,
	}
	log3 := &models.AuditLog{
		ActorID:    "adm-2",
		ActorName:  "Admin Two",
		Action:     "account_delete",
		TargetType: "user",
		TargetID:   "u-target-2",
		Detail:     "requested by user",
		CreatedAt:  t2,
	}

	for _, l := range []*models.AuditLog{log1, log2, log3} {
		if err := s.CreateAuditLog(ctx, l); err != nil {
			t.Fatalf("CreateAuditLog: %v", err)
		}
		if l.ID == "" {
			t.Fatalf("expected CreateAuditLog to assign an ID")
		}
	}

	// List audit logs: newest first -> log3, log2, log1
	logs, total, err := s.ListAuditLogs(ctx, 1, 2)
	if err != nil {
		t.Fatalf("ListAuditLogs page 1 limit 2: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3 audit logs, got %d", total)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 items on page 1, got %d", len(logs))
	}
	if logs[0].Action != "account_delete" || logs[1].Action != "account_reactivate" {
		t.Fatalf("expected newest first order, got %s then %s", logs[0].Action, logs[1].Action)
	}

	logsPage2, total2, err := s.ListAuditLogs(ctx, 2, 2)
	if err != nil {
		t.Fatalf("ListAuditLogs page 2 limit 2: %v", err)
	}
	if total2 != 3 || len(logsPage2) != 1 || logsPage2[0].Action != "account_suspend" {
		t.Fatalf("unexpected page 2 results: total=%d, len=%d, action=%s", total2, len(logsPage2), logsPage2[0].Action)
	}

	// 2. ListUsers with status and search filters
	now := time.Now().Truncate(time.Millisecond)
	u1 := &models.User{
		ID:           "u-search-1",
		FullName:     "Hassan Mohamed",
		Email:        "hassan@example.com",
		Phone:        "+201011112222",
		PasswordHash: "h1",
		Role:         models.RoleUser,
		Status:       models.StatusActive,
		CreatedAt:    now.Add(-3 * time.Minute),
	}
	u2 := &models.User{
		ID:           "u-search-2",
		FullName:     "Special .* Name",
		Email:        "regex@example.com",
		Phone:        "+201033334444",
		PasswordHash: "h2",
		Role:         models.RoleUser,
		Status:       models.StatusSuspended,
		CreatedAt:    now.Add(-2 * time.Minute),
	}
	u3 := &models.User{
		ID:           "u-search-3",
		FullName:     "Deleted Person",
		Email:        "deleted@example.com",
		Phone:        "+201055556666",
		PasswordHash: "h3",
		Role:         models.RoleUser,
		Status:       models.StatusDeleted,
		CreatedAt:    now.Add(-1 * time.Minute),
	}
	for _, u := range []*models.User{u1, u2, u3} {
		if err := s.Create(ctx, u); err != nil {
			t.Fatalf("Create user %s: %v", u.ID, err)
		}
		// In Mongo, Create sets status to active if unset, but u2 is suspended and u3 is deleted; ensure status persisted
		if u.Status != models.StatusActive {
			_ = s.SetStatus(ctx, u.ID, string(models.StatusActive), string(u.Status), "init", now)
		}
	}

	// Status filter
	activeList, totalActive, err := s.ListUsers(ctx, UserFilter{Status: "active"})
	if err != nil {
		t.Fatalf("ListUsers active: %v", err)
	}
	if totalActive < 1 {
		t.Fatalf("expected at least 1 active user, got %d", totalActive)
	}
	for _, u := range activeList {
		if u.EffectiveStatus() != models.StatusActive {
			t.Errorf("expected active user, got %s (%s)", u.ID, u.EffectiveStatus())
		}
	}

	// Regex escaping check: Search ".*" must match ONLY u2 literally, NOT all users
	regexList, totalRegex, err := s.ListUsers(ctx, UserFilter{Search: ".*"})
	if err != nil {
		t.Fatalf("ListUsers regex escaping: %v", err)
	}
	if totalRegex != 1 || len(regexList) != 1 || regexList[0].ID != "u-search-2" {
		t.Fatalf("expected exact literal match for '.*' (u-search-2), got total=%d, len=%d", totalRegex, len(regexList))
	}

	// Search by normalized phone
	phoneList, totalPhone, err := s.ListUsers(ctx, UserFilter{Search: "01011112222", NormalizedPhone: "+201011112222"})
	if err != nil {
		t.Fatalf("ListUsers phone: %v", err)
	}
	if totalPhone != 1 || len(phoneList) != 1 || phoneList[0].ID != "u-search-1" {
		t.Fatalf("expected match by normalized phone for u-search-1, got %d matches", totalPhone)
	}

	// Search by ID exact
	idList, totalID, err := s.ListUsers(ctx, UserFilter{Search: "u-search-1"})
	if err != nil {
		t.Fatalf("ListUsers by id: %v", err)
	}
	if totalID != 1 || len(idList) != 1 || idList[0].ID != "u-search-1" {
		t.Fatalf("expected match by id for u-search-1, got %d matches", totalID)
	}

	// 3. SetStatus CAS active|suspended -> deleted
	if err := s.SetStatus(ctx, "u-search-1", "active|suspended", string(models.StatusDeleted), "deleted CAS test", now); err != nil {
		t.Fatalf("SetStatus active->deleted failed: %v", err)
	}
	// Try deleting again -> must fail with ErrStatusConflict (already deleted)
	if err := s.SetStatus(ctx, "u-search-1", "active|suspended", string(models.StatusDeleted), "repeat delete", now); !errors.Is(err, ErrStatusConflict) {
		t.Fatalf("expected ErrStatusConflict when deleting already deleted user, got: %v", err)
	}
}

func TestMemoryStore_CRUD(t *testing.T) {
	s := NewMemoryStore()
	runUserStoreSuite(t, s)
	runAdminStoreSuite(t, s)
	runPhase15StoreSuite(t, s)
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
	runAdminStoreSuite(t, s)
	runPhase15StoreSuite(t, s)
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
