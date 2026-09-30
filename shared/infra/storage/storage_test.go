package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"os"
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

	// 1. Test Upload success
	fileContent := "test document binary content"
	key := "tenant-1/docs/id_front.jpg"
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
	_, err = store.OpenFile("tenant-1/docs/non-existent.png")
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
	key := "docs/user-1/avatar.png"

	if err := store.Upload(ctx, key, strings.NewReader(plainContent), "image/png"); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	rawBytes, err := os.ReadFile(tempDir + "/" + key)
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

	// Write a file that is too short to even contain the nonce
	corruptPath := tempDir + "/short.bin"
	if err := os.WriteFile(corruptPath, []byte("short"), 0600); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	_, err = store.OpenFile("short.bin")
	if err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected 'too short' error for truncated file, got %v", err)
	}
}
