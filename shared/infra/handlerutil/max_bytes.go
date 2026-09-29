package handlerutil

import (
	"net/http"
	"strings"
)

// MaxBytesMiddleware returns an HTTP middleware that bounds request bodies with http.MaxBytesReader
// to protect against memory exhaustion (DoS) attacks.
// Standard endpoints use defaultLimit (e.g. 1MB or 2MB).
// File upload / document endpoints are allowed up to 10MB (10 << 20).
func MaxBytesMiddleware(defaultLimit int64) func(http.Handler) http.Handler {
	if defaultLimit <= 0 {
		defaultLimit = 1 << 20 // 1 MB
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := defaultLimit
			path := r.URL.Path
			if strings.Contains(path, "/upload") || strings.Contains(path, "/documents") || strings.Contains(path, "/attachment") {
				if limit < 10<<20 {
					limit = 10 << 20 // 10 MB for file, document, and attachment uploads
				}
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
