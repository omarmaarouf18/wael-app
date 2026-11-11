package jwtutil

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestCheckRevocation_RefusedWithoutRedisOutsideLocalTest tests item 1.4:
// When redisClient is nil and APP_ENV is outside local|test (production, staging, or unset),
// CheckRevocation and ValidateToken must fail-closed with an error instead of skipping revocation checks.
func TestCheckRevocation_RefusedWithoutRedisOutsideLocalTest(t *testing.T) {
	Init("super-secret-key-that-is-at-least-thirty-two-bytes-long")
	SetRedisClient(nil)

	claims := &Claims{
		UserID: "user-123",
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        "jti-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	for _, env := range []string{"production", "staging", ""} {
		t.Run("env="+env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			err := CheckRevocation(claims)
			if err == nil {
				t.Fatalf("expected CheckRevocation to fail when redisClient is nil outside local|test (APP_ENV=%q), but got nil", env)
			}
		})
	}

	for _, env := range []string{"local", "test"} {
		t.Run("env="+env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			err := CheckRevocation(claims)
			if err != nil {
				t.Fatalf("expected CheckRevocation to succeed in %s mode without Redis, got: %v", env, err)
			}
		})
	}
}
