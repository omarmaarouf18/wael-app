// Package secretcheck validates shared-secret strength at startup. It has no
// dependencies so every service config (including api-gateway) can use it
// without pulling the JWT library into its build.
package secretcheck

import (
	"fmt"
	"strings"
)

// MinSecretBytes is the minimum length of a shared secret (JWT_SECRET,
// GATEWAY_SECRET, INTERNAL_SERVICE_TOKEN) outside APP_ENV=local|test. It
// matches `openssl rand -hex 32` (64 chars) with margin and the 32-character
// floor in infrastructure/deploy/scripts/preflight.sh.
const MinSecretBytes = 32

// placeholderMarkers are template or dev-default fragments that must never
// reach a production secret. Matched case-insensitively, like preflight.sh.
var placeholderMarkers = []string{"paste_", "change_me", "devpassword123"}

// Check refuses a weak shared secret outside APP_ENV=local|test.
// Any other appEnv, including empty or unknown, is treated as production.
// Production rules: at least MinSecretBytes bytes and no placeholder marker
// (PASTE_, CHANGE_ME, devpassword123). The error names the variable, never
// its value. Emptiness is the caller's required-variable check.
func Check(name, value, appEnv string) error {
	if appEnv == "local" || appEnv == "test" {
		return nil
	}
	if len(value) < MinSecretBytes {
		return fmt.Errorf("config: %s must be at least %d bytes outside APP_ENV=local|test", name, MinSecretBytes)
	}
	lower := strings.ToLower(value)
	for _, m := range placeholderMarkers {
		if strings.Contains(lower, m) {
			return fmt.Errorf("config: %s still holds a placeholder or dev default", name)
		}
	}
	return nil
}
