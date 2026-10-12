package handlers

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/storesafety"
)

// ForbiddenSettingsWords defines the store-safety payment terms forbidden in settings text fields (A1).
// The single source of truth lives in shared/infra/storesafety; this alias
// stays so existing references keep compiling.
var ForbiddenSettingsWords = storesafety.ForbiddenWords

// HasForbiddenSettingsWord checks if the raw text contains any forbidden payment terms with word boundaries.
func HasForbiddenSettingsWord(text string) bool {
	return storesafety.HasForbiddenWord(text)
}

// cleanSettingsText strips control characters, trims leading/trailing spaces, and enforces rune limit.
func cleanSettingsText(raw string, maxRunes int) (string, bool) {
	return storesafety.CleanText(raw, maxRunes)
}

// cleanMapURL validates that center_map_url is either empty or an https URL with max 500 runes.
func cleanMapURL(raw string) (string, bool) {
	u, ok := cleanSettingsText(raw, 500)
	if !ok {
		return "", false
	}
	if u == "" {
		return "", true
	}
	if !strings.HasPrefix(u, "https://") {
		return "", false
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", false
	}
	return u, true
}

// cleanWhatsAppNumber validates that support_whatsapp is 10-15 digits only without "+" in international format.
func cleanWhatsAppNumber(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if len(s) < 10 || len(s) > 15 {
		return "", false
	}
	if s[0] == '0' {
		return "", false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return s, true
}

// AdminSettingsRequest is the request body for PUT /internal/admin/settings.
type AdminSettingsRequest struct {
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

// effectiveAppSettings returns the stored settings with the support number
// resolved: the stored number when valid, else SUPPORT_WHATSAPP when valid,
// else "" (no fallback number; the app then shows no support link).
func (s *Server) effectiveAppSettings(ctx context.Context) (*models.AppSettings, error) {
	stored, err := s.Store.GetAppSettings(ctx)
	if err != nil {
		return nil, err
	}
	return models.WithWhatsAppDefault(stored, s.SupportWhatsApp), nil
}

// supportWhatsAppURL is the one resolver for every support link (app-config,
// access-request response, pending subject detail): the resolved number
// (stored if valid, else SUPPORT_WHATSAPP if valid) through the same 60 s
// settings cache, as https://wa.me/<digits>, or "" when there is no valid
// number (callers omit the field; never a bare https://wa.me/).
func (s *Server) supportWhatsAppURL(ctx context.Context) (string, error) {
	settings, err := s.effectiveAppSettings(ctx)
	if err != nil {
		return "", err
	}
	if settings.SupportWhatsApp == "" {
		return "", nil
	}
	return models.FormatWhatsAppURLStrict(settings.SupportWhatsApp), nil
}

// WarnIfNoSupportWhatsApp logs one warning when neither the stored number
// nor SUPPORT_WHATSAPP is a valid WhatsApp number, so app-config will omit
// support_whatsapp_url. Called once at startup; it reports whether it
// warned. A store error is logged and treated as no warning.
func (s *Server) WarnIfNoSupportWhatsApp(ctx context.Context) bool {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	settings, err := s.effectiveAppSettings(dbCtx)
	if err != nil {
		log.Printf("[ACADEMY] warning: could not read app settings to check the support WhatsApp number: %v", err)
		return false
	}
	if settings.SupportWhatsApp != "" {
		return false
	}
	log.Printf("[ACADEMY] warning: no valid support WhatsApp number (neither the console setting nor SUPPORT_WHATSAPP); app-config omits support_whatsapp_url until one is saved")
	return true
}

// AdminSettings dispatches GET and PUT /internal/admin/settings on the admin listener.
func (s *Server) AdminSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getAdminSettings(w, r)
	case http.MethodPut:
		s.putAdminSettings(w, r)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

// getAdminSettings serves GET /internal/admin/settings: returns current settings with env defaults merged.
func (s *Server) getAdminSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	current, err := s.effectiveAppSettings(dbCtx)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, current)
}

