// Package models defines notification-service domain types.
package models

import "time"

// Notification is a user-scoped inbox item. Ar fields carry the Arabic
// rendering produced by the trigger; clients fall back to the base fields.
// SubjectID is the academy's own subject id when the notification is about
// one subject (deep link target); it is absent on older rows and on
// non-subject notices.
type Notification struct {
	ID          string    `json:"id" bson:"_id"`
	UserID      string    `json:"user_id" bson:"user_id"`
	Title       string    `json:"title" bson:"title"`
	TitleAr     string    `json:"title_ar,omitempty" bson:"title_ar,omitempty"`
	Body        string    `json:"body" bson:"body"`
	BodyAr      string    `json:"body_ar,omitempty" bson:"body_ar,omitempty"`
	Type        string    `json:"type" bson:"type"`
	TargetRoute string    `json:"target_route,omitempty" bson:"target_route,omitempty"`
	SubjectID   string    `json:"subject_id,omitempty" bson:"subject_id,omitempty"`
	Read        bool      `json:"read" bson:"read"`
	CreatedAt   time.Time `json:"created_at" bson:"created_at"`
}

// Broadcast statuses: a send moves queued -> sending -> done, partial or
// failed. Retry moves failed/partial back to sending.
const (
	BroadcastQueued  = "queued"
	BroadcastSending = "sending"
	BroadcastDone    = "done"
	BroadcastPartial = "partial"
	BroadcastFailed  = "failed"
)

// Target statuses for one (broadcast, user) delivery.
const (
	TargetPending = "pending"
	TargetSent    = "sent"
	TargetFailed  = "failed"
)

// Broadcast is one admin send (SPEC Phase 7, ADR-0013). Audience is "all"
// or "subject:<id>". ResolveCursor/ResolveDone track audience resolution
// across restarts; they are internal and never serialized to admins.
type Broadcast struct {
	ID            string    `json:"id" bson:"_id"`
	Audience      string    `json:"audience" bson:"audience"`
	Title         string    `json:"title" bson:"title"`
	TitleAr       string    `json:"title_ar" bson:"title_ar"`
	Body          string    `json:"body" bson:"body"`
	BodyAr        string    `json:"body_ar" bson:"body_ar"`
	SubjectID     string    `json:"subject_id,omitempty" bson:"subject_id,omitempty"`
	CreatedBy     string    `json:"created_by" bson:"created_by"`
	CreatedAt     time.Time `json:"created_at" bson:"created_at"`
	Status        string    `json:"status" bson:"status"`
	Total         int       `json:"total" bson:"total"`
	Delivered     int       `json:"delivered" bson:"delivered"`
	Failed        int       `json:"failed" bson:"failed"`
	ResolveCursor string    `json:"-" bson:"resolve_cursor,omitempty"`
	ResolveDone   bool      `json:"-" bson:"resolve_done"`
}

// BroadcastTarget tracks delivery to one user. The _id is deterministic
// per (broadcast, user) so resolution and retries are idempotent.
type BroadcastTarget struct {
	ID          string    `bson:"_id"`
	BroadcastID string    `bson:"broadcast_id"`
	UserID      string    `bson:"user_id"`
	Status      string    `bson:"status"`
	Attempts    int       `bson:"attempts"`
	LastError   string    `bson:"last_error,omitempty"`
	UpdatedAt   time.Time `bson:"updated_at"`
}

// AuditLog records one admin mutation (ADR-0008 Section 7: no secrets, no
// IP addresses; broadcast entries carry counts, never message text).
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
