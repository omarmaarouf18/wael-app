package handlers

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Shared admin input validation (Phase 4.2+).

var (
	// levelKeyPattern constrains level keys: server-generated
	// "diploma-<8 hex>" ids and the seeded fixed keys.
	levelKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
	// resourceIDPattern constrains subject/video/file/request ids (UUIDs and
	// the short test-style ids). Validated before any DB call.
	resourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
)

// validLevelKey reports whether key is a well-formed level id.
// Checked before any DB call; malformed ids return 400 invalid_level_id.
func validLevelKey(key string) bool {
	return levelKeyPattern.MatchString(key)
}

// validResourceID reports whether id is a well-formed subject/video id.
// Checked before any DB call; malformed ids return 400.
func validResourceID(id string) bool {
	return resourceIDPattern.MatchString(id)
}

// cleanAdminName trims an admin-supplied name, removes CR/LF characters, and
// enforces 1-200 characters. It reports false for empty or over-long names.
func cleanAdminName(raw string) (string, bool) {
	s := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), "\r", ""), "\n", "")
	if s == "" {
		return "", false
	}
	if utf8.RuneCountInString(s) > 200 {
		return "", false
	}
	return s, true
}

// cleanAdminText trims admin-supplied free text (descriptions, reasons),
// strips CR/LF, and enforces maxRunes. Empty is allowed (caller decides
// whether empty is valid).
func cleanAdminText(raw string, maxRunes int) (string, bool) {
	s := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), "\r", " "), "\n", " ")
	if utf8.RuneCountInString(s) > maxRunes {
		return "", false
	}
	return s, true
}

// cleanAdminReason validates an admin-supplied mandatory reason:
// strips CR/LF, enforces 1-1000 characters, and refuses empty strings.
func cleanAdminReason(raw string) (string, bool) {
	s, ok := cleanAdminText(raw, 1000)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}
