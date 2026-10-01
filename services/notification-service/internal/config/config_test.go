package config

import (
	"os"
	"strings"
	"testing"
)

func fullProdEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("GATEWAY_SECRET", "test-gateway-secret")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("REDIS_URI", "redis://localhost:6379")
	t.Setenv("TLS_CERT_PATH", "/tmp/cert.pem")
	t.Setenv("TLS_KEY_PATH", "/tmp/key.pem")
	t.Setenv("TLS_CA_PATH", "/tmp/ca.pem")
}

func TestLoad_MinimalDev(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("GATEWAY_SECRET", "test-gateway-secret")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
	_ = os.Unsetenv("MONGO_URI")
	_ = os.Unsetenv("REDIS_URI")
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
			fullProdEnv(t)
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
		for _, v := range []string{"MONGO_URI", "REDIS_URI", "TLS_CERT_PATH", "TLS_KEY_PATH", "TLS_CA_PATH"} {
			_ = os.Unsetenv(v)
		}
		t.Setenv("APP_ENV", "local")
		t.Setenv("JWT_SECRET", "test-jwt-secret")
		t.Setenv("GATEWAY_SECRET", "test-gateway-secret")
		t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("expected Load to succeed with local dev defaults, got: %v", err)
		}
		if cfg.Port != "3004" {
			t.Errorf("expected default Port 3004, got %q", cfg.Port)
		}
		if cfg.TLSEnabled() {
			t.Error("expected TLS disabled by default in local dev")
		}
		if cfg.MongoDatabase != "notification_db" {
			t.Errorf("expected default MongoDatabase notification_db, got %q", cfg.MongoDatabase)
		}
		if cfg.StreamMaxConcurrent != 3 {
			t.Errorf("expected default StreamMaxConcurrent 3, got %d", cfg.StreamMaxConcurrent)
		}
		if cfg.StreamOpenRateLimit != 10 {
			t.Errorf("expected default StreamOpenRateLimit 10, got %d", cfg.StreamOpenRateLimit)
		}
	})
}

func TestLoad_StreamCapsConfig(t *testing.T) {
	setBaseDev := func(t *testing.T) {
		t.Helper()
		t.Setenv("APP_ENV", "local")
		t.Setenv("JWT_SECRET", "test-jwt-secret")
		t.Setenv("GATEWAY_SECRET", "test-gateway-secret")
		t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
		_ = os.Unsetenv("STREAM_MAX_CONCURRENT")
		_ = os.Unsetenv("STREAM_OPEN_RATE_LIMIT")
		_ = os.Unsetenv("NOTIFICATION_STREAM_MAX_CONCURRENT")
		_ = os.Unsetenv("NOTIFICATION_STREAM_OPEN_RATE_LIMIT")
	}

	t.Run("defaults", func(t *testing.T) {
		setBaseDev(t)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.StreamMaxConcurrent != 3 {
			t.Errorf("StreamMaxConcurrent = %d, want 3", cfg.StreamMaxConcurrent)
		}
		if cfg.StreamOpenRateLimit != 10 {
			t.Errorf("StreamOpenRateLimit = %d, want 10", cfg.StreamOpenRateLimit)
		}
	})

	t.Run("custom_via_STREAM_vars", func(t *testing.T) {
		setBaseDev(t)
		t.Setenv("STREAM_MAX_CONCURRENT", "5")
		t.Setenv("STREAM_OPEN_RATE_LIMIT", "25")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.StreamMaxConcurrent != 5 {
			t.Errorf("StreamMaxConcurrent = %d, want 5", cfg.StreamMaxConcurrent)
		}
		if cfg.StreamOpenRateLimit != 25 {
			t.Errorf("StreamOpenRateLimit = %d, want 25", cfg.StreamOpenRateLimit)
		}
	})

	t.Run("custom_via_NOTIFICATION_STREAM_vars", func(t *testing.T) {
		setBaseDev(t)
		t.Setenv("NOTIFICATION_STREAM_MAX_CONCURRENT", "7")
		t.Setenv("NOTIFICATION_STREAM_OPEN_RATE_LIMIT", "30")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.StreamMaxConcurrent != 7 {
			t.Errorf("StreamMaxConcurrent = %d, want 7", cfg.StreamMaxConcurrent)
		}
		if cfg.StreamOpenRateLimit != 30 {
			t.Errorf("StreamOpenRateLimit = %d, want 30", cfg.StreamOpenRateLimit)
		}
	})

	t.Run("invalid_STREAM_MAX_CONCURRENT", func(t *testing.T) {
		for _, invalid := range []string{"0", "-1", "abc"} {
			setBaseDev(t)
			t.Setenv("STREAM_MAX_CONCURRENT", invalid)
			if _, err := Load(); err == nil {
				t.Errorf("expected error for STREAM_MAX_CONCURRENT=%q", invalid)
			}
		}
	})

	t.Run("invalid_STREAM_OPEN_RATE_LIMIT", func(t *testing.T) {
		for _, invalid := range []string{"0", "-5", "xyz"} {
			setBaseDev(t)
			t.Setenv("STREAM_OPEN_RATE_LIMIT", invalid)
			if _, err := Load(); err == nil {
				t.Errorf("expected error for STREAM_OPEN_RATE_LIMIT=%q", invalid)
			}
		}
	})
}
