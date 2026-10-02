// Package otp provides short-lived single-use code storage (email OTP,
// password-reset codes/tokens, opaque refresh tokens) with Redis or
// in-process memory backends, plus code/token generators.
package otp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	CodeCooldown       = 60 * time.Second
	MaxCodesPerHour    = 5
	MaxFailuresPerHour = 15
)

// Store is a TTL key→hash store with atomic compare-and-delete consume.
type Store interface {
	Set(ctx context.Context, key, hash string, ttl time.Duration) error
	// Consume deletes key only when the stored hash matches; true on match.
	Consume(ctx context.Context, key, hash string) (bool, error)
	// ConsumeWithAttempts checks code with an attempt limit. After maxAttempts wrong
	// attempts, the code is deleted. Resets attempts on match.
	ConsumeWithAttempts(ctx context.Context, key, hash string, maxAttempts int, ttl time.Duration) (bool, error)
	// ResetAttempts resets the attempt counter for key.
	ResetAttempts(ctx context.Context, key string) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
	// Take atomically returns the stored value and deletes the key,
	// so concurrent takers race for exactly one winner. Empty when absent.
	Take(ctx context.Context, key string) (string, error)

	// AllowIssue atomically checks if code issuance is allowed for (purpose, email),
	// enforcing 60s cooldown and max 5 codes per hour. Returns false if blocked.
	AllowIssue(ctx context.Context, purpose, email string) (bool, error)
	// FailuresExceeded checks whether the cumulative failure count for (purpose, email) >= maxFailures.
	FailuresExceeded(ctx context.Context, purpose, email string, maxFailures int) (bool, error)
	// RecordFailure atomically increments the failure count for (purpose, email) with TTL.
	RecordFailure(ctx context.Context, purpose, email string, ttl time.Duration) (int, error)
	// ClearFailures resets/deletes the failure counter for (purpose, email).
	ClearFailures(ctx context.Context, purpose, email string) error
	// ClearCooldown clears the issuance cooldown for (purpose, email).
	ClearCooldown(ctx context.Context, purpose, email string) error
}

// HashToken returns the hex SHA-256 of s for at-rest comparison.
func HashToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// GenerateNumericCode returns an n-digit crypto-random numeric code.
func GenerateNumericCode(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("otp: invalid code length %d", n)
	}
	var b [1]byte
	digits := make([]byte, 0, n)
	for len(digits) < n {
		if _, err := rand.Read(b[:]); err != nil {
			return "", fmt.Errorf("otp: random: %w", err)
		}
		d := b[0] % 10
		// Reject the 256 % 10 bias tail (250-255) for uniform digits.
		if b[0] > 249 {
			continue
		}
		digits = append(digits, '0'+d)
	}
	return string(digits), nil
}

// GenerateOpaqueToken returns a 32-byte hex token for refresh/reset possession.
func GenerateOpaqueToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("otp: random: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

type memEntry struct {
	hash      string
	expiresAt time.Time
}

// MemoryStore is an in-process TTL code store (tests, localhost without redis).
type MemoryStore struct {
	mu          sync.Mutex
	data        map[string]memEntry
	attempts    map[string]int
	cooldowns   map[string]time.Time
	issued      map[string]int
	issuedExp   map[string]time.Time
	failures    map[string]int
	failuresExp map[string]time.Time
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data:        map[string]memEntry{},
		attempts:    map[string]int{},
		cooldowns:   map[string]time.Time{},
		issued:      map[string]int{},
		issuedExp:   map[string]time.Time{},
		failures:    map[string]int{},
		failuresExp: map[string]time.Time{},
	}
}

