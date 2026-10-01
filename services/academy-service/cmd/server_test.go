package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/config"
)

func TestBuildServer_RefusesPlainHTTPOutsideDev(t *testing.T) {
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	nonDevEnvs := []string{"production", "staging", "unknown", ""}

	for _, env := range nonDevEnvs {
		t.Run("env_"+env, func(t *testing.T) {
			cfg := &config.Config{
				AppEnv: env,
			}
			runner, err := buildServer(cfg, ":9002", dummy)
			if err == nil {
				t.Fatalf("expected error for plain HTTP outside dev (env=%q), got nil", env)
			}
			if runner != nil {
				t.Fatalf("expected nil runner on error, got %+v", runner)
			}
			if !strings.Contains(err.Error(), "plain HTTP not permitted outside dev") {
				t.Fatalf("expected error message to contain 'plain HTTP not permitted outside dev', got %q", err.Error())
			}
		})
	}
}

func TestBuildServer_RefusesTLSWithoutCAOutsideDev(t *testing.T) {
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	nonDevEnvs := []string{"production", "staging", "unknown", ""}

	for _, env := range nonDevEnvs {
		t.Run("env_"+env, func(t *testing.T) {
			cfg := &config.Config{
				AppEnv:      env,
				TLSCertPath: "/tmp/cert.pem",
				TLSKeyPath:  "/tmp/key.pem",
				TLSCAPath:   "",
			}
			runner, err := buildServer(cfg, ":9002", dummy)
			if err == nil {
				t.Fatalf("expected error for TLS without CA outside dev (env=%q), got nil", env)
			}
			if runner != nil {
				t.Fatalf("expected nil runner on error, got %+v", runner)
			}
			if !strings.Contains(err.Error(), "server TLS without client CA not permitted outside dev") {
				t.Fatalf("expected error message to contain 'server TLS without client CA not permitted outside dev', got %q", err.Error())
			}
		})
	}
}

func TestBuildServer_DevModesPermitted(t *testing.T) {
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	devEnvs := []string{"local", "test"}

	for _, env := range devEnvs {
		t.Run(env+"_plain_HTTP", func(t *testing.T) {
			cfg := &config.Config{
				AppEnv: env,
			}
			runner, err := buildServer(cfg, ":9002", dummy)
			if err != nil {
				t.Fatalf("unexpected error for %s plain HTTP: %v", env, err)
			}
			if runner == nil || runner.desc != "HTTP" {
				t.Fatalf("expected runner desc HTTP, got %+v", runner)
			}
			if runner.server.TLSConfig != nil {
				t.Error("expected nil TLSConfig for plain HTTP in dev")
			}
		})

		t.Run(env+"_TLS_without_CA", func(t *testing.T) {
			cfg := &config.Config{
				AppEnv:      env,
				TLSCertPath: "/tmp/cert.pem",
				TLSKeyPath:  "/tmp/key.pem",
			}
			runner, err := buildServer(cfg, ":9002", dummy)
			if err != nil {
				t.Fatalf("unexpected error for %s TLS without CA: %v", env, err)
			}
			if runner == nil || runner.desc != "HTTPS" {
				t.Fatalf("expected runner desc HTTPS, got %+v", runner)
			}
			if runner.server.TLSConfig == nil {
				t.Error("expected non-nil TLSConfig for TLS in dev")
			}
		})
	}
}
