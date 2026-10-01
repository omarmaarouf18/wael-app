// Package config loads notification-service configuration from the environment.
// Mongo/Redis/TLS are optional for localhost dev (memory stores, plain HTTP);
// production compose sets them all.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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
	StreamMaxConcurrent  int
	StreamOpenRateLimit  int
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

	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "production"
	} else if appEnv != "local" && appEnv != "test" && appEnv != "production" {
		return nil, fmt.Errorf("config: invalid APP_ENV %q: must be one of local, test, production", appEnv)
	}
	dev := appEnv == "local" || appEnv == "test"

	mongoURI := os.Getenv("MONGO_URI")
	redisURI := os.Getenv("REDIS_URI")
	tlsCertPath := os.Getenv("TLS_CERT_PATH")
	tlsKeyPath := os.Getenv("TLS_KEY_PATH")
	tlsCAPath := os.Getenv("TLS_CA_PATH")

	if !dev {
		if mongoURI == "" {
			return nil, errors.New("config: required env var MONGO_URI is empty")
		}
		if redisURI == "" {
			return nil, errors.New("config: required env var REDIS_URI is empty")
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
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3004"
	}
	dbName := os.Getenv("NOTIFICATION_MONGO_DATABASE")
	if dbName == "" {
		dbName = os.Getenv("MONGO_INITDB_DATABASE")
	}
	if dbName == "" {
		dbName = "notification_db"
	}
	streamMaxConcurrent := 3
	if v := os.Getenv("STREAM_MAX_CONCURRENT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("config: invalid STREAM_MAX_CONCURRENT %q: must be a positive integer", v)
		}
		streamMaxConcurrent = n
	} else if v := os.Getenv("NOTIFICATION_STREAM_MAX_CONCURRENT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("config: invalid NOTIFICATION_STREAM_MAX_CONCURRENT %q: must be a positive integer", v)
		}
		streamMaxConcurrent = n
	}

	streamOpenRateLimit := 10
	if v := os.Getenv("STREAM_OPEN_RATE_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("config: invalid STREAM_OPEN_RATE_LIMIT %q: must be a positive integer", v)
		}
		streamOpenRateLimit = n
	} else if v := os.Getenv("NOTIFICATION_STREAM_OPEN_RATE_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("config: invalid NOTIFICATION_STREAM_OPEN_RATE_LIMIT %q: must be a positive integer", v)
		}
		streamOpenRateLimit = n
	}

	return &Config{
		Port:                 port,
		AppEnv:               appEnv,
		MongoURI:             mongoURI,
		MongoDatabase:        dbName,
		RedisURI:             redisURI,
		JWTSecret:            jwtSecret,
		GatewaySecret:        gatewaySecret,
		InternalServiceToken: internalToken,
		TLSCertPath:          tlsCertPath,
		TLSKeyPath:           tlsKeyPath,
		TLSCAPath:            tlsCAPath,
		StreamMaxConcurrent:  streamMaxConcurrent,
		StreamOpenRateLimit:  streamOpenRateLimit,
	}, nil
}
