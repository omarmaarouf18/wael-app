package jwtutil

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

func TestValidateToken_Expired(t *testing.T) {
	Init("super-secret-key-that-is-at-least-thirty-two-bytes-long")

	// 1. Test valid token
	tokenStr, err := GenerateToken("user123", "owner", "user@example.com")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("failed to validate valid token: %v", err)
	}
	if claims.UserID != "user123" {
		t.Errorf("expected userID user123, got %s", claims.UserID)
	}

	// 2. Test expired token (1 hour ago)
	expiredClaims := Claims{
		UserID: "expiredUser",
		Role:   "customer",
		Email:  "expired@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	expiredToken := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims)
	expiredTokenStr, err := expiredToken.SignedString(getSecret())
	if err != nil {
		t.Fatalf("failed to sign expired token: %v", err)
	}

	claims, err = ValidateToken(expiredTokenStr)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
	if claims == nil {
		t.Fatalf("expected claims to be populated even when token is expired, got nil")
	}
	if claims.UserID != "expiredUser" {
		t.Errorf("expected userID expiredUser, got %s", claims.UserID)
	}
}

func TestValidateToken_ExpiredAndInvalidSignature(t *testing.T) {
	Init("super-secret-key-that-is-at-least-thirty-two-bytes-long")

	// Create an expired token signed with a DIFFERENT key
	expiredClaims := Claims{
		UserID: "attacker",
		Role:   "owner",
		Email:  "attacker@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	expiredToken := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims)
	badSecret := []byte("wrong-secret-key-that-is-also-long-enough")
	expiredTokenStr, err := expiredToken.SignedString(badSecret)
	if err != nil {
		t.Fatalf("failed to sign expired token: %v", err)
	}

	claims, err := ValidateToken(expiredTokenStr)
	// We expect validation to fail with a signature error, NOT ErrExpiredToken!
	if err == nil {
		t.Fatalf("expected error for token with bad signature and expired, got nil")
	}
	if errors.Is(err, ErrExpiredToken) {
		t.Fatalf("security violation: token with invalid signature was treated as expired but otherwise valid claims returned (claims: %+v)", claims)
	}
}

func TestJWT_ExtraCoverage(t *testing.T) {
	Init("super-secret-key-that-is-at-least-thirty-two-bytes-long")

	// 1. None algorithm token is rejected
	t.Run("NoneAlgorithmRejected", func(t *testing.T) {
		claims := Claims{
			UserID: "user123",
			Role:   "owner",
			Email:  "user@example.com",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
		tokenStr, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("failed to sign none algorithm token: %v", err)
		}

		_, err = ValidateToken(tokenStr)
		if err == nil {
			t.Error("expected none algorithm token to be rejected, but err was nil")
		}
	})

	// 2. Mismatched signing method (RSA key signed, expecting HMAC) is rejected
	t.Run("MismatchedSigningMethod", func(t *testing.T) {
		claims := Claims{
			UserID: "user123",
			Role:   "owner",
			Email:  "user@example.com",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}
		// Generate small RSA private key for testing signature mismatch
		privKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("failed to generate RSA key: %v", err)
		}

		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tokenStr, err := token.SignedString(privKey)
		if err != nil {
			t.Fatalf("failed to sign RS256 token: %v", err)
		}

		_, err = ValidateToken(tokenStr)
		if err == nil {
			t.Error("expected RS256 token to be rejected by HMAC validation, but err was nil")
		}
	})

	// 3. Expired token behavior in ValidateToken vs RevokeToken, and TTL calculation correctness
	t.Run("ExpiredTokenAndTTLAndRevokingTwice", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()

		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		SetRedisClient(rdb)
		defer SetRedisClient(nil)

		// Create a token that expired 1 hour ago
		jti, err := GenerateUUID()
		if err != nil {
			t.Fatalf("failed to generate UUID: %v", err)
		}

		expiresAt := time.Now().Add(-1 * time.Hour)
		expiredClaims := Claims{
			UserID: "expiredUser",
			Role:   "customer",
			Email:  "expired@example.com",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(expiresAt),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
				ID:        jti,
			},
		}
		expiredToken := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims)
		expiredTokenStr, err := expiredToken.SignedString(getSecret())
		if err != nil {
			t.Fatalf("failed to sign expired token: %v", err)
		}

		// ValidateToken returns the claims AND ErrExpiredToken
		claims, err := ValidateToken(expiredTokenStr)
		if !errors.Is(err, ErrExpiredToken) {
			t.Fatalf("expected ValidateToken to return ErrExpiredToken, got %v", err)
		}
		if claims == nil {
			t.Fatal("expected claims to not be nil even for expired token")
		}

		// RevokeToken succeeds for an expired token if it's within the 7-day refresh window
		err = RevokeToken(expiredTokenStr)
		if err != nil {
			t.Fatalf("expected RevokeToken to succeed on expired token, got: %v", err)
		}

		// Verify key is denylisted in Redis and check the TTL
		denylistKey := "jwt:denylist:" + jti
		if !mr.Exists(denylistKey) {
			t.Error("expected key to exist in Redis denylist")
		}

		// Expected TTL = 7 days + token expiry (since token expiry is -1h, TTL is 7 days - 1h)
		expectedTTL := 7*24*time.Hour + time.Until(expiresAt)
		mrTTL := mr.TTL(denylistKey)
		// Check that the TTL is within 5 seconds of the expected value
		if math.Abs(float64(mrTTL-expectedTTL)) > float64(5*time.Second) {
			t.Errorf("expected TTL close to %v, got %v", expectedTTL, mrTTL)
		}

		// Revoking a token twice should return the revocation error
		err = RevokeToken(expiredTokenStr)
		if err == nil || err.Error() != "jwtutil: token has been revoked" {
			t.Errorf("expected second RevokeToken to fail with 'jwtutil: token has been revoked', got: %v", err)
		}
	})

	// 5. Revoking an already-expired token that is way past the 7-day refresh window
	t.Run("RevokingWayPastRefreshWindow", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()

		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		SetRedisClient(rdb)
		defer SetRedisClient(nil)

		// Create a token that expired 8 days ago
		jti, err := GenerateUUID()
		if err != nil {
			t.Fatalf("failed to generate UUID: %v", err)
		}

		expiresAt := time.Now().Add(-8 * 24 * time.Hour)
		oldClaims := Claims{
			UserID: "oldUser",
			Role:   "customer",
			Email:  "old@example.com",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(expiresAt),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-9 * 24 * time.Hour)),
				ID:        jti,
			},
		}
		oldToken := jwt.NewWithClaims(jwt.SigningMethodHS256, oldClaims)
		oldTokenStr, err := oldToken.SignedString(getSecret())
		if err != nil {
			t.Fatalf("failed to sign old token: %v", err)
		}

		// RevokeToken returns nil early (no-op) and does not write to Redis
		err = RevokeToken(oldTokenStr)
		if err != nil {
			t.Fatalf("expected RevokeToken to return nil for token past refresh window, got: %v", err)
		}

		denylistKey := "jwt:denylist:" + jti
		if mr.Exists(denylistKey) {
			t.Error("expected token past refresh window NOT to be written to Redis denylist")
		}
	})

	// 6. Redis denylist fail-closed behavior when Redis is unreachable
	t.Run("RedisFailClosed", func(t *testing.T) {
		// Generate valid token
		tokenStr, err := GenerateToken("user123", "owner", "user@example.com")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		// Setup unreachable Redis client by using a client that is closed
		rdb := redis.NewClient(&redis.Options{Addr: "localhost:9999"})
		rdb.Close() // Force immediate failure without network wait times

		SetRedisClient(rdb)
		defer SetRedisClient(nil)

		// ValidateToken should fail-closed and return error
		_, err = ValidateToken(tokenStr)
		if err == nil {
			t.Error("expected ValidateToken to fail-closed on unreachable Redis, but err was nil")
		}

		// RevokeToken should fail-closed and return error
		err = RevokeToken(tokenStr)
		if err == nil {
			t.Error("expected RevokeToken to fail-closed on unreachable Redis, but err was nil")
		}
	})
}

