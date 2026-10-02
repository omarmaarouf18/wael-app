package config

import (
	"os"
	"strings"
	"testing"
)

var allVars = []string{
	"APP_ENV", "PORT", "INTERNAL_SERVICE_TOKEN", "AUTH_ADMIN_URL", "ACADEMY_ADMIN_URL",
	"TLS_CERT_PATH", "TLS_KEY_PATH", "TLS_CA_PATH", "TRUSTED_PROXY_IPS",
}

// setEnv clears every variable the package reads, then applies kv.
func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, v := range allVars {
		t.Setenv(v, "")
		_ = os.Unsetenv(v)
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func prodEnv() map[string]string {
	return map[string]string{
		"APP_ENV":                "production",
		"INTERNAL_SERVICE_TOKEN": "internal-secret",
		"AUTH_ADMIN_URL":         "https://auth-service:9001",
		"ACADEMY_ADMIN_URL":      "https://academy-service:9002",
		"TLS_CERT_PATH":          "/certs/admin-console.crt",
		"TLS_KEY_PATH":           "/certs/admin-console.key",
		"TLS_CA_PATH":            "/certs/ca.crt",
		"TRUSTED_PROXY_IPS":      "172.30.0.10",
	}
}

func TestLoad_ProductionHappyPath(t *testing.T) {
	setEnv(t, prodEnv())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Dev() || cfg.AppEnv != "production" {
		t.Fatalf("expected production, got %q", cfg.AppEnv)
	}
	if cfg.Port != "3005" || cfg.AuthAdminURL != "https://auth-service:9001" || cfg.AcademyAdminURL != "https://academy-service:9002" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if !cfg.TLSEnabled() || !cfg.MTLSClientEnabled() || len(cfg.TrustedProxies) != 1 {
		t.Fatalf("expected TLS, mTLS client and one trusted proxy: %+v", cfg)
	}
}

func TestLoad_EmptyAppEnvIsProduction(t *testing.T) {
	env := prodEnv()
	delete(env, "APP_ENV")
	setEnv(t, env)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AppEnv != "production" || cfg.Dev() {
		t.Fatalf("empty APP_ENV must mean production, got %q", cfg.AppEnv)
	}
}

func TestLoad_EmptyAppEnvStillRequiresProductionVars(t *testing.T) {
	setEnv(t, map[string]string{"INTERNAL_SERVICE_TOKEN": "internal-secret"})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AUTH_ADMIN_URL") {
		t.Fatalf("expected AUTH_ADMIN_URL required with empty APP_ENV, got %v", err)
	}
}

func TestLoad_UnknownAppEnvRefused(t *testing.T) {
	for _, v := range []string{"prod", "staging", "Local", "LOCAL", "dev", "development"} {
		t.Run(v, func(t *testing.T) {
			env := prodEnv()
			env["APP_ENV"] = v
			setEnv(t, env)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "APP_ENV") {
				t.Fatalf("expected APP_ENV error for %q, got %v", v, err)
			}
		})
	}
}

func TestLoad_InternalTokenAlwaysRequired(t *testing.T) {
	for _, appEnv := range []string{"local", "test", "production"} {
		for _, val := range []string{"", " ", "\t\n"} {
			t.Run(appEnv+"/"+strings.TrimSpace(val)+"x", func(t *testing.T) {
				env := prodEnv()
				env["APP_ENV"] = appEnv
				env["INTERNAL_SERVICE_TOKEN"] = val
				setEnv(t, env)
				if _, err := Load(); err == nil || !strings.Contains(err.Error(), "INTERNAL_SERVICE_TOKEN") {
					t.Fatalf("expected INTERNAL_SERVICE_TOKEN error, got %v", err)
				}
			})
		}
	}
}

func TestLoad_ProductionMissingVarsTable(t *testing.T) {
	for _, name := range []string{
		"INTERNAL_SERVICE_TOKEN", "AUTH_ADMIN_URL", "ACADEMY_ADMIN_URL",
		"TLS_CERT_PATH", "TLS_KEY_PATH", "TLS_CA_PATH", "TRUSTED_PROXY_IPS",
	} {
		t.Run("missing_"+name, func(t *testing.T) {
			env := prodEnv()
			delete(env, name)
			setEnv(t, env)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("expected error naming %s, got %v", name, err)
			}
		})
		t.Run("blank_"+name, func(t *testing.T) {
			env := prodEnv()
			env[name] = "   "
			setEnv(t, env)
			if _, err := Load(); err == nil {
				t.Fatalf("expected error for blank %s", name)
			}
		})
	}
}

