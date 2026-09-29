package iputil

import (
	"net/http/httptest"
	"testing"
)

func TestExtractIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"127.0.0.1:8080", "127.0.0.1"},
		{"203.0.113.195", "203.0.113.195"},
		{"[::1]:1234", "::1"},
		{"::1", "::1"},
		{" 192.168.1.1:5000 ", "192.168.1.1"},
	}

	for _, tt := range tests {
		got := ExtractIP(tt.input)
		if got != tt.expected {
			t.Errorf("ExtractIP(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsTrustedProxy(t *testing.T) {
	trusted := []string{"127.0.0.1", "::1", "172.16.0.0/12"}

	tests := []struct {
		ip       string
		expected bool
	}{
		{"127.0.0.1:54321", true},
		{"[::1]:80", true},
		{"172.18.0.5:1234", true},
		{"203.0.113.5:8080", false},
		{"10.0.0.1:80", false},
		{"invalid-ip", false},
	}

	for _, tt := range tests {
		got := IsTrustedProxy(tt.ip, trusted)
		if got != tt.expected {
			t.Errorf("IsTrustedProxy(%q) = %v; want %v", tt.ip, got, tt.expected)
		}
	}
}

func TestResolveClientIP(t *testing.T) {
	trusted := []string{"127.0.0.1", "::1"}

	t.Run("Trusted proxy with X-Forwarded-For header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.195, 127.0.0.1")

		got := ResolveClientIP(req, trusted)
		if got != "203.0.113.195" {
			t.Errorf("ResolveClientIP = %q; want %q", got, "203.0.113.195")
		}
	})

	t.Run("Untrusted remote connection with spoofed X-Forwarded-For header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "198.51.100.1:54321"
		req.Header.Set("X-Forwarded-For", "1.1.1.1")

		got := ResolveClientIP(req, trusted)
		if got != "198.51.100.1" {
			t.Errorf("ResolveClientIP = %q; want %q", got, "198.51.100.1")
		}
	})

	t.Run("Trusted proxy with empty X-Forwarded-For header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "127.0.0.1:12345"

		got := ResolveClientIP(req, trusted)
		if got != "127.0.0.1" {
			t.Errorf("ResolveClientIP = %q; want %q", got, "127.0.0.1")
		}
	})
}
