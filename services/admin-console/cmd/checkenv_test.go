package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func setProdEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("PORT", "")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
	t.Setenv("AUTH_ADMIN_URL", "https://auth-service:9001")
	t.Setenv("ACADEMY_ADMIN_URL", "https://academy-service:9002")
	t.Setenv("TLS_CERT_PATH", "/tmp/cert.pem")
	t.Setenv("TLS_KEY_PATH", "/tmp/key.pem")
	t.Setenv("TLS_CA_PATH", "/tmp/ca.pem")
	t.Setenv("TRUSTED_PROXY_IPS", "172.30.0.10")
}

func TestRunCheckEnv_ProductionOK(t *testing.T) {
	setProdEnv(t)
	var stdout, stderr bytes.Buffer
	if code := runCheckEnv(&stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "check-env: ok") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunCheckEnv_ProductionMissingVarsTable(t *testing.T) {
	for _, name := range []string{
		"INTERNAL_SERVICE_TOKEN", "AUTH_ADMIN_URL", "ACADEMY_ADMIN_URL",
		"TLS_CERT_PATH", "TLS_KEY_PATH", "TLS_CA_PATH", "TRUSTED_PROXY_IPS",
	} {
		t.Run("missing_"+name, func(t *testing.T) {
			setProdEnv(t)
			_ = os.Unsetenv(name)
			var stdout, stderr bytes.Buffer
			if code := runCheckEnv(&stdout, &stderr); code != 1 {
				t.Fatalf("expected exit 1, got %d", code)
			}
			if !strings.Contains(stderr.String(), name) {
				t.Fatalf("stderr should name %s: %s", name, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout should be empty on failure: %q", stdout.String())
			}
		})
	}
}

func TestRunCheckEnv_FailsOnEmptySecretEvenInLocal(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "")
	var stdout, stderr bytes.Buffer
	if code := runCheckEnv(&stdout, &stderr); code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "INTERNAL_SERVICE_TOKEN") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunCheckEnv_LocalOK(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "x")
	for _, v := range []string{"AUTH_ADMIN_URL", "ACADEMY_ADMIN_URL", "TLS_CERT_PATH", "TLS_KEY_PATH", "TLS_CA_PATH", "TRUSTED_PROXY_IPS", "PORT"} {
		t.Setenv(v, "")
	}
	var stdout, stderr bytes.Buffer
	if code := runCheckEnv(&stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
}
