package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// Shared-secret fixtures: exactly the 32-byte floor that
// secret strength checks require outside local/test, built at runtime.
var (
	testGatewaySecret = strings.Repeat("g", 32)
)

func setGatewayProdEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("GATEWAY_SECRET", testGatewaySecret)
	t.Setenv("REDIS_URI", "redis://localhost:6379")
	t.Setenv("TLS_CERT_PATH", "/tmp/cert.pem")
	t.Setenv("TLS_KEY_PATH", "/tmp/key.pem")
	t.Setenv("TLS_CA_PATH", "/tmp/ca.pem")
	t.Setenv("AUTH_SERVICE_URL", "https://auth-service:3002")
	t.Setenv("NOTIFICATION_SERVICE_URL", "https://notification-service:3004")
	t.Setenv("ACADEMY_SERVICE_URL", "https://academy-service:3003")
}

func setGatewayLocalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "local")
	t.Setenv("GATEWAY_SECRET", testGatewaySecret)
	t.Setenv("REDIS_URI", "redis://localhost:6379")
	_ = os.Unsetenv("TLS_CERT_PATH")
	_ = os.Unsetenv("TLS_KEY_PATH")
	_ = os.Unsetenv("TLS_CA_PATH")
	_ = os.Unsetenv("AUTH_SERVICE_URL")
	_ = os.Unsetenv("NOTIFICATION_SERVICE_URL")
	_ = os.Unsetenv("ACADEMY_SERVICE_URL")
}

func TestRunCheckEnv_ProductionMissingRequiredVar(t *testing.T) {
	setGatewayProdEnv(t)
	_ = os.Unsetenv("REDIS_URI")

	var stdout, stderr bytes.Buffer
	code := runCheckEnv(&stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing REDIS_URI, got %d", code)
	}
	if !strings.Contains(stderr.String(), "REDIS_URI") {
		t.Fatalf("expected stderr to contain 'REDIS_URI', got: %s", stderr.String())
	}
}

func TestRunCheckEnv_ProductionMissingVarsTable(t *testing.T) {
	requiredVars := []string{
		"GATEWAY_SECRET",
		"REDIS_URI",
		"TLS_CERT_PATH",
		"TLS_KEY_PATH",
		"TLS_CA_PATH",
	}
	for _, v := range requiredVars {
		t.Run("missing_"+v, func(t *testing.T) {
			setGatewayProdEnv(t)
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
	setGatewayProdEnv(t)

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
	setGatewayLocalEnv(t)

	var stdout, stderr bytes.Buffer
	code := runCheckEnv(&stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 for minimal local env, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "check-env: ok") {
		t.Fatalf("expected stdout to contain 'check-env: ok', got: %s", stdout.String())
	}
}
