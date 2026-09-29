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
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
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
	mu   sync.Mutex
	data map[string]memEntry
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]memEntry{}}
}

// Set stores hash under key for ttl.
func (s *MemoryStore) Set(_ context.Context, key, hash string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = memEntry{hash: hash, expiresAt: time.Now().Add(ttl)}
	return nil
}

// Consume deletes key when the stored unexpired hash matches.
func (s *MemoryStore) Consume(_ context.Context, key, hash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok {
		return false, nil
	}
	if time.Now().After(e.expiresAt) {
		delete(s.data, key)
		return false, nil
	}
	if e.hash != hash {
		return false, nil
	}
	delete(s.data, key)
	return true, nil
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
	return nil
}

const consumeScript = `
local v = redis.call('GET', KEYS[1])
if not v then return 0 end
if v ~= ARGV[1] then return 0 end
redis.call('DEL', KEYS[1])
return 1
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

// Set stores hash under key for ttl.
func (s *RedisStore) Set(ctx context.Context, key, hash string, ttl time.Duration) error {
	return s.client.Set(ctx, s.fullKey(key), hash, ttl).Err()
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

// Get returns the stored hash, or "" when absent.
func (s *RedisStore) Get(ctx context.Context, key string) (string, error) {
	v, err := s.client.Get(ctx, s.fullKey(key)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return v, err
}

// Delete removes key.
func (s *RedisStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.fullKey(key)).Err()
}
