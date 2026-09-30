// Package storage provides secure encrypted document and file storage.
package storage

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrKeyExists indicates an attempt to upload with an existing storage key.
var ErrKeyExists = errors.New("storage: key already exists")

// Storage defines the interface for secure document and attachment storage.
type Storage interface {
	Upload(ctx context.Context, key string, reader io.Reader, contentType string) error
	OpenFile(key string) (io.ReadCloser, error)
}

// LocalStorage implements Storage using local disk with AES-256-GCM encryption at rest.
type LocalStorage struct {
	baseDir string
	aead    cipher.AEAD
}

var canonicalUUIDRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validateUUIDKey(key string) error {
	if !canonicalUUIDRegex.MatchString(key) {
		return fmt.Errorf("storage: invalid key %q: must be a canonical lowercase UUID", key)
	}
	return nil
}

func checkPathContainment(baseDir, destPath string) error {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("storage: invalid base directory: %w", err)
	}
	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return fmt.Errorf("storage: invalid destination path: %w", err)
	}
	rel, err := filepath.Rel(absBase, absDest)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("storage: directory traversal detected")
	}
	return nil
}

func createDocAEAD(hexKey, appEnv string) (cipher.AEAD, error) {
	relaxed := appEnv == "local" || appEnv == "test"

	var keyBytes []byte
	var err error

	if hexKey == "" {
		if !relaxed {
			return nil, fmt.Errorf("storage: DOCUMENT_ENCRYPTION_KEY is required in environment: %q", appEnv)
		}
		// Ephemeral key allowed only in relaxed environments
		keyBytes = make([]byte, 32)
		if _, err := rand.Read(keyBytes); err != nil {
			return nil, fmt.Errorf("storage: failed to generate ephemeral random key: %w", err)
		}
		log.Printf("WARNING: storage: DOCUMENT_ENCRYPTION_KEY not set; using ephemeral random key for %s", appEnv)
	} else {
		keyBytes, err = hex.DecodeString(hexKey)
		if err != nil {
			return nil, fmt.Errorf("storage: DOCUMENT_ENCRYPTION_KEY must be a valid hex string: %w", err)
		}
		if len(keyBytes) != 32 {
			return nil, fmt.Errorf("storage: DOCUMENT_ENCRYPTION_KEY must be exactly 32 bytes (64 hex characters), got %d bytes", len(keyBytes))
		}
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("storage: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("storage: new GCM: %w", err)
	}

	return gcm, nil
}

// NewLocalStorage initializes a new LocalStorage with AES-256-GCM encryption at rest.
func NewLocalStorage(baseDir, encKey, appEnv string) (*LocalStorage, error) {
	aead, err := createDocAEAD(encKey, appEnv)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to create encryption cipher: %w", err)
	}

	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("storage: failed to create base directory: %w", err)
	}

	return &LocalStorage{
		baseDir: baseDir,
		aead:    aead,
	}, nil
}

// Upload writes an encrypted document file to the local disk.
func (l *LocalStorage) Upload(ctx context.Context, key string, reader io.Reader, contentType string) error {
	if err := validateUUIDKey(key); err != nil {
		return err
	}

	destPath := filepath.Join(l.baseDir, filepath.Clean(key))
	if err := checkPathContainment(l.baseDir, destPath); err != nil {
		return err
	}

	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return fmt.Errorf("storage: failed to create subdirectories: %w", err)
	}

	// Fail fast if destination file already exists
	if _, err := os.Stat(destPath); err == nil {
		return ErrKeyExists
	}

	plaintext, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("storage: failed to read file content: %w", err)
	}

	nonce := make([]byte, l.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("storage: failed to generate nonce: %w", err)
	}

	ciphertext := l.aead.Seal(nonce, nonce, plaintext, nil)

	tmpFile, err := os.CreateTemp(destDir, ".upload-*.tmp")
	if err != nil {
		return fmt.Errorf("storage: failed to create temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(ciphertext); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("storage: failed to write encrypted file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("storage: failed to sync temporary file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("storage: failed to close temporary file: %w", err)
	}

	if err := os.Link(tmpPath, destPath); err != nil {
		if errors.Is(err, os.ErrExist) || os.IsExist(err) {
			return ErrKeyExists
		}
		return fmt.Errorf("storage: failed to publish destination file: %w", err)
	}

	return nil
}

// OpenFile opens and decrypts the local file for reading.
func (l *LocalStorage) OpenFile(key string) (io.ReadCloser, error) {
	if err := validateUUIDKey(key); err != nil {
		return nil, err
	}

	destPath := filepath.Join(l.baseDir, filepath.Clean(key))
	if err := checkPathContainment(l.baseDir, destPath); err != nil {
		return nil, err
	}

	// #nosec G304 //nolint:gosec -- canonical UUID key validation and filepath.Rel containment check ensure file path stays within base directory
	data, err := os.ReadFile(destPath)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to open file %s: %w", key, err)
	}

	nonceSize := l.aead.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("storage: file %s is too short to contain valid ciphertext", key)
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := l.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to decrypt file %s: %w", key, err)
	}

	return io.NopCloser(bytes.NewReader(plaintext)), nil
}
