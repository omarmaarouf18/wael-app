// Package config loads academy-service configuration from environment variables.
// TLS/mTLS and MongoDB are optional for localhost dev: the server runs
// with in-process memory store when MONGO_URI is empty, and
// serves plain HTTP when TLS_* paths are empty. Production compose sets all.
package config

import (
	"errors"
	"fmt"
	"os"
)

// Config holds all configuration required by academy-service.
type Config struct {
	Port                  string
	AppEnv                string
	MongoURI              string
	MongoDatabase         string
	RedisURI              string
	JWTSecret             string
	GatewaySecret         string
	InternalServiceToken  string
	AuthServiceURL        string
	TLSCertPath           string
	TLSKeyPath            string
	TLSCAPath             string
	AdminListenAddr       string
	ExposePriceToStudents bool
	SupportWhatsApp       string
}

// TLSEnabled reports whether server-side TLS is configured.
func (c *Config) TLSEnabled() bool {
	return c.TLSCertPath != "" && c.TLSKeyPath != ""
}

// Load reads and validates configuration from environment variables.
// Follows the allowlist policy: only APP_ENV=local|test relaxes security.
func Load() (*Config, error) {
	gatewaySecret := os.Getenv("GATEWAY_SECRET")
	if gatewaySecret == "" {
		return nil, errors.New("config: required env var GATEWAY_SECRET is empty")
	}
	internalToken := os.Getenv("INTERNAL_SERVICE_TOKEN")
	if internalToken == "" {
		return nil, errors.New("config: required env var INTERNAL_SERVICE_TOKEN is empty")
	}

	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "production"
	} else if appEnv != "local" && appEnv != "test" && appEnv != "production" {
		return nil, fmt.Errorf("config: invalid APP_ENV %q: must be one of local, test, production", appEnv)
	}
	dev := appEnv == "local" || appEnv == "test"

	mongoURI := os.Getenv("MONGO_URI")
	tlsCertPath := os.Getenv("TLS_CERT_PATH")
	tlsKeyPath := os.Getenv("TLS_KEY_PATH")
	tlsCAPath := os.Getenv("TLS_CA_PATH")
	authServiceURL := os.Getenv("AUTH_SERVICE_URL")
	adminListenAddr := os.Getenv("ADMIN_LISTEN_ADDR")

	jwtSecret := os.Getenv("JWT_SECRET")
	redisURI := os.Getenv("REDIS_URI")
	if redisURI == "" {
		redisURI = os.Getenv("REDIS_URL")
	}

	if !dev {
		if mongoURI == "" {
			return nil, errors.New("config: required env var MONGO_URI is empty")
		}
		if tlsCertPath == "" {
			return nil, errors.New("config: required env var TLS_CERT_PATH is empty")
		}
		if tlsKeyPath == "" {
			return nil, errors.New("config: required env var TLS_KEY_PATH is empty")
		}
		if tlsCAPath == "" {
			return nil, errors.New("config: required env var TLS_CA_PATH is empty")
		}
		if adminListenAddr == "" {
			return nil, errors.New("config: required env var ADMIN_LISTEN_ADDR is empty")
		}
		if authServiceURL == "" {
			return nil, errors.New("config: required env var AUTH_SERVICE_URL is empty")
		}
		if jwtSecret == "" {
			return nil, errors.New("config: required env var JWT_SECRET is empty")
		}
		if redisURI == "" {
			return nil, errors.New("config: required env var REDIS_URI is empty")
		}
		if os.Getenv("SUPPORT_WHATSAPP") == "" {
			return nil, errors.New("config: required env var SUPPORT_WHATSAPP is empty")
		}
	}

	supportWhatsApp := os.Getenv("SUPPORT_WHATSAPP")
	if supportWhatsApp == "" {
		supportWhatsApp = "+201000000000"
	}

	if adminListenAddr == "" {
		adminListenAddr = ":9002"
	}

	if authServiceURL == "" {
		authServiceURL = "https://auth-service:3002"
	}

	if dev && jwtSecret == "" {
		jwtSecret = "dev-secret-change-me-0123456789"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3003"
	}

	dbName := os.Getenv("ACADEMY_MONGO_DATABASE")
	if dbName == "" {
		dbName = os.Getenv("MONGO_INITDB_DATABASE")
	}
	if dbName == "" {
		dbName = "academy_db"
	}

	exposePrice := os.Getenv("EXPOSE_PRICE_TO_STUDENTS") == "true" || os.Getenv("EXPOSE_PRICE_TO_STUDENTS") == "1"

	return &Config{
		Port:                  port,
		AppEnv:                appEnv,
		MongoURI:              mongoURI,
		MongoDatabase:         dbName,
		RedisURI:              redisURI,
		JWTSecret:             jwtSecret,
		GatewaySecret:         gatewaySecret,
		InternalServiceToken:  internalToken,
		AuthServiceURL:        authServiceURL,
		TLSCertPath:           tlsCertPath,
		TLSKeyPath:            tlsKeyPath,
		TLSCAPath:             tlsCAPath,
		AdminListenAddr:       adminListenAddr,
		ExposePriceToStudents: exposePrice,
		SupportWhatsApp:       supportWhatsApp,
	}, nil
}
