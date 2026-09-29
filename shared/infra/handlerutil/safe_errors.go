package handlerutil

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
)

// Stable machine codes for sanitized client errors (A3). The code is safe
// to expose and to branch on; the human message carries no backend
// internals.
const (
	ErrCodeInvalidJSON  = "invalid_json"
	ErrCodeInvalidToken = "invalid_token"
	ErrCodeUnauthorized = "unauthorized"
	ErrCodeConflict     = "conflict"
	ErrCodeInternal     = "internal_error"
	ErrCodeUnavailable  = "service_unavailable"
)

// SafeError is the client-safe error body: a stable generic message plus a
// machine-readable code plus the request id that correlates to the
// server-side log line. It never carries backend internals (DB/JWT/library
// error text stays server-side).
type SafeError struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}

// WriteSafeError logs the full internal error server-side (method + path
// only, never the query string) with a generated request id, and writes
// the sanitized body to the client. Status codes are unchanged by design.
// Curated validation/business messages that clients intentionally display
// verbatim (429 lockout text, price-proposal bounds/expiry copy) must NOT
// be routed through here — they are written directly with writeJSON.
func WriteSafeError(w http.ResponseWriter, r *http.Request, status int, code, message string, err error) {
	rid := newRequestID()
	method := sanitizeLogToken(r.Method)
	path := sanitizeLogToken(r.URL.Path)
	if err != nil {
		// #nosec G706 -- method/path/err are CR/LF-sanitized above/below; log injection is not possible
		//nolint:gosec -- method/path/err are CR/LF-sanitized above/below; log injection is not possible
		log.Printf("[ERROR] %s %s %s %s: %v", rid, code, method, path, sanitizeLogToken(err.Error()))
	} else {
		// #nosec G706 -- method/path are CR/LF-sanitized above; log injection is not possible
		//nolint:gosec -- method/path are CR/LF-sanitized above; log injection is not possible
		log.Printf("[ERROR] %s %s %s %s", rid, code, method, path)
	}
	WriteJSON(w, status, SafeError{Error: message, Code: code, RequestID: rid})
}

// BearerToken extracts a Bearer JWT from the Authorization header,
// returning "" when absent. Query-string tokens are deprecated (URLs land
// in proxy/gateway/CDN logs and shell history); handlers must prefer this
// helper and keep query-token reads as a deprecated fallback only, until
// the shipped mobile app migrates (removal date TBD by product).
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

func sanitizeLogToken(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
