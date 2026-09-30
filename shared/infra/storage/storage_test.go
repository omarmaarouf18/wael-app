package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
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

func TestLocalStorage_KeyAndEnvironmentMatrix(t *testing.T) {
	envs := []string{"", "staging", "production", "local", "test"}

	key16B := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 16))      // 32 hex chars
	key31B := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 31))      // 62 hex chars
	key33B := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 33))      // 66 hex chars
	keyValid32B := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32)) // 64 hex chars
	keyNonHex := "not-a-valid-hex-string-of-any-kind!!"

	keyCases := []struct {
		name   string
		hexKey string
	}{
		{"missing", ""},
		{"16B", key16B},
		{"31B", key31B},
		{"33B", key33B},
		{"non-hex", keyNonHex},
		{"valid 32B", keyValid32B},
	}

	for _, env := range envs {
		isRelaxed := env == "local" || env == "test"
		for _, kc := range keyCases {
			t.Run(fmt.Sprintf("env=%q/key=%s", env, kc.name), func(t *testing.T) {
				tempDir, err := os.MkdirTemp("", "storage-matrix-*")
				if err != nil {
					t.Fatalf("failed to create temp dir: %v", err)
				}
				defer os.RemoveAll(tempDir)

				targetBase := filepath.Join(tempDir, "storage")

				expectAllow := false
				if kc.name == "valid 32B" {
					expectAllow = true
				} else if kc.name == "missing" && isRelaxed {
					expectAllow = true
				}

				store, err := NewLocalStorage(targetBase, kc.hexKey, env)
				if expectAllow {
					if err != nil {
						t.Fatalf("expected allow for env=%q, key=%s, but got error: %v", env, kc.name, err)
					}
					if store == nil {
						t.Fatalf("expected non-nil store for allowed combination")
					}
				} else {
					if err == nil {
						t.Fatalf("expected deny (error) for env=%q, key=%s, but got nil", env, kc.name)
					}
					// Assert that no file is created on any error
					entries, readErr := os.ReadDir(tempDir)
					if readErr == nil {
						for _, e := range entries {
							if e.Name() == "storage" {
								subEntries, _ := os.ReadDir(targetBase)
								if len(subEntries) > 0 {
									t.Errorf("expected 0 files in storage on error, found %d", len(subEntries))
								}
							} else {
								t.Errorf("unexpected file/dir created on error: %s", e.Name())
							}
						}
					}
				}
			})
		}
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

type failingReader struct{}

func (f *failingReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated read error")
}

func TestLocalStorage_AtomicNoOverwrite(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage-atomic-*")
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
	key := "a1eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"
	firstContent := "first immutable content"
	secondContent := "second attempted overwrite"

	// 1. Initial upload succeeds
	if err := store.Upload(ctx, key, strings.NewReader(firstContent), "text/plain"); err != nil {
		t.Fatalf("initial upload failed: %v", err)
	}

	// 2. Duplicate upload returns ErrKeyExists and never truncates
	err = store.Upload(ctx, key, strings.NewReader(secondContent), "text/plain")
	if err == nil {
		t.Fatalf("expected ErrKeyExists on duplicate key upload, got nil")
	}
	if !errors.Is(err, ErrKeyExists) {
		t.Fatalf("expected errors.Is(err, ErrKeyExists), got %v", err)
	}

	// Verify first content is intact
	rc, err := store.OpenFile(key)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer rc.Close()

	readBytes, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("failed to read content: %v", err)
	}
	if string(readBytes) != firstContent {
		t.Fatalf("content was altered or overwritten! got %q, want %q", string(readBytes), firstContent)
	}

	// 3. Simulated failure during upload leaves no file at dest and no leftover temp files
	failKey := "b2eebc99-9c0b-4ef8-bb6d-6bb9bd380a22"
	err = store.Upload(ctx, failKey, &failingReader{}, "text/plain")
	if err == nil {
		t.Fatalf("expected error on failing reader, got nil")
	}

	// Assert failKey does not exist
	failDest := filepath.Join(tempDir, failKey)
	if _, statErr := os.Stat(failDest); !os.IsNotExist(statErr) {
		t.Fatalf("expected destPath to not exist on failed upload, stat err: %v", statErr)
	}

	// Assert no temp files exist
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("failed to read tempDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".upload-") || strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("found leftover temp file: %s", entry.Name())
		}
	}
}

func TestLocalStorage_SymlinkContainment(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage-symlink-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storageBase := filepath.Join(tempDir, "storage")
	outsideDir := filepath.Join(tempDir, "outside")
	if err := os.MkdirAll(outsideDir, 0700); err != nil {
		t.Fatalf("failed to create outside dir: %v", err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("outside secret"), 0600); err != nil {
		t.Fatalf("failed to write outside file: %v", err)
	}

	encKey := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	store, err := NewLocalStorage(storageBase, encKey, "test")
	if err != nil {
		t.Fatalf("failed to create LocalStorage: %v", err)
	}

	ctx := context.Background()

	// A valid canonical lowercase UUID
	validUUIDKey := "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"
	symlinkPath := filepath.Join(storageBase, validUUIDKey)

	// Create a symlink inside storageBase pointing outside storageBase
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// 1. Upload must be rejected by containment alone (not regex)
	err = store.Upload(ctx, validUUIDKey, strings.NewReader("malicious overwrite"), "text/plain")
	if err == nil {
		t.Fatalf("expected Upload to be rejected for symlink pointing outside, got nil")
	}
	if !strings.Contains(err.Error(), "directory traversal detected") {
		t.Fatalf("expected 'directory traversal detected' in error, got: %v", err)
	}

	// Verify outside file was NOT overwritten
	outsideContent, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside file: %v", err)
	}
	if string(outsideContent) != "outside secret" {
		t.Fatalf("vulnerability: outside file was overwritten! got %q", string(outsideContent))
	}

	// 2. OpenFile must be rejected by containment alone (not regex)
	rc, err := store.OpenFile(validUUIDKey)
	if err == nil {
		rc.Close()
		t.Fatalf("expected OpenFile to be rejected for symlink pointing outside, got nil")
	}
	if !strings.Contains(err.Error(), "directory traversal detected") {
		t.Fatalf("expected 'directory traversal detected' in error, got: %v", err)
	}
}

