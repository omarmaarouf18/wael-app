package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
)

func TestOnboardAdmin_Success_TokenPrintedOnceAndOnlyHashStored(t *testing.T) {
	st := store.NewMemoryStore()
	getStore := func(ctx context.Context) (store.Store, error) {
		return st, nil
	}

	var stdout, stderr bytes.Buffer
	args := []string{"--name", "Alice Admin", "--ttl", "30d"}
	code := runOnboardAdmin(args, &stdout, &stderr, getStore)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	token := strings.TrimSpace(stdout.String())
	if token == "" {
		t.Fatal("expected token on stdout, got empty string")
	}

	// Verify token is base64url encoded and represents at least 32 bytes
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not valid base64url: %v", err)
	}
	if len(decoded) < 32 {
		t.Fatalf("expected >= 32 bytes of entropy, got %d", len(decoded))
	}

	// Compute expected SHA-256 hash
	h := sha256.Sum256([]byte(token))
	expectedHash := hex.EncodeToString(h[:])

	// Lookup admin by hash in store
	adm, err := st.FindAdminByTokenHash(context.Background(), expectedHash)
	if err != nil || adm == nil {
		t.Fatalf("admin not found in store by token hash: %v", err)
	}
	if adm.Name != "Alice Admin" {
		t.Fatalf("expected admin name %q, got %q", "Alice Admin", adm.Name)
	}
	if adm.TokenHash != expectedHash {
		t.Fatalf("stored token_hash %q != expected %q", adm.TokenHash, expectedHash)
	}
	// Plaintext token must not equal the stored token_hash
	if adm.TokenHash == token {
		t.Fatal("security violation: plaintext token stored directly in database")
	}

	// Verify expiry is approximately 30 days from now
	expectedExpiry := time.Now().UTC().Add(30 * 24 * time.Hour)
	if diff := adm.ExpiresAt.Sub(expectedExpiry); diff > time.Minute || diff < -time.Minute {
		t.Fatalf("unexpected expiry: %v (expected close to %v)", adm.ExpiresAt, expectedExpiry)
	}
}

func TestOnboardAdmin_MissingName(t *testing.T) {
	st := store.NewMemoryStore()
	getStore := func(ctx context.Context) (store.Store, error) {
		return st, nil
	}

	var stdout, stderr bytes.Buffer
	code := runOnboardAdmin([]string{"--ttl", "30d"}, &stdout, &stderr, getStore)
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing name, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--name is required") {
		t.Fatalf("expected '--name is required' in stderr, got: %s", stderr.String())
	}
}

func TestOnboardAdmin_MaxTTLConstraint(t *testing.T) {
	st := store.NewMemoryStore()
	getStore := func(ctx context.Context) (store.Store, error) {
		return st, nil
	}

	var stdout, stderr bytes.Buffer
	code := runOnboardAdmin([]string{"--name", "Bob", "--ttl", "366d"}, &stdout, &stderr, getStore)
	if code != 1 {
		t.Fatalf("expected exit code 1 for TTL > 365d, got %d", code)
	}
	if !strings.Contains(stderr.String(), "ttl exceeds maximum") {
		t.Fatalf("expected 'ttl exceeds maximum' in stderr, got: %s", stderr.String())
	}
}
