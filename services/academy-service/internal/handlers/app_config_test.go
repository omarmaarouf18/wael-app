package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
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
	for _, k := range []string{"support_whatsapp_url", "terms_url", "privacy_url", "min_version", "latest_version", "update_url"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("missing key %q: %s", k, rec.Body.String())
		}
	}
	if len(raw) != 6 {
		t.Fatalf("unexpected keys: %s", rec.Body.String())
	}
	var cfg models.AppConfigDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode DTO: %v", err)
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
	assertNoForbiddenStoreWords(t, "app-config", rec.Body.String())
	if tok := makeStudentToken(t, "cfg-student"); true {
		rec2 := doPublic(t, h, http.MethodGet, "/academy/app-config", tok)
		if rec2.Code != http.StatusOK || rec2.Body.String() != rec.Body.String() {
			t.Fatalf("authed app-config differs: %d %s", rec2.Code, rec2.Body.String())
		}
	}
}

// TestAppConfig_EmptyOptionalsOmitted: empty versions are absent (no update prompt).
func TestAppConfig_EmptyOptionalsOmitted(t *testing.T) {
	s := newTestServer(false)
	s.TermsURL = "https://elmetracademy.app/terms"
	s.PrivacyURL = "https://elmetracademy.app/privacy"
	h := s.PublicHandler()

	rec := doPublic(t, h, http.MethodGet, "/academy/app-config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("app-config = %d (%s)", rec.Code, rec.Body.String())
	}
	for _, k := range []string{"min_version", "latest_version", "update_url"} {
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