func TestLocalStorage_EncryptionProperties(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage-enc-props-*")
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

	// 1. Two uploads of identical plaintext produce different ciphertext on disk (12-byte random nonce from crypto/rand)
	key1 := "11111111-1111-1111-1111-111111111111"
	key2 := "22222222-2222-2222-2222-222222222222"
	identicalPlaintext := "identical proprietary textbook content"

	if err := store.Upload(ctx, key1, strings.NewReader(identicalPlaintext), "text/plain"); err != nil {
		t.Fatalf("Upload key1 failed: %v", err)
	}
	if err := store.Upload(ctx, key2, strings.NewReader(identicalPlaintext), "text/plain"); err != nil {
		t.Fatalf("Upload key2 failed: %v", err)
	}

	raw1, err := os.ReadFile(filepath.Join(tempDir, key1))
	if err != nil {
		t.Fatalf("reading raw1 failed: %v", err)
	}
	raw2, err := os.ReadFile(filepath.Join(tempDir, key2))
	if err != nil {
		t.Fatalf("reading raw2 failed: %v", err)
	}

	// Plaintext never appears on disk
	if bytes.Contains(raw1, []byte(identicalPlaintext)) || bytes.Contains(raw2, []byte(identicalPlaintext)) {
		t.Fatalf("vulnerability: plaintext appears unencrypted on disk")
	}

	// 12-byte nonce stored with ciphertext
	if len(raw1) < 12 || len(raw2) < 12 {
		t.Fatalf("file on disk too short to contain 12-byte nonce")
	}
	nonce1 := raw1[:12]
	nonce2 := raw2[:12]
	if bytes.Equal(nonce1, nonce2) {
		t.Fatalf("expected different random 12-byte nonces, got identical nonces")
	}
	if bytes.Equal(raw1, raw2) {
		t.Fatalf("expected different ciphertexts on disk for identical plaintext, got identical bytes")
	}

	// 2. AAD = the storage key, so swapping two encrypted files makes OpenFile fail
	// Swap key1 and key2 on disk
	tmpSwap := filepath.Join(tempDir, "tmp-swap")
	path1 := filepath.Join(tempDir, key1)
	path2 := filepath.Join(tempDir, key2)
	if err := os.Rename(path1, tmpSwap); err != nil {
		t.Fatalf("rename 1 failed: %v", err)
	}
	if err := os.Rename(path2, path1); err != nil {
		t.Fatalf("rename 2 failed: %v", err)
	}
	if err := os.Rename(tmpSwap, path2); err != nil {
		t.Fatalf("rename 3 failed: %v", err)
	}

	// Reading key1 (which now contains ciphertext encrypted with key2 as AAD) must fail
	rc1, err := store.OpenFile(key1)
	if err == nil {
		rc1.Close()
		t.Fatalf("expected OpenFile to fail after file swap due to AAD mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decrypt") {
		t.Fatalf("expected 'failed to decrypt' error on swapped file, got: %v", err)
	}

	// Reading key2 (which now contains ciphertext encrypted with key1 as AAD) must fail
	rc2, err := store.OpenFile(key2)
	if err == nil {
		rc2.Close()
		t.Fatalf("expected OpenFile to fail after file swap due to AAD mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decrypt") {
		t.Fatalf("expected 'failed to decrypt' error on swapped file, got: %v", err)
	}

	// Swap back so files are correct again
	if err := os.Rename(path2, tmpSwap); err != nil {
		t.Fatalf("swap back 1 failed: %v", err)
	}
	if err := os.Rename(path1, path2); err != nil {
		t.Fatalf("swap back 2 failed: %v", err)
	}
	if err := os.Rename(tmpSwap, path1); err != nil {
		t.Fatalf("swap back 3 failed: %v", err)
	}

	// Verify both now decrypt correctly again
	rcValid, err := store.OpenFile(key1)
	if err != nil {
		t.Fatalf("OpenFile failed after restoring file: %v", err)
	}
	rcValid.Close()

	// 3. Flipping one byte on disk makes OpenFile fail (no partial plaintext)
	key3 := "33333333-3333-3333-3333-333333333333"
	secretPlaintext := "highly confidential secret material for testing tamper detection"
	if err := store.Upload(ctx, key3, strings.NewReader(secretPlaintext), "text/plain"); err != nil {
		t.Fatalf("Upload key3 failed: %v", err)
	}

	path3 := filepath.Join(tempDir, key3)
	raw3, err := os.ReadFile(path3)
	if err != nil {
		t.Fatalf("reading raw3 failed: %v", err)
	}

	// Flip a byte in the ciphertext body
	tampered := make([]byte, len(raw3))
	copy(tampered, raw3)
	tampered[len(tampered)-5] ^= 0xFF
	if err := os.WriteFile(path3, tampered, 0600); err != nil {
		t.Fatalf("writing tampered file failed: %v", err)
	}

	rcTampered, err := store.OpenFile(key3)
	if err == nil {
		rcTampered.Close()
		t.Fatalf("expected OpenFile to fail on tampered file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decrypt") {
		t.Fatalf("expected 'failed to decrypt' error on tampered file, got: %v", err)
	}
}