// AllowIssue atomically checks if code issuance is allowed for (purpose, email),
// enforcing 60s cooldown and max 5 codes per hour.
func (s *MemoryStore) AllowIssue(_ context.Context, purpose, email string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	emailHash := HashToken(strings.ToLower(strings.TrimSpace(email)))
	cdKey := purpose + ":cooldown:" + emailHash
	issKey := purpose + ":issued:" + emailHash
	now := time.Now()

	if exp, ok := s.cooldowns[cdKey]; ok {
		if now.Before(exp) {
			return false, nil
		}
		delete(s.cooldowns, cdKey)
	}

	if exp, ok := s.issuedExp[issKey]; ok {
		if now.After(exp) {
			delete(s.issued, issKey)
			delete(s.issuedExp, issKey)
		}
	}
	cnt := s.issued[issKey]
	if cnt >= MaxCodesPerHour {
		return false, nil
	}

	s.cooldowns[cdKey] = now.Add(CodeCooldown)
	s.issued[issKey] = cnt + 1
	if cnt == 0 {
		s.issuedExp[issKey] = now.Add(time.Hour)
	}
	return true, nil
}

// FailuresExceeded checks whether the cumulative failure count for (purpose, email) >= maxFailures.
func (s *MemoryStore) FailuresExceeded(_ context.Context, purpose, email string, maxFailures int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	emailHash := HashToken(strings.ToLower(strings.TrimSpace(email)))
	failKey := purpose + ":fails:" + emailHash
	now := time.Now()

	if exp, ok := s.failuresExp[failKey]; ok {
		if now.After(exp) {
			delete(s.failures, failKey)
			delete(s.failuresExp, failKey)
			return false, nil
		}
	}
	return s.failures[failKey] >= maxFailures, nil
}

// RecordFailure atomically increments the failure count for (purpose, email) with TTL.
func (s *MemoryStore) RecordFailure(_ context.Context, purpose, email string, ttl time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	emailHash := HashToken(strings.ToLower(strings.TrimSpace(email)))
	failKey := purpose + ":fails:" + emailHash
	now := time.Now()

	if exp, ok := s.failuresExp[failKey]; ok {
		if now.After(exp) {
			delete(s.failures, failKey)
			delete(s.failuresExp, failKey)
		}
	}

	cnt := s.failures[failKey] + 1
	s.failures[failKey] = cnt
	if cnt == 1 {
		s.failuresExp[failKey] = now.Add(ttl)
	}
	return cnt, nil
}

// ClearFailures resets/deletes the failure counter for (purpose, email).
func (s *MemoryStore) ClearFailures(_ context.Context, purpose, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	emailHash := HashToken(strings.ToLower(strings.TrimSpace(email)))
	failKey := purpose + ":fails:" + emailHash
	delete(s.failures, failKey)
	delete(s.failuresExp, failKey)
	return nil
}

// ClearCooldown clears the issuance cooldown for (purpose, email).
func (s *MemoryStore) ClearCooldown(_ context.Context, purpose, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	emailHash := HashToken(strings.ToLower(strings.TrimSpace(email)))
	delete(s.cooldowns, purpose+":cooldown:"+emailHash)
	return nil
}

// Set stores hash under key for ttl and resets attempts.
func (s *MemoryStore) Set(_ context.Context, key, hash string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = memEntry{hash: hash, expiresAt: time.Now().Add(ttl)}
	delete(s.attempts, key)
	return nil
}

// Consume deletes key when the stored unexpired hash matches.
func (s *MemoryStore) Consume(ctx context.Context, key, hash string) (bool, error) {
	return s.ConsumeWithAttempts(ctx, key, hash, 0, 0)
}

// ConsumeWithAttempts checks code with an attempt limit. After maxAttempts wrong
// attempts, the code is deleted. Resets attempts on match.
func (s *MemoryStore) ConsumeWithAttempts(_ context.Context, key, hash string, maxAttempts int, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	attempts := s.attempts[key]
	if maxAttempts > 0 && attempts >= maxAttempts {
		delete(s.data, key)
		return false, nil
	}

	e, ok := s.data[key]
	if !ok {
		return false, nil
	}
	if time.Now().After(e.expiresAt) {
		delete(s.data, key)
		delete(s.attempts, key)
		return false, nil
	}

	if e.hash != hash {
		if maxAttempts > 0 {
			s.attempts[key] = attempts + 1
			if s.attempts[key] >= maxAttempts {
				delete(s.data, key)
			}
		}
		return false, nil
	}

	delete(s.data, key)
	delete(s.attempts, key)
	return true, nil
}

// ResetAttempts clears the attempt counter for key.
func (s *MemoryStore) ResetAttempts(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, key)
	return nil
}

