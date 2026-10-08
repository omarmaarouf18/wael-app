// Package config loads academy-service configuration from environment variables.
// TLS/mTLS and MongoDB are optional for localhost dev: the server runs
// with in-process memory store when MONGO_URI is empty, and
// serves plain HTTP when TLS_* paths are empty. Production compose sets all.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil/secretcheck"
)

// Config holds all configuration required by academy-service.
type Config struct {
	Port                   string
	AppEnv                 string
	MongoURI               string
	MongoDatabase          string
	RedisURI               string
	JWTSecret              string
	GatewaySecret          string
	InternalServiceToken   string
	AuthServiceURL         string
	AuthAdminURL           string
	NotificationServiceURL string
	TLSCertPath            string
	TLSKeyPath             string
	TLSCAPath              string
	AdminListenAddr        string
	ExposePriceToStudents  bool
	SupportWhatsApp        string
	TermsURL               string
	PrivacyURL             string
	MinVersion             string
	LatestVersion          string
	UpdateURL              string
	RateLimitRead          int
	RateLimitPlay          int
	RateLimitDownload      int
	RateLimitWrite         int
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
	authAdminURL := os.Getenv("AUTH_ADMIN_URL")
	notificationServiceURL := os.Getenv("NOTIFICATION_SERVICE_URL")
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
		if authAdminURL == "" {
			return nil, errors.New("config: required env var AUTH_ADMIN_URL is empty")
		}
		if notificationServiceURL == "" {
			return nil, errors.New("config: required env var NOTIFICATION_SERVICE_URL is empty")
		}
		if !strings.HasPrefix(notificationServiceURL, "https://") {
			return nil, errors.New("config: NOTIFICATION_SERVICE_URL must use https in production")
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

	supportWhatsApp := os.Getenv("SUPPORT_WHATSAPP")
	if supportWhatsApp == "" {
		supportWhatsApp = "+201000000000"
	}

	// Public app config (F-UX2 A7): terms and privacy URLs are https-only and
	// required outside local/test; version metadata is optional (empty means
	// no update prompt).
	termsURL := strings.TrimSpace(os.Getenv("TERMS_URL"))
	privacyURL := strings.TrimSpace(os.Getenv("PRIVACY_URL"))
	minVersion := strings.TrimSpace(os.Getenv("MIN_VERSION"))
	latestVersion := strings.TrimSpace(os.Getenv("LATEST_VERSION"))
	updateURL := strings.TrimSpace(os.Getenv("UPDATE_URL"))
	for _, v := range []struct {
		name, val string
	}{{"TERMS_URL", termsURL}, {"PRIVACY_URL", privacyURL}, {"UPDATE_URL", updateURL}} {
		if v.val != "" && !strings.HasPrefix(v.val, "https://") {
			return nil, fmt.Errorf("config: %s must use https", v.name)
		}
	}
	if !dev {
		if termsURL == "" {
			return nil, errors.New("config: required env var TERMS_URL is empty")
		}
		if privacyURL == "" {
			return nil, errors.New("config: required env var PRIVACY_URL is empty")
		}
	}

	if adminListenAddr == "" {
		adminListenAddr = ":9002"
	}

	if authServiceURL == "" {
		authServiceURL = "https://auth-service:3002"
	}

	if authAdminURL == "" {
		authAdminURL = "https://auth-service:9001"
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

	rateLimitRead := 120
	if v := os.Getenv("RATE_LIMIT_READ"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("config: invalid RATE_LIMIT_READ %q: must be a positive integer", v)
		}
		rateLimitRead = n
	}

	rateLimitPlay := 60
	if v := os.Getenv("RATE_LIMIT_PLAY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("config: invalid RATE_LIMIT_PLAY %q: must be a positive integer", v)
		}
		rateLimitPlay = n
	}

	rateLimitDownload := 10
	if v := os.Getenv("RATE_LIMIT_DOWNLOAD"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("config: invalid RATE_LIMIT_DOWNLOAD %q: must be a positive integer", v)
		}
		rateLimitDownload = n
	}

	rateLimitWrite := 5
	if v := os.Getenv("RATE_LIMIT_WRITE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("config: invalid RATE_LIMIT_WRITE %q: must be a positive integer", v)
		}
		rateLimitWrite = n
	}

	return &Config{
		Port:                   port,
		AppEnv:                 appEnv,
		MongoURI:               mongoURI,
		MongoDatabase:          dbName,
		RedisURI:               redisURI,
		JWTSecret:              jwtSecret,
		GatewaySecret:          gatewaySecret,
		InternalServiceToken:   internalToken,
		AuthServiceURL:         authServiceURL,
		AuthAdminURL:           authAdminURL,
		NotificationServiceURL: notificationServiceURL,
		TLSCertPath:            tlsCertPath,
		TLSKeyPath:             tlsKeyPath,
		TLSCAPath:              tlsCAPath,
		AdminListenAddr:        adminListenAddr,
		ExposePriceToStudents:  exposePrice,
		SupportWhatsApp:        supportWhatsApp,
		TermsURL:               termsURL,
		PrivacyURL:             privacyURL,
		MinVersion:             minVersion,
		LatestVersion:          latestVersion,
		UpdateURL:              updateURL,
		RateLimitRead:          rateLimitRead,
		RateLimitPlay:          rateLimitPlay,
		RateLimitDownload:      rateLimitDownload,
		RateLimitWrite:         rateLimitWrite,
	}, nil
}
