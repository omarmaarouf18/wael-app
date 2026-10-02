package models

import "time"

// AuditLog represents an entry in the academy admin_audit_log collection.
// Schema mirrors auth-service's admin_audit_log (SPEC Section 5, ADR-0008
// Section 9): _id, actor_id, actor_name, action, target_type, target_id,
// detail, created_at. No IP address is persisted.
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
