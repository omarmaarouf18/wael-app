package jwtutil

import (
	"strings"
	"testing"
)

func TestCheckSecretStrength(t *testing.T) {
	strong := strings.Repeat("a1", 32) // 64 hex chars, like openssl rand -hex 32
	cases := []struct {
		name    string
		value   string
		appEnv  string
		wantErr bool
	}{
		{"strong production", strong, "production", false},
		{"exactly 32 bytes", strings.Repeat("x", 32), "production", false},
		{"31 bytes", strings.Repeat("x", 31), "production", true},
		{"short, empty APP_ENV is production", "short", "", true},
		{"short, unknown APP_ENV is production", "short", "staging", true},
		{"PASTE_ marker", "PASTE_64_HEX" + strong, "production", true},
		{"paste_ lowercase", "paste_" + strong, "production", true},
		{"CHANGE_ME marker", strong + "CHANGE_ME", "production", true},
		{"devpassword123 marker", strong + "devpassword123", "production", true},
		{"DEVPASSWORD123 uppercase", strong + "DEVPASSWORD123", "production", true},
		{"short allowed in local", "short", "local", false},
		{"placeholder allowed in test", "PASTE_ME", "test", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckSecretStrength("JWT_SECRET", tc.value, tc.appEnv)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), "JWT_SECRET") {
					t.Fatalf("error does not name the variable: %v", err)
				}
				if strings.Contains(err.Error(), tc.value) {
					t.Fatalf("error leaks the secret value: %v", err)
				}
			}
		})
	}
}
