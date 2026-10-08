package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func validSettingsBody() map[string]any {
	return map[string]any{
		"show_prices":       true,
		"support_whatsapp":  "201098765432",
		"center_name_ar":    "مركز النور",
		"center_name_en":    "Al-Nour Center",
		"center_address_ar": "١٢ شارع التحرير، الدقي",
		"center_address_en": "12 Tahrir St, Dokki",
		"center_hours_ar":   "السبت - الخميس ٩ص - ٩م",
		"center_hours_en":   "Sat - Thu 9am - 9pm",
		"center_map_url":    "https://maps.google.com/?q=dokki",
	}
}

// TestAdminSettings_Auth verifies that GET and PUT require admin credentials.
func TestAdminSettings_Auth(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	// GET without credentials -> 401
	req := httptest.NewRequest(http.MethodGet, "/internal/admin/settings", nil)
	rec := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET without tokens = %d, want 401", rec.Code)
	}

	// PUT without credentials -> 401
	reqPut := httptest.NewRequest(http.MethodPut, "/internal/admin/settings", strings.NewReader(`{}`))
	recPut := httptest.NewRecorder()
	s.AdminHandler().ServeHTTP(recPut, reqPut)
	if recPut.Code != http.StatusUnauthorized {
		t.Fatalf("PUT without tokens = %d, want 401", recPut.Code)
	}
}

// TestAdminSettings_GetDefaults verifies GET returns env defaults merged before any save.
func TestAdminSettings_GetDefaults(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	rec := doAdminJSON(t, s, http.MethodGet, "/internal/admin/settings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /internal/admin/settings = %d (%s)", rec.Code, rec.Body.String())
	}

	var got models.AppSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode settings: %v", err)
	}

	if got.ShowPrices != false {
		t.Errorf("got.ShowPrices = %v, want false", got.ShowPrices)
	}
	if got.SupportWhatsApp != "201000000000" {
		t.Errorf("got.SupportWhatsApp = %q, want 201000000000", got.SupportWhatsApp)
	}
	if got.CenterNameAr != "" || got.CenterNameEn != "" || got.CenterMapURL != "" {
		t.Errorf("center fields should be empty, got %+v", got)
	}
	if !got.UpdatedAt.IsZero() {
		t.Errorf("got.UpdatedAt should be zero, got %v", got.UpdatedAt)
	}
	if got.UpdatedBy != "" {
		t.Errorf("got.UpdatedBy should be empty, got %q", got.UpdatedBy)
	}
}

// TestAdminSettings_PutAndGet verifies full replace, save, and reading back saved settings.
func TestAdminSettings_PutAndGet(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	body := validSettingsBody()
	rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /internal/admin/settings = %d (%s)", rec.Code, rec.Body.String())
	}

	var saved models.AppSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}

	if !saved.ShowPrices {
		t.Errorf("saved.ShowPrices = false, want true")
	}
	if saved.SupportWhatsApp != "201098765432" {
		t.Errorf("saved.SupportWhatsApp = %q, want 201098765432", saved.SupportWhatsApp)
	}
	if saved.CenterNameAr != "مركز النور" || saved.CenterNameEn != "Al-Nour Center" {
		t.Errorf("saved center names mismatch: %+v", saved)
	}
	if saved.CenterMapURL != "https://maps.google.com/?q=dokki" {
		t.Errorf("saved map url = %q", saved.CenterMapURL)
	}
	if saved.UpdatedBy != "adm-1" {
		t.Errorf("saved.UpdatedBy = %q, want adm-1", saved.UpdatedBy)
	}
	if saved.UpdatedAt.IsZero() {
		t.Errorf("saved.UpdatedAt is zero")
	}

	// GET now returns the saved settings
	recGet := doAdminJSON(t, s, http.MethodGet, "/internal/admin/settings", nil)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET after PUT = %d (%s)", recGet.Code, recGet.Body.String())
	}

	var fetched models.AppSettings
	if err := json.Unmarshal(recGet.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}

	if fetched.ShowPrices != saved.ShowPrices || fetched.SupportWhatsApp != saved.SupportWhatsApp {
		t.Errorf("fetched settings mismatch: %+v vs %+v", fetched, saved)
	}
}