// Get returns the stored hash, or "" when absent/expired.
func (s *MemoryStore) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok || time.Now().After(e.expiresAt) {
		delete(s.data, key)
		return "", nil
	}
	return e.hash, nil
}

// Delete removes key.
func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	delete(s.attempts, key)
	return nil
}

// Take atomically returns the value and deletes the key under one lock.
func (s *MemoryStore) Take(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok || time.Now().After(e.expiresAt) {
		delete(s.data, key)
		delete(s.attempts, key)
		return "", nil
	}
	delete(s.data, key)
	delete(s.attempts, key)
	return e.hash, nil
}

const consumeScript = `
local v = redis.call('GET', KEYS[1])
if not v then return 0 end
if v ~= ARGV[1] then return 0 end
redis.call('DEL', KEYS[1])
return 1
`

const consumeWithAttemptsScript = `
local codeKey = KEYS[1]
local attemptsKey = KEYS[2]
local expectedHash = ARGV[1]
local maxAttempts = tonumber(ARGV[2])
local ttlSec = tonumber(ARGV[3])

local attempts = tonumber(redis.call('GET', attemptsKey) or '0')
if maxAttempts > 0 and attempts >= maxAttempts then
    redis.call('DEL', codeKey)
    return 0
end

local storedHash = redis.call('GET', codeKey)
if not storedHash then
    return 0
end

if storedHash == expectedHash then
    redis.call('DEL', codeKey)
    redis.call('DEL', attemptsKey)
    return 1
else
    if maxAttempts > 0 then
        attempts = redis.call('INCR', attemptsKey)
        if attempts == 1 and ttlSec > 0 then
            redis.call('EXPIRE', attemptsKey, ttlSec)
        end
        if attempts >= maxAttempts then
            redis.call('DEL', codeKey)
        end
    end
    return 0
end
`

const allowIssueScript = `
local cooldownKey = KEYS[1]
local issuedKey = KEYS[2]
local cooldownSec = tonumber(ARGV[1])
local maxIssued = tonumber(ARGV[2])
local issuedTTL = tonumber(ARGV[3])

if redis.call('EXISTS', cooldownKey) == 1 then
    return 0
end

local issued = tonumber(redis.call('GET', issuedKey) or '0')
if issued >= maxIssued then
    return 0
end

redis.call('SET', cooldownKey, '1', 'EX', cooldownSec)
local newIssued = redis.call('INCR', issuedKey)
if newIssued == 1 then
    redis.call('EXPIRE', issuedKey, issuedTTL)
end
return 1
`

const recordFailureScript = `
local failsKey = KEYS[1]
local failsTTL = tonumber(ARGV[1])

local cnt = redis.call('INCR', failsKey)
if cnt == 1 then
    redis.call('EXPIRE', failsKey, failsTTL)
end
return cnt
`

// RedisStore is a Redis-backed TTL code store.
type RedisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore wraps client with a key prefix.
func NewRedisStore(client *redis.Client, prefix string) *RedisStore {
	return &RedisStore{client: client, prefix: prefix}
}

func (s *RedisStore) fullKey(key string) string {
	return "otp:" + s.prefix + ":" + key
}

func (s *RedisStore) attemptsKey(key string) string {
	return "otp:" + s.prefix + ":attempts:" + key
}

func (s *RedisStore) cooldownKey(purpose, email string) string {
	return "otp:" + s.prefix + ":" + purpose + ":cooldown:" + HashToken(strings.ToLower(strings.TrimSpace(email)))
}

func (s *RedisStore) issuedKey(purpose, email string) string {
	return "otp:" + s.prefix + ":" + purpose + ":issued:" + HashToken(strings.ToLower(strings.TrimSpace(email)))
}

func (s *RedisStore) failsKey(purpose, email string) string {
	return "otp:" + s.prefix + ":" + purpose + ":fails:" + HashToken(strings.ToLower(strings.TrimSpace(email)))
}

