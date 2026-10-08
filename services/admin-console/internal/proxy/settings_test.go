package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func settingsRoutes() []route {
	return []route{
		{
			name: "settings.get", handler: func(p *Proxy) http.HandlerFunc { return p.SettingsGet },
			method: http.MethodGet, target: "/api/settings",
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/settings",
			upstreamStatus: 200, upstreamReply: `{"show_prices":false,"support_whatsapp":"201000000000"}`,
		},
		{
			name: "settings.update", handler: func(p *Proxy) http.HandlerFunc { return p.SettingsUpdate },
			method: http.MethodPost, target: "/api/settings/update",
			body:           `{"show_prices":true,"support_whatsapp":"201555555555","center_name_ar":"المركز"}`,
			upstreamMethod: http.MethodPut, upstreamPath: "/internal/admin/settings",
			upstreamBody:   `{"center_address_ar":"","center_address_en":"","center_hours_ar":"","center_hours_en":"","center_map_url":"","center_name_ar":"المركز","center_name_en":"","show_prices":true,"support_whatsapp":"201555555555"}`,
			upstreamStatus: 200, upstreamReply: `{"show_prices":true,"support_whatsapp":"201555555555","center_name_ar":"المركز"}`,
		},
	}
}

func TestSettingsUpdate_MissingShowPrices(t *testing.T) {
	restore := logWriter(ioDiscard{})
	defer restore()

	upstream := newUpstream(t, 200, `{"ok":true}`)
	p := newProxy(t, upstream)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/update", strings.NewReader(`{"support_whatsapp":"201000000000"}`))
	req.Header.Set(AdminTokenHeader, testToken)
	rec := httptest.NewRecorder()
	p.SettingsUpdate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
	var errBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if errBody["code"] != "bad_request" {
		t.Fatalf("code = %q, want bad_request", errBody["code"])
	}
}

func TestSettingsUpdate_DisallowUnknownFields(t *testing.T) {
	restore := logWriter(ioDiscard{})
	defer restore()

	upstream := newUpstream(t, 200, `{"ok":true}`)
	p := newProxy(t, upstream)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/update", strings.NewReader(`{"show_prices":true,"unknown_field":"value"}`))
	req.Header.Set(AdminTokenHeader, testToken)
	rec := httptest.NewRecorder()
	p.SettingsUpdate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestSettingsUpdate_RelaysUpstreamDomainErrors(t *testing.T) {
	restore := logWriter(ioDiscard{})
	defer restore()

	for _, code := range []string{"invalid_whatsapp", "settings_forbidden_word", "invalid_map_url"} {
		t.Run(code, func(t *testing.T) {
			upstream := newUpstream(t, http.StatusBadRequest, `{"error":"validation failed","code":"`+code+`"}`)
			p := newProxy(t, upstream)

			req := httptest.NewRequest(http.MethodPost, "/api/settings/update", strings.NewReader(`{"show_prices":true,"support_whatsapp":"invalid"}`))
			req.Header.Set(AdminTokenHeader, testToken)
			rec := httptest.NewRecorder()
			p.SettingsUpdate(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400", rec.Code)
			}
			var res map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Fatalf("unmarshal body: %v", err)
			}
			if res["code"] != code {
				t.Fatalf("relayed code = %q, want %q", res["code"], code)
			}
		})
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (n int, err error) { return len(p), nil }
