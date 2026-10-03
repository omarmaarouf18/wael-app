package models

import "time"

// Payment source constants (mirror the entitlement sources).
const (
	PaymentSourceRequest    = "request"
	PaymentSourceAdminGrant = "admin_grant"
)

// PaymentRecord is one append-only financial row per activation (SPEC Section
// 5, decision 19). The amount always equals price_at_grant: the subject's
// price at the moment of activation; the admin never types an amount. Rows
// are never updated or deleted; a refund correction is a new row with
// CorrectsID plus an audit-log entry (a later feature). Students never see
// payment records.
type PaymentRecord struct {
	ID            string    `bson:"_id" json:"id"`
	UserID        string    `bson:"user_id" json:"user_id"`
	SubjectID     string    `bson:"subject_id" json:"subject_id"`
	EntitlementID string    `bson:"entitlement_id" json:"entitlement_id"`
	RequestID     string    `bson:"request_id,omitempty" json:"request_id,omitempty"`
	Amount        int       `bson:"amount" json:"amount"`
	PriceAtGrant  int       `bson:"price_at_grant" json:"price_at_grant"`
	Source        string    `bson:"source" json:"source"` // "request" or "admin_grant"
	RecordedBy    string    `bson:"recorded_by" json:"recorded_by"`
	RecordedAt    time.Time `bson:"recorded_at" json:"recorded_at"`
	CorrectsID    string    `bson:"corrects_id,omitempty" json:"corrects_id,omitempty"`
}
