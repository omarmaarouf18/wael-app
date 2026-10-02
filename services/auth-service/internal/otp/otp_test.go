package otp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestMemoryStore_ConsumeSingleUse(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if err := s.Set(ctx, "k", HashToken("123456"), time.Minute); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Consume(ctx, "k", HashToken("123456"))
	if err != nil || !ok {
		t.Fatalf("Consume = %v, %v", ok, err)
	}
	ok, _ = s.Consume(ctx, "k", HashToken("123456"))
	if ok {
		t.Fatal("second consume must fail (single-use)")
	}
}

func TestMemoryStore_WrongCodeAndExpiry(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if err := s.Set(ctx, "k", HashToken("123456"), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Consume(ctx, "k", HashToken("000000")); ok {
		t.Fatal("wrong code must not consume")
	}
	time.Sleep(40 * time.Millisecond)
	if ok, _ := s.Consume(ctx, "k", HashToken("123456")); ok {
		t.Fatal("expired code must not consume")
	}
}

func TestGenerateNumericCode_Length(t *testing.T) {
	code, err := GenerateNumericCode(6)
	if err != nil || len(code) != 6 {
		t.Fatalf("code = %q, %v", code, err)
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Fatalf("non-digit in %q", code)
		}
	}
}

func testSingleRedemption(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	key := fmt.Sprintf("race-key-%d", time.Now().UnixNano())
	code := "123456"
	hash := HashToken(code)

	if err := s.Set(ctx, key, hash, time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	var winners atomic.Int32

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := s.Consume(ctx, key, hash)
			if err != nil {
				t.Errorf("Consume error: %v", err)
				return
			}
			if ok {
				winners.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if got := winners.Load(); got != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", got)
	}

	ok, _ := s.Consume(ctx, key, hash)
	if ok {
		t.Fatal("second Consume succeeded, expected false (single-use)")
	}
}

func TestMemoryStore_SingleRedemption(t *testing.T) {
	s := NewMemoryStore()
	testSingleRedemption(t, s)
}

func testConsumeWithAttempts(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	key := fmt.Sprintf("attempt-key-%d", time.Now().UnixNano())
	code := "123456"
	hash := HashToken(code)

	if err := s.Set(ctx, key, hash, time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// 5 wrong attempts -> all refused
	for i := 1; i <= 5; i++ {
		ok, err := s.ConsumeWithAttempts(ctx, key, HashToken("000000"), 5, time.Minute)
		if err != nil {
			t.Fatalf("wrong attempt %d error: %v", i, err)
		}
		if ok {
			t.Fatalf("wrong attempt %d succeeded, want false", i)
		}
	}

	// 6th attempt with the RIGHT code is refused (code was deleted after 5 wrong attempts)
	ok, err := s.ConsumeWithAttempts(ctx, key, hash, 5, time.Minute)
	if err != nil {
		t.Fatalf("6th attempt error: %v", err)
	}
	if ok {
		t.Fatal("6th attempt with RIGHT code succeeded, want refused")
	}

	// Code was deleted from store
	val, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if val != "" {
		t.Fatalf("code still exists after 5 wrong attempts: %q", val)
	}

	// New code works
	newCode := "654321"
	newHash := HashToken(newCode)
	if err := s.Set(ctx, key, newHash, time.Minute); err != nil {
		t.Fatalf("Set new code failed: %v", err)
	}
	ok, err = s.ConsumeWithAttempts(ctx, key, newHash, 5, time.Minute)
	if err != nil {
		t.Fatalf("new code attempt error: %v", err)
	}
	if !ok {
		t.Fatal("new code with RIGHT code failed, want succeeded")
	}

	// Counter resets on success:
	// Set code, do 2 wrong attempts, then 3rd is RIGHT code -> succeeds.
	resetKey := fmt.Sprintf("reset-counter-%d", time.Now().UnixNano())
	code2 := "111111"
	if err := s.Set(ctx, resetKey, HashToken(code2), time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	for i := 1; i <= 2; i++ {
		ok, err := s.ConsumeWithAttempts(ctx, resetKey, HashToken("000000"), 5, time.Minute)
		if err != nil {
			t.Fatalf("wrong attempt %d error: %v", i, err)
		}
		if ok {
			t.Fatalf("wrong attempt %d succeeded, want false", i)
		}
	}
	// 3rd attempt with right code succeeds and resets attempts
	ok, err = s.ConsumeWithAttempts(ctx, resetKey, HashToken(code2), 5, time.Minute)
	if err != nil {
		t.Fatalf("3rd attempt error: %v", err)
	}
	if !ok {
		t.Fatal("3rd attempt with right code failed, want succeeded")
	}

	// Set another code on same key: 4 wrong attempts, 5th is right code -> succeeds
	// (proving prior 2 wrong attempts were cleared on success).
	code3 := "222222"
	if err := s.Set(ctx, resetKey, HashToken(code3), time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	for i := 1; i <= 4; i++ {
		ok, err := s.ConsumeWithAttempts(ctx, resetKey, HashToken("000000"), 5, time.Minute)
		if err != nil {
			t.Fatalf("wrong attempt %d error: %v", i, err)
		}
		if ok {
			t.Fatalf("wrong attempt %d succeeded, want false", i)
		}
	}
	ok, err = s.ConsumeWithAttempts(ctx, resetKey, HashToken(code3), 5, time.Minute)
	if err != nil {
		t.Fatalf("5th attempt with right code error: %v", err)
	}
	if !ok {
		t.Fatal("5th attempt with right code failed, want succeeded")
	}
}

func TestMemoryStore_ConsumeWithAttempts(t *testing.T) {
	s := NewMemoryStore()
	testConsumeWithAttempts(t, s)
}

func TestRedisStore_SingleRedemption(t *testing.T) {
	_, redisURI := requireDB(t)
	opts, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("invalid REDIS_URI %q: %v", redisURI, err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("redis ping failed: %v", err)
		}
		t.Skipf("skipping test: redis unreachable: %v", err)
	}

	prefix := fmt.Sprintf("test_otp_%d", time.Now().UnixNano())
	s := NewRedisStore(client, prefix)
	testSingleRedemption(t, s)
}

func TestRedisStore_ConsumeWithAttempts(t *testing.T) {
	_, redisURI := requireDB(t)
	opts, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("invalid REDIS_URI %q: %v", redisURI, err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("redis ping failed: %v", err)
		}
		t.Skipf("skipping test: redis unreachable: %v", err)
	}

	prefix := fmt.Sprintf("test_otp_attempts_%d", time.Now().UnixNano())
	s := NewRedisStore(client, prefix)
	testConsumeWithAttempts(t, s)
}

func testAllowIssue(t *testing.T, s Store, clearCooldown func(purpose, email string)) {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("user-%d@example.com", time.Now().UnixNano())
	purpose := "reset"

	// 1. First call: allowed
	allowed, err := s.AllowIssue(ctx, purpose, email)
	if err != nil {
		t.Fatalf("AllowIssue error: %v", err)
	}
	if !allowed {
		t.Fatal("first AllowIssue returned false, want true")
	}

	// 2. Second call within 60s for same email: refused by cooldown
	allowed, err = s.AllowIssue(ctx, purpose, email)
	if err != nil {
		t.Fatalf("AllowIssue error: %v", err)
	}
	if allowed {
		t.Fatal("second AllowIssue within 60s returned true, want false (cooldown)")
	}

	// 3. Different email within 60s: allowed
	otherEmail := fmt.Sprintf("other-%d@example.com", time.Now().UnixNano())
	allowed, err = s.AllowIssue(ctx, purpose, otherEmail)
	if err != nil {
		t.Fatalf("AllowIssue other email error: %v", err)
	}
	if !allowed {
		t.Fatal("AllowIssue for other email returned false, want true")
	}

	// 4. Exhaust 5 codes per hour:
	for i := 2; i <= 5; i++ {
		clearCooldown(purpose, email)
		allowed, err = s.AllowIssue(ctx, purpose, email)
		if err != nil {
			t.Fatalf("AllowIssue %d error: %v", i, err)
		}
		if !allowed {
			t.Fatalf("AllowIssue %d returned false, want true", i)
		}
	}

	// 5. 6th code in an hour: refused even after cooldown cleared
	clearCooldown(purpose, email)
	allowed, err = s.AllowIssue(ctx, purpose, email)
	if err != nil {
		t.Fatalf("AllowIssue 6th error: %v", err)
	}
	if allowed {
		t.Fatal("6th AllowIssue returned true, want false (hourly cap)")
	}
}

func testFailures(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("fail-user-%d@example.com", time.Now().UnixNano())
	purpose := "reset"

	// 1. Initial state: not exceeded
	exceeded, err := s.FailuresExceeded(ctx, purpose, email, 15)
	if err != nil {
		t.Fatalf("FailuresExceeded error: %v", err)
	}
	if exceeded {
		t.Fatal("FailuresExceeded initially returned true, want false")
	}

	// 2. 20 parallel wrong verifies -> fails counter is exactly 20
	const goroutines = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, recErr := s.RecordFailure(ctx, purpose, email, time.Hour)
			if recErr != nil {
				errs <- recErr
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("RecordFailure error during concurrency: %v", err)
	}

	// Fails counter is >= 15
	exceeded, err = s.FailuresExceeded(ctx, purpose, email, 15)
	if err != nil {
		t.Fatalf("FailuresExceeded error: %v", err)
	}
	if !exceeded {
		t.Fatal("FailuresExceeded after 20 failures returned false, want true")
	}

	// 3. Clear failures
	if err := s.ClearFailures(ctx, purpose, email); err != nil {
		t.Fatalf("ClearFailures error: %v", err)
	}
	exceeded, err = s.FailuresExceeded(ctx, purpose, email, 15)
	if err != nil {
		t.Fatalf("FailuresExceeded error: %v", err)
	}
	if exceeded {
		t.Fatal("FailuresExceeded after ClearFailures returned true, want false")
	}

	// 4. Set does not clear fails
	cnt, err := s.RecordFailure(ctx, purpose, email, time.Hour)
	if err != nil {
		t.Fatalf("RecordFailure error: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("RecordFailure count = %d, want 1", cnt)
	}

	// Call Set on code store
	if err := s.Set(ctx, "reset-code:"+email, HashToken("123456"), 10*time.Minute); err != nil {
		t.Fatalf("Set error: %v", err)
	}

	// Record another failure -> count must be 2 (Set did not reset fails)
	cnt, err = s.RecordFailure(ctx, purpose, email, time.Hour)
	if err != nil {
		t.Fatalf("RecordFailure error: %v", err)
	}
	if cnt != 2 {
		t.Fatalf("RecordFailure count after Set = %d, want 2", cnt)
	}
}

func TestMemoryStore_AllowIssue(t *testing.T) {
	mem := NewMemoryStore()
	clearCooldown := func(purpose, email string) {
		mem.mu.Lock()
		delete(mem.cooldowns, purpose+":cooldown:"+HashToken(strings.ToLower(strings.TrimSpace(email))))
		mem.mu.Unlock()
	}
	testAllowIssue(t, mem, clearCooldown)
}

func TestMemoryStore_Failures(t *testing.T) {
	mem := NewMemoryStore()
	testFailures(t, mem)
}

func TestRedisStore_AllowIssue(t *testing.T) {
	_, redisURI := requireDB(t)
	opts, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("invalid REDIS_URI %q: %v", redisURI, err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("redis ping failed: %v", err)
		}
		t.Skipf("skipping test: redis unreachable: %v", err)
	}

	prefix := fmt.Sprintf("test_otp_allow_%d", time.Now().UnixNano())
	red := NewRedisStore(client, prefix)
	clearCooldown := func(purpose, email string) {
		_ = client.Del(context.Background(), red.cooldownKey(purpose, email)).Err()
	}
	testAllowIssue(t, red, clearCooldown)
}

func TestRedisStore_Failures(t *testing.T) {
	_, redisURI := requireDB(t)
	opts, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("invalid REDIS_URI %q: %v", redisURI, err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("redis ping failed: %v", err)
		}
		t.Skipf("skipping test: redis unreachable: %v", err)
	}

	prefix := fmt.Sprintf("test_otp_fails_%d", time.Now().UnixNano())
	red := NewRedisStore(client, prefix)
	testFailures(t, red)
}
