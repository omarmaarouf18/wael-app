package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func setAcademyProdEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("GATEWAY_SECRET", "test-gateway-secret")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("TLS_CERT_PATH", "/tmp/cert.pem")
	t.Setenv("TLS_KEY_PATH", "/tmp/key.pem")
	t.Setenv("TLS_CA_PATH", "/tmp/ca.pem")
	t.Setenv("AUTH_SERVICE_URL", "https://auth-service:3002")
	t.Setenv("AUTH_ADMIN_URL", "https://auth-service:9001")
	t.Setenv("NOTIFICATION_SERVICE_URL", "https://notification-service:3004")
	t.Setenv("ADMIN_LISTEN_ADDR", ":9002")
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("REDIS_URI", "redis://localhost:6379")
	t.Setenv("SUPPORT_WHATSAPP", "+201000000000")
	t.Setenv("TERMS_URL", "https://elmetracademy.app/terms")
	t.Setenv("PRIVACY_URL", "https://elmetracademy.app/privacy")
}

func setAcademyLocalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "local")
	t.Setenv("GATEWAY_SECRET", "test-gateway-secret")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
	for _, v := range []string{
		"MONGO_URI", "TLS_CERT_PATH", "TLS_KEY_PATH",
		"TLS_CA_PATH", "AUTH_SERVICE_URL", "AUTH_ADMIN_URL", "NOTIFICATION_SERVICE_URL", "ADMIN_LISTEN_ADDR",
		"JWT_SECRET", "REDIS_URI", "SUPPORT_WHATSAPP",
	} {
		_ = os.Unsetenv(v)
	}
}

func TestRunCheckEnv_ProductionMissingRequiredVar(t *testing.T) {
	setAcademyProdEnv(t)
	_ = os.Unsetenv("MONGO_URI")

	var stdout, stderr bytes.Buffer
	code := runCheckEnv(&stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing MONGO_URI, got %d", code)
	}
	if !strings.Contains(stderr.String(), "MONGO_URI") {
		t.Fatalf("expected stderr to contain 'MONGO_URI', got: %s", stderr.String())
	}
}

func TestRunCheckEnv_ProductionMissingVarsTable(t *testing.T) {
	requiredVars := []string{
		"GATEWAY_SECRET",
		"INTERNAL_SERVICE_TOKEN",
		"MONGO_URI",
		"TLS_CERT_PATH",
		"TLS_KEY_PATH",
		"TLS_CA_PATH",
		"AUTH_SERVICE_URL",
		"AUTH_ADMIN_URL",
		"NOTIFICATION_SERVICE_URL",
		"ADMIN_LISTEN_ADDR",
		"JWT_SECRET",
		"REDIS_URI",
		"SUPPORT_WHATSAPP",
		"TERMS_URL",
		"PRIVACY_URL",
	}
	for _, v := range requiredVars {
		t.Run("missing_"+v, func(t *testing.T) {
			setAcademyProdEnv(t)
			_ = os.Unsetenv(v)
			var stdout, stderr bytes.Buffer
			code := runCheckEnv(&stdout, &stderr)
			if code != 1 {
				t.Fatalf("expected exit code 1 for missing %s, got %d", v, code)
			}
			if !strings.Contains(stderr.String(), v) {
				t.Fatalf("expected stderr to contain %q, got: %s", v, stderr.String())
			}
		})
	}
}

func TestRunCheckEnv_ValidProduction(t *testing.T) {
	setAcademyProdEnv(t)

	var stdout, stderr bytes.Buffer
	code := runCheckEnv(&stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 for valid production env, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "check-env: ok") {
		t.Fatalf("expected stdout to contain 'check-env: ok', got: %s", stdout.String())
	}
}

func TestRunCheckEnv_LocalMinimal(t *testing.T) {
	setAcademyLocalEnv(t)

	var stdout, stderr bytes.Buffer
	code := runCheckEnv(&stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 for minimal local env, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "check-env: ok") {
		t.Fatalf("expected stdout to contain 'check-env: ok', got: %s", stdout.String())
	}
}