func TestLoad_ErrorsNeverContainSecretValues(t *testing.T) {
	env := prodEnv()
	env["INTERNAL_SERVICE_TOKEN"] = "super-secret-value"
	env["AUTH_ADMIN_URL"] = "http://auth-service:9001"
	setEnv(t, env)
	_, err := Load()
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "super-secret-value") {
		t.Fatalf("error leaked a secret: %v", err)
	}
}

func TestLoad_DevDefaults(t *testing.T) {
	for _, appEnv := range []string{"local", "test"} {
		t.Run(appEnv, func(t *testing.T) {
			setEnv(t, map[string]string{"APP_ENV": appEnv, "INTERNAL_SERVICE_TOKEN": "x"})
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !cfg.Dev() || cfg.TLSEnabled() || cfg.MTLSClientEnabled() {
				t.Fatalf("expected dev without TLS: %+v", cfg)
			}
			if cfg.AuthAdminURL != "http://localhost:9001" || cfg.AcademyAdminURL != "http://localhost:9002" {
				t.Fatalf("unexpected dev defaults: %+v", cfg)
			}
			if len(cfg.TrustedProxies) != 0 {
				t.Fatalf("dev must trust no proxy by default")
			}
		})
	}
}

func TestLoad_UpstreamURLValidation(t *testing.T) {
	cases := []struct {
		name, appEnv, value string
		ok                  bool
	}{
		{"https prod", "production", "https://auth-service:9001", true},
		{"https prod trailing slash", "production", "https://auth-service:9001/", true},
		{"http prod refused", "production", "http://auth-service:9001", false},
		{"http dev allowed", "local", "http://localhost:9001", true},
		{"ftp dev refused", "local", "ftp://localhost:9001", false},
		{"no scheme", "production", "auth-service:9001", false},
		{"no host", "production", "https://", false},
		{"path", "production", "https://auth-service:9001/internal", false},
		{"query", "production", "https://auth-service:9001?x=1", false},
		{"userinfo", "production", "https://user:pw@auth-service:9001", false},
		{"fragment", "production", "https://auth-service:9001#x", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := prodEnv()
			env["APP_ENV"] = c.appEnv
			env["AUTH_ADMIN_URL"] = c.value
			setEnv(t, env)
			_, err := Load()
			if (err == nil) != c.ok {
				t.Fatalf("value %q: ok=%v err=%v", c.value, c.ok, err)
			}
			if err != nil && strings.Contains(err.Error(), "pw") {
				t.Fatalf("error leaked URL credentials: %v", err)
			}
		})
	}
}

func TestLoad_TrustedProxyParsing(t *testing.T) {
	cases := []struct {
		value string
		count int
		ok    bool
	}{
		{"172.30.0.10", 1, true},
		{"172.30.0.10, 10.0.0.0/8 ,::1", 3, true},
		{"172.16.0.0/12", 1, true},
		{"2001:db8::/32", 1, true},
		{"::ffff:10.0.0.5", 1, true},
		{"0.0.0.0/0", 0, false},
		{"::/0", 0, false},
		{"not-an-ip", 0, false},
		{"10.0.0.0/33", 0, false},
		{"10.0.0.1,garbage", 0, false},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			env := prodEnv()
			env["TRUSTED_PROXY_IPS"] = c.value
			setEnv(t, env)
			cfg, err := Load()
			if (err == nil) != c.ok {
				t.Fatalf("ok=%v err=%v", c.ok, err)
			}
			if err == nil && len(cfg.TrustedProxies) != c.count {
				t.Fatalf("expected %d prefixes, got %d", c.count, len(cfg.TrustedProxies))
			}
		})
	}
}

func TestLoad_OnlySeparatorsIsEmptyList(t *testing.T) {
	// Production needs at least one trusted proxy entry; separators alone do
	// not count. Dev may run with none.
	env := prodEnv()
	env["TRUSTED_PROXY_IPS"] = " , ,"
	setEnv(t, env)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY_IPS") {
		t.Fatalf("expected TRUSTED_PROXY_IPS error in production, got %v", err)
	}

	env["APP_ENV"] = "local"
	setEnv(t, env)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load (local): %v", err)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Fatalf("expected no trusted proxies")
	}
}

func TestLoad_PortValidation(t *testing.T) {
	for _, bad := range []string{"0", "65536", "abc", "-1", "30 05"} {
		t.Run(bad, func(t *testing.T) {
			env := prodEnv()
			env["PORT"] = bad
			setEnv(t, env)
			if _, err := Load(); err == nil {
				t.Fatalf("expected error for PORT=%q", bad)
			}
		})
	}
	env := prodEnv()
	env["PORT"] = "8443"
	setEnv(t, env)
	cfg, err := Load()
	if err != nil || cfg.Port != "8443" {
		t.Fatalf("expected port 8443, got %v %v", cfg, err)
	}
}
