package store

import (
	"context"
	"testing"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
)

func TestMemoryStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	u := &models.User{ID: "u-1", Email: "a@example.com", PasswordHash: "hash", Role: models.RoleUser}
	if err := s.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &models.User{ID: "u-2", Email: "a@example.com"}); err == nil {
		t.Fatal("expected duplicate email error")
	}
	got, err := s.FindByEmail(ctx, "a@example.com")
	if err != nil || got == nil || got.ID != "u-1" {
		t.Fatalf("FindByEmail = %+v, %v", got, err)
	}
	got.Role = models.RoleUser
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	byID, err := s.FindByID(ctx, "u-1")
	if err != nil || byID.Role != models.RoleUser {
		t.Fatalf("FindByID = %+v, %v", byID, err)
	}
	n, err := s.Count(ctx)
	if err != nil || n != 1 {
		t.Fatalf("Count = %d, %v", n, err)
	}
	if missing, _ := s.FindByEmail(ctx, "nope@example.com"); missing != nil {
		t.Fatal("expected nil for unknown email")
	}
}
