package handlerutil

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
	"github.com/redis/go-redis/v9"
)

func TestHandlerRateLimiter(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	rl := ratelimit.NewRateLimiter(rdb, 5, time.Minute, "test:")
	hrl := NewRateLimiter(rl)

	limited, remaining := hrl.CheckAndRecord("test-key")
	if limited {
		t.Errorf("Expected initial check not to be limited")
	}
	if remaining != 0 {
		t.Errorf("Expected 0 remaining lockout for non-limited check, got %v", remaining)
	}
}

func TestGetIP(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expectedIP string
	}{
		{
			name:       "X-Forwarded-For Multiple IPs",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.195, 70.41.3.18"},
			remoteAddr: "10.0.0.1:1234",
			expectedIP: "203.0.113.195",
		},
		{
			name:       "X-Real-IP Single IP",
			headers:    map[string]string{"X-Real-IP": "198.51.100.1"},
			remoteAddr: "10.0.0.1:1234",
			expectedIP: "198.51.100.1",
		},
		{
			name:       "RemoteAddr IPv4 with Port",
			headers:    map[string]string{},
			remoteAddr: "192.0.2.1:12345",
			expectedIP: "192.0.2.1",
		},
		{
			name:       "RemoteAddr IPv6 with Port and Brackets",
			headers:    map[string]string{},
			remoteAddr: "[2001:db8::1]:8080",
			expectedIP: "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ip := GetIP(req)
			if ip != tt.expectedIP {
				t.Errorf("Expected IP %q, got %q", tt.expectedIP, ip)
			}
		})
	}
}
