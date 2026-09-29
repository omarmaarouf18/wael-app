// Package middleware provides HTTP middleware for the API Gateway.
package middleware

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.statusCode = code
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := sr.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("http.ResponseWriter does not support hijacking")
}

func (sr *statusRecorder) Flush() {
	if flusher, ok := sr.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func isOriginAllowed(origin, allowedOrigin string) bool {
	if allowedOrigin == "*" {
		return true
	}
	return origin == allowedOrigin
}

// Logging adds CORS headers and structured request logging with CR/LF sanitization.
func Logging(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sr := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			origin := r.Header.Get("Origin")
			if origin != "" && isOriginAllowed(origin, allowedOrigin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(sr, r)
			method := strings.ReplaceAll(strings.ReplaceAll(r.Method, "\n", " "), "\r", " ")
			path := strings.ReplaceAll(strings.ReplaceAll(r.URL.Path, "\n", " "), "\r", " ")
			// #nosec G706 -- method/path sanitized above; log injection not possible
			log.Printf("[GATEWAY] %s %s %d %s", method, path, sr.statusCode, time.Since(start))
		})
	}
}
