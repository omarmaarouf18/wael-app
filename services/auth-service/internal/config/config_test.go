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
	setEnv(t, "JWT_SECRET", "test-jwt-secret-0123456789abcdef")
	setEnv(t, "GATEWAY_SECRET", "test-gateway-secret")
	setEnv(t, "INTERNAL_SERVICE_TOKEN", "test-internal-token")
	_ = os.Unsetenv("TLS_CERT_PATH")
	_ = os.Unsetenv("TLS_KEY_PATH")
	_ = os.Unsetenv("TLS_CA_PATH")
	_ = os.Unsetenv("RESEND_API_KEY")
	_ = os.Unsetenv("RESEND_FROM_EMAIL")
}

func TestLoad_MinimalDev(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Port != "3002" {
		t.Fatalf("port = %q", cfg.Port)
	}
	if cfg.TLSEnabled() {
		t.Fatal("expected TLS disabled without cert paths")
	}
}

func TestLoad_MissingJWTSecret(t *testing.T) {
	baseEnv(t)
	_ = os.Unsetenv("JWT_SECRET")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing JWT_SECRET")
	}
}

func TestLoad_ResendKeyRequiresFrom(t *testing.T) {
	baseEnv(t)
	setEnv(t, "RESEND_API_KEY", "re_test")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for RESEND_API_KEY without RESEND_FROM_EMAIL")
	}
}
