package models

import "time"

// Entitlement source constants.
const (
	EntitlementSourceAdminGrant = "admin_grant"
	EntitlementSourceRequest    = "request"
)

// Entitlement represents a student's access grant to a subject.
type Entitlement struct {
	ID        string    `bson:"_id" json:"id"`
	UserID    string    `bson:"user_id" json:"user_id"`
	SubjectID string    `bson:"subject_id" json:"subject_id"`
	ExpiresAt time.Time `bson:"expires_at" json:"expires_at"`
	GrantedAt time.Time `bson:"granted_at" json:"granted_at"`
	Source    string    `bson:"source" json:"source"` // "request" or "admin_grant"
	GrantedBy string    `bson:"granted_by" json:"granted_by"`
	RequestID string    `bson:"request_id,omitempty" json:"request_id,omitempty"`
	Active    bool      `bson:"active" json:"active"` // true for current active entitlement
}

// IsExpired reports whether the entitlement has passed its expiration time.
func (e *Entitlement) IsExpired(now time.Time) bool {
	return !e.ExpiresAt.After(now)
}
