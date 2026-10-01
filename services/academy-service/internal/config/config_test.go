package config

import (
	"os"
	"strings"
	"testing"
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
	setEnv(t, "GATEWAY_SECRET", "test-gateway-secret")
	setEnv(t, "INTERNAL_SERVICE_TOKEN", "test-internal-token")
	_ = os.Unsetenv("MONGO_URI")
	_ = os.Unsetenv("TLS_CERT_PATH")
	_ = os.Unsetenv("TLS_KEY_PATH")
	_ = os.Unsetenv("TLS_CA_PATH")
	_ = os.Unsetenv("AUTH_SERVICE_URL")
	_ = os.Unsetenv("ADMIN_LISTEN_ADDR")
	_ = os.Unsetenv("JWT_SECRET")
	_ = os.Unsetenv("REDIS_URI")
}

func fullProdEnv(t *testing.T) {
	t.Helper()
	setEnv(t, "APP_ENV", "production")
	setEnv(t, "GATEWAY_SECRET", "test-gateway-secret")
	setEnv(t, "INTERNAL_SERVICE_TOKEN", "test-internal-token")
	setEnv(t, "MONGO_URI", "mongodb://localhost:27017")
	setEnv(t, "TLS_CERT_PATH", "/tmp/cert.pem")
	setEnv(t, "TLS_KEY_PATH", "/tmp/key.pem")
	setEnv(t, "TLS_CA_PATH", "/tmp/ca.pem")
	setEnv(t, "AUTH_SERVICE_URL", "https://auth-service:3002")
	setEnv(t, "ADMIN_LISTEN_ADDR", ":9002")
	setEnv(t, "JWT_SECRET", "test-jwt-secret")
	setEnv(t, "REDIS_URI", "redis://localhost:6379")
	setEnv(t, "SUPPORT_WHATSAPP", "+201000000000")
}

func TestLoad_MinimalDev(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Port != "3003" {
		t.Fatalf("port = %q, want 3003", cfg.Port)
	}
	if cfg.AdminListenAddr != ":9002" {
		t.Fatalf("adminListenAddr = %q, want :9002", cfg.AdminListenAddr)
	}
	if cfg.AuthServiceURL != "https://auth-service:3002" {
		t.Fatalf("authServiceURL = %q, want https://auth-service:3002", cfg.AuthServiceURL)
	}
	if cfg.MongoDatabase != "academy_db" {
		t.Fatalf("mongoDatabase = %q, want academy_db", cfg.MongoDatabase)
	}
	if cfg.TLSEnabled() {
		t.Fatal("expected TLS disabled without cert paths")
	}
	if cfg.SupportWhatsApp != "+201000000000" {
		t.Fatalf("supportWhatsApp = %q, want +201000000000", cfg.SupportWhatsApp)
	}
}

func TestLoad_MissingGatewaySecret(t *testing.T) {
	baseEnv(t)
	_ = os.Unsetenv("GATEWAY_SECRET")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing GATEWAY_SECRET")
	}
}

func TestLoad_MissingInternalToken(t *testing.T) {
	baseEnv(t)
	_ = os.Unsetenv("INTERNAL_SERVICE_TOKEN")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing INTERNAL_SERVICE_TOKEN")
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
		"GATEWAY_SECRET",
		"INTERNAL_SERVICE_TOKEN",
		"MONGO_URI",
		"TLS_CERT_PATH",
		"TLS_KEY_PATH",
		"TLS_CA_PATH",
		"ADMIN_LISTEN_ADDR",
		"AUTH_SERVICE_URL",
		"JWT_SECRET",
		"REDIS_URI",
		"SUPPORT_WHATSAPP",
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
		if !strings.Contains(err.Error(), "GATEWAY_SECRET") {
			t.Fatalf("expected error to name first missing variable GATEWAY_SECRET, got %q", err.Error())
		}
	})

	t.Run("local_dev_with_only_secrets_succeeds_with_defaults", func(t *testing.T) {
		for _, v := range []string{"MONGO_URI", "TLS_CERT_PATH", "TLS_KEY_PATH", "TLS_CA_PATH", "AUTH_SERVICE_URL", "ADMIN_LISTEN_ADDR", "JWT_SECRET", "REDIS_URI", "SUPPORT_WHATSAPP"} {
			_ = os.Unsetenv(v)
		}
		setEnv(t, "APP_ENV", "local")
		setEnv(t, "GATEWAY_SECRET", "test-gateway-secret")
		setEnv(t, "INTERNAL_SERVICE_TOKEN", "test-internal-token")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("expected Load to succeed with local dev defaults, got: %v", err)
		}
		if cfg.Port != "3003" {
			t.Errorf("expected default Port 3003, got %q", cfg.Port)
		}
		if cfg.SupportWhatsApp != "+201000000000" {
			t.Errorf("expected default SupportWhatsApp +201000000000, got %q", cfg.SupportWhatsApp)
		}
		if cfg.TLSEnabled() {
			t.Error("expected TLS disabled by default in local dev")
		}
		if cfg.MongoDatabase != "academy_db" {
			t.Errorf("expected default MongoDatabase academy_db, got %q", cfg.MongoDatabase)
		}
		if cfg.AdminListenAddr != ":9002" {
			t.Errorf("expected default AdminListenAddr :9002, got %q", cfg.AdminListenAddr)
		}
		if cfg.AuthServiceURL != "https://auth-service:3002" {
			t.Errorf("expected default AuthServiceURL https://auth-service:3002, got %q", cfg.AuthServiceURL)
		}
	})
}
