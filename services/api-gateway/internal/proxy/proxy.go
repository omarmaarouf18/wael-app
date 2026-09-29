// Package proxy provides reverse proxy handler creation for backend services.
package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/omarmaarouf18/wael-app/api-gateway/internal/config"
	"github.com/omarmaarouf18/wael-app/api-gateway/internal/iputil"
)

// New creates an http.Handler that reverse-proxies requests matching
// the given ServiceRoute to its target backend.
func New(route config.ServiceRoute, gatewaySecret, internalToken string, trustedProxies []string, transport http.RoundTripper) (http.Handler, error) {
	target, err := url.Parse(route.Target)
	if err != nil {
		return nil, fmt.Errorf("proxy: invalid target URL %q for %s: %w", route.Target, route.Prefix, err)
	}
	proxy := &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1,
		Director: func(req *http.Request) {
			req.Header.Del("X-Internal-Token")
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			originalPath := req.URL.Path
			trimmed := strings.TrimPrefix(originalPath, route.StripPrefix)
			if trimmed == "" || trimmed[0] != '/' {
				trimmed = "/" + trimmed
			}
			req.URL.Path = trimmed
			req.Header.Set("X-Forwarded-Prefix", route.Prefix)
			immediateIP := iputil.ExtractIP(req.RemoteAddr)
			existingXFF := req.Header.Get("X-Forwarded-For")
			if iputil.IsTrustedProxy(immediateIP, trustedProxies) && existingXFF != "" {
				req.Header.Set("X-Forwarded-For", existingXFF)
			} else {
				req.Header.Set("X-Forwarded-For", req.RemoteAddr)
			}
			req.Header.Set("X-Gateway-Secret", gatewaySecret)
			if internalToken != "" {
				req.Header.Set("X-Internal-Token", internalToken)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// #nosec G706 -- sanitised path/method; log injection not possible
			log.Printf("[PROXY ERROR] %s %s -> %s: %v", r.Method, r.URL.Path, route.Target, err)
			http.Error(w, fmt.Sprintf(`{"error": "service unavailable", "target": %q}`, route.Prefix), http.StatusBadGateway)
		},
	}
	return proxy, nil
}
