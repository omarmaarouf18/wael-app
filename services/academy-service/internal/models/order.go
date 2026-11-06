package models

import "time"

// Order status constants (ADR-0014 2026-11-05 orders brief).
const (
	OrderStatusOpen   = "open"
	OrderStatusClosed = "closed"
)

// Order source constants.
const (
	OrderSourceWeb = "web"
	OrderSourceApp = "app"
)

// Order is one payment event. It owns one purchase request per subject.
// Web orders carry the transfer proof; app orders are grouped by the server
// and carry no proof fields.
type Order struct {
	ID                  string     `bson:"_id" json:"id"`
	UserID              string     `bson:"user_id" json:"user_id"`
	Source              string     `bson:"source" json:"source"` // "web" | "app"
	Status              string     `bson:"status" json:"status"` // "open" | "closed"
	PaymentMethod       string     `bson:"payment_method,omitempty" json:"payment_method,omitempty"`
	TransferReference   string     `bson:"transfer_reference,omitempty" json:"transfer_reference,omitempty"`
	NormalizedReference string     `bson:"normalized_reference,omitempty" json:"normalized_reference,omitempty"`
	SenderNumber        string     `bson:"sender_number,omitempty" json:"sender_number,omitempty"`
	AmountPaid          int        `bson:"amount_paid,omitempty" json:"amount_paid,omitempty"`
	CreatedAt           time.Time  `bson:"created_at" json:"created_at"`
	ClosedAt            *time.Time `bson:"closed_at,omitempty" json:"closed_at,omitempty"`
}

// OrderItemDTO is one subject line of an order, as the student sees it.
type OrderItemDTO struct {
	RequestID    string     `json:"request_id"`
	SubjectID    string     `json:"subject_id"`
	TitleAr      string     `json:"title_ar"`
	TitleEn      string     `json:"title_en,omitempty"`
	LevelNameAr  string     `json:"level_name_ar,omitempty"`
	Price        int        `json:"price"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	DecidedAt    *time.Time `json:"decided_at,omitempty"`
	RejectReason string     `json:"reject_reason,omitempty"`
}

// OrderDTO is the student view of an order (POST /academy/orders,
// GET /academy/me/orders and GET /academy/me/orders/{id}).
type OrderDTO struct {
	ID                string         `json:"id"`
	Source            string         `json:"source"`
	Status            string         `json:"status"`
	CreatedAt         time.Time      `json:"created_at"`
	DecidedAt         *time.Time     `json:"decided_at,omitempty"`
	PaymentMethod     string         `json:"payment_method,omitempty"`
	TransferReference string         `json:"transfer_reference,omitempty"`
	SenderNumber      string         `json:"sender_number,omitempty"`
	AmountPaid        int            `json:"amount_paid,omitempty"`
	Total             int            `json:"total"`
	AmountMismatch    bool           `json:"amount_mismatch"`
	Items             []OrderItemDTO `json:"items"`
}

// AdminOrderDTO is one order inside the console's grouped pending read model.
type AdminOrderDTO struct {
	OrderID           string         `json:"order_id"`
	Source            string         `json:"source"`
	Status            string         `json:"status"`
	CreatedAt         time.Time      `json:"created_at"`
	PaymentMethod     string         `json:"payment_method,omitempty"`
	TransferReference string         `json:"transfer_reference,omitempty"`
	SenderNumber      string         `json:"sender_number,omitempty"`
	AmountPaid        int            `json:"amount_paid,omitempty"`
	Total             int            `json:"total"`
	AmountMismatch    bool           `json:"amount_mismatch"`
	Items             []OrderItemDTO `json:"items"`
}

// StudentOrderGroupDTO groups one student's orders in the console read model.
type StudentOrderGroupDTO struct {
	UserID string          `json:"user_id"`
	Orders []AdminOrderDTO `json:"orders"`
}

// OrderGroupsResponseDTO is the console's grouped pending read model.
type OrderGroupsResponseDTO struct {
	Items         []StudentOrderGroupDTO `json:"items"`
	TotalStudents int                    `json:"total_students"`
	TotalOrders   int                    `json:"total_orders"`
	TotalRequests int                    `json:"total_requests"`
	PendingCount  int                    `json:"pending_count"`
}

// BulkRequestResult is one per-id outcome of the bulk accept/reject endpoint.
type BulkRequestResult struct {
	ID        string `json:"id"`
	Status    string `json:"status"` // "accepted" | "rejected" | "failed"
	ErrorCode string `json:"error_code,omitempty"`
	MessageAr string `json:"message_ar,omitempty"`
	MessageEn string `json:"message_en,omitempty"`
}

// BulkRequestsResponseDTO wraps one result per requested id, in input order.
type BulkRequestsResponseDTO struct {
	Results []BulkRequestResult `json:"results"`
}
