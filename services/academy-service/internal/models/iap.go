package models

import "time"

// IAPEnvironment constants.
const (
	IAPEnvironmentProduction = "Production"
	IAPEnvironmentSandbox    = "Sandbox"
)

// IAPStatus constants for transaction responses.
const (
	IAPStatusGranted          = "granted"
	IAPStatusAlreadyProcessed = "already_processed"
)

// IAPSkipReason constants.
const (
	IAPSkipReasonAlreadyOwned = "already_owned"
	IAPSkipReasonExpired      = "expired"
	IAPSkipReasonUnavailable  = "unavailable"
)

// IAPPackage represents a curated bundle of subjects sold via Apple In-App Purchase.
type IAPPackage struct {
	ID             string     `bson:"_id" json:"id"`
	ProductID      string     `bson:"product_id" json:"product_id"`
	TitleAr        string     `bson:"title_ar" json:"title_ar"`
	TitleEn        string     `bson:"title_en" json:"title_en"`
	DescriptionAr  string     `bson:"description_ar" json:"description_ar"`
	DescriptionEn  string     `bson:"description_en" json:"description_en"`
	SubjectIDs     []string   `bson:"subject_ids" json:"subject_ids"`
	AvailableUntil *time.Time `bson:"available_until,omitempty" json:"available_until,omitempty"`
	Published      bool       `bson:"published" json:"published"`
	Position       int        `bson:"position" json:"position"`
	CreatedAt      time.Time  `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `bson:"updated_at" json:"updated_at"`
}

// HasSubject returns true if the package contains the specified subject ID.
func (p *IAPPackage) HasSubject(subjectID string) bool {
	for _, id := range p.SubjectIDs {
		if id == subjectID {
			return true
		}
	}
	return false
}

// IAPSkipped represents a subject that was not granted during transaction processing.
type IAPSkipped struct {
	SubjectID string `bson:"subject_id" json:"subject_id"`
	Reason    string `bson:"reason" json:"reason"` // "already_owned" | "expired" | "unavailable"
}

// IAPPurchase records a verified Apple In-App Purchase transaction.
type IAPPurchase struct {
	ID                    string       `bson:"_id" json:"id"`
	TransactionID         string       `bson:"transaction_id" json:"transaction_id"`
	OriginalTransactionID string       `bson:"original_transaction_id" json:"original_transaction_id"`
	UserID                string       `bson:"user_id" json:"user_id"`
	PackageID             string       `bson:"package_id" json:"package_id"`
	ProductID             string       `bson:"product_id" json:"product_id"`
	Environment           string       `bson:"environment" json:"environment"` // "Production" | "Sandbox"
	PurchaseDate          time.Time    `bson:"purchase_date" json:"purchase_date"`
	GrantedSubjectIDs     []string     `bson:"granted_subject_ids" json:"granted_subject_ids"`
	Skipped               []IAPSkipped `bson:"skipped" json:"skipped"`
	CreatedAt             time.Time    `bson:"created_at" json:"created_at"`
}

// IAPPurchaseFilter specifies filter criteria for admin purchase queries.
type IAPPurchaseFilter struct {
	UserID    string
	PackageID string
}
