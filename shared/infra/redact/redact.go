// Package redact provides logging-safe redaction helpers shared by all
// services (A2). Credentials must never reach stdout log shippers:
// connection URIs routinely embed userinfo and credential-bearing query
// parameters, and device tokens are bearer-equivalent secrets.
package redact

import (
	"net/url"
	"strings"
)

// sensitiveQueryParams are URL query keys whose values are replaced with
// "REDACTED" by RedactURI. Matched case-insensitively on the exact key.
var sensitiveQueryParams = map[string]bool{
	"password":      true,
	"passwd":        true,
	"pwd":           true,
	"pass":          true,
	"secret":        true,
	"secret_key":    true,
	"client_secret": true,
	"token":         true,
	"access_token":  true,
	"auth_token":    true,
	"api_key":       true,
	"apikey":        true,
	"private_key":   true,
	"session_token": true,
}

// RedactURI returns a logging-safe rendering of a connection URI: userinfo
// (username/password) is dropped and credential-bearing query parameters
// are replaced with "REDACTED". Host, port, path, and non-sensitive
// parameters (e.g. authSource, replicaSet) are preserved so the log line
// stays useful for ops triage. If the URI cannot be parsed, a fixed
// placeholder is returned — fail closed rather than log raw input that
// may itself contain credentials.
func RedactURI(rawURI string) string {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "[redacted:unparseable-uri]"
	}
	u.User = nil
	q := u.Query()
	changed := false
	for key := range q {
		if sensitiveQueryParams[strings.ToLower(key)] {
			q.Set(key, "REDACTED")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// PreviewToken renders a short, non-sensitive prefix of a bearer-equivalent
// token (FCM/APNs device tokens) for log correlation. The full value must
// never be logged: anyone holding it can target the device.
func PreviewToken(token string) string {
	const keep = 10
	if len(token) <= keep {
		return token
	}
	return token[:keep]
}
