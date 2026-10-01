package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
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