// TestAdminSettings_AuditEntryNoValues verifies that PUT writes one settings_updated
// audit entry with changed field NAMES only, and NO values.
func TestAdminSettings_AuditEntryNoValues(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	body := validSettingsBody()
	rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /internal/admin/settings = %d (%s)", rec.Code, rec.Body.String())
	}

	logs, total, err := s.Store.ListAuditLogs(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d (%d items)", total, len(logs))
	}

	entry := logs[0]
	if entry.Action != "settings_updated" {
		t.Errorf("entry.Action = %q, want settings_updated", entry.Action)
	}
	if entry.TargetType != "settings" || entry.TargetID != "app" {
		t.Errorf("entry target = %s/%s, want settings/app", entry.TargetType, entry.TargetID)
	}

	// Detail must list changed field names
	detail := entry.Detail
	for _, field := range []string{"show_prices", "support_whatsapp", "center_name_ar", "center_map_url"} {
		if !strings.Contains(detail, field) {
			t.Errorf("detail %q missing field name %q", detail, field)
		}
	}

	// Detail must NOT contain values
	for _, val := range []string{"201098765432", "مركز النور", "Al-Nour Center", "https://maps.google.com"} {
		if strings.Contains(detail, val) {
			t.Fatalf("LEAK: audit detail contains setting value %q: %s", val, detail)
		}
	}
}

// TestAdminSettings_DisallowUnknownFields asserts unknown fields are rejected with 400.
func TestAdminSettings_DisallowUnknownFields(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	body := validSettingsBody()
	body["unknown_field"] = "malicious_payload"

	rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d (%s)", rec.Code, rec.Body.String())
	}
	if code := adminCode(t, rec); code != "invalid_json" {
		t.Fatalf("expected invalid_json code, got %q", code)
	}
}

// TestAdminSettings_Validation_MissingShowPrices asserts missing show_prices is rejected.
func TestAdminSettings_Validation_MissingShowPrices(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	body := validSettingsBody()
	delete(body, "show_prices")

	rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing show_prices, got %d", rec.Code)
	}
}

// TestAdminSettings_Validation_WhatsApp asserts support_whatsapp rules.
func TestAdminSettings_Validation_WhatsApp(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	for _, invalid := range []string{
		"+201000000000",    // "+" rejected
		"0100000000",       // Egyptian local without country code (9 digits)
		"123456789",        // 9 digits (too short)
		"1234567890123456", // 16 digits (too long)
		"20100000000a",     // non-digit
		"2010 000 0000",    // spaces inside
		"",                 // empty
	} {
		t.Run("invalid_"+invalid, func(t *testing.T) {
			body := validSettingsBody()
			body["support_whatsapp"] = invalid
			rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for whatsapp %q, got %d (%s)", invalid, rec.Code, rec.Body.String())
			}
			if code := adminCode(t, rec); code != "invalid_whatsapp" {
				t.Fatalf("expected invalid_whatsapp, got %q", code)
			}
		})
	}
}

// TestAdminSettings_Validation_Lengths asserts field length limits.
func TestAdminSettings_Validation_Lengths(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	tests := []struct {
		name    string
		field   string
		length  int
		errCode string
	}{
		{"center_name_ar_too_long", "center_name_ar", 101, "invalid_center_name"},
		{"center_name_en_too_long", "center_name_en", 101, "invalid_center_name"},
		{"center_address_ar_too_long", "center_address_ar", 301, "invalid_center_address"},
		{"center_address_en_too_long", "center_address_en", 301, "invalid_center_address"},
		{"center_hours_ar_too_long", "center_hours_ar", 201, "invalid_center_hours"},
		{"center_hours_en_too_long", "center_hours_en", 201, "invalid_center_hours"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := validSettingsBody()
			body[tc.field] = strings.Repeat("أ", tc.length)
			rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d", tc.name, rec.Code)
			}
			if code := adminCode(t, rec); code != tc.errCode {
				t.Fatalf("expected %s, got %q", tc.errCode, code)
			}
		})
	}
}

