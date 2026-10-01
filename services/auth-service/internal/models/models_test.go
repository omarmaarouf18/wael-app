package models

import (
	"testing"
	"time"
)

func TestUser_EffectiveStatus(t *testing.T) {
	var nilUser *User
	if got := nilUser.EffectiveStatus(); got != StatusActive {
		t.Fatalf("nil user EffectiveStatus = %q, want %q", got, StatusActive)
	}

	uEmpty := &User{ID: "u1"}
	if got := uEmpty.EffectiveStatus(); got != StatusActive {
		t.Fatalf("empty status EffectiveStatus = %q, want %q", got, StatusActive)
	}

	uActive := &User{ID: "u2", Status: StatusActive}
	if got := uActive.EffectiveStatus(); got != StatusActive {
		t.Fatalf("active EffectiveStatus = %q, want %q", got, StatusActive)
	}

	uSuspended := &User{ID: "u3", Status: StatusSuspended}
	if got := uSuspended.EffectiveStatus(); got != StatusSuspended {
		t.Fatalf("suspended EffectiveStatus = %q, want %q", got, StatusSuspended)
	}

	uDeleted := &User{ID: "u4", Status: StatusDeleted}
	if got := uDeleted.EffectiveStatus(); got != StatusDeleted {
		t.Fatalf("deleted EffectiveStatus = %q, want %q", got, StatusDeleted)
	}
}

func TestValidRole(t *testing.T) {
	if !ValidRole(RoleUser) {
		t.Fatal("expected RoleUser to be valid")
	}
	if ValidRole("admin") {
		t.Fatal("admin role must not be a client valid role in auth-service")
	}
}

func TestAdmin_IsActive(t *testing.T) {
	now := time.Now().UTC()

	var nilAdmin *Admin
	if nilAdmin.IsActive(now) {
		t.Fatal("nil admin must not be active")
	}

	activeAdmin := &Admin{
		ID:        "a1",
		ExpiresAt: now.Add(time.Hour),
	}
	if !activeAdmin.IsActive(now) {
		t.Fatal("unrevoked admin with future expiry must be active")
	}

	expiredAdmin := &Admin{
		ID:        "a2",
		ExpiresAt: now.Add(-time.Hour),
	}
	if expiredAdmin.IsActive(now) {
		t.Fatal("expired admin must not be active")
	}

	revokedAdmin := &Admin{
		ID:        "a3",
		ExpiresAt: now.Add(time.Hour),
		RevokedAt: now.Add(-time.Minute),
	}
	if revokedAdmin.IsActive(now) {
		t.Fatal("revoked admin must not be active")
	}
}

func TestUser_ToDTO(t *testing.T) {
	var nilUser *User
	if nilUser.ToDTO() != nil {
		t.Fatal("expected nil DTO for nil user")
	}

	now := time.Now().UTC()
	u := &User{
		ID:                  "u1",
		FullName:            "Alice Test",
		Email:               "alice@example.com",
		Phone:               "+201012345678",
		PasswordHash:        "secret-hash",
		OTPHash:             "otp-hash",
		OTPExpiresAt:        now.Add(time.Minute),
		ResetTokenHash:      "reset-hash",
		ResetTokenExpiresAt: now.Add(time.Minute),
		Role:                RoleUser,
		Status:              StatusActive,
		CreatedAt:           now,
	}

	dto := u.ToDTO()
	if dto.ID != u.ID || dto.FullName != u.FullName || dto.Email != u.Email || dto.Phone != u.Phone || dto.Status != StatusActive {
		t.Fatalf("DTO fields mismatch: %+v", dto)
	}
}