// AllowIssue atomically checks if code issuance is allowed for (purpose, email),
// enforcing 60s cooldown and max 5 codes per hour.
func (s *RedisStore) AllowIssue(ctx context.Context, purpose, email string) (bool, error) {
	cdKey := s.cooldownKey(purpose, email)
	issKey := s.issuedKey(purpose, email)
	res, err := s.client.Eval(ctx, allowIssueScript, []string{cdKey, issKey}, int(CodeCooldown.Seconds()), MaxCodesPerHour, 3600).Result()
	if err != nil {
		return false, err
	}
	n, _ := res.(int64)
	return n == 1, nil
}

// FailuresExceeded checks whether cumulative failures for (purpose, email) >= maxFailures.
func (s *RedisStore) FailuresExceeded(ctx context.Context, purpose, email string, maxFailures int) (bool, error) {
	v, err := s.client.Get(ctx, s.failsKey(purpose, email)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cnt, _ := strconv.Atoi(v)
	return cnt >= maxFailures, nil
}

// RecordFailure atomically increments the failure count for (purpose, email) with TTL.
func (s *RedisStore) RecordFailure(ctx context.Context, purpose, email string, ttl time.Duration) (int, error) {
	ttlSec := int(ttl.Seconds())
	if ttlSec <= 0 {
		ttlSec = 3600
	}
	res, err := s.client.Eval(ctx, recordFailureScript, []string{s.failsKey(purpose, email)}, ttlSec).Result()
	if err != nil {
		return 0, err
	}
	cnt, _ := res.(int64)
	return int(cnt), nil
}

// ClearFailures resets/deletes the failure counter for (purpose, email).
func (s *RedisStore) ClearFailures(ctx context.Context, purpose, email string) error {
	return s.client.Del(ctx, s.failsKey(purpose, email)).Err()
}

// ClearCooldown clears the issuance cooldown for (purpose, email).
func (s *RedisStore) ClearCooldown(ctx context.Context, purpose, email string) error {
	return s.client.Del(ctx, s.cooldownKey(purpose, email)).Err()
}

// Set stores hash under key for ttl and resets attempts.
func (s *RedisStore) Set(ctx context.Context, key, hash string, ttl time.Duration) error {
	pipe := s.client.Pipeline()
	pipe.Del(ctx, s.attemptsKey(key))
	pipe.Set(ctx, s.fullKey(key), hash, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// Consume atomically compares and deletes on match.
func (s *RedisStore) Consume(ctx context.Context, key, hash string) (bool, error) {
	res, err := s.client.Eval(ctx, consumeScript, []string{s.fullKey(key)}, hash).Result()
	if err != nil {
		return false, err
	}
	n, _ := res.(int64)
	return n == 1, nil
}

// ConsumeWithAttempts checks code with an attempt limit. After maxAttempts wrong
// attempts, the code is deleted. Resets attempts on match.
func (s *RedisStore) ConsumeWithAttempts(ctx context.Context, key, hash string, maxAttempts int, ttl time.Duration) (bool, error) {
	ttlSec := int(ttl.Seconds())
	if ttlSec <= 0 {
		ttlSec = 600
	}
	res, err := s.client.Eval(ctx, consumeWithAttemptsScript, []string{s.fullKey(key), s.attemptsKey(key)}, hash, maxAttempts, ttlSec).Result()
	if err != nil {
		return false, err
	}
	n, _ := res.(int64)
	return n == 1, nil
}

// ResetAttempts clears the attempt counter for key.
func (s *RedisStore) ResetAttempts(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.attemptsKey(key)).Err()
}

// Get returns the stored hash, or "" when absent.
func (s *RedisStore) Get(ctx context.Context, key string) (string, error) {
	v, err := s.client.Get(ctx, s.fullKey(key)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return v, err
}

// Delete removes key and its attempt counter.
func (s *RedisStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.fullKey(key), s.attemptsKey(key)).Err()
}

// Take atomically returns the value and deletes the key via GETDEL
// (server-side atomic; supported since Redis 6.2, image is redis:7).
func (s *RedisStore) Take(ctx context.Context, key string) (string, error) {
	_ = s.client.Del(ctx, s.attemptsKey(key))
	v, err := s.client.GetDel(ctx, s.fullKey(key)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return v, err
}
