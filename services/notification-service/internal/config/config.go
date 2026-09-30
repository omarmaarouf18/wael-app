// Package config loads notification-service configuration from the environment.
// Mongo/Redis/TLS are optional for localhost dev (memory stores, plain HTTP);
// production compose sets them all.
package config

import (
	"errors"
	"fmt"
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
	port := os.Getenv("PORT")
	if port == "" {
		port = "3004"
	}
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "production"
	} else if appEnv != "local" && appEnv != "test" && appEnv != "production" {
		return nil, fmt.Errorf("config: invalid APP_ENV %q: must be one of local, test, production", appEnv)
	}
	dbName := os.Getenv("NOTIFICATION_MONGO_DATABASE")
	if dbName == "" {
		dbName = os.Getenv("MONGO_INITDB_DATABASE")
	}
	if dbName == "" {
		dbName = "notification_db"
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
	}, nil
}
