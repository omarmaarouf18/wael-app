package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func setAuthProdEnv(t *testing.T) {
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
	t.Setenv("RESEND_API_KEY", "re_test_12345")
	t.Setenv("RESEND_FROM_EMAIL", "noreply@example.com")
	t.Setenv("BLOCKLIST_HMAC_KEY", "test-blocklist-hmac-key")
}

func setAuthLocalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "local")
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("GATEWAY_SECRET", "test-gateway-secret")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
	for _, v := range []string{
		"MONGO_URI", "REDIS_URI", "TLS_CERT_PATH", "TLS_KEY_PATH",
		"TLS_CA_PATH", "RESEND_API_KEY", "RESEND_FROM_EMAIL",
		"BLOCKLIST_HMAC_KEY",
	} {
		_ = os.Unsetenv(v)
	}
}

func TestRunCheckEnv_ProductionMissingRequiredVar(t *testing.T) {
	setAuthProdEnv(t)
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
	}
	for _, v := range requiredVars {
		t.Run("missing_"+v, func(t *testing.T) {
			setAuthProdEnv(t)
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
	setAuthProdEnv(t)

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
	setAuthLocalEnv(t)

	var stdout, stderr bytes.Buffer
	code := runCheckEnv(&stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 for minimal local env, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "check-env: ok") {
		t.Fatalf("expected stdout to contain 'check-env: ok', got: %s", stdout.String())
	}
}
