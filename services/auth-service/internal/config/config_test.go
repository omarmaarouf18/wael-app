package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Shared-secret fixtures: exactly the 32-byte floor that
// secret strength checks require outside local/test, built at runtime.
var (
	testJWTSecret     = strings.Repeat("j", 32)
	testGatewaySecret = strings.Repeat("g", 32)
	testInternalToken = strings.Repeat("i", 32)
)

func setEnv(t *testing.T, k, v string) {
	t.Helper()
	if err := os.Setenv(k, v); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(k) })
}

func baseEnv(t *testing.T) {
	t.Helper()
	setEnv(t, "APP_ENV", "local")
	setEnv(t, "JWT_SECRET", testJWTSecret)
	setEnv(t, "GATEWAY_SECRET", testGatewaySecret)
	setEnv(t, "INTERNAL_SERVICE_TOKEN", testInternalToken)
	_ = os.Unsetenv("MONGO_URI")
	_ = os.Unsetenv("REDIS_URI")
	_ = os.Unsetenv("TLS_CERT_PATH")
	_ = os.Unsetenv("TLS_KEY_PATH")
	_ = os.Unsetenv("TLS_CA_PATH")
	_ = os.Unsetenv("RESEND_API_KEY")
	_ = os.Unsetenv("RESEND_FROM_EMAIL")
}

func fullProdEnv(t *testing.T) {
	t.Helper()
	setEnv(t, "APP_ENV", "production")
	setEnv(t, "JWT_SECRET", testJWTSecret)
	setEnv(t, "GATEWAY_SECRET", testGatewaySecret)
	setEnv(t, "INTERNAL_SERVICE_TOKEN", testInternalToken)
	setEnv(t, "MONGO_URI", "mongodb://localhost:27017")
	setEnv(t, "REDIS_URI", "redis://localhost:6379")
	setEnv(t, "TLS_CERT_PATH", "/tmp/cert.pem")
	setEnv(t, "TLS_KEY_PATH", "/tmp/key.pem")
	setEnv(t, "TLS_CA_PATH", "/tmp/ca.pem")
	setEnv(t, "RESEND_API_KEY", "re_test_12345")
	setEnv(t, "RESEND_FROM_EMAIL", "noreply@example.com")
	setEnv(t, "BLOCKLIST_HMAC_KEY", "test-blocklist-hmac-key")
	setEnv(t, "ADMIN_LISTEN_ADDR", ":9001")
}

func TestLoad_MinimalDev(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Port != "3002" {
		t.Fatalf("port = %q", cfg.Port)
	}
	if cfg.DefaultPhoneRegion != "EG" {
		t.Fatalf("defaultPhoneRegion = %q, want EG", cfg.DefaultPhoneRegion)
	}
	if cfg.TLSEnabled() {
		t.Fatal("expected TLS disabled without cert paths")
	}
}

func TestLoad_MissingJWTSecret(t *testing.T) {
	baseEnv(t)
	_ = os.Unsetenv("JWT_SECRET")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing JWT_SECRET")
	}
}

