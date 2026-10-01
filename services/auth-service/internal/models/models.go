// Package models defines the auth-service domain types.
// Single role: user. There are no admin/role screens; authorization is
// identity only.
package models

import "time"

// Role is the authorization level of an account.
type Role string

const (
	RoleUser Role = "user"
)

// AllowedRoles is the configurable set of valid roles.
var AllowedRoles = map[Role]bool{
	RoleUser: true,
}

// ValidRole reports whether r is in the configured role set.
func ValidRole(r Role) bool {
	return AllowedRoles[r]
}

// UserStatus represents the lifecycle state of a user account.
type UserStatus string

const (
	StatusActive    UserStatus = "active"
	StatusSuspended UserStatus = "suspended"
	StatusDeleted   UserStatus = "deleted"
)

// User is a registered account.
type User struct {
	ID                  string     `json:"id" bson:"_id"`
	Email               string     `json:"email" bson:"email"`
	PasswordHash        string     `json:"-" bson:"password_hash"`
	Role                Role       `json:"role" bson:"role"`
	EmailVerified       bool       `json:"email_verified" bson:"email_verified"`
	FullName            string     `json:"full_name,omitempty" bson:"full_name,omitempty"`
	Phone               string     `json:"phone,omitempty" bson:"phone,omitempty"`
	Status              UserStatus `json:"status,omitempty" bson:"status,omitempty"`
	StatusReason        string     `json:"status_reason,omitempty" bson:"status_reason,omitempty"`
	SuspendedAt         time.Time  `json:"suspended_at,omitempty" bson:"suspended_at,omitempty"`
	ReactivatedAt       time.Time  `json:"reactivated_at,omitempty" bson:"reactivated_at,omitempty"`
	DeletedAt           time.Time  `json:"deleted_at,omitempty" bson:"deleted_at,omitempty"`
	OTPHash             string     `json:"-" bson:"otp_hash,omitempty"`
	OTPExpiresAt        time.Time  `json:"-" bson:"otp_expires_at,omitempty"`
	ResetTokenHash      string     `json:"-" bson:"reset_token_hash,omitempty"`
	ResetTokenExpiresAt time.Time  `json:"-" bson:"reset_token_expires_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at" bson:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at" bson:"updated_at"`
}

// EffectiveStatus returns the account's active status, treating empty as active.
func (u *User) EffectiveStatus() UserStatus {
	if u == nil || u.Status == "" {
		return StatusActive
	}
	return u.Status
}

// BlocklistEntry represents a blocked email or phone hash.
type BlocklistEntry struct {
	Kind      string    `json:"kind" bson:"kind"`
	Hash      string    `json:"hash" bson:"hash"`
	Reason    string    `json:"reason,omitempty" bson:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
}

// Admin represents an administrative operator identity (not an account).
type Admin struct {
	ID        string    `json:"id" bson:"_id"`
	Name      string    `json:"name" bson:"name"`
	TokenHash string    `json:"-" bson:"token_hash"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
	ExpiresAt time.Time `json:"expires_at" bson:"expires_at"`
	RevokedAt time.Time `json:"revoked_at,omitempty" bson:"revoked_at,omitempty"`
}

// IsActive reports whether the admin token is currently valid (unrevoked and unexpired).
func (a *Admin) IsActive(now time.Time) bool {
	if a == nil {
		return false
	}
	if !a.RevokedAt.IsZero() {
		return false
	}
	return a.ExpiresAt.After(now)
}
