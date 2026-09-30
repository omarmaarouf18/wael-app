package config

import (
	"os"
	"testing"
)

func TestLoad_MinimalDev(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("GATEWAY_SECRET", "test-gateway-secret")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
	_ = os.Unsetenv("TLS_CERT_PATH")
	_ = os.Unsetenv("TLS_KEY_PATH")
	_ = os.Unsetenv("TLS_CA_PATH")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Port != "3004" || cfg.TLSEnabled() {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoad_MissingSecrets(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	t.Setenv("GATEWAY_SECRET", "x")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "x")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing JWT_SECRET")
	}
}

func TestLoad_AppEnvValidation(t *testing.T) {
	cases := []struct {
		envVal      string
		expectEnv   string
		expectError bool
	}{
		{"", "production", false},
		{"production", "production", false},
		{"local", "local", false},
		{"test", "test", false},
		{"staging", "", true},
		{"development", "", true},
		{"unknown", "", true},
	}

	for _, tc := range cases {
		t.Run("APP_ENV="+tc.envVal, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "test-jwt-secret")
			t.Setenv("GATEWAY_SECRET", "test-gateway-secret")
			t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
			_ = os.Unsetenv("TLS_CERT_PATH")
			_ = os.Unsetenv("TLS_KEY_PATH")
			_ = os.Unsetenv("TLS_CA_PATH")

			if tc.envVal == "" {
				_ = os.Unsetenv("APP_ENV")
			} else {
				t.Setenv("APP_ENV", tc.envVal)
			}

			cfg, err := Load()
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error for APP_ENV=%q, got nil", tc.envVal)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for APP_ENV=%q: %v", tc.envVal, err)
				}
				if cfg.AppEnv != tc.expectEnv {
					t.Errorf("AppEnv mismatch: got %q, want %q", cfg.AppEnv, tc.expectEnv)
				}
			}
		})
	}
}
