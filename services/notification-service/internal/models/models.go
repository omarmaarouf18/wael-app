// Package models defines notification-service domain types.
package models

import "time"

// Notification is a user-scoped inbox item. Ar fields carry the Arabic
// rendering produced by the trigger; clients fall back to the base fields.
type Notification struct {
	ID          string    `json:"id" bson:"_id"`
	UserID      string    `json:"user_id" bson:"user_id"`
	Title       string    `json:"title" bson:"title"`
	TitleAr     string    `json:"title_ar,omitempty" bson:"title_ar,omitempty"`
	Body        string    `json:"body" bson:"body"`
	BodyAr      string    `json:"body_ar,omitempty" bson:"body_ar,omitempty"`
	Type        string    `json:"type" bson:"type"`
	TargetRoute string    `json:"target_route,omitempty" bson:"target_route,omitempty"`
	Read        bool      `json:"read" bson:"read"`
	CreatedAt   time.Time `json:"created_at" bson:"created_at"`
}
