// Package config loads auth-service configuration from environment variables.
// TLS/mTLS and Mongo/Redis are optional for localhost dev: the server runs
// with in-process memory stores when MONGO_URI/REDIS_URI are empty, and
// serves plain HTTP when TLS_* paths are empty. Production compose sets all.
package config

import (
	"errors"
	"os"
)

type Config struct {
	Port                 string
	AppEnv               string
	MongoURI             string
	MongoDatabase        string
	RedisURI             string
	JWTSecret            string
	GatewaySecret        string
	InternalServiceToken string
	TLSCertPath          string
	TLSKeyPath           string
	TLSCAPath            string
	ResendAPIKey         string
	ResendFromEmail      string
}

// TLSEnabled reports whether server-side TLS is configured.
func (c *Config) TLSEnabled() bool {
	return c.TLSCertPath != "" && c.TLSKeyPath != ""
}

func Load() (*Config, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, errors.New("config: required env var JWT_SECRET is empty")
	}
	gatewaySecret := os.Getenv("GATEWAY_SECRET")
	if gatewaySecret == "" {
		return nil, errors.New("config: required env var GATEWAY_SECRET is empty")
	}
	internalToken := os.Getenv("INTERNAL_SERVICE_TOKEN")
	if internalToken == "" {
		return nil, errors.New("config: required env var INTERNAL_SERVICE_TOKEN is empty")
	}
	resendAPIKey := os.Getenv("RESEND_API_KEY")
	resendFrom := os.Getenv("RESEND_FROM_EMAIL")
	if resendAPIKey != "" && resendFrom == "" {
		return nil, errors.New("config: RESEND_FROM_EMAIL is required when RESEND_API_KEY is set")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "3002"
	}
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "production"
	}
	dbName := os.Getenv("AUTH_MONGO_DATABASE")
	if dbName == "" {
		dbName = os.Getenv("MONGO_INITDB_DATABASE")
	}
	if dbName == "" {
		dbName = "auth_db"
	}
	return &Config{
		Port:                 port,
		AppEnv:               appEnv,
		MongoURI:             os.Getenv("MONGO_URI"),
		MongoDatabase:        dbName,
		RedisURI:             os.Getenv("REDIS_URI"),
		JWTSecret:            jwtSecret,
		GatewaySecret:        gatewaySecret,
		InternalServiceToken: internalToken,
		TLSCertPath:          os.Getenv("TLS_CERT_PATH"),
		TLSKeyPath:           os.Getenv("TLS_KEY_PATH"),
		TLSCAPath:            os.Getenv("TLS_CA_PATH"),
		ResendAPIKey:         resendAPIKey,
		ResendFromEmail:      resendFrom,
	}, nil
}