func TestRevokeAllUserTokens(t *testing.T) {
	Init("super-secret-key-that-is-at-least-thirty-two-bytes-long")

	// 1. Missing inputs / uninitialized redis errors
	t.Run("ValidationErrors", func(t *testing.T) {
		SetRedisClient(nil)
		if err := RevokeAllUserTokens("user123"); err == nil || err.Error() != "jwtutil: redis client not initialized" {
			t.Errorf("expected 'jwtutil: redis client not initialized', got %v", err)
		}

		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		SetRedisClient(rdb)
		defer SetRedisClient(nil)

		if err := RevokeAllUserTokens(""); err == nil || err.Error() != "jwtutil: missing user_id" {
			t.Errorf("expected 'jwtutil: missing user_id', got %v", err)
		}
	})

	// 2. Successful revocation of pre-invalidation tokens & validity of post-invalidation tokens
	t.Run("RevocationLifecycle", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		SetRedisClient(rdb)
		defer SetRedisClient(nil)

		userID1 := "user-revoke-1"
		userID2 := "user-revoke-2"

		// Generate token1 for user1 BEFORE revocation
		token1, err := GenerateToken(userID1, "customer", "user1@example.com")
		if err != nil {
			t.Fatalf("failed to generate token1: %v", err)
		}

		// Generate token3 for user2 BEFORE revocation
		token3, err := GenerateToken(userID2, "customer", "user2@example.com")
		if err != nil {
			t.Fatalf("failed to generate token3: %v", err)
		}

		// Ensure token issuance timestamp is strictly before the revocation timestamp
		time.Sleep(1 * time.Second)

		// Revoke all user tokens for user1
		if err := RevokeAllUserTokens(userID1); err != nil {
			t.Fatalf("RevokeAllUserTokens failed: %v", err)
		}

		// Key should exist in miniredis
		key := "jwt:invalidated_before:" + userID1
		if !mr.Exists(key) {
			t.Fatalf("expected key %s to exist in miniredis", key)
		}

		// Generate token2 for user1 AFTER revocation
		token2, err := GenerateToken(userID1, "customer", "user1@example.com")
		if err != nil {
			t.Fatalf("failed to generate token2: %v", err)
		}

		// Validate token1 (user1, pre-revocation) -> MUST BE REJECTED
		claims1, err1 := ValidateToken(token1)
		if err1 == nil || err1.Error() != "jwtutil: token has been revoked" {
			t.Errorf("expected token1 to be revoked, got err: %v, claims: %+v", err1, claims1)
		}

		// Validate token2 (user1, post-revocation) -> MUST BE VALID
		claims2, err2 := ValidateToken(token2)
		if err2 != nil {
			t.Errorf("expected token2 to be valid after revocation timestamp, got err: %v", err2)
		}
		if claims2 == nil || claims2.UserID != userID1 {
			t.Errorf("expected claims2 UserID to be %s, got %+v", userID1, claims2)
		}

		// Validate token3 (user2, unaffected user) -> MUST BE VALID
		claims3, err3 := ValidateToken(token3)
		if err3 != nil {
			t.Errorf("expected token3 for user2 to be unaffected, got err: %v", err3)
		}
		if claims3 == nil || claims3.UserID != userID2 {
			t.Errorf("expected claims3 UserID to be %s, got %+v", userID2, claims3)
		}
	})

	// 3. Fail-closed behavior on Redis lookup failure
	t.Run("RedisFailClosedOnUserCheck", func(t *testing.T) {
		tokenStr, err := GenerateToken("user-fail-closed", "customer", "fail@example.com")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		rdb := redis.NewClient(&redis.Options{Addr: "localhost:9999"})
		rdb.Close()

		SetRedisClient(rdb)
		defer SetRedisClient(nil)

		_, err = ValidateToken(tokenStr)
		if err == nil || !errors.Is(err, errors.New("jwtutil: security check failed (user invalidation lookup unreachable)")) {
			if err == nil || !testing.Verbose() && err.Error() == "" {
				t.Errorf("expected ValidateToken to fail closed on user invalidation check error, got: %v", err)
			}
		}

		err = RevokeAllUserTokens("user-fail-closed")
		if err == nil {
			t.Error("expected RevokeAllUserTokens to fail on closed Redis client")
		}
	})
}

