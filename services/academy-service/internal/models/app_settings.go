package models

import (
	"strings"
	"time"
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

// CenterHasContent reports whether any center field is non-empty.
func (s *AppSettings) CenterHasContent() bool {
	if s == nil {
		return false
	}
	return s.CenterNameAr != "" || s.CenterNameEn != "" ||
		s.CenterAddressAr != "" || s.CenterAddressEn != "" ||
		s.CenterHoursAr != "" || s.CenterHoursEn != "" ||
		s.CenterMapURL != ""
}

// CenterDTO returns the student-facing CenterDTO, or nil if all center fields are empty.
func (s *AppSettings) CenterDTO() *CenterDTO {
	if !s.CenterHasContent() {
		return nil
	}
	return &CenterDTO{
		Name:    centerText(s.CenterNameAr, s.CenterNameEn),
		Address: centerText(s.CenterAddressAr, s.CenterAddressEn),
		Hours:   centerText(s.CenterHoursAr, s.CenterHoursEn),
		MapURL:  s.CenterMapURL,
	}
}

// centerText returns nil when both languages are empty, so the key is
// omitted from the JSON.
func centerText(ar, en string) *CenterText {
	if ar == "" && en == "" {
		return nil
	}
	return &CenterText{Ar: ar, En: en}
}

// ValidWhatsAppNumber returns the digits of raw when they form a usable
// international WhatsApp number: 10-15 digits, not starting with 0, written
// with digits only or with "+", spaces, dashes or parentheses around them.
// Anything else returns "": there is deliberately no fallback number, so a
// missing or broken number hides the support link instead of showing a
// wrong one.
func ValidWhatsAppNumber(raw string) string {
	var sb strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		switch {
		case r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')':
		default:
			return ""
		}
	}
	d := sb.String()
	if len(d) < 10 || len(d) > 15 || d[0] == '0' {
		return ""
	}
	return d
}

// WithWhatsAppDefault returns a copy of s (empty settings when s is nil)
// whose support number is the stored one when valid, else envDefault
// (SUPPORT_WHATSAPP) when valid, else "".
func WithWhatsAppDefault(s *AppSettings, envDefault string) *AppSettings {
	res := &AppSettings{}
	if s != nil {
		res = s.Clone()
	}
	res.ID = AppSettingsID
	number := ValidWhatsAppNumber(res.SupportWhatsApp)
	if number == "" {
		number = ValidWhatsAppNumber(envDefault)
	}
	res.SupportWhatsApp = number
	return res
}
