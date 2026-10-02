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

func parseOnboardOutput(out string) (adminID, token string) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "admin_id: ") {
			adminID = strings.TrimPrefix(line, "admin_id: ")
		} else if strings.HasPrefix(line, "token: ") {
			token = strings.TrimPrefix(line, "token: ")
		}
	}
	return adminID, token
}

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

	adminID, token := parseOnboardOutput(stdout.String())
	if adminID == "" {
		t.Fatal("expected admin_id on stdout, got empty string")
	}
	if !strings.HasPrefix(adminID, "adm_") {
		t.Fatalf("expected admin_id to start with adm_, got %q", adminID)
	}
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
	if adm.ID != adminID {
		t.Fatalf("expected stored adm.ID %q == printed adminID %q", adm.ID, adminID)
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

func TestOnboardAdmin_AdminIDOutput(t *testing.T) {
	st := store.NewMemoryStore()
	getStore := func(ctx context.Context) (store.Store, error) {
		return st, nil
	}

	var stdout, stderr bytes.Buffer
	args := []string{"--name", "Test Admin", "--ttl", "14d"}
	code := runOnboardAdmin(args, &stdout, &stderr, getStore)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	out := stdout.String()
	adminID, token := parseOnboardOutput(out)
	if adminID == "" {
		t.Fatal("expected admin_id in stdout, got none")
	}
	if !strings.HasPrefix(adminID, "adm_") || len(adminID) != 28 {
		t.Fatalf("expected admin_id to start with 'adm_' and be 28 chars long, got %q", adminID)
	}
	if token == "" {
		t.Fatal("expected token in stdout, got none")
	}

	// Verify token is printed exactly once
	if strings.Count(out, token) != 1 {
		t.Fatalf("expected token to be printed exactly once, found %d occurrences", strings.Count(out, token))
	}
	// Verify admin_id is printed exactly once
	if strings.Count(out, adminID) != 1 {
		t.Fatalf("expected admin_id to be printed exactly once, found %d occurrences", strings.Count(out, adminID))
	}

	// Stored admin has matching ID
	hashBytes := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(hashBytes[:])
	adm, err := st.FindAdminByTokenHash(context.Background(), hash)
	if err != nil || adm == nil {
		t.Fatalf("failed to find admin by hash: %v", err)
	}
	if adm.ID != adminID {
		t.Fatalf("stored admin ID %q != printed admin ID %q", adm.ID, adminID)
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
