// Package config loads admin-console configuration from environment variables.
//
// Security posture (ADR-0008, CLAUDE.md "Security defaults"): only
// APP_ENV=local|test relaxes anything. Empty APP_ENV means production and an
// unknown value is refused. INTERNAL_SERVICE_TOKEN is required in every
// environment, because the console must never start able to reach an internal
// admin endpoint without it, and an empty or blank secret never counts as set.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds everything admin-console needs at runtime.
type Config struct {
	Port                 string
	AppEnv               string
	InternalServiceToken string
	// AuthAdminURL is the auth-service admin listener (/internal/admin/*).
	AuthAdminURL string
	// AcademyAdminURL is the academy-service admin listener. The catalog
	// routes (/api/levels, /api/subjects, /api/videos) forward to it.
	AcademyAdminURL string
	TLSCertPath     string
	TLSKeyPath      string
	TLSCAPath       string
	// TrustedProxies are the only peers whose X-Forwarded-For is believed
	// (Caddy). Empty means X-Forwarded-For is never read.
	TrustedProxies []netip.Prefix
	// MaxPDFBytes caps one uploaded PDF (MAX_PDF_BYTES, same value as the
	// academy). Only the upload route accepts a body above 1 MiB.
	MaxPDFBytes int64
}

// DefaultMaxPDFBytes is the MAX_PDF_BYTES default (20 MB, SPEC D14).
const DefaultMaxPDFBytes int64 = 20 * 1024 * 1024

// Dev reports whether APP_ENV relaxes security (local or test only).
func (c *Config) Dev() bool {
	return c.AppEnv == "local" || c.AppEnv == "test"
}

// TLSEnabled reports whether the listener serves HTTPS.
func (c *Config) TLSEnabled() bool {
	return c.TLSCertPath != "" && c.TLSKeyPath != ""
}

// MTLSClientEnabled reports whether calls to the admin listeners present a
// client certificate and verify the server against the local CA.
func (c *Config) MTLSClientEnabled() bool {
	return c.TLSCertPath != "" && c.TLSKeyPath != "" && c.TLSCAPath != ""
}

const (
	defaultPort            = "3005"
	devDefaultAuthAdminURL = "http://localhost:9001"
	devDefaultAcademyURL   = "http://localhost:9002"
)

// Load reads and validates configuration. Errors name the variable, never
// its value (values can be secrets).
func Load() (*Config, error) {
	internalToken := os.Getenv("INTERNAL_SERVICE_TOKEN")
	if strings.TrimSpace(internalToken) == "" {
		return nil, errors.New("config: required env var INTERNAL_SERVICE_TOKEN is empty")
	}

	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "production"
	} else if appEnv != "local" && appEnv != "test" && appEnv != "production" {
		return nil, fmt.Errorf("config: invalid APP_ENV %q: must be one of local, test, production", appEnv)
	}
	dev := appEnv == "local" || appEnv == "test"

	cfg := &Config{
		AppEnv:               appEnv,
		InternalServiceToken: internalToken,
		TLSCertPath:          os.Getenv("TLS_CERT_PATH"),
		TLSKeyPath:           os.Getenv("TLS_KEY_PATH"),
		TLSCAPath:            os.Getenv("TLS_CA_PATH"),
	}

	if !dev {
		for _, name := range []string{
			"AUTH_ADMIN_URL", "ACADEMY_ADMIN_URL",
			"TLS_CERT_PATH", "TLS_KEY_PATH", "TLS_CA_PATH",
			"TRUSTED_PROXY_IPS",
		} {
			if strings.TrimSpace(os.Getenv(name)) == "" {
				return nil, fmt.Errorf("config: required env var %s is empty", name)
			}
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return nil, errors.New("config: invalid PORT: must be an integer from 1 to 65535")
	}
	cfg.Port = port

	var err error
	if cfg.AuthAdminURL, err = upstreamURL("AUTH_ADMIN_URL", devDefaultAuthAdminURL, dev); err != nil {
		return nil, err
	}
	if cfg.AcademyAdminURL, err = upstreamURL("ACADEMY_ADMIN_URL", devDefaultAcademyURL, dev); err != nil {
		return nil, err
	}
	if cfg.TrustedProxies, err = parseTrustedProxies(os.Getenv("TRUSTED_PROXY_IPS")); err != nil {
		return nil, err
	}
	if !dev && len(cfg.TrustedProxies) == 0 {
		return nil, errors.New("config: required env var TRUSTED_PROXY_IPS is empty")
	}
	cfg.MaxPDFBytes = DefaultMaxPDFBytes
	if v := os.Getenv("MAX_PDF_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("config: invalid MAX_PDF_BYTES %q: must be a positive integer", v)
		}
		cfg.MaxPDFBytes = n
	}
	return cfg, nil
}

// upstreamURL validates an admin listener base URL. Outside dev it must be
// https (the listeners enforce mTLS there). The value is reduced to scheme and
// host so a path, query or credentials can never reach a request.
func upstreamURL(name, devDefault string, dev bool) (string, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		if !dev {
			return "", fmt.Errorf("config: required env var %s is empty", name)
		}
		raw = devDefault
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("config: invalid %s: must be an absolute http(s) URL", name)
	}
	if u.Scheme != "https" && !(dev && u.Scheme == "http") {
		if dev {
			return "", fmt.Errorf("config: invalid %s: scheme must be http or https", name)
		}
		return "", fmt.Errorf("config: invalid %s: scheme must be https outside local/test", name)
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("config: invalid %s: must be scheme://host[:port] only", name)
	}
	return u.Scheme + "://" + u.Host, nil
}

// parseTrustedProxies accepts a comma-separated list of IPs and CIDRs.
// A /0 prefix is refused: trusting every peer would let any client forge the
// address used for the auth-service lockout.
func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var p netip.Prefix
		if strings.Contains(part, "/") {
			parsed, err := netip.ParsePrefix(part)
			if err != nil {
				return nil, errors.New("config: invalid TRUSTED_PROXY_IPS: entries must be IP addresses or CIDR ranges")
			}
			p = parsed.Masked()
		} else {
			addr, err := netip.ParseAddr(part)
			if err != nil {
				return nil, errors.New("config: invalid TRUSTED_PROXY_IPS: entries must be IP addresses or CIDR ranges")
			}
			addr = addr.Unmap().WithZone("")
			p = netip.PrefixFrom(addr, addr.BitLen())
		}
		if p.Bits() == 0 {
			return nil, errors.New("config: invalid TRUSTED_PROXY_IPS: a /0 range would trust every client")
		}
		out = append(out, p)
	}
	return out, nil
}