func TestGenerateSecureToken(t *testing.T) {
	tok, err := GenerateSecureToken()
	if err != nil {
		t.Fatalf("GenerateSecureToken failed: %v", err)
	}
	if len(tok) != 64 {
		t.Errorf("Expected hex string of length 64 (32 bytes), got length %d", len(tok))
	}
}

type failNHook struct {
	mu          sync.Mutex
	failCount   int
	maxFailures int
	failMessage string
}

func (h *failNHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *failNHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.mu.Lock()
		if h.failCount < h.maxFailures {
			h.failCount++
			h.mu.Unlock()
			return errors.New(h.failMessage)
		}
		h.mu.Unlock()
		return next(ctx, cmd)
	}
}
func (h *failNHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

type customCmdHook struct {
	onProcess func(ctx context.Context, cmd redis.Cmder) error
}

func (h *customCmdHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *customCmdHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.onProcess != nil {
			if err := h.onProcess(ctx, cmd); err != nil {
				return err
			}
		}
		return next(ctx, cmd)
	}
}
func (h *customCmdHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestValidateToken_GracefulDegradation(t *testing.T) {
	Init("super-secret-key-that-is-at-least-thirty-two-bytes-long")

	// 1. Single transient connectivity blip is absorbed on retry and token validates successfully
	t.Run("TransientBlipAbsorbed", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()

		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		// Add hook that fails exactly once with a connection reset error
		hook := &failNHook{
			maxFailures: 1,
			failMessage: "read: connection reset by peer (transient blip)",
		}
		rdb.AddHook(hook)

		SetRedisClient(rdb)
		defer SetRedisClient(nil)
		ResetHealthTracker()

		tokenStr, err := GenerateToken("user-blip-1", "customer", "blip@example.com")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		// ValidateToken should catch the 1st failure, retry immediately, succeed, and return valid claims
		claims, err := ValidateToken(tokenStr)
		if err != nil {
			t.Fatalf("expected transient blip to be absorbed and token validated successfully, got error: %v", err)
		}
		if claims == nil || claims.UserID != "user-blip-1" {
			t.Errorf("expected valid claims with UserID 'user-blip-1', got: %+v", claims)
		}

		lastSuccess, failures := GetHealthTrackerStats()
		if failures != 0 {
			t.Errorf("expected consecutiveFailures to be 0 after absorbed blip and successful retry, got %d", failures)
		}
		if lastSuccess.IsZero() {
			t.Error("expected lastSuccessTime to be recorded after successful retry")
		}
	})

	// 2. Genuine sustained outage fails closed and preserves security guarantees
	t.Run("SustainedOutageFailsClosed", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()

		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		// Add hook that fails all requests (simulating sustained network partition / dead cluster)
		hook := &failNHook{
			maxFailures: 1000,
			failMessage: "dial tcp 127.0.0.1:6379: connect: connection refused (sustained outage)",
		}
		rdb.AddHook(hook)

		SetRedisClient(rdb)
		defer SetRedisClient(nil)
		ResetHealthTracker()

		tokenStr, err := GenerateToken("user-outage-1", "customer", "outage@example.com")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		// First call: tries initial + 1 retry, both fail -> fails closed
		claims, err := ValidateToken(tokenStr)
		if err == nil {
			t.Fatal("expected ValidateToken to fail closed on sustained Redis outage, but it succeeded")
		}
		if claims != nil {
			t.Errorf("expected nil claims on fail-closed rejection, got: %+v", claims)
		}

		// Check consecutive failures recorded
		_, failures := GetHealthTrackerStats()
		if failures < 2 {
			t.Errorf("expected at least 2 recorded failures from initial + retry attempt, got %d", failures)
		}

		// Subsequent call when threshold exceeded should fail closed
		_, err2 := ValidateToken(tokenStr)
		if err2 == nil {
			t.Fatal("expected second call during sustained outage to fail closed, but it succeeded")
		}
	})

	// 3. User Invalidation Check absorbs transient blip on retry
	t.Run("UserInvalidationTransientBlipAbsorbed", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()

		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		// Hook that fails only on "get" command (per-user check), exactly once
		var getFailCount int
		var getMu sync.Mutex
		hook := &customCmdHook{
			onProcess: func(ctx context.Context, cmd redis.Cmder) error {
				if cmd.Name() == "get" {
					getMu.Lock()
					defer getMu.Unlock()
					if getFailCount == 0 {
						getFailCount++
						return errors.New("temporary redis timeout on get")
					}
				}
				return nil
			},
		}
		rdb.AddHook(hook)

		SetRedisClient(rdb)
		defer SetRedisClient(nil)
		ResetHealthTracker()

		tokenStr, err := GenerateToken("user-blip-usercheck", "customer", "usercheck@example.com")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		claims, err := ValidateToken(tokenStr)
		if err != nil {
			t.Fatalf("expected transient blip on user invalidation check to be absorbed, got: %v", err)
		}
		if claims == nil || claims.UserID != "user-blip-usercheck" {
			t.Errorf("expected valid claims, got: %+v", claims)
		}
	})

	// 4. Write path RevokeAllUserTokens fails loudly without silent degradation
	t.Run("WritePathFailsLoudlyOnOutage", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()

		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer rdb.Close()

		hook := &failNHook{
			maxFailures: 10,
			failMessage: "redis: connection closed",
		}
		rdb.AddHook(hook)

		SetRedisClient(rdb)
		defer SetRedisClient(nil)

		err = RevokeAllUserTokens("user-write-test")
		if err == nil {
			t.Fatal("expected RevokeAllUserTokens write path to fail loudly on Redis error")
		}
	})
}

