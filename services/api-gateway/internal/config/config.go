// Package config loads gateway configuration from environment variables.
// TLS/mTLS is optional: when TLS_* paths are empty the gateway serves plain
// HTTP and dials backends over plain HTTP (localhost dev). When set, backends
// must use https and the gateway presents client certs (mTLS).
package config

import (
	"fmt"
	"os"
	"strings"
)

// ServiceRoute maps a URL path prefix to a backend service address.
type ServiceRoute struct {
	// Name identifies the upstream (auth, notification, academy); the gateway
	// gives each upstream its own circuit breaker named after it.
	Name        string
	Prefix      string
	Target      string
	StripPrefix string
	EnvKey      string
}

// Config holds all runtime configuration for the API Gateway.
type Config struct {
	Port                string
	AppEnv              string
	Routes              []ServiceRoute
	GatewaySecret       string
	AllowedOrigin       string
	TLSCertPath         string
	TLSKeyPath          string
	TLSCAPath           string
	ExternalTLSCertPath string
	ExternalTLSKeyPath  string
	RedisURI            string
	TrustedProxyIPs     []string
	AppDomain           string
}

// TLSEnabled reports whether server-side TLS is configured.
func (c *Config) TLSEnabled() bool {
	return c.TLSCertPath != "" && c.TLSKeyPath != ""
}

// MTLSClientEnabled reports whether mTLS client certs are configured.
func (c *Config) MTLSClientEnabled() bool {
	return c.TLSCertPath != "" && c.TLSKeyPath != "" && c.TLSCAPath != ""
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	gatewaySecret := os.Getenv("GATEWAY_SECRET")
	if gatewaySecret == "" {
		return nil, fmt.Errorf("config: required env var GATEWAY_SECRET is required and must not be empty")
	}

	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "production"
	} else if appEnv != "local" && appEnv != "test" && appEnv != "production" {
		return nil, fmt.Errorf("config: invalid APP_ENV %q: must be one of local, test, production", appEnv)
	}
	dev := appEnv == "local" || appEnv == "test"

	redisURI := os.Getenv("REDIS_URI")
	if redisURI == "" {
		return nil, fmt.Errorf("config: required env var REDIS_URI is empty")
	}
	tlsCertPath := os.Getenv("TLS_CERT_PATH")
	tlsKeyPath := os.Getenv("TLS_KEY_PATH")
	tlsCAPath := os.Getenv("TLS_CA_PATH")

	if !dev {
		if tlsCertPath == "" {
			return nil, fmt.Errorf("config: required env var TLS_CERT_PATH is empty")
		}
		if tlsKeyPath == "" {
			return nil, fmt.Errorf("config: required env var TLS_KEY_PATH is empty")
		}
		if tlsCAPath == "" {
			return nil, fmt.Errorf("config: required env var TLS_CA_PATH is empty")
		}
	}

	trustedProxyRaw := envOrDefault("TRUSTED_PROXY_IPS", "127.0.0.1,::1")
	var trustedProxyIPs []string
	for _, ip := range strings.Split(trustedProxyRaw, ",") {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			trustedProxyIPs = append(trustedProxyIPs, ip)
		}
	}

	cfg := &Config{
		Port:                envOrDefault("PORT", "8080"),
		AppEnv:              appEnv,
		GatewaySecret:       gatewaySecret,
		AllowedOrigin:       envOrDefault("ALLOWED_ORIGIN", "http://localhost:3000"),
		TLSCertPath:         tlsCertPath,
		TLSKeyPath:          tlsKeyPath,
		TLSCAPath:           tlsCAPath,
		ExternalTLSCertPath: os.Getenv("EXTERNAL_TLS_CERT_PATH"),
		ExternalTLSKeyPath:  os.Getenv("EXTERNAL_TLS_KEY_PATH"),
		RedisURI:            redisURI,
		TrustedProxyIPs:     trustedProxyIPs,
		AppDomain:           envOrDefault("APP_DOMAIN", "localhost"),
	}

	routeDefs := []struct {
		name       string
		prefix     string
		envKey     string
		defaultURL string
	}{
		{"auth", "/api/v1/auth/", "AUTH_SERVICE_URL", "http://auth-service:3002"},
		{"notification", "/api/v1/notifications/", "NOTIFICATION_SERVICE_URL", "http://notification-service:3004"},
		{"academy", "/api/v1/academy/", "ACADEMY_SERVICE_URL", "http://academy-service:3003"},
	}
	for _, rd := range routeDefs {
		target := envOrDefault(rd.envKey, rd.defaultURL)
		if target == "" {
			return nil, fmt.Errorf("config: required env var %s is empty", rd.envKey)
		}
		if cfg.MTLSClientEnabled() && !strings.HasPrefix(target, "https://") {
			return nil, fmt.Errorf("config: route %s target %q must use https scheme when mTLS client config is active", rd.prefix, target)
		}
		cfg.Routes = append(cfg.Routes, ServiceRoute{
			Name:        rd.name,
			Prefix:      rd.prefix,
			Target:      target,
			StripPrefix: "/api/v1",
			EnvKey:      rd.envKey,
		})
	}
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
