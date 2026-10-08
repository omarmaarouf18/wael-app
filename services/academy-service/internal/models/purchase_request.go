package models

import (
	"strings"
	"time"
	"unicode"
)

// PurchaseRequest status constants.
const (
	RequestStatusPending  = "pending"
	RequestStatusAccepted = "accepted"
	RequestStatusRejected = "rejected"
)

// PurchaseRequest represents a student's access request for a subject.
type PurchaseRequest struct {
	ID           string     `bson:"_id" json:"id"`
	UserID       string     `bson:"user_id" json:"user_id"`
	SubjectID    string     `bson:"subject_id" json:"subject_id"`
	Status       string     `bson:"status" json:"status"` // "pending", "accepted", "rejected"
	CreatedAt    time.Time  `bson:"created_at" json:"created_at"`
	DecidedAt    *time.Time `bson:"decided_at,omitempty" json:"decided_at,omitempty"`
	DecidedBy    string     `bson:"decided_by,omitempty" json:"decided_by,omitempty"`
	RejectReason string     `bson:"reject_reason,omitempty" json:"reject_reason,omitempty"`
}

// SubjectRequestDTO is included in SubjectDetailDTO when unowned and a request exists.
type SubjectRequestDTO struct {
	Status string `json:"status"`
	// WhatsappURL is the support chat link, present only while a pending
	// request exists (F-UX2 A8). Same value and https-only rule as the
	// access-request response; absent otherwise.
	WhatsappURL string `json:"whatsapp_url,omitempty"`
}

// AccessRequestResponseDTO is the student-facing response for POST /academy/subjects/{id}/access-request.
// It carries the request status and the support WhatsApp link. Contains no payment wording or admin fields.
type AccessRequestResponseDTO struct {
	ID          string    `json:"id"`
	SubjectID   string    `json:"subject_id"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	WhatsAppURL string    `json:"whatsapp_url,omitempty"`
}

// FormatWhatsAppURLStrict converts a phone number or URL into a WhatsApp
// click-to-chat URL and enforces the https-only rule: it returns "" unless
// the result starts with "https://", so the app never opens an insecure
// link. Used by the access-request response, the subject detail pending
// request (F-UX2 A8), and the public app config (F-UX2 A7).
func FormatWhatsAppURLStrict(phone string) string {
	u := FormatWhatsAppURL(phone)
	if strings.HasPrefix(u, "https://") {
		return u
	}
	return ""
}

// FormatWhatsAppURL converts a phone number or URL into a WhatsApp click-to-chat URL (https://wa.me/<digits>).
func FormatWhatsAppURL(phone string) string {
	trimmed := strings.TrimSpace(phone)
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}
	var digits strings.Builder
	for _, r := range trimmed {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return "https://wa.me/" + trimmed
	}
	return "https://wa.me/" + digits.String()
}
