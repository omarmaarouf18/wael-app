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
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
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
	mu       sync.Mutex
	data     map[string]memEntry
	attempts map[string]int
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data:     map[string]memEntry{},
		attempts: map[string]int{},
	}
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
