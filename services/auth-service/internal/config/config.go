// Package config loads auth-service configuration from environment variables.
// TLS/mTLS and Mongo/Redis are optional for localhost dev: the server runs
// with in-process memory stores when MONGO_URI/REDIS_URI are empty, and
// serves plain HTTP when TLS_* paths are empty. Production compose sets all.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil/secretcheck"
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
	NotificationURL      string
	TLSCertPath          string
	TLSKeyPath           string
	TLSCAPath            string
	ResendAPIKey         string
	ResendFromEmail      string
	BlocklistHMACKey     string
	DefaultPhoneRegion   string
	AdminListenAddr      string
	// JWTAccessTTL is the access-token lifetime (JWT_ACCESS_TTL, a Go
	// duration within [5m, 24h], default 24h).
	JWTAccessTTL time.Duration
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
	resendAPIKey := os.Getenv("RESEND_API_KEY")
	resendFrom := os.Getenv("RESEND_FROM_EMAIL")
	blocklistHMACKey := os.Getenv("BLOCKLIST_HMAC_KEY")
	defaultPhoneRegion := os.Getenv("DEFAULT_PHONE_REGION")
	if defaultPhoneRegion == "" {
		defaultPhoneRegion = "EG"
	}

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
		if resendAPIKey == "" {
			return nil, errors.New("config: required env var RESEND_API_KEY is empty")
		}
		if resendFrom == "" {
			return nil, errors.New("config: required env var RESEND_FROM_EMAIL is empty")
		}
		if blocklistHMACKey == "" {
			return nil, errors.New("config: required env var BLOCKLIST_HMAC_KEY is empty")
		}
		if os.Getenv("ADMIN_LISTEN_ADDR") == "" {
			return nil, errors.New("config: required env var ADMIN_LISTEN_ADDR is empty")
		}
	} else {
		if resendAPIKey != "" && resendFrom == "" {
			return nil, errors.New("config: RESEND_FROM_EMAIL is required when RESEND_API_KEY is set")
		}
	}

	accessTTL := jwtutil.DefaultAccessTTL
	if raw := os.Getenv("JWT_ACCESS_TTL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("config: JWT_ACCESS_TTL %q is not a Go duration (e.g. 15m, 24h)", raw)
		}
		if err := jwtutil.ValidateAccessTTL(d); err != nil {
			return nil, fmt.Errorf("config: JWT_ACCESS_TTL must be between %v and %v", jwtutil.MinAccessTTL, jwtutil.MaxAccessTTL)
		}
		accessTTL = d
	}

	adminListenAddr := os.Getenv("ADMIN_LISTEN_ADDR")
	if adminListenAddr == "" {
		adminListenAddr = ":9001"
	}

	notificationURL := os.Getenv("NOTIFICATION_SERVICE_URL")
	if notificationURL == "" {
		notificationURL = "https://notification-service:3004"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "3002"
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
		MongoURI:             mongoURI,
		MongoDatabase:        dbName,
		RedisURI:             redisURI,
		JWTSecret:            jwtSecret,
		GatewaySecret:        gatewaySecret,
		InternalServiceToken: internalToken,
		NotificationURL:      notificationURL,
		TLSCertPath:          tlsCertPath,
		TLSKeyPath:           tlsKeyPath,
		TLSCAPath:            tlsCAPath,
		ResendAPIKey:         resendAPIKey,
		ResendFromEmail:      resendFrom,
		BlocklistHMACKey:     blocklistHMACKey,
		DefaultPhoneRegion:   defaultPhoneRegion,
		AdminListenAddr:      adminListenAddr,
		JWTAccessTTL:         accessTTL,
	}, nil
}
