package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
)

func appConfigTestServer() *Server {
	s := newTestServer(false)
	s.TermsURL = "https://elmetracademy.app/terms"
	s.PrivacyURL = "https://elmetracademy.app/privacy"
	s.MinVersion = "1.4.0"
	s.LatestVersion = "1.5.0"
	s.UpdateURL = "https://elmetracademy.app/app"
	return s
}

func doPublic(t *testing.T, h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestAppConfig_PublicShape serves the config without any student JWT.
func TestAppConfig_PublicShape(t *testing.T) {
	s := appConfigTestServer()
	h := s.PublicHandler()

	rec := doPublic(t, h, http.MethodGet, "/academy/app-config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("app-config = %d (%s)", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Fatalf("Cache-Control = %q, want public, max-age=300", cc)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"show_prices", "support_whatsapp_url", "terms_url", "privacy_url", "min_version", "latest_version", "update_url"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("missing key %q: %s", k, rec.Body.String())
		}
	}
	if len(raw) != 7 {
		t.Fatalf("unexpected keys count %d, want 7: %s", len(raw), rec.Body.String())
	}
	var cfg models.AppConfigDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode DTO: %v", err)
	}
	if cfg.ShowPrices != false {
		t.Errorf("show_prices = %v, want false by default", cfg.ShowPrices)
	}
	if cfg.Center != nil {
		t.Errorf("center should be nil by default, got %+v", cfg.Center)
	}
	if cfg.SupportWhatsAppURL != "https://wa.me/201000000000" {
		t.Errorf("support_whatsapp_url = %q", cfg.SupportWhatsAppURL)
	}
	if cfg.TermsURL != "https://elmetracademy.app/terms" || cfg.PrivacyURL != "https://elmetracademy.app/privacy" {
		t.Errorf("terms/privacy = %q %q", cfg.TermsURL, cfg.PrivacyURL)
	}
	if cfg.MinVersion != "1.4.0" || cfg.LatestVersion != "1.5.0" || cfg.UpdateURL != "https://elmetracademy.app/app" {
		t.Errorf("versions = %+v", cfg)
	}
	assertNoForbiddenStoreWords(t, "app-config", strings.ReplaceAll(rec.Body.String(), "show_prices", ""))
	if tok := makeStudentToken(t, "cfg-student"); true {
		rec2 := doPublic(t, h, http.MethodGet, "/academy/app-config", tok)
		if rec2.Code != http.StatusOK || rec2.Body.String() != rec.Body.String() {
			t.Fatalf("authed app-config differs: %d %s", rec2.Code, rec2.Body.String())
		}
	}
}

// TestAppConfig_EmptyOptionalsOmitted: empty versions and empty center are absent.
func TestAppConfig_EmptyOptionalsOmitted(t *testing.T) {
	s := newTestServer(false)
	s.TermsURL = "https://elmetracademy.app/terms"
	s.PrivacyURL = "https://elmetracademy.app/privacy"
	h := s.PublicHandler()

	rec := doPublic(t, h, http.MethodGet, "/academy/app-config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("app-config = %d (%s)", rec.Code, rec.Body.String())
	}
	for _, k := range []string{"min_version", "latest_version", "update_url", "center"} {
		if strings.Contains(rec.Body.String(), k) {
			t.Fatalf("empty optional %q present: %s", k, rec.Body.String())
		}
	}
}

// TestAppConfig_MethodNotAllowed refuses non-GET.
func TestAppConfig_MethodNotAllowed(t *testing.T) {
	s := appConfigTestServer()
	h := s.PublicHandler()
	rec := doPublic(t, h, http.MethodPost, "/academy/app-config", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST app-config = %d, want 405", rec.Code)
	}
}

