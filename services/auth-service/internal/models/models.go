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

// User is a registered account.
type User struct {
	ID                  string    `json:"id" bson:"_id"`
	Email               string    `json:"email" bson:"email"`
	PasswordHash        string    `json:"-" bson:"password_hash"`
	Role                Role      `json:"role" bson:"role"`
	EmailVerified       bool      `json:"email_verified" bson:"email_verified"`
	OTPHash             string    `json:"-" bson:"otp_hash,omitempty"`
	OTPExpiresAt        time.Time `json:"-" bson:"otp_expires_at,omitempty"`
	ResetTokenHash      string    `json:"-" bson:"reset_token_hash,omitempty"`
	ResetTokenExpiresAt time.Time `json:"-" bson:"reset_token_expires_at,omitempty"`
	CreatedAt           time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt           time.Time `json:"updated_at" bson:"updated_at"`
}