func TestLoad_ResendKeyRequiresFrom(t *testing.T) {
	baseEnv(t)
	setEnv(t, "RESEND_API_KEY", "re_test")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for RESEND_API_KEY without RESEND_FROM_EMAIL")
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
			fullProdEnv(t)
			if tc.envVal == "" {
				_ = os.Unsetenv("APP_ENV")
			} else {
				setEnv(t, "APP_ENV", tc.envVal)
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

func TestLoad_RequiredVariablesTable(t *testing.T) {
	requiredVars := []string{
		"JWT_SECRET",
		"GATEWAY_SECRET",
		"INTERNAL_SERVICE_TOKEN",
		"MONGO_URI",
		"REDIS_URI",
		"TLS_CERT_PATH",
		"TLS_KEY_PATH",
		"TLS_CA_PATH",
		"RESEND_API_KEY",
		"RESEND_FROM_EMAIL",
		"BLOCKLIST_HMAC_KEY",
		"ADMIN_LISTEN_ADDR",
	}

	for _, v := range requiredVars {
		t.Run("missing_"+v+"_in_production", func(t *testing.T) {
			fullProdEnv(t)
			_ = os.Unsetenv(v)
			_, err := Load()
			if err == nil {
				t.Fatalf("expected error for empty %s in production, got nil", v)
			}
			if !strings.Contains(err.Error(), v) {
				t.Fatalf("expected error to contain %q, got %q", v, err.Error())
			}
		})
	}

	t.Run("all_required_unset_default_production", func(t *testing.T) {
		for _, v := range append(requiredVars, "APP_ENV") {
			_ = os.Unsetenv(v)
		}
		_, err := Load()
		if err == nil {
			t.Fatal("expected error when all variables unset with default production, got nil")
		}
		if !strings.Contains(err.Error(), "JWT_SECRET") {
			t.Fatalf("expected error to name first missing variable JWT_SECRET, got %q", err.Error())
		}
	})

	t.Run("local_dev_with_only_secrets_succeeds_with_defaults", func(t *testing.T) {
		for _, v := range []string{"MONGO_URI", "REDIS_URI", "TLS_CERT_PATH", "TLS_KEY_PATH", "TLS_CA_PATH", "RESEND_API_KEY", "RESEND_FROM_EMAIL", "BLOCKLIST_HMAC_KEY"} {
			_ = os.Unsetenv(v)
		}
		setEnv(t, "APP_ENV", "local")
		setEnv(t, "JWT_SECRET", testJWTSecret)
		setEnv(t, "GATEWAY_SECRET", testGatewaySecret)
		setEnv(t, "INTERNAL_SERVICE_TOKEN", testInternalToken)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("expected Load to succeed with local dev defaults, got: %v", err)
		}
		if cfg.Port != "3002" {
			t.Errorf("expected default Port 3002, got %q", cfg.Port)
		}
		if cfg.DefaultPhoneRegion != "EG" {
			t.Errorf("expected default DefaultPhoneRegion EG, got %q", cfg.DefaultPhoneRegion)
		}
		if cfg.TLSEnabled() {
			t.Error("expected TLS disabled by default in local dev")
		}
		if cfg.MongoDatabase != "auth_db" {
			t.Errorf("expected default MongoDatabase auth_db, got %q", cfg.MongoDatabase)
		}
		if cfg.AdminListenAddr != ":9001" {
			t.Errorf("expected default AdminListenAddr :9001, got %q", cfg.AdminListenAddr)
		}
	})
}

// Weak shared secrets are refused outside APP_ENV=local|test (review P1);
// the full rule table is secretcheck.TestCheck.
func TestLoad_WeakSecretsRefusedOutsideLocal(t *testing.T) {
	weak := []string{
		"short-secret",
		"PASTE_64_HEX" + strings.Repeat("a", 32),
		strings.Repeat("b", 32) + "CHANGE_ME",
		strings.Repeat("c", 32) + "devpassword123",
	}
	for _, name := range []string{"JWT_SECRET", "GATEWAY_SECRET", "INTERNAL_SERVICE_TOKEN"} {
		for _, value := range weak {
			fullProdEnv(t)
			t.Setenv(name, value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("%s=%q in production: err = %v, want refusal naming %s", name, value, err, name)
			}
			if strings.Contains(err.Error(), value) {
				t.Fatalf("error leaks the secret value: %v", err)
			}
			t.Setenv("APP_ENV", "local")
			if _, err := Load(); err != nil {
				t.Fatalf("%s=%q in local: unexpected error %v", name, value, err)
			}
		}
	}
}

func TestLoad_JWTAccessTTL(t *testing.T) {
	cases := []struct {
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{"", 24 * time.Hour, false}, // unset keeps the 24h default
		{"5m", 5 * time.Minute, false},
		{"15m", 15 * time.Minute, false},
		{"24h", 24 * time.Hour, false},
		{"4m59s", 0, true},
		{"24h1s", 0, true},
		{"0", 0, true},
		{"-1h", 0, true},
		{"900", 0, true}, // a bare number is not a Go duration
		{"1d", 0, true},
	}
	for _, tc := range cases {
		fullProdEnv(t)
		if tc.raw == "" {
			_ = os.Unsetenv("JWT_ACCESS_TTL")
		} else {
			t.Setenv("JWT_ACCESS_TTL", tc.raw)
		}
		cfg, err := Load()
		if tc.wantErr {
			if err == nil || !strings.Contains(err.Error(), "JWT_ACCESS_TTL") {
				t.Fatalf("JWT_ACCESS_TTL=%q: err = %v, want refusal", tc.raw, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("JWT_ACCESS_TTL=%q: %v", tc.raw, err)
		}
		if cfg.JWTAccessTTL != tc.want {
			t.Fatalf("JWT_ACCESS_TTL=%q: got %v, want %v", tc.raw, cfg.JWTAccessTTL, tc.want)
		}
	}
}
