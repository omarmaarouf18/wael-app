package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStorage(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	encKey := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32)) // 64 hex chars (32 bytes)
	store, err := NewLocalStorage(tempDir, encKey, "test")
	if err != nil {
		t.Fatalf("failed to create LocalStorage: %v", err)
	}

	ctx := context.Background()

	// 1. Test Upload success with canonical UUID
	fileContent := "test document binary content"
	key := "b1eebc99-9c0b-4ef8-bb6d-6bb9bd380a22"
	err = store.Upload(ctx, key, strings.NewReader(fileContent), "image/jpeg")
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// 2. Test Upload directory traversal protection
	err = store.Upload(ctx, "../../../etc/passwd", strings.NewReader("bad content"), "text/plain")
	if err == nil {
		t.Fatalf("expected upload directory traversal to be rejected, got nil")
	}

	// 3. Test OpenFile success
	rc, err := store.OpenFile(key)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer rc.Close()

	readBytes, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("failed to read file content: %v", err)
	}
	if string(readBytes) != fileContent {
		t.Errorf("Expected content %q, got %q", fileContent, string(readBytes))
	}

	// 4. Test OpenFile non-existent file
	_, err = store.OpenFile("e4eebc99-9c0b-4ef8-bb6d-6bb9bd380a55")
	if err == nil {
		t.Errorf("Expected error for non-existent file, got nil")
	}

	// 5. Test OpenFile directory traversal protection
	_, err = store.OpenFile("../../etc/passwd")
	if err == nil {
		t.Errorf("Expected error for directory traversal open, got nil")
	}
}

func TestLocalStorage_EncryptionAtRest(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage-test-enc-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	encKey := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	store, err := NewLocalStorage(tempDir, encKey, "test")
	if err != nil {
		t.Fatalf("failed to create LocalStorage: %v", err)
	}

	ctx := context.Background()
	plainContent := "Super confidential plaintext document"
	key := "c2eebc99-9c0b-4ef8-bb6d-6bb9bd380a33"

	if err := store.Upload(ctx, key, strings.NewReader(plainContent), "image/png"); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	rawBytes, err := os.ReadFile(filepath.Join(tempDir, key))
	if err != nil {
		t.Fatalf("Failed to read raw on-disk file: %v", err)
	}

	if bytes.Contains(rawBytes, []byte(plainContent)) {
		t.Fatalf("Vulnerability detected: Raw file on disk contains unencrypted plaintext!")
	}

	rc, err := store.OpenFile(key)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer rc.Close()

	decryptedBytes, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("Failed to read decrypted content: %v", err)
	}

	if string(decryptedBytes) != plainContent {
		t.Fatalf("Decrypted content mismatch! Got %q, want %q", string(decryptedBytes), plainContent)
	}
}

func TestLocalStorage_ProductionKeyValidation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage-test-prod-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 1. Missing key in production fails fast
	_, err = NewLocalStorage(tempDir, "", "production")
	if err == nil || !strings.Contains(err.Error(), "DOCUMENT_ENCRYPTION_KEY is required") {
		t.Fatalf("expected error for empty DOCUMENT_ENCRYPTION_KEY in production, got %v", err)
	}

	// 2. Non-hex key in production fails fast
	_, err = NewLocalStorage(tempDir, "not-a-valid-hex-string!!", "production")
	if err == nil || !strings.Contains(err.Error(), "must be a valid hex string") {
		t.Fatalf("expected error for invalid hex key in production, got %v", err)
	}

	// 3. Short hex key in production fails fast
	shortKey := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 8)) // 16 hex chars (8 bytes)
	_, err = NewLocalStorage(tempDir, shortKey, "production")
	if err == nil || !strings.Contains(err.Error(), "must be exactly 32 bytes") {
		t.Fatalf("expected error for short key in production, got %v", err)
	}

	// 4. Valid 64-hex char key in production succeeds
	validKey := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	prodStore, err := NewLocalStorage(tempDir, validKey, "production")
	if err != nil {
		t.Fatalf("expected success with valid 64-hex key in production, got %v", err)
	}
	if prodStore == nil {
		t.Fatalf("expected non-nil prodStore")
	}
}

func TestLocalStorage_CorruptFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage-test-corrupt-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	encKey := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	store, err := NewLocalStorage(tempDir, encKey, "test")
	if err != nil {
		t.Fatalf("failed to create LocalStorage: %v", err)
	}

	// Write a file that is too short to even contain the nonce with a valid canonical UUID
	corruptKey := "d3eebc99-9c0b-4ef8-bb6d-6bb9bd380a44"
	corruptPath := filepath.Join(tempDir, corruptKey)
	if err := os.WriteFile(corruptPath, []byte("short"), 0600); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	_, err = store.OpenFile(corruptKey)
	if err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected 'too short' error for truncated file, got %v", err)
	}
}

