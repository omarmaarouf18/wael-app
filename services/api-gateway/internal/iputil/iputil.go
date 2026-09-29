// Package iputil provides client IP resolution and trusted proxy verification for API Gateway.
package iputil

import (
	"net"
	"net/http"
	"strings"
)

// ExtractIP extracts the IP address without port from a host:port or raw IP string.
func ExtractIP(remoteAddr string) string {
	ip := strings.TrimSpace(remoteAddr)
	if strings.Contains(ip, "]") {
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			ip = ip[:idx]
		}
		ip = strings.Trim(ip, "[]")
	} else {
		if count := strings.Count(ip, ":"); count == 1 {
			if idx := strings.LastIndex(ip, ":"); idx != -1 {
				ip = ip[:idx]
			}
		}
	}
	return ip
}

// IsTrustedProxy checks whether the given immediate connection IP matches an IP or CIDR in trustedList.
func IsTrustedProxy(immediateIP string, trustedList []string) bool {
	cleanIPStr := ExtractIP(immediateIP)
	parsedIP := net.ParseIP(cleanIPStr)
	if parsedIP == nil {
		return false
	}

	for _, entry := range trustedList {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, subnet, err := net.ParseCIDR(entry)
			if err == nil && subnet.Contains(parsedIP) {
				return true
			}
		} else {
			trustedIP := net.ParseIP(entry)
			if trustedIP != nil && trustedIP.Equal(parsedIP) {
				return true
			}
		}
	}
	return false
}

// ResolveClientIP resolves the real client IP for rate limiting.
// If req.RemoteAddr comes from a trusted proxy and X-Forwarded-For is present, it extracts the first IP.
// Otherwise, it falls back to req.RemoteAddr's IP.
func ResolveClientIP(r *http.Request, trustedList []string) string {
	immediateIP := ExtractIP(r.RemoteAddr)
	if IsTrustedProxy(immediateIP, trustedList) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			firstIP := strings.TrimSpace(parts[0])
			if firstIP != "" {
				return ExtractIP(firstIP)
			}
		}
	}
	return immediateIP
}
