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
	testJWTSecret     = strings.Repeat("j", 32)
	testGatewaySecret = strings.Repeat("g", 32)
	testInternalToken = strings.Repeat("i", 32)
)

func setNotifProdEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("GATEWAY_SECRET", testGatewaySecret)
	t.Setenv("INTERNAL_SERVICE_TOKEN", testInternalToken)
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("REDIS_URI", "redis://localhost:6379")
	t.Setenv("TLS_CERT_PATH", "/tmp/cert.pem")
	t.Setenv("TLS_KEY_PATH", "/tmp/key.pem")
	t.Setenv("TLS_CA_PATH", "/tmp/ca.pem")
}

func setNotifLocalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "local")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("GATEWAY_SECRET", testGatewaySecret)
	t.Setenv("INTERNAL_SERVICE_TOKEN", testInternalToken)
	for _, v := range []string{
		"MONGO_URI", "REDIS_URI", "TLS_CERT_PATH", "TLS_KEY_PATH", "TLS_CA_PATH",
	} {
		_ = os.Unsetenv(v)
	}
}

func TestRunCheckEnv_ProductionMissingRequiredVar(t *testing.T) {
	setNotifProdEnv(t)
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
		t.Run("missing_"+v, func(t *testing.T) {
			setNotifProdEnv(t)
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
	setNotifProdEnv(t)

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
	setNotifLocalEnv(t)

	var stdout, stderr bytes.Buffer
	code := runCheckEnv(&stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 for minimal local env, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "check-env: ok") {
		t.Fatalf("expected stdout to contain 'check-env: ok', got: %s", stdout.String())
	}
}

func TestRunCheckEnv_InvalidStreamCaps(t *testing.T) {
	setNotifLocalEnv(t)
	t.Setenv("STREAM_MAX_CONCURRENT", "-1")

	var stdout, stderr bytes.Buffer
	code := runCheckEnv(&stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid STREAM_MAX_CONCURRENT, got %d", code)
	}
	if !strings.Contains(stderr.String(), "STREAM_MAX_CONCURRENT") {
		t.Fatalf("expected stderr to contain 'STREAM_MAX_CONCURRENT', got: %s", stderr.String())
	}
}
