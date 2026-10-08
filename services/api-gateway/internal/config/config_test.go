package config

import (
	"os"
	"strings"
	"testing"
)

// Shared-secret fixtures: exactly the 32-byte floor that
// secret strength checks require outside local/test, built at runtime.
var (
	testGatewaySecret = strings.Repeat("g", 32)
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
	setEnv(t, "GATEWAY_SECRET", testGatewaySecret)
	setEnv(t, "REDIS_URI", "redis://localhost:6379")
	setEnv(t, "AUTH_SERVICE_URL", "http://auth-service:3002")
	setEnv(t, "NOTIFICATION_SERVICE_URL", "http://notification-service:3004")
	setEnv(t, "ACADEMY_SERVICE_URL", "http://academy-service:3003")
	_ = os.Unsetenv("TLS_CERT_PATH")
	_ = os.Unsetenv("TLS_KEY_PATH")
	_ = os.Unsetenv("TLS_CA_PATH")
	_ = os.Unsetenv("EXTERNAL_TLS_CERT_PATH")
	_ = os.Unsetenv("EXTERNAL_TLS_KEY_PATH")
}

func fullProdEnv(t *testing.T) {
	t.Helper()
	setEnv(t, "APP_ENV", "production")
	setEnv(t, "GATEWAY_SECRET", testGatewaySecret)
	setEnv(t, "REDIS_URI", "redis://localhost:6379")
	setEnv(t, "TLS_CERT_PATH", "/tmp/cert.pem")
	setEnv(t, "TLS_KEY_PATH", "/tmp/key.pem")
	setEnv(t, "TLS_CA_PATH", "/tmp/ca.pem")
	setEnv(t, "AUTH_SERVICE_URL", "https://auth-service:3002")
	setEnv(t, "NOTIFICATION_SERVICE_URL", "https://notification-service:3004")
	setEnv(t, "ACADEMY_SERVICE_URL", "https://academy-service:3003")
}

func TestLoad_HTTPDevNoTLS(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.TLSEnabled() {
		t.Fatal("expected TLS disabled when cert paths empty")
	}
	if len(cfg.Routes) != 3 {
		t.Fatalf("expected 3 routes, got %d", len(cfg.Routes))
	}
	// Each upstream has a distinct name: the gateway names one circuit
	// breaker per upstream after it.
	for i, want := range []string{"auth", "notification", "academy"} {
		if got := cfg.Routes[i].Name; got != want {
			t.Fatalf("route %d name = %q, want %q", i, got, want)
		}
	}
}

func TestLoad_MissingSecretFails(t *testing.T) {
	baseEnv(t)
	_ = os.Unsetenv("GATEWAY_SECRET")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing GATEWAY_SECRET")
	}
}

func TestLoad_MTLSRequiresHTTPS(t *testing.T) {
	baseEnv(t)
	setEnv(t, "TLS_CERT_PATH", "/tmp/x.crt")
	setEnv(t, "TLS_KEY_PATH", "/tmp/x.key")
	setEnv(t, "TLS_CA_PATH", "/tmp/ca.crt")
	setEnv(t, "AUTH_SERVICE_URL", "http://auth-service:3002")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for http target with mTLS active")
	}
}

func TestLoad_AppEnvValidation(t *testing.T) {
	cases := []struct {
		envVal      string
		expectError bool
	}{
		{"local", false},
		{"test", false},
		{"production", false},
		{"staging", true},
		{"development", true},
		{"unknown", true},
	}

	for _, tc := range cases {
		t.Run("APP_ENV="+tc.envVal, func(t *testing.T) {
			fullProdEnv(t)
			setEnv(t, "APP_ENV", tc.envVal)
			_, err := Load()
			if tc.expectError && err == nil {
				t.Fatalf("expected error for APP_ENV=%q, got nil", tc.envVal)
			}
			if !tc.expectError && err != nil {
				t.Fatalf("unexpected error for APP_ENV=%q: %v", tc.envVal, err)
			}
		})
	}
}

func TestLoad_RequiredVariablesTable(t *testing.T) {
	requiredVars := []string{
		"GATEWAY_SECRET",
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
		for _, v := range append(requiredVars, "APP_ENV", "AUTH_SERVICE_URL", "NOTIFICATION_SERVICE_URL", "ACADEMY_SERVICE_URL") {
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

	t.Run("missing_REDIS_URI_in_local", func(t *testing.T) {
		baseEnv(t)
		_ = os.Unsetenv("REDIS_URI")
		_, err := Load()
		if err == nil {
			t.Fatal("expected error for empty REDIS_URI in local, got nil")
		}
		if !strings.Contains(err.Error(), "REDIS_URI") {
			t.Fatalf("expected error to contain %q, got %q", "REDIS_URI", err.Error())
		}
	})
}

// Weak shared secrets are refused outside APP_ENV=local|test (review P1);
// the full rule table is jwtutil.TestCheckSecretStrength.
func TestLoad_WeakSecretsRefusedOutsideLocal(t *testing.T) {
	weak := []string{
		"short-secret",
		"PASTE_64_HEX" + strings.Repeat("a", 32),
		strings.Repeat("b", 32) + "CHANGE_ME",
		strings.Repeat("c", 32) + "devpassword123",
	}
	for _, name := range []string{"GATEWAY_SECRET"} {
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