// TestAppConfig_FailClosed: outside dev a missing limiter is 503.
func TestAppConfig_FailClosed(t *testing.T) {
	s := newTestServer(false)
	s.AppEnv = "production"
	s.TermsURL = "https://elmetracademy.app/terms"
	s.PrivacyURL = "https://elmetracademy.app/privacy"
	h := s.PublicHandler()
	rec := doPublic(t, h, http.MethodGet, "/academy/app-config", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("production app-config without limiter = %d, want 503", rec.Code)
	}
}

// TestSubjectDetail_WhatsAppURLPresentWhenPending (F-UX2 A8): the detail
// carries the same whatsapp_url as the access-request response.
func TestSubjectDetail_WhatsAppURLPresentWhenPending(t *testing.T) {
	s := newTestServer(false)
	ctx := context.Background()
	now := time.Now().UTC()
	subj := &models.Subject{
		ID:              "subj-a8-1",
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة",
		TitleEn:         "Subject",
		Status:          models.StatusPublished,
		AccessExpiresAt: now.Add(30 * 24 * time.Hour),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.Store.CreateSubject(ctx, subj); err != nil {
		t.Fatalf("create subject: %v", err)
	}
	h := s.PublicHandler()
	student := makeStudentToken(t, "a8-student")

	// No pending request: no whatsapp_url key at all.
	rec := doPublic(t, h, http.MethodGet, "/academy/subjects/subj-a8-1", student)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail = %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "whatsapp_url") {
		t.Fatalf("whatsapp_url present without pending request: %s", rec.Body.String())
	}

	// Create the pending request.
	req := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-a8-1/access-request", nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+student)
	arec := httptest.NewRecorder()
	h.ServeHTTP(arec, req)
	if arec.Code != http.StatusOK {
		t.Fatalf("access-request = %d (%s)", arec.Code, arec.Body.String())
	}
	var ar models.AccessRequestResponseDTO
	if err := json.Unmarshal(arec.Body.Bytes(), &ar); err != nil {
		t.Fatalf("decode access-request: %v", err)
	}

	// Detail now carries the same value.
	rec = doPublic(t, h, http.MethodGet, "/academy/subjects/subj-a8-1", student)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail = %d (%s)", rec.Code, rec.Body.String())
	}
	var detail models.SubjectDetailDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Request == nil || detail.Request.Status != models.RequestStatusPending {
		t.Fatalf("request missing: %s", rec.Body.String())
	}
	if detail.Request.WhatsappURL != ar.WhatsAppURL || detail.Request.WhatsappURL == "" {
		t.Fatalf("detail whatsapp_url = %q, request value = %q", detail.Request.WhatsappURL, ar.WhatsAppURL)
	}
	assertNoForbiddenStoreWords(t, "subject-detail-a8", rec.Body.String())
	assertNoForbiddenStudentFields(t, "subject-detail-a8", rec.Body.Bytes())
}