func TestAMRClaimRoundTrip(t *testing.T) {
	Init("super-secret-key-that-is-at-least-thirty-two-bytes-long")

	// 1. Password-only token
	tokPwd, err := GenerateToken("user-pwd", "owner", "pwd@test.com", []string{"pwd"})
	if err != nil {
		t.Fatalf("failed to generate pwd token: %v", err)
	}
	claimsPwd, err := ValidateToken(tokPwd)
	if err != nil {
		t.Fatalf("failed to validate pwd token: %v", err)
	}
	if len(claimsPwd.AMR) != 1 || claimsPwd.AMR[0] != "pwd" {
		t.Errorf("expected amr [pwd], got %v", claimsPwd.AMR)
	}

	// 2. Password + OTP token
	tokMfa, err := GenerateToken("user-mfa", "owner", "mfa@test.com", []string{"pwd", "otp"})
	if err != nil {
		t.Fatalf("failed to generate mfa token: %v", err)
	}
	claimsMfa, err := ValidateToken(tokMfa)
	if err != nil {
		t.Fatalf("failed to validate mfa token: %v", err)
	}
	if len(claimsMfa.AMR) != 2 || claimsMfa.AMR[0] != "pwd" || claimsMfa.AMR[1] != "otp" {
		t.Errorf("expected amr [pwd otp], got %v", claimsMfa.AMR)
	}

	// 3. GenerateTokenWithAMR helper
	tokWithAMR, err := GenerateTokenWithAMR("user-helper", "user", "helper@test.com", []string{"pwd", "otp"})
	if err != nil {
		t.Fatalf("failed to generate token with AMR helper: %v", err)
	}
	claimsHelper, err := ValidateToken(tokWithAMR)
	if err != nil {
		t.Fatalf("failed to validate token with AMR helper: %v", err)
	}
	if len(claimsHelper.AMR) != 2 || claimsHelper.AMR[0] != "pwd" || claimsHelper.AMR[1] != "otp" {
		t.Errorf("expected amr [pwd otp], got %v", claimsHelper.AMR)
	}

	// 4. Backward compatibility (no amr parameter passed)
	tokLegacy, err := GenerateToken("user-legacy", "owner", "legacy@test.com")
	if err != nil {
		t.Fatalf("failed to generate legacy token: %v", err)
	}
	claimsLegacy, err := ValidateToken(tokLegacy)
	if err != nil {
		t.Fatalf("failed to validate legacy token: %v", err)
	}
	if len(claimsLegacy.AMR) != 0 {
		t.Errorf("expected empty amr for legacy token, got %v", claimsLegacy.AMR)
	}
}

