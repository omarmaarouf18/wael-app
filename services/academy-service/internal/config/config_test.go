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
	_ = os.Unsetenv("AUTH_ADMIN_URL")
	_ = os.Unsetenv("NOTIFICATION_SERVICE_URL")
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
	setEnv(t, "AUTH_ADMIN_URL", "https://auth-service:9001")
	setEnv(t, "NOTIFICATION_SERVICE_URL", "https://notification-service:3004")
	setEnv(t, "ADMIN_LISTEN_ADDR", ":9002")
	setEnv(t, "JWT_SECRET", "test-jwt-secret")
	setEnv(t, "REDIS_URI", "redis://localhost:6379")
	setEnv(t, "SUPPORT_WHATSAPP", "+201000000000")
	setEnv(t, "TERMS_URL", "https://elmetracademy.app/terms")
	setEnv(t, "PRIVACY_URL", "https://elmetracademy.app/privacy")
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
	if cfg.AuthAdminURL != "https://auth-service:9001" {
		t.Fatalf("authAdminURL = %q, want https://auth-service:9001", cfg.AuthAdminURL)
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
		"AUTH_ADMIN_URL",
		"NOTIFICATION_SERVICE_URL",
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

	t.Run("notification_url_must_use_https_in_production", func(t *testing.T) {
		fullProdEnv(t)
		setEnv(t, "NOTIFICATION_SERVICE_URL", "http://notification-service:3004")
		_, err := Load()
		if err == nil {
			t.Fatal("expected error for http NOTIFICATION_SERVICE_URL in production, got nil")
		}
		if !strings.Contains(err.Error(), "must use https") {
			t.Fatalf("expected error to mention 'must use https', got %v", err)
		}
	})

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
		for _, v := range []string{"MONGO_URI", "TLS_CERT_PATH", "TLS_KEY_PATH", "TLS_CA_PATH", "AUTH_SERVICE_URL", "AUTH_ADMIN_URL", "ADMIN_LISTEN_ADDR", "JWT_SECRET", "REDIS_URI", "SUPPORT_WHATSAPP"} {
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
		if cfg.AuthAdminURL != "https://auth-service:9001" {
			t.Errorf("expected default AuthAdminURL https://auth-service:9001, got %q", cfg.AuthAdminURL)
		}
		if cfg.RateLimitRead != 120 {
			t.Errorf("expected default RateLimitRead 120, got %d", cfg.RateLimitRead)
		}
		if cfg.RateLimitPlay != 60 {
			t.Errorf("expected default RateLimitPlay 60, got %d", cfg.RateLimitPlay)
		}
		if cfg.RateLimitDownload != 10 {
			t.Errorf("expected default RateLimitDownload 10, got %d", cfg.RateLimitDownload)
		}
		if cfg.RateLimitWrite != 5 {
			t.Errorf("expected default RateLimitWrite 5, got %d", cfg.RateLimitWrite)
		}
	})
}

func TestLoad_RateLimitTiers(t *testing.T) {
	t.Run("custom_valid_limits", func(t *testing.T) {
		baseEnv(t)
		setEnv(t, "RATE_LIMIT_READ", "60")
		setEnv(t, "RATE_LIMIT_PLAY", "45")
		setEnv(t, "RATE_LIMIT_DOWNLOAD", "20")
		setEnv(t, "RATE_LIMIT_WRITE", "15")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.RateLimitRead != 60 {
			t.Errorf("RateLimitRead = %d, want 60", cfg.RateLimitRead)
		}
		if cfg.RateLimitPlay != 45 {
			t.Errorf("RateLimitPlay = %d, want 45", cfg.RateLimitPlay)
		}
		if cfg.RateLimitDownload != 20 {
			t.Errorf("RateLimitDownload = %d, want 20", cfg.RateLimitDownload)
		}
		if cfg.RateLimitWrite != 15 {
			t.Errorf("RateLimitWrite = %d, want 15", cfg.RateLimitWrite)
		}
	})

	invalidCases := []struct {
		envVar string
		value  string
	}{
		{"RATE_LIMIT_READ", "invalid"},
		{"RATE_LIMIT_READ", "0"},
		{"RATE_LIMIT_READ", "-5"},
		{"RATE_LIMIT_PLAY", "invalid"},
		{"RATE_LIMIT_PLAY", "0"},
		{"RATE_LIMIT_PLAY", "-5"},
		{"RATE_LIMIT_DOWNLOAD", "abc"},
		{"RATE_LIMIT_DOWNLOAD", "0"},
		{"RATE_LIMIT_DOWNLOAD", "-1"},
		{"RATE_LIMIT_WRITE", "xyz"},
		{"RATE_LIMIT_WRITE", "0"},
		{"RATE_LIMIT_WRITE", "-10"},
	}

	for _, tc := range invalidCases {
		t.Run("invalid_"+tc.envVar+"="+tc.value, func(t *testing.T) {
			baseEnv(t)
			setEnv(t, tc.envVar, tc.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error for %s=%q, got nil", tc.envVar, tc.value)
			}
			if !strings.Contains(err.Error(), tc.envVar) {
				t.Fatalf("expected error to contain %q, got %q", tc.envVar, err.Error())
			}
		})
	}
}

