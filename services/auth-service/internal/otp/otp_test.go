package otp

import (
	"context"
	"testing"
	"time"
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