func TestSessionRevocation(t *testing.T) {
	Init("super-secret-key-that-is-at-least-thirty-two-bytes-long")

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	SetRedisClient(rdb)
	defer SetRedisClient(nil)

	// 1. Token without sid validates
	tokLegacy, err := GenerateToken("user-1", "user", "user1@example.com")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	claims, err := ValidateToken(tokLegacy)
	if err != nil {
		t.Fatalf("ValidateToken for legacy token failed: %v", err)
	}
	if claims.SID != "" {
		t.Errorf("expected empty SID, got %q", claims.SID)
	}

	// 2. Token with sid validates initially
	sid1 := "session-uuid-1"
	tok1, err := GenerateTokenWithSession("user-1", "user", "user1@example.com", sid1)
	if err != nil {
		t.Fatalf("GenerateTokenWithSession failed: %v", err)
	}
	claims1, err := ValidateToken(tok1)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims1.SID != sid1 {
		t.Errorf("expected SID %q, got %q", sid1, claims1.SID)
	}

	// 3. Token for second session
	sid2 := "session-uuid-2"
	tok2, err := GenerateTokenWithSession("user-1", "user", "user1@example.com", sid2)
	if err != nil {
		t.Fatalf("GenerateTokenWithSession failed: %v", err)
	}

	// 4. Revoke session 1
	if err := RevokeSession(sid1); err != nil {
		t.Fatalf("RevokeSession failed: %v", err)
	}

	// Session 1 is rejected
	_, err = ValidateToken(tok1)
	if err == nil || err.Error() != "jwtutil: session has been revoked" {
		t.Fatalf("expected session has been revoked error, got %v", err)
	}

	// Session 2 is still valid
	_, err = ValidateToken(tok2)
	if err != nil {
		t.Fatalf("expected session 2 to remain valid, got: %v", err)
	}

	// Legacy token without sid is still valid
	_, err = ValidateToken(tokLegacy)
	if err != nil {
		t.Fatalf("expected legacy token without sid to remain valid, got: %v", err)
	}

	// 5. Fail closed when Redis unreachable
	badRdb := redis.NewClient(&redis.Options{Addr: "localhost:9999"})
	badRdb.Close()
	SetRedisClient(badRdb)

	_, err = ValidateToken(tok2)
	if err == nil {
		t.Fatal("expected fail-closed error when Redis unreachable, got nil")
	}

	// Token without sid does not query Redis sid key, so it validates even if Redis is unreachable (unless user check triggers)
	// (Note: user invalidation check also queries Redis so badRdb fails on user check for non-empty UserID, which is expected fail-closed).
}

