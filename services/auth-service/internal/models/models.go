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
	StatusActive          UserStatus = "active"
	StatusSuspended       UserStatus = "suspended"
	StatusDeleted         UserStatus = "deleted"
	StatusPendingDeletion UserStatus = "pending_deletion"
)

// User is a registered account.
type User struct {
	ID            string     `json:"id" bson:"_id"`
	Email         string     `json:"email" bson:"email"`
	PasswordHash  string     `json:"-" bson:"password_hash"`
	Role          Role       `json:"role" bson:"role"`
	EmailVerified bool       `json:"email_verified" bson:"email_verified"`
	FullName      string     `json:"full_name,omitempty" bson:"full_name,omitempty"`
	Phone         string     `json:"phone,omitempty" bson:"phone,omitempty"`
	Status        UserStatus `json:"status,omitempty" bson:"status,omitempty"`
	StatusReason  string     `json:"status_reason,omitempty" bson:"status_reason,omitempty"`
	SuspendedAt   time.Time  `json:"suspended_at,omitempty" bson:"suspended_at,omitempty"`
	ReactivatedAt time.Time  `json:"reactivated_at,omitempty" bson:"reactivated_at,omitempty"`
	DeletedAt     time.Time  `json:"deleted_at,omitempty" bson:"deleted_at,omitempty"`
	// NameChangedAt / PhoneChangedAt enforce the owner-decided 30-day edit
	// limit per field (F-UX2). Zero means the field was never changed.
	NameChangedAt  time.Time `json:"-" bson:"name_changed_at,omitempty"`
	PhoneChangedAt time.Time `json:"-" bson:"phone_changed_at,omitempty"`
	// DeletionRequestedAt / PurgeAfter implement the 30-day self-deletion
	// grace period (F-UX2). Set while Status is pending_deletion; cleared on
	// cancel (login) or admin suspend.
	DeletionRequestedAt time.Time `json:"-" bson:"deletion_requested_at,omitempty"`
	PurgeAfter          time.Time `json:"-" bson:"purge_after,omitempty"`
	OTPHash             string    `json:"-" bson:"otp_hash,omitempty"`
	OTPExpiresAt        time.Time `json:"-" bson:"otp_expires_at,omitempty"`
	ResetTokenHash      string    `json:"-" bson:"reset_token_hash,omitempty"`
	ResetTokenExpiresAt time.Time `json:"-" bson:"reset_token_expires_at,omitempty"`
	// PendingIDHash is the SHA-256 hash of the signup pending_id issued to an
	// unverified account. It binds OTP verification to the signup session that
	// created it, so a replacement signup rotates it and the old pending_id
	// fails. Cleared on verification.
	PendingIDHash string    `json:"-" bson:"pending_id_hash,omitempty"`
	CreatedAt     time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" bson:"updated_at"`
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

// AuditLog represents an entry in the admin_audit_log collection.
// Schema matches SPEC Section 5: _id, actor_id, actor_name, action, target_type, target_id, detail, created_at.
// No IP address is stored.
type AuditLog struct {
	ID         string    `json:"id" bson:"_id"`
	ActorID    string    `json:"actor_id" bson:"actor_id"`
	ActorName  string    `json:"actor_name" bson:"actor_name"`
	Action     string    `json:"action" bson:"action"`
	TargetType string    `json:"target_type" bson:"target_type"`
	TargetID   string    `json:"target_id" bson:"target_id"`
	Detail     string    `json:"detail,omitempty" bson:"detail,omitempty"`
	CreatedAt  time.Time `json:"created_at" bson:"created_at"`
}

// AccountEvent is one entry in the account_events collection (SPEC Section 5,
// F-UX2). It records that a self-service account change happened, without any
// PII values: only the user id, the event type, and the timestamp.
type AccountEvent struct {
	ID        string    `json:"id" bson:"_id"`
	UserID    string    `json:"user_id" bson:"user_id"`
	Type      string    `json:"type" bson:"type"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
}

// Account event types written by the self-service account endpoints.
const (
	AccountEventPasswordChanged   = "password_changed"
	AccountEventProfileChanged    = "profile_changed"
	AccountEventEmailChangeSent   = "email_change_requested"
	AccountEventEmailChanged      = "email_changed"
	AccountEventDeletionRequested = "deletion_requested"
	AccountEventDeletionCancelled = "deletion_cancelled"
	AccountEventAccountPurged     = "account_purged"
	AccountEventSessionRevoked    = "session_revoked"
)

// UserDTO represents a sanitized account view for admin listing and detail endpoints.
// Never exposes password_hash, OTP, or reset token fields.
type UserDTO struct {
	ID            string     `json:"id"`
	FullName      string     `json:"full_name"`
	Email         string     `json:"email"`
	Phone         string     `json:"phone"`
	Status        UserStatus `json:"status"`
	StatusReason  string     `json:"status_reason,omitempty"`
	EmailVerified bool       `json:"email_verified"`
	CreatedAt     time.Time  `json:"created_at"`
	SuspendedAt   time.Time  `json:"suspended_at,omitempty"`
	ReactivatedAt time.Time  `json:"reactivated_at,omitempty"`
	DeletedAt     time.Time  `json:"deleted_at,omitempty"`
}

// ToDTO converts a User into a sanitized UserDTO.
func (u *User) ToDTO() *UserDTO {
	if u == nil {
		return nil
	}
	return &UserDTO{
		ID:            u.ID,
		FullName:      u.FullName,
		Email:         u.Email,
		Phone:         u.Phone,
		Status:        u.EffectiveStatus(),
		StatusReason:  u.StatusReason,
		EmailVerified: u.EmailVerified,
		CreatedAt:     u.CreatedAt,
		SuspendedAt:   u.SuspendedAt,
		ReactivatedAt: u.ReactivatedAt,
		DeletedAt:     u.DeletedAt,
	}
}

// SessionEndReason indicates why a session was terminated.
type SessionEndReason string

const (
	EndReasonReplaced SessionEndReason = "replaced"
	EndReasonLogout   SessionEndReason = "logout"
	EndReasonAdmin    SessionEndReason = "admin"
	// EndReasonUser marks a session ended by the student's own device list
	// (DELETE /auth/sessions/{sid}, F-UX2). Behavior matches logout.
	EndReasonUser SessionEndReason = "user"
)

// Session represents an active or terminated login session (SPEC Section 5).
// No IP address is stored.
type Session struct {
	ID          string           `json:"id" bson:"_id"` // sid (UUID)
	UserID      string           `json:"user_id" bson:"user_id"`
	DeviceID    string           `json:"device_id" bson:"device_id"`
	DeviceLabel string           `json:"device_label,omitempty" bson:"device_label,omitempty"`
	RefreshHash string           `json:"-" bson:"refresh_hash"`
	CreatedAt   time.Time        `json:"created_at" bson:"created_at"`
	LastUsedAt  time.Time        `json:"last_used_at" bson:"last_used_at"`
	EndedAt     *time.Time       `json:"ended_at,omitempty" bson:"ended_at,omitempty"`
	EndReason   SessionEndReason `json:"end_reason,omitempty" bson:"end_reason,omitempty"`
}

// IsActive reports whether the session has not been ended.
func (s *Session) IsActive() bool {
	return s != nil && s.EndedAt == nil
}