func TestLoad_AppConfigURLs(t *testing.T) {
	baseEnv(t)
	_ = os.Unsetenv("TERMS_URL")
	_ = os.Unsetenv("PRIVACY_URL")
	_ = os.Unsetenv("MIN_VERSION")
	_ = os.Unsetenv("LATEST_VERSION")
	_ = os.Unsetenv("UPDATE_URL")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("dev Load without app-config env: %v", err)
	}
	if cfg.TermsURL != "" || cfg.PrivacyURL != "" {
		t.Fatalf("expected empty terms/privacy in dev, got %q %q", cfg.TermsURL, cfg.PrivacyURL)
	}
	if cfg.MinVersion != "" || cfg.LatestVersion != "" || cfg.UpdateURL != "" {
		t.Fatalf("expected empty versions in dev, got %+v", cfg)
	}

	setEnv(t, "TERMS_URL", "https://elmetracademy.app/terms")
	setEnv(t, "PRIVACY_URL", "https://elmetracademy.app/privacy")
	setEnv(t, "MIN_VERSION", "1.4.0")
	setEnv(t, "LATEST_VERSION", "1.5.0")
	setEnv(t, "UPDATE_URL", "https://elmetracademy.app/app")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("dev Load with app-config env: %v", err)
	}
	if cfg.TermsURL != "https://elmetracademy.app/terms" || cfg.PrivacyURL != "https://elmetracademy.app/privacy" {
		t.Fatalf("terms/privacy = %q %q", cfg.TermsURL, cfg.PrivacyURL)
	}
	if cfg.MinVersion != "1.4.0" || cfg.LatestVersion != "1.5.0" || cfg.UpdateURL != "https://elmetracademy.app/app" {
		t.Fatalf("versions = %+v", cfg)
	}
}

func TestLoad_AppConfigHTTPSOnly(t *testing.T) {
	for _, v := range []struct{ key, val string }{
		{"TERMS_URL", "http://elmetracademy.app/terms"},
		{"PRIVACY_URL", "http://elmetracademy.app/privacy"},
		{"UPDATE_URL", "http://elmetracademy.app/app"},
	} {
		t.Run(v.key, func(t *testing.T) {
			baseEnv(t)
			setEnv(t, "TERMS_URL", "https://elmetracademy.app/terms")
			setEnv(t, "PRIVACY_URL", "https://elmetracademy.app/privacy")
			setEnv(t, v.key, v.val)
			if _, err := Load(); err == nil {
				t.Fatalf("expected https-only error for %s=%q", v.key, v.val)
			}
		})
	}
}

func TestLoad_AppConfigRequiredOutsideDev(t *testing.T) {
	fullProdEnv(t)
	_ = os.Unsetenv("TERMS_URL")
	_ = os.Unsetenv("PRIVACY_URL")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing TERMS_URL/PRIVACY_URL in production")
	}
	setEnv(t, "TERMS_URL", "https://elmetracademy.app/terms")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing PRIVACY_URL in production")
	}
}