func TestLocalStorage_HardenedKeyValidationAndContainment(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage-test-harden-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storageBase := filepath.Join(tempDir, "storage")
	siblingEvil := filepath.Join(tempDir, "storage-evil")

	encKey := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	store, err := NewLocalStorage(storageBase, encKey, "test")
	if err != nil {
		t.Fatalf("failed to create LocalStorage: %v", err)
	}

	ctx := context.Background()

	// 1. Direct unit test of checkPathContainment for sibling-prefix escape
	t.Run("checkPathContainment sibling-prefix escape", func(t *testing.T) {
		err := checkPathContainment(storageBase, filepath.Join(siblingEvil, "file.bin"))
		if err == nil {
			t.Errorf("expected directory traversal error for sibling prefix, got nil")
		}
		if !strings.Contains(err.Error(), "directory traversal detected") {
			t.Errorf("expected 'directory traversal detected', got: %v", err)
		}
	})

	// 2. Invalid key cases for Upload and OpenFile
	invalidCases := []struct {
		name string
		key  string
	}{
		{name: "sibling-prefix escape with UUID suffix", key: "../storage-evil/a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"},
		{name: "sibling-prefix escape plain", key: "../storage-evil"},
		{name: "dot-dot component traversal relative", key: "../../etc/passwd"},
		{name: "dot-dot component intermediate", key: "a0eebc99/../evil"},
		{name: "absolute path", key: "/etc/passwd"},
		{name: "empty key", key: ""},
		{name: "non-UUID filename", key: "document.pdf"},
		{name: "non-UUID directory path", key: "tenant-1/docs/id.jpg"},
		{name: "non-UUID uppercase UUID", key: "A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11"},
		{name: "non-UUID mixed case UUID", key: "a0eebc99-9C0B-4ef8-bb6d-6bb9bd380a11"},
		{name: "non-UUID invalid hex character", key: "g0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"},
		{name: "non-UUID too short", key: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a1"},
		{name: "non-UUID too long", key: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a110"},
		{name: "non-UUID missing hyphen", key: "a0eebc999c0b4ef8bb6d6bb9bd380a110000"},
	}

	for _, tc := range invalidCases {
		t.Run("Upload rejected: "+tc.name, func(t *testing.T) {
			err := store.Upload(ctx, tc.key, strings.NewReader("malicious content"), "text/plain")
			if err == nil {
				t.Fatalf("expected error for key %q, got nil", tc.key)
			}

			// Verify that no file was created on disk in storageBase
			entries, readErr := os.ReadDir(storageBase)
			if readErr == nil && len(entries) > 0 {
				t.Errorf("expected storageBase to be empty, found %d entries", len(entries))
			}

			// Verify that no sibling-evil directory or file was created
			if _, statErr := os.Stat(siblingEvil); !os.IsNotExist(statErr) {
				t.Errorf("expected siblingEvil to not exist, stat err: %v", statErr)
			}
		})

		t.Run("OpenFile rejected: "+tc.name, func(t *testing.T) {
			rc, err := store.OpenFile(tc.key)
			if err == nil {
				rc.Close()
				t.Fatalf("expected OpenFile to error for key %q, got nil", tc.key)
			}
		})
	}

	// 3. Valid UUID round-trip
	t.Run("valid canonical UUID round-trip", func(t *testing.T) {
		validKey := "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"
		content := "encrypted sensitive student document content"

		if err := store.Upload(ctx, validKey, strings.NewReader(content), "application/pdf"); err != nil {
			t.Fatalf("Upload failed with valid UUID: %v", err)
		}

		// Verify file was created on disk
		diskPath := filepath.Join(storageBase, validKey)
		rawBytes, err := os.ReadFile(diskPath)
		if err != nil {
			t.Fatalf("expected file on disk at %s: %v", diskPath, err)
		}

		// Verify content is encrypted on disk
		if bytes.Contains(rawBytes, []byte(content)) {
			t.Fatalf("vulnerability: plaintext was written unencrypted to disk")
		}

		// Verify OpenFile decrypts successfully
		rc, err := store.OpenFile(validKey)
		if err != nil {
			t.Fatalf("OpenFile failed: %v", err)
		}
		defer rc.Close()

		readBytes, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("reading decrypted content failed: %v", err)
		}
		if string(readBytes) != content {
			t.Fatalf("decrypted content mismatch: got %q, want %q", string(readBytes), content)
		}
	})
}
