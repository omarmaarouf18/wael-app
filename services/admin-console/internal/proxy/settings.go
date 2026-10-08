package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
)

// SettingsGet handles GET /api/settings -> GET academy /internal/admin/settings.
func (p *Proxy) SettingsGet(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "settings.get", method: http.MethodGet, path: "/internal/admin/settings",
	})
}

// settingsUpdateRequest is the body of POST /api/settings/update.
type settingsUpdateRequest struct {
	ShowPrices      *bool  `json:"show_prices"`
	SupportWhatsApp string `json:"support_whatsapp"`
	CenterNameAr    string `json:"center_name_ar"`
	CenterNameEn    string `json:"center_name_en"`
	CenterAddressAr string `json:"center_address_ar"`
	CenterAddressEn string `json:"center_address_en"`
	CenterHoursAr   string `json:"center_hours_ar"`
	CenterHoursEn   string `json:"center_hours_en"`
	CenterMapURL    string `json:"center_map_url"`
}

// SettingsUpdate handles POST /api/settings/update -> PUT academy /internal/admin/settings.
func (p *Proxy) SettingsUpdate(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in settingsUpdateRequest
	if !decodeStrict(w, r, &in) {
		return
	}
	if in.ShowPrices == nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	body, err := json.Marshal(map[string]any{
		"show_prices":       *in.ShowPrices,
		"support_whatsapp":  strings.TrimSpace(in.SupportWhatsApp),
		"center_name_ar":    in.CenterNameAr,
		"center_name_en":    in.CenterNameEn,
		"center_address_ar": in.CenterAddressAr,
		"center_address_en": in.CenterAddressEn,
		"center_hours_ar":   in.CenterHoursAr,
		"center_hours_en":   in.CenterHoursEn,
		"center_map_url":    strings.TrimSpace(in.CenterMapURL),
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	p.relayAcademy(w, r, token, upstreamCall{
		route: "settings.update", method: http.MethodPut, path: "/internal/admin/settings", body: body,
	})
}
