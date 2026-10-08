package models

import "testing"

func TestValidWhatsAppNumber(t *testing.T) {
	cases := map[string]string{
		"201000000000":        "201000000000",
		"+201000000000":       "201000000000",
		"+20 100 000-0000":    "201000000000",
		"(20) 1000000000":     "201000000000",
		"123456789012345":     "123456789012345",
		"":                    "", // unset
		"0100000000":          "", // local form, leading 0
		"123456789":           "", // 9 digits
		"1234567890123456":    "", // 16 digits
		"PASTE_WHATSAPP":      "", // placeholder
		"+PASTE_201000000000": "",
		"https://wa.me/2010":  "", // a URL is not a number
		"20100000000a":        "",
	}
	for raw, want := range cases {
		if got := ValidWhatsAppNumber(raw); got != want {
			t.Errorf("ValidWhatsAppNumber(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestWithWhatsAppDefault(t *testing.T) {
	cases := []struct {
		name, stored, env, want string
	}{
		{"stored valid wins", "201555555555", "+201000000000", "201555555555"},
		{"stored empty, env valid", "", "+201000000000", "201000000000"},
		{"stored invalid, env valid", "0100", "+201000000000", "201000000000"},
		{"nothing valid: no fallback number", "", "PASTE_WHATSAPP", ""},
		{"both empty", "", "", ""},
	}
	for _, tc := range cases {
		stored := &AppSettings{SupportWhatsApp: tc.stored, CenterNameAr: "x"}
		got := WithWhatsAppDefault(stored, tc.env)
		if got.SupportWhatsApp != tc.want {
			t.Errorf("%s: number = %q, want %q", tc.name, got.SupportWhatsApp, tc.want)
		}
		if got.ID != AppSettingsID || got.CenterNameAr != "x" {
			t.Errorf("%s: other fields lost: %+v", tc.name, got)
		}
		if stored.SupportWhatsApp != tc.stored {
			t.Errorf("%s: input mutated", tc.name)
		}
	}
	if got := WithWhatsAppDefault(nil, ""); got == nil || got.SupportWhatsApp != "" || got.ID != AppSettingsID {
		t.Errorf("nil settings: got %+v", got)
	}
}
