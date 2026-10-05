package models

// AppConfigDTO is the public app configuration served by
// GET /academy/app-config (F-UX2 A7): support WhatsApp link, terms and
// privacy URLs, and optional update metadata. It carries no payment wording
// and no per-user data, so it is public and cacheable.
type AppConfigDTO struct {
	SupportWhatsAppURL string `json:"support_whatsapp_url"`
	TermsURL           string `json:"terms_url"`
	PrivacyURL         string `json:"privacy_url"`
	MinVersion         string `json:"min_version,omitempty"`
	LatestVersion      string `json:"latest_version,omitempty"`
	UpdateURL          string `json:"update_url,omitempty"`
}
