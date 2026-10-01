package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
)

func TestRevokeAdmin_SuccessAndIdempotent(t *testing.T) {
	st := store.NewMemoryStore()
	getStore := func(ctx context.Context) (store.Store, error) {
		return st, nil
	}

	// Setup active admin
	now := time.Now().UTC()
	adm := &models.Admin{
		ID:        "adm-test-1",
		Name:      "Operator To Revoke",
		TokenHash: "hash-revoke-1",
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
	}
	if err := st.CreateAdmin(context.Background(), adm); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	// 1. Revoke the admin
	var stdout, stderr bytes.Buffer
	code := runRevokeAdmin([]string{"--id", "adm-test-1"}, &stdout, &stderr, getStore)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "revoked") {
		t.Fatalf("expected stdout to mention revoked, got: %s", stdout.String())
	}

	revokedAdm, err := st.FindAdminByID(context.Background(), "adm-test-1")
	if err != nil || revokedAdm == nil {
		t.Fatalf("FindAdminByID: %v", err)
	}
	if revokedAdm.RevokedAt.IsZero() {
		t.Fatal("expected RevokedAt to be set")
	}

	// 2. Revoking again must be idempotent and succeed
	stdout.Reset()
	stderr.Reset()
	code2 := runRevokeAdmin([]string{"--id", "adm-test-1"}, &stdout, &stderr, getStore)
	if code2 != 0 {
		t.Fatalf("expected idempotent exit code 0, got %d (stderr: %s)", code2, stderr.String())
	}
}

func TestRevokeAdmin_NotFound(t *testing.T) {
	st := store.NewMemoryStore()
	getStore := func(ctx context.Context) (store.Store, error) {
		return st, nil
	}

	var stdout, stderr bytes.Buffer
	code := runRevokeAdmin([]string{"--id", "non-existent-adm"}, &stdout, &stderr, getStore)
	if code != 1 {
		t.Fatalf("expected exit code 1 for non-existent admin, got %d", code)
	}
	if !strings.Contains(stderr.String(), "not found") {
		t.Fatalf("expected stderr to contain 'not found', got: %s", stderr.String())
	}
}

func TestRevokeAdmin_MissingID(t *testing.T) {
	st := store.NewMemoryStore()
	getStore := func(ctx context.Context) (store.Store, error) {
		return st, nil
	}

	var stdout, stderr bytes.Buffer
	code := runRevokeAdmin([]string{}, &stdout, &stderr, getStore)
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing --id, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--id is required") {
		t.Fatalf("expected stderr to contain '--id is required', got: %s", stderr.String())
	}
}
