package config

import (
	"os"
	"testing"
)

func TestLoad_MinimalDev(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("GATEWAY_SECRET", "test-gateway-secret")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
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
