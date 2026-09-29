package config

import (
	"os"
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
	setEnv(t, "GATEWAY_SECRET", "test-gateway-secret-1234567890")
	setEnv(t, "INTERNAL_SERVICE_TOKEN", "test-internal-token-1234567890")
	setEnv(t, "REDIS_URI", "redis://localhost:6379")
	setEnv(t, "AUTH_SERVICE_URL", "http://auth-service:3002")
	setEnv(t, "NOTIFICATION_SERVICE_URL", "http://notification-service:3004")
	_ = os.Unsetenv("TLS_CERT_PATH")
	_ = os.Unsetenv("TLS_KEY_PATH")
	_ = os.Unsetenv("TLS_CA_PATH")
	_ = os.Unsetenv("EXTERNAL_TLS_CERT_PATH")
	_ = os.Unsetenv("EXTERNAL_TLS_KEY_PATH")
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
	if len(cfg.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(cfg.Routes))
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