func TestCheckRevocationAndPublish(t *testing.T) {
	Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	SetRedisClient(rdb)
	defer SetRedisClient(nil)

	// Subscribe to account:events channel
	ctx := context.Background()
	pubsub := rdb.Subscribe(ctx, "account:events")
	defer pubsub.Close()
	if _, err := pubsub.Receive(ctx); err != nil {
		t.Fatalf("subscribe to account:events failed: %v", err)
	}

	sid := "sess-test-pubsub-1"
	tok, err := GenerateTokenWithSession("user-pub-1", "user", "pub1@example.com", sid)
	if err != nil {
		t.Fatalf("GenerateTokenWithSession failed: %v", err)
	}
	claims, err := ValidateToken(tok)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	// CheckRevocation on valid token returns nil
	if err := CheckRevocation(claims); err != nil {
		t.Fatalf("expected nil from CheckRevocation on valid claims, got %v", err)
	}

	// RevokeSession should publish to account:events
	if err := RevokeSession(sid); err != nil {
		t.Fatalf("RevokeSession failed: %v", err)
	}

	msgCh := pubsub.Channel()
	select {
	case msg := <-msgCh:
		var evt struct {
			Action string `json:"action"`
			SID    string `json:"sid"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
			t.Fatalf("failed to parse event payload: %v", err)
		}
		if evt.Action != "SESSION_REVOKED" || evt.SID != sid {
			t.Fatalf("unexpected event payload: %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session revocation event on account:events")
	}

	// CheckRevocation now returns ErrSessionRevoked
	if err := CheckRevocation(claims); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked, got %v", err)
	}
}

// Only HS256 is accepted: a token signed with the right secret but another
// HMAC size, an RSA key, or no signature at all is rejected, expired or not.
func TestValidateToken_OnlyHS256(t *testing.T) {
	Init("super-secret-key-that-is-at-least-thirty-two-bytes-long")
	SetRedisClient(nil)

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(t *testing.T, m jwt.SigningMethod, key interface{}, exp time.Time) string {
		t.Helper()
		claims := Claims{UserID: "u1", Role: "user", RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ID:        "jti-1",
		}}
		s, err := jwt.NewWithClaims(m, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	for _, exp := range []time.Time{time.Now().Add(time.Hour), time.Now().Add(-time.Hour)} {
		cases := []struct {
			name   string
			method jwt.SigningMethod
			key    interface{}
		}{
			{"none", jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType},
			{"HS384", jwt.SigningMethodHS384, getSecret()},
			{"HS512", jwt.SigningMethodHS512, getSecret()},
			{"RS256", jwt.SigningMethodRS256, rsaKey},
		}
		for _, tc := range cases {
			claims, err := ValidateToken(sign(t, tc.method, tc.key, exp))
			if err == nil || errors.Is(err, ErrExpiredToken) || claims != nil {
				t.Fatalf("%s (exp %v): got claims=%v err=%v, want rejection", tc.name, exp, claims, err)
			}
		}
	}

	if _, err := ValidateToken(sign(t, jwt.SigningMethodHS256, getSecret(), time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("HS256 token rejected: %v", err)
	}
}

func TestAccessTTL_Bounds(t *testing.T) {
	defer func() { _ = SetAccessTTL(DefaultAccessTTL) }()
	if AccessTTL() != 24*time.Hour {
		t.Fatalf("default access TTL = %v, want 24h", AccessTTL())
	}
	for _, d := range []time.Duration{0, -time.Minute, 4*time.Minute + 59*time.Second, 24*time.Hour + time.Second, 48 * time.Hour} {
		if err := SetAccessTTL(d); err == nil {
			t.Fatalf("SetAccessTTL(%v) accepted, want refusal", d)
		}
		if AccessTTL() != 24*time.Hour {
			t.Fatalf("refused SetAccessTTL(%v) changed the TTL to %v", d, AccessTTL())
		}
	}
	for _, d := range []time.Duration{5 * time.Minute, time.Hour, 24 * time.Hour} {
		if err := SetAccessTTL(d); err != nil {
			t.Fatalf("SetAccessTTL(%v): %v", d, err)
		}
		before := time.Now()
		tok, err := GenerateToken("ttl-user", "user", "ttl@example.com")
		if err != nil {
			t.Fatal(err)
		}
		claims := &Claims{}
		if _, _, err := jwt.NewParser().ParseUnverified(tok, claims); err != nil {
			t.Fatal(err)
		}
		got := claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time)
		if got != d {
			t.Fatalf("TTL %v: exp - iat = %v", d, got)
		}
		if claims.IssuedAt.Time.After(before.Add(time.Second)) {
			t.Fatalf("iat %v not near issue time %v", claims.IssuedAt.Time, before)
		}
	}
}
