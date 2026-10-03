package jwtutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

var (
	ErrInvalidToken   = errors.New("invalid token")
	ErrExpiredToken   = errors.New("token has expired")
	ErrTokenRevoked   = errors.New("jwtutil: token has been revoked")
	ErrSessionRevoked = errors.New("jwtutil: session has been revoked")
)

type Claims struct {
	UserID string   `json:"user_id"`
	Role   string   `json:"role"`
	Email  string   `json:"email"`
	AMR    []string `json:"amr,omitempty"`
	SID    string   `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

var jwtSecret []byte
var redisClient *redis.Client

type redisHealthTracker struct {
	mu                  sync.Mutex
	lastSuccessTime     time.Time
	consecutiveFailures int
	maxBlipFailures     int
	maxBlipWindow       time.Duration
	retryTimeout        time.Duration
}

var healthTracker = &redisHealthTracker{
	maxBlipFailures: 3,
	maxBlipWindow:   5 * time.Second,
	retryTimeout:    500 * time.Millisecond,
}

func (t *redisHealthTracker) recordSuccess() {
	t.mu.Lock()
	t.lastSuccessTime = time.Now()
	t.consecutiveFailures = 0
	t.mu.Unlock()
}

func (t *redisHealthTracker) recordFailure() {
	t.mu.Lock()
	t.consecutiveFailures++
	t.mu.Unlock()
}

func (t *redisHealthTracker) canRetryBlip() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.consecutiveFailures < t.maxBlipFailures {
		if t.lastSuccessTime.IsZero() || time.Since(t.lastSuccessTime) <= t.maxBlipWindow {
			return true
		}
	}
	return false
}

// ResetHealthTracker resets health tracking metrics (used in tests).
func ResetHealthTracker() {
	healthTracker.mu.Lock()
	healthTracker.lastSuccessTime = time.Time{}
	healthTracker.consecutiveFailures = 0
	healthTracker.mu.Unlock()
}

// GetHealthTrackerStats returns the current health metrics (used in tests).
func GetHealthTrackerStats() (time.Time, int) {
	healthTracker.mu.Lock()
	defer healthTracker.mu.Unlock()
	return healthTracker.lastSuccessTime, healthTracker.consecutiveFailures
}

func Init(secret string) {
	if secret == "" {
		panic("JWT_SECRET is required and must not be empty")
	}
	jwtSecret = []byte(secret)
}

func SetRedisClient(client *redis.Client) {
	redisClient = client
}

func getSecret() []byte {
	if len(jwtSecret) == 0 {
		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			panic("JWT_SECRET environment variable is required and must not be empty")
		}
		return []byte(secret)
	}
	return jwtSecret
}

// GenerateUUID generates an RFC 4122 compliant UUID v4.
func GenerateUUID() (string, error) {
	uuid := make([]byte, 16)
	_, err := rand.Read(uuid)
	if err != nil {
		return "", err
	}
	// Set version to 4
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	// Set variant to RFC 4122
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:]), nil
}

func GenerateTokenWithSession(userID string, role string, email string, sid string, amr ...[]string) (string, error) {
	uuidStr, err := GenerateUUID()
	if err != nil {
		return "", fmt.Errorf("jwtutil: failed to generate token uuid: %w", err)
	}

	var amrVal []string
	if len(amr) > 0 {
		amrVal = amr[0]
	}

	claims := Claims{
		UserID: userID,
		Role:   role,
		Email:  email,
		AMR:    amrVal,
		SID:    sid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        uuidStr,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getSecret())
}

func GenerateToken(userID string, role string, email string, amr ...[]string) (string, error) {
	return GenerateTokenWithSession(userID, role, email, "", amr...)
}

// GenerateTokenWithAMR generates a token with the specified authentication method references.
func GenerateTokenWithAMR(userID string, role string, email string, amr []string) (string, error) {
	return GenerateToken(userID, role, email, amr)
}

func ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return getSecret(), nil
	})

	var isExpired bool
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			isExpired = true
		} else {
			return nil, err
		}
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}

	// Redis-backed denylist check.
	// We explicitly fail-closed if Redis is unreachable to maintain security integrity.
	if redisClient != nil && claims.RegisteredClaims.ID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		isDenylisted, err := redisClient.Exists(ctx, "jwt:denylist:"+claims.RegisteredClaims.ID).Result()
		cancel()

		if err != nil {
			healthTracker.recordFailure()
			if healthTracker.canRetryBlip() {
				retryCtx, retryCancel := context.WithTimeout(context.Background(), healthTracker.retryTimeout)
				retryDenylisted, retryErr := redisClient.Exists(retryCtx, "jwt:denylist:"+claims.RegisteredClaims.ID).Result()
				retryCancel()

				if retryErr == nil {
					healthTracker.recordSuccess()
					log.Printf("[REDIS] Transient connectivity blip absorbed, denylist check succeeded on retry for jti: %s", claims.RegisteredClaims.ID)
					isDenylisted = retryDenylisted
					err = nil
				} else {
					healthTracker.recordFailure()
					log.Printf("[SECURITY CRITICAL] Redis error checking JWT denylist (FAIL CLOSED after retry): %v. Rejecting token jti: %s", retryErr, claims.RegisteredClaims.ID)
					return nil, fmt.Errorf("jwtutil: security check failed (denylist unreachable): %w", retryErr)
				}
			} else {
				log.Printf("[SECURITY CRITICAL] Redis error checking JWT denylist (FAIL CLOSED): %v. Rejecting token jti: %s", err, claims.RegisteredClaims.ID)
				return nil, fmt.Errorf("jwtutil: security check failed (denylist unreachable): %w", err)
			}
		} else {
			healthTracker.recordSuccess()
		}

		if isDenylisted > 0 {
			return nil, ErrTokenRevoked
		}
	}

	// Redis-backed session revocation check.
	// Rejects tokens whose sid has key jwt:sid:<sid> in Redis (same fail-closed + one-retry path as the jti denylist).
	// Tokens without sid (issued before deploy) stay valid until expiry.
	if redisClient != nil && claims.SID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		isSessionRevoked, err := redisClient.Exists(ctx, "jwt:sid:"+claims.SID).Result()
		cancel()

		if err != nil {
			healthTracker.recordFailure()
			if healthTracker.canRetryBlip() {
				retryCtx, retryCancel := context.WithTimeout(context.Background(), healthTracker.retryTimeout)
				retrySessionRevoked, retryErr := redisClient.Exists(retryCtx, "jwt:sid:"+claims.SID).Result()
				retryCancel()

				if retryErr == nil {
					healthTracker.recordSuccess()
					log.Printf("[REDIS] Transient connectivity blip absorbed, sid check succeeded on retry for sid: %s", claims.SID)
					isSessionRevoked = retrySessionRevoked
					err = nil
				} else {
					healthTracker.recordFailure()
					log.Printf("[SECURITY CRITICAL] Redis error checking JWT sid revocation (FAIL CLOSED after retry): %v. Rejecting token sid: %s", retryErr, claims.SID)
					return nil, fmt.Errorf("jwtutil: security check failed (session lookup unreachable): %w", retryErr)
				}
			} else {
				log.Printf("[SECURITY CRITICAL] Redis error checking JWT sid revocation (FAIL CLOSED): %v. Rejecting token sid: %s", err, claims.SID)
				return nil, fmt.Errorf("jwtutil: security check failed (session lookup unreachable): %w", err)
			}
		} else {
			healthTracker.recordSuccess()
		}

		if isSessionRevoked > 0 {
			return nil, ErrSessionRevoked
		}
	}

	// Redis-backed per-user token invalidation check (tokens issued before stored timestamp).
	// We explicitly fail-closed if Redis is unreachable to maintain security integrity.
	if redisClient != nil && claims.UserID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		invalidatedStr, err := redisClient.Get(ctx, "jwt:invalidated_before:"+claims.UserID).Result()
		cancel()

		if err != nil && !errors.Is(err, redis.Nil) {
			healthTracker.recordFailure()
			if healthTracker.canRetryBlip() {
				retryCtx, retryCancel := context.WithTimeout(context.Background(), healthTracker.retryTimeout)
				retryStr, retryErr := redisClient.Get(retryCtx, "jwt:invalidated_before:"+claims.UserID).Result()
				retryCancel()

				if retryErr == nil || errors.Is(retryErr, redis.Nil) {
					healthTracker.recordSuccess()
					log.Printf("[REDIS] Transient connectivity blip absorbed, user invalidation check succeeded on retry for user_id: %s", claims.UserID)
					invalidatedStr = retryStr
					err = retryErr
				} else {
					healthTracker.recordFailure()
					log.Printf("[SECURITY CRITICAL] Redis error checking user token invalidation (FAIL CLOSED after retry): %v. Rejecting user_id: %s", retryErr, claims.UserID)
					return nil, fmt.Errorf("jwtutil: security check failed (user invalidation lookup unreachable): %w", retryErr)
				}
			} else {
				log.Printf("[SECURITY CRITICAL] Redis error checking user token invalidation (FAIL CLOSED): %v. Rejecting user_id: %s", err, claims.UserID)
				return nil, fmt.Errorf("jwtutil: security check failed (user invalidation lookup unreachable): %w", err)
			}
		} else {
			healthTracker.recordSuccess()
		}

		if err == nil && invalidatedStr != "" {
			ts, parseErr := strconv.ParseInt(invalidatedStr, 10, 64)
			if parseErr != nil {
				log.Printf("[SECURITY CRITICAL] Redis error parsing user token invalidation timestamp (FAIL CLOSED): %v. Rejecting user_id: %s", parseErr, claims.UserID)
				return nil, fmt.Errorf("jwtutil: security check failed (invalid timestamp format): %w", parseErr)
			}
			if claims.IssuedAt == nil || claims.IssuedAt.Time.Unix() < ts {
				return nil, ErrTokenRevoked
			}
		}
	}

	if isExpired {
		return claims, ErrExpiredToken
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// RevokeAllUserTokens invalidates all tokens issued for a specific user prior to the current timestamp.
// It sets a Redis key jwt:invalidated_before:<user_id> to the current Unix timestamp.
func RevokeAllUserTokens(userID string) error {
	if userID == "" {
		return errors.New("jwtutil: missing user_id")
	}
	if redisClient == nil {
		return errors.New("jwtutil: redis client not initialized")
	}

	now := time.Now()
	timestamp := now.Unix()

	// TTL: 24h token expiry + 7-day refresh window (matches RevokeToken TTL pattern)
	ttl := 24*time.Hour + 7*24*time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := "jwt:invalidated_before:" + userID
	err := redisClient.Set(ctx, key, strconv.FormatInt(timestamp, 10), ttl).Err()
	if err != nil {
		log.Printf("[SECURITY CRITICAL] Redis error storing user token invalidation timestamp (FAIL CLOSED): %v. User ID: %s", err, userID)
		return fmt.Errorf("jwtutil: revoke all user tokens failed: %w", err)
	}

	// Publish account revocation / suspension event to account:events channel (F-03)
	eventPayload := fmt.Sprintf(`{"action":"ACCOUNT_SUSPENDED","user_id":%q}`, userID)
	if pubErr := redisClient.Publish(ctx, "account:events", eventPayload).Err(); pubErr != nil {
		log.Printf("[SECURITY WARNING] Redis pubsub error publishing revocation event for %s: %v", userID, pubErr)
	}

	return nil
}

// RevokeToken denylists a token's jti in Redis.
// Expired tokens are held in the denylist until the end of their 7-day refresh window.
func RevokeToken(tokenStr string) error {
	claims, err := ValidateToken(tokenStr)
	// We allow revoking expired tokens so they cannot be refreshed
	if err != nil && !errors.Is(err, ErrExpiredToken) {
		return err
	}
	if claims == nil || claims.RegisteredClaims.ID == "" {
		return errors.New("jwtutil: invalid token or missing jti")
	}
	if redisClient == nil {
		return errors.New("jwtutil: redis client not initialized")
	}

	var ttl time.Duration
	if claims.ExpiresAt != nil {
		// Hold until token expiry + 7 days refresh window to prevent refresh reuse
		ttl = time.Until(claims.ExpiresAt.Time.Add(7 * 24 * time.Hour))
	}

	if ttl <= 0 {
		return nil // Already past the refresh window
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = redisClient.Set(ctx, "jwt:denylist:"+claims.RegisteredClaims.ID, "1", ttl).Err()
	if err != nil {
		log.Printf("[SECURITY CRITICAL] Redis error storing revoked JWT (FAIL CLOSED): %v. Token jti: %s", err, claims.RegisteredClaims.ID)
		return fmt.Errorf("jwtutil: revoke failed: %w", err)
	}
	return nil
}

// RevokeSession denylists a session's sid in Redis.
// TTL = access token lifetime (24h).
func RevokeSession(sid string) error {
	if sid == "" {
		return errors.New("jwtutil: missing sid")
	}
	if redisClient == nil {
		return errors.New("jwtutil: redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := redisClient.Set(ctx, "jwt:sid:"+sid, "1", 24*time.Hour).Err()
	if err != nil {
		log.Printf("[SECURITY CRITICAL] Redis error storing revoked session (FAIL CLOSED): %v. Session sid: %s", err, sid)
		return fmt.Errorf("jwtutil: revoke session failed: %w", err)
	}
	return nil
}

// GenerateSecureToken generates a cryptographically secure 32-byte hex token.
func GenerateSecureToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
