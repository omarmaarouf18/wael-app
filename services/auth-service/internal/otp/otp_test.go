package otp

import (
	"context"
	"fmt"
	"os"
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
