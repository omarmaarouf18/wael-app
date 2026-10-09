package models

// CenterText is one localized center field. An empty language is omitted,
// so the app sees only the languages the admin filled in.
type CenterText struct {
	Ar string `json:"ar,omitempty"`
	En string `json:"en,omitempty"`
}

// CenterDTO represents physical learning center information in public app
// config (A3). Exact shape: {"name": {"ar","en"}, "address": {"ar","en"},
// "hours": {"ar","en"}, "map_url": "https://..."}; a field with no text in
// either language and an empty map_url are omitted, and the whole center is
// omitted when nothing is set (AppSettings.CenterDTO returns nil).
type CenterDTO struct {
	Name    *CenterText `json:"name,omitempty"`
	Address *CenterText `json:"address,omitempty"`
	Hours   *CenterText `json:"hours,omitempty"`
	MapURL  string      `json:"map_url,omitempty"`
}

// AppConfigDTO is the public app configuration served by
// GET /academy/app-config (F-UX2 A7, 2026-10-08 settings): support WhatsApp link,
// terms and privacy URLs, optional update metadata, show_prices flag, and optional center info.
// It carries no payment wording and no per-user data, so it is public and cacheable.
type AppConfigDTO struct {
	ShowPrices         bool       `json:"show_prices"`
	SupportWhatsAppURL string     `json:"support_whatsapp_url,omitempty"`
	TermsURL           string     `json:"terms_url"`
	PrivacyURL         string     `json:"privacy_url"`
	MinVersion         string     `json:"min_version,omitempty"`
	LatestVersion      string     `json:"latest_version,omitempty"`
	UpdateURL          string     `json:"update_url,omitempty"`
	Center             *CenterDTO `json:"center,omitempty"`
	// Features is always present (SPEC Section 6 amendment 2026-10-08).
	Features AppFeaturesDTO `json:"features"`
}

// AppFeaturesDTO holds named feature switches the app reads from app-config.
// files turns on notes & books downloads (SPEC Phase 5); the app treats a
// missing, false or non-boolean value as off.
type AppFeaturesDTO struct {
	Files bool `json:"files"`
}