// TestWhatsAppURL_HTTPSOnly: an http support URL never reaches students.
func TestWhatsAppURL_HTTPSOnly(t *testing.T) {
	s := newTestServer(false)
	s.SupportWhatsApp = "http://wa.me/201000000000"
	s.TermsURL = "https://elmetracademy.app/terms"
	s.PrivacyURL = "https://elmetracademy.app/privacy"
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.Store.CreateSubject(ctx, &models.Subject{
		ID: "subj-a8-http", LevelKey: "bachelor-y1", Term: "first",
		TitleAr: "مادة", TitleEn: "Subject", Status: models.StatusPublished,
		AccessExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create subject: %v", err)
	}
	h := s.PublicHandler()
	student := makeStudentToken(t, "a8-http-student")

	req := httptest.NewRequest(http.MethodPost, "/academy/subjects/subj-a8-http/access-request", nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+student)
	arec := httptest.NewRecorder()
	h.ServeHTTP(arec, req)
	if arec.Code != http.StatusOK {
		t.Fatalf("access-request = %d", arec.Code)
	}
	var ar models.AccessRequestResponseDTO
	if err := json.Unmarshal(arec.Body.Bytes(), &ar); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ar.WhatsAppURL != "" {
		t.Fatalf("http whatsapp leaked into access-request: %q", ar.WhatsAppURL)
	}
	rec := doPublic(t, h, http.MethodGet, "/academy/subjects/subj-a8-http", student)
	if strings.Contains(rec.Body.String(), "whatsapp_url") {
		t.Fatalf("http whatsapp leaked into detail: %s", rec.Body.String())
	}
	crec := doPublic(t, h, http.MethodGet, "/academy/app-config", "")
	if strings.Contains(crec.Body.String(), "http://") {
		t.Fatalf("http whatsapp leaked into app-config: %s", crec.Body.String())
	}
}

// TestAppConfig_ShowPricesAndCenterAfterSave verifies that saving settings
// with show_prices: true, custom whatsapp, and center information is reflected
// in GET /academy/app-config.
func TestAppConfig_ShowPricesAndCenterAfterSave(t *testing.T) {
	s := appConfigTestServer()
	ctx := context.Background()
	now := time.Now().UTC()
	toSave := &models.AppSettings{
		ShowPrices:      true,
		SupportWhatsApp: "201555555555",
		CenterNameAr:    "مركز النور التعليمي",
		CenterNameEn:    "Al-Nour Educational Center",
		CenterAddressAr: "١٢ شارع التحرير، الدقي، الجيزة",
		CenterAddressEn: "12 Tahrir St, Dokki, Giza",
		CenterHoursAr:   "السبت إلى الخميس: ٩ ص - ٩ م",
		CenterHoursEn:   "Sat-Thu: 9 AM - 9 PM",
		CenterMapURL:    "https://maps.google.com/?q=dokki",
		UpdatedAt:       now,
		UpdatedBy:       "admin-123",
	}
	if err := s.Store.SaveAppSettings(ctx, toSave); err != nil {
		t.Fatalf("SaveAppSettings: %v", err)
	}

	h := s.PublicHandler()
	rec := doPublic(t, h, http.MethodGet, "/academy/app-config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("app-config = %d (%s)", rec.Code, rec.Body.String())
	}
	var cfg models.AppConfigDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode DTO: %v", err)
	}

	if !cfg.ShowPrices {
		t.Errorf("show_prices = false, want true after save")
	}
	if cfg.SupportWhatsAppURL != "https://wa.me/201555555555" {
		t.Errorf("support_whatsapp_url = %q, want https://wa.me/201555555555", cfg.SupportWhatsAppURL)
	}
	if cfg.Center == nil {
		t.Fatalf("center is nil, expected populated center info")
	}
	if cfg.Center.Name.Ar != "مركز النور التعليمي" || cfg.Center.Name.En != "Al-Nour Educational Center" {
		t.Errorf("center name mismatch: %+v", cfg.Center.Name)
	}
	if cfg.Center.Address.Ar != "١٢ شارع التحرير، الدقي، الجيزة" || cfg.Center.Address.En != "12 Tahrir St, Dokki, Giza" {
		t.Errorf("center address mismatch: %+v", cfg.Center.Address)
	}
	if cfg.Center.Hours.Ar != "السبت إلى الخميس: ٩ ص - ٩ م" || cfg.Center.Hours.En != "Sat-Thu: 9 AM - 9 PM" {
		t.Errorf("center hours mismatch: %+v", cfg.Center.Hours)
	}
	if cfg.Center.MapURL != "https://maps.google.com/?q=dokki" {
		t.Errorf("center map_url = %q", cfg.Center.MapURL)
	}

	assertNoForbiddenStoreWords(t, "app-config-saved", strings.ReplaceAll(rec.Body.String(), "show_prices", ""))
}

