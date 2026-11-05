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

func parseAllowedOrigins(allowed string) []string {
	var origins []string
	for _, o := range strings.Split(allowed, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			origins = append(origins, o)
		}
	}
	return origins
}

func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		if allowed != "*" && origin == allowed {
			return true
		}
	}
	return false
}

// Logging adds CORS headers and structured request logging with CR/LF sanitization.
func Logging(allowedOrigin string) func(http.Handler) http.Handler {
	origins := parseAllowedOrigins(allowedOrigin)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sr := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			origin := r.Header.Get("Origin")
			if origin != "" && isOriginAllowed(origin, origins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Client")
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

// CSRF enforces CSRF protection on cookie-using routes: allowlisted Origin and X-Client: web, or refuse.
func CSRF(allowedOrigin string) func(http.Handler) http.Handler {
	origins := parseAllowedOrigins(allowedOrigin)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isCookieRoute := strings.HasPrefix(r.URL.Path, "/api/v1/auth/refresh")
			hasCookie := false
			if c, err := r.Cookie("refresh_token"); err == nil && c.Value != "" {
				hasCookie = true
			}
			isWebClient := strings.TrimSpace(r.Header.Get("X-Client")) == "web"

			if (isCookieRoute && (hasCookie || isWebClient)) || hasCookie {
				origin := r.Header.Get("Origin")
				if !isOriginAllowed(origin, origins) || !isWebClient {
					http.Error(w, `{"error":"forbidden: invalid CSRF origin or client","code":"csrf_refused"}`, http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
