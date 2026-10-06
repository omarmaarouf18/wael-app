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
