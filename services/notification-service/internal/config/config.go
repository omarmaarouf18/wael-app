// Package config loads notification-service configuration from the environment.
// Mongo/Redis/TLS are optional for localhost dev (memory stores, plain HTTP);
// production compose sets them all.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil/secretcheck"
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
	// AdminListenAddr is the internal admin listener (broadcast API, ADR-0008).
	AdminListenAddr string
	// AuthAdminURL verifies admin tokens (auth-service admin listener).
	AuthAdminURL string
	// AcademyAdminURL resolves subject audiences (academy admin listener).
	AcademyAdminURL string
}

// TLSEnabled reports whether server-side TLS is configured.
func (c *Config) TLSEnabled() bool {
	return c.TLSCertPath != "" && c.TLSKeyPath != ""
}

// validateAdminURL requires scheme://host[:port] only, https outside dev,
// so a path, query or credentials can never reach an upstream request.
func validateAdminURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("config: invalid %s: must be an absolute http(s) URL", name)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("config: invalid %s: scheme must be https outside local/test", name)
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("config: invalid %s: must be scheme://host[:port] only", name)
	}
	return nil
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

	// Shared secrets must be strong outside APP_ENV=local|test (review P1).
	for _, s := range []struct{ name, value string }{
		{"JWT_SECRET", jwtSecret},
		{"GATEWAY_SECRET", gatewaySecret},
		{"INTERNAL_SERVICE_TOKEN", internalToken},
	} {
		if err := secretcheck.Check(s.name, s.value, appEnv); err != nil {
			return nil, err
		}
	}

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
	}

	streamOpenRateLimit := 10
	if v := os.Getenv("STREAM_OPEN_RATE_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("config: invalid STREAM_OPEN_RATE_LIMIT %q: must be a positive integer", v)
		}
		streamOpenRateLimit = n
	}

	adminListenAddr := os.Getenv("ADMIN_LISTEN_ADDR")
	if adminListenAddr == "" {
		adminListenAddr = ":9003"
	}
	authAdminURL := strings.TrimSpace(os.Getenv("AUTH_ADMIN_URL"))
	academyAdminURL := strings.TrimSpace(os.Getenv("ACADEMY_ADMIN_URL"))
	if !dev {
		if authAdminURL == "" {
			return nil, errors.New("config: required env var AUTH_ADMIN_URL is empty")
		}
		if academyAdminURL == "" {
			return nil, errors.New("config: required env var ACADEMY_ADMIN_URL is empty")
		}
		for _, u := range []struct{ name, value string }{
			{"AUTH_ADMIN_URL", authAdminURL},
			{"ACADEMY_ADMIN_URL", academyAdminURL},
		} {
			if err := validateAdminURL(u.name, u.value); err != nil {
				return nil, err
			}
		}
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
		AdminListenAddr:      adminListenAddr,
		AuthAdminURL:         authAdminURL,
		AcademyAdminURL:      academyAdminURL,
	}, nil
}