// putAdminSettings serves PUT /internal/admin/settings: full replace of editable fields with audit logging.
func (s *Server) putAdminSettings(w http.ResponseWriter, r *http.Request) {
	adm, ok := AdminFromRequest(r)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var req AdminSettingsRequest
	if !decodeAdminJSON(w, r, &req) {
		return
	}

	if req.ShowPrices == nil {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "show_prices is required", nil)
		return
	}

	cleanWhatsApp, ok := cleanWhatsAppNumber(req.SupportWhatsApp)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_whatsapp", "support_whatsapp must be 10-15 digits without +", nil)
		return
	}

	cleanNameAr, ok := cleanSettingsText(req.CenterNameAr, 100)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_center_name", "center_name_ar must be at most 100 characters", nil)
		return
	}

	cleanNameEn, ok := cleanSettingsText(req.CenterNameEn, 100)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_center_name", "center_name_en must be at most 100 characters", nil)
		return
	}

	cleanAddressAr, ok := cleanSettingsText(req.CenterAddressAr, 300)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_center_address", "center_address_ar must be at most 300 characters", nil)
		return
	}

	cleanAddressEn, ok := cleanSettingsText(req.CenterAddressEn, 300)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_center_address", "center_address_en must be at most 300 characters", nil)
		return
	}

	cleanHoursAr, ok := cleanSettingsText(req.CenterHoursAr, 200)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_center_hours", "center_hours_ar must be at most 200 characters", nil)
		return
	}

	cleanHoursEn, ok := cleanSettingsText(req.CenterHoursEn, 200)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_center_hours", "center_hours_en must be at most 200 characters", nil)
		return
	}

	cleanMap, ok := cleanMapURL(req.CenterMapURL)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_map_url", "center_map_url must be https and at most 500 characters", nil)
		return
	}

	// Check forbidden store-safety words in every text field
	textFields := []string{cleanNameAr, cleanNameEn, cleanAddressAr, cleanAddressEn, cleanHoursAr, cleanHoursEn, cleanMap}
	for _, txt := range textFields {
		if HasForbiddenSettingsWord(txt) {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "settings_forbidden_word", "contains forbidden payment term", nil)
			return
		}
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	current, err := s.effectiveAppSettings(dbCtx)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	// Compare changed field names (field names only, no values)
	var changed []string
	if current.ShowPrices != *req.ShowPrices {
		changed = append(changed, "show_prices")
	}
	if current.SupportWhatsApp != cleanWhatsApp {
		changed = append(changed, "support_whatsapp")
	}
	if current.CenterNameAr != cleanNameAr {
		changed = append(changed, "center_name_ar")
	}
	if current.CenterNameEn != cleanNameEn {
		changed = append(changed, "center_name_en")
	}
	if current.CenterAddressAr != cleanAddressAr {
		changed = append(changed, "center_address_ar")
	}
	if current.CenterAddressEn != cleanAddressEn {
		changed = append(changed, "center_address_en")
	}
	if current.CenterHoursAr != cleanHoursAr {
		changed = append(changed, "center_hours_ar")
	}
	if current.CenterHoursEn != cleanHoursEn {
		changed = append(changed, "center_hours_en")
	}
	if current.CenterMapURL != cleanMap {
		changed = append(changed, "center_map_url")
	}

	now := time.Now().UTC()
	newSettings := &models.AppSettings{
		ID:              models.AppSettingsID,
		ShowPrices:      *req.ShowPrices,
		SupportWhatsApp: cleanWhatsApp,
		CenterNameAr:    cleanNameAr,
		CenterNameEn:    cleanNameEn,
		CenterAddressAr: cleanAddressAr,
		CenterAddressEn: cleanAddressEn,
		CenterHoursAr:   cleanHoursAr,
		CenterHoursEn:   cleanHoursEn,
		CenterMapURL:    cleanMap,
		UpdatedAt:       now,
		UpdatedBy:       adm.ID,
	}

	if err := s.Store.SaveAppSettings(dbCtx, newSettings); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	// Write academy audit log with changed field NAMES only (no values)
	detail := strings.Join(changed, ", ")
	if len(changed) == 0 {
		detail = "none"
	}
	if err := s.writeAdminAudit(r.Context(), adm, "settings_updated", "settings", "app", detail); err != nil {
		logAuditFailure(adm, "settings_updated", "app", err)
	}

	handlerutil.WriteJSON(w, http.StatusOK, newSettings)
}
