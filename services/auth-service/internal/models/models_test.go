package models

import "testing"

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