// TestAppConfig_CenterExactShape pins the public app-config JSON that the
// app parses (frontend AppConfigData / CenterInfo): show_prices is always a
// boolean; center is {"name":{"ar","en"},"address":{"ar","en"},
// "hours":{"ar","en"},"map_url":"https://..."} with empty languages and
// empty keys omitted, and center itself omitted when nothing is set.
func TestAppConfig_CenterExactShape(t *testing.T) {
	cases := []struct {
		name       string
		settings   *models.AppSettings
		showPrices bool
		center     string // exact JSON of "center"; "" means the key is absent
	}{
		{
			name:     "nothing set",
			settings: nil,
		},
		{
			name: "everything set",
			settings: &models.AppSettings{
				ShowPrices: true, SupportWhatsApp: "201555555555",
				CenterNameAr: "مركز", CenterNameEn: "Center",
				CenterAddressAr: "عنوان", CenterAddressEn: "Address",
				CenterHoursAr: "مواعيد", CenterHoursEn: "Hours",
				CenterMapURL: "https://maps.example/x",
			},
			showPrices: true,
			center:     `{"name":{"ar":"مركز","en":"Center"},"address":{"ar":"عنوان","en":"Address"},"hours":{"ar":"مواعيد","en":"Hours"},"map_url":"https://maps.example/x"}`,
		},
		{
			name: "one language and a map only",
			settings: &models.AppSettings{
				SupportWhatsApp: "201555555555",
				CenterNameAr:    "مركز",
				CenterHoursEn:   "Hours",
				CenterMapURL:    "https://maps.example/x",
			},
			center: `{"name":{"ar":"مركز"},"hours":{"en":"Hours"},"map_url":"https://maps.example/x"}`,
		},
		{
			name:     "map only",
			settings: &models.AppSettings{SupportWhatsApp: "201555555555", CenterMapURL: "https://maps.example/x"},
			center:   `{"map_url":"https://maps.example/x"}`,
		},
		{
			name:       "show_prices true, no center",
			settings:   &models.AppSettings{ShowPrices: true, SupportWhatsApp: "201555555555"},
			showPrices: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := appConfigTestServer()
			if tc.settings != nil {
				if err := s.Store.SaveAppSettings(context.Background(), tc.settings); err != nil {
					t.Fatal(err)
				}
			}
			rec := doPublic(t, s.PublicHandler(), http.MethodGet, "/academy/app-config", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("app-config = %d (%s)", rec.Code, rec.Body.String())
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			sp, ok := raw["show_prices"]
			if !ok {
				t.Fatalf("show_prices missing: %s", rec.Body.String())
			}
			if want := strconv.FormatBool(tc.showPrices); string(sp) != want {
				t.Fatalf("show_prices = %s, want boolean %s", sp, want)
			}
			got, present := raw["center"]
			if tc.center == "" {
				if present {
					t.Fatalf("center present, want omitted: %s", got)
				}
				return
			}
			if !present {
				t.Fatalf("center missing: %s", rec.Body.String())
			}
			if string(got) != tc.center {
				t.Fatalf("center =\n%s\nwant\n%s", got, tc.center)
			}
		})
	}
}

type errSettingsStore struct {
	store.Store
}

func (errSettingsStore) GetAppSettings(context.Context) (*models.AppSettings, error) {
	return nil, errors.New("settings store down")
}

// TestAppConfig_StoreErrorFailsClosed: when the settings cannot be read the
// public config answers 503 with no body fields, never a config built from
// env defaults that could show prices or a stale center.
func TestAppConfig_StoreErrorFailsClosed(t *testing.T) {
	s := appConfigTestServer()
	s.Store = errSettingsStore{Store: s.Store}
	rec := doPublic(t, s.PublicHandler(), http.MethodGet, "/academy/app-config", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("app-config with a failing settings store = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	for _, k := range []string{"show_prices", "support_whatsapp_url", "center"} {
		if strings.Contains(rec.Body.String(), k) {
			t.Fatalf("503 body leaks %q: %s", k, rec.Body.String())
		}
	}
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "public") {
		t.Fatalf("503 is cacheable: %q", cc)
	}
}
