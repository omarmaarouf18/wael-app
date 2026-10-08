package models

import (
	"strings"
	"time"
	"unicode"
)

// AppSettingsID is the fixed MongoDB _id for the single app_settings document.
const AppSettingsID = "app"

// AppSettings stores app-facing settings configured by the admin console (A1).
type AppSettings struct {
	ID              string    `bson:"_id" json:"-"`
	ShowPrices      bool      `bson:"show_prices" json:"show_prices"`
	SupportWhatsApp string    `bson:"support_whatsapp" json:"support_whatsapp"`
	CenterNameAr    string    `bson:"center_name_ar" json:"center_name_ar"`
	CenterNameEn    string    `bson:"center_name_en" json:"center_name_en"`
	CenterAddressAr string    `bson:"center_address_ar" json:"center_address_ar"`
	CenterAddressEn string    `bson:"center_address_en" json:"center_address_en"`
	CenterHoursAr   string    `bson:"center_hours_ar" json:"center_hours_ar"`
	CenterHoursEn   string    `bson:"center_hours_en" json:"center_hours_en"`
	CenterMapURL    string    `bson:"center_map_url" json:"center_map_url"`
	UpdatedAt       time.Time `bson:"updated_at,omitempty" json:"updated_at,omitempty"`
	UpdatedBy       string    `bson:"updated_by,omitempty" json:"updated_by,omitempty"`
}

// Clone creates a shallow copy of AppSettings.
func (s *AppSettings) Clone() *AppSettings {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

// NormalizeWhatsAppDigits extracts digits only from raw input, stripping any "+",
// spaces or formatting characters. Returns fallback "201000000000" if no digits are present.
func NormalizeWhatsAppDigits(raw string) string {
	var sb strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	if sb.Len() == 0 {
		return "201000000000"
	}
	return sb.String()
}

// DefaultAppSettings returns the initial settings merged with env defaults.
func DefaultAppSettings(envWhatsApp string) *AppSettings {
	return &AppSettings{
		ID:              AppSettingsID,
		ShowPrices:      false,
		SupportWhatsApp: NormalizeWhatsAppDigits(envWhatsApp),
	}
}

// MergeAppSettingsDefaults merges env defaults into settings if any defaults apply.
func MergeAppSettingsDefaults(s *AppSettings, envWhatsApp string) *AppSettings {
	if s == nil {
		return DefaultAppSettings(envWhatsApp)
	}
	res := s.Clone()
	if res.ID == "" {
		res.ID = AppSettingsID
	}
	if res.SupportWhatsApp == "" {
		res.SupportWhatsApp = NormalizeWhatsAppDigits(envWhatsApp)
	}
	return res
}