// TestAdminSettings_Validation_MapURL asserts map URL rules (empty or https only, max 500).
func TestAdminSettings_Validation_MapURL(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	// Insecure http rejected
	body := validSettingsBody()
	body["center_map_url"] = "http://maps.google.com/?q=dokki"
	rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for http map url, got %d", rec.Code)
	}
	if code := adminCode(t, rec); code != "invalid_map_url" {
		t.Fatalf("expected invalid_map_url, got %q", code)
	}

	// Over 500 chars rejected
	body["center_map_url"] = "https://maps.google.com/" + strings.Repeat("a", 500)
	rec = doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >500 char map url, got %d", rec.Code)
	}

	// Empty map URL is valid
	body["center_map_url"] = ""
	rec = doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty map url, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestAdminSettings_Validation_ForbiddenWords asserts all forbidden store-safety words
// are rejected on save with 400 settings_forbidden_word.
func TestAdminSettings_Validation_ForbiddenWords(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	forbiddenWords := []string{
		"دفع",
		"الدفع",
		"ادفع",
		"شراء",
		"الشراء",
		"اشتري",
		"استرداد",
		"اشتراك مدفوع",
		"payment",
		"pay",
		"buy",
		"purchase",
		"refund",
		"InstaPay",
		"INSTAPAY",
		"instapay",
		"فودافون كاش",
		"محفظة",
		"المحفظة",
		"wallet",
		"WALLET",
	}

	textFields := []string{
		"center_name_ar",
		"center_name_en",
		"center_address_ar",
		"center_address_en",
		"center_hours_ar",
		"center_hours_en",
	}

	for _, word := range forbiddenWords {
		t.Run("forbidden_"+word, func(t *testing.T) {
			for _, field := range textFields {
				body := validSettingsBody()
				body[field] = "مكتب " + word + " هنا"
				rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("word %q in field %q: expected 400, got %d (%s)", word, field, rec.Code, rec.Body.String())
				}
				if code := adminCode(t, rec); code != "settings_forbidden_word" {
					t.Fatalf("word %q in field %q: expected settings_forbidden_word, got %q", word, field, code)
				}
			}
		})
	}
}

// TestAdminSettings_WordBoundaries asserts that words containing substrings
// (such as company, players, or Arabic roots) are NOT rejected when they are not forbidden words.
func TestAdminSettings_WordBoundaries(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	safeTexts := []string{
		"company policy",
		"video player center",
		"display hall",
		"ميدان المدفعية",
		"مدينة نصر",
		"مكتب التحرير",
	}

	for _, safe := range safeTexts {
		t.Run("safe_"+safe, func(t *testing.T) {
			body := validSettingsBody()
			body["center_name_en"] = safe
			rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
			if rec.Code != http.StatusOK {
				t.Fatalf("safe text %q was rejected: %d (%s)", safe, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestAdminSettings_TrimAndControlChars asserts control characters and padding are cleaned.
func TestAdminSettings_TrimAndControlChars(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	body := validSettingsBody()
	body["center_name_ar"] = "  مركز\r\n النور\t  "
	body["center_name_en"] = "  Al-Nour\x00 Center  "

	rec := doAdminJSON(t, s, http.MethodPut, "/internal/admin/settings", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT with control chars = %d (%s)", rec.Code, rec.Body.String())
	}

	var saved models.AppSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if strings.Contains(saved.CenterNameAr, "\r") || strings.Contains(saved.CenterNameAr, "\n") || strings.Contains(saved.CenterNameAr, "\t") {
		t.Errorf("control chars not stripped from center_name_ar: %q", saved.CenterNameAr)
	}
	if strings.Contains(saved.CenterNameEn, "\x00") {
		t.Errorf("null byte not stripped from center_name_en: %q", saved.CenterNameEn)
	}
	if strings.HasPrefix(saved.CenterNameAr, " ") || strings.HasSuffix(saved.CenterNameAr, " ") {
		t.Errorf("center_name_ar not trimmed: %q", saved.CenterNameAr)
	}
}

// TestAdminSettings_MethodNotAllowed asserts non-GET/PUT methods return 405.
func TestAdminSettings_MethodNotAllowed(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPatch} {
		rec := doAdminJSON(t, s, method, "/internal/admin/settings", nil)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /internal/admin/settings = %d, want 405", method, rec.Code)
		}
	}
}
