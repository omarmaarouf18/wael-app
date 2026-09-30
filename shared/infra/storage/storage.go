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
	"io/fs"
	"log"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const fileFormatVersion byte = 1

var (
	// ErrKeyExists indicates an attempt to upload with an existing storage key.
	ErrKeyExists = errors.New("storage: key already exists")
	// ErrLinkUnsupported indicates that the underlying filesystem does not support hard links.
	ErrLinkUnsupported = errors.New("storage: hard links not supported by underlying filesystem")
)

// Storage defines the interface for secure document and attachment storage.
type Storage interface {
	Upload(ctx context.Context, key string, reader io.Reader, contentType string) error
	OpenFile(key string) (io.ReadCloser, error)
}

// LocalStorage implements Storage using local disk with AES-256-GCM encryption at rest and os.Root containment.
type LocalStorage struct {
	baseDir string
	root    *os.Root
	aead    cipher.AEAD
}

var canonicalUUIDRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validateUUIDKey(key string) error {
	if !canonicalUUIDRegex.MatchString(key) {
		return fmt.Errorf("storage: invalid key %q: must be a canonical lowercase UUID", key)
	}
	return nil
}

func isEscapeError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "escapes from parent") || strings.Contains(msg, "outside the root")
}

func isLinkUnsupported(err error) bool {
	if errors.Is(err, errors.ErrUnsupported) {
		return true
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		if errors.Is(linkErr.Err, errors.ErrUnsupported) {
			return true
		}
		var errno syscall.Errno
		if errors.As(linkErr.Err, &errno) {
			return errno == syscall.ENOSYS || errno == syscall.EOPNOTSUPP || errno == syscall.EXDEV || errno == syscall.EPERM
		}
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.ENOSYS || errno == syscall.EOPNOTSUPP || errno == syscall.EXDEV || errno == syscall.EPERM
	}
	return false
}

func sweepStaleTempFiles(r *os.Root, maxAge time.Duration) {
	entries, err := fs.ReadDir(r.FS(), ".")
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, ".upload-") && strings.HasSuffix(name, ".tmp") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) > maxAge {
				_ = r.Remove(name)
			}
		}
	}
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

// NewLocalStorage initializes a new LocalStorage with AES-256-GCM encryption at rest and os.Root containment.
func NewLocalStorage(baseDir, encKey, appEnv string) (*LocalStorage, error) {
	aead, err := createDocAEAD(encKey, appEnv)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to create encryption cipher: %w", err)
	}

	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("storage: failed to create base directory: %w", err)
	}

	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to open base directory root: %w", err)
	}

	sweepStaleTempFiles(root, 1*time.Hour)

	return &LocalStorage{
		baseDir: baseDir,
		root:    root,
		aead:    aead,
	}, nil
}

// Close closes the underlying base directory root handle.
func (l *LocalStorage) Close() error {
	if l.root != nil {
		return l.root.Close()
	}
	return nil
}

func (l *LocalStorage) createTemp() (*os.File, string, error) {
	for i := 0; i < 100; i++ {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, "", fmt.Errorf("storage: failed to generate temp file name: %w", err)
		}
		name := fmt.Sprintf(".upload-%x.tmp", b)
		f, err := l.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			return f, name, nil
		}
		if !errors.Is(err, os.ErrExist) && !os.IsExist(err) {
			return nil, "", fmt.Errorf("storage: failed to create temporary file: %w", err)
		}
	}
	return nil, "", errors.New("storage: failed to create temporary file: too many collisions")
}

// Upload writes an encrypted document file to the local disk within os.Root.
func (l *LocalStorage) Upload(ctx context.Context, key string, reader io.Reader, contentType string) error {
	if err := validateUUIDKey(key); err != nil {
		return err
	}

	// Check containment and existence within root
	if _, err := l.root.Stat(key); err == nil {
		return ErrKeyExists
	} else if isEscapeError(err) {
		return fmt.Errorf("storage: directory traversal detected: %w", err)
	}

	if _, err := l.root.Lstat(key); err == nil {
		return ErrKeyExists
	} else if isEscapeError(err) {
		return fmt.Errorf("storage: directory traversal detected: %w", err)
	}

	plaintext, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("storage: failed to read file content: %w", err)
	}

	nonce := make([]byte, l.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("storage: failed to generate nonce: %w", err)
	}

	// File format: [version=1][nonce 12][ciphertext+tag]
	header := make([]byte, 1+len(nonce))
	header[0] = fileFormatVersion
	copy(header[1:], nonce)
	aad := append([]byte{fileFormatVersion}, []byte(key)...)

	ciphertext := l.aead.Seal(header, nonce, plaintext, aad)

	tmpFile, tmpName, err := l.createTemp()
	if err != nil {
		return err
	}
	defer func() {
		_ = l.root.Remove(tmpName)
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

	if err := l.root.Link(tmpName, key); err != nil {
		if errors.Is(err, os.ErrExist) || os.IsExist(err) {
			return ErrKeyExists
		}
		if isLinkUnsupported(err) {
			return fmt.Errorf("%w: %v", ErrLinkUnsupported, err)
		}
		if isEscapeError(err) {
			return fmt.Errorf("storage: directory traversal detected: %w", err)
		}
		return fmt.Errorf("storage: failed to publish destination file: %w", err)
	}

	return nil
}

// OpenFile opens and decrypts the local file for reading using os.Root.
func (l *LocalStorage) OpenFile(key string) (io.ReadCloser, error) {
	if err := validateUUIDKey(key); err != nil {
		return nil, err
	}

	// #nosec G304 //nolint:gosec -- canonical UUID key validation and os.Root containment ensure file path stays within base directory
	data, err := l.root.ReadFile(key)
	if err != nil {
		if isEscapeError(err) {
			return nil, fmt.Errorf("storage: directory traversal detected: %w", err)
		}
		return nil, fmt.Errorf("storage: failed to open file %s: %w", key, err)
	}

	nonceSize := l.aead.NonceSize()
	if len(data) < 1+nonceSize {
		return nil, fmt.Errorf("storage: file %s is too short to contain valid header", key)
	}

	version := data[0]
	if version != fileFormatVersion {
		return nil, fmt.Errorf("storage: unsupported file format version %d", version)
	}

	nonce, ciphertext := data[1:1+nonceSize], data[1+nonceSize:]
	aad := append([]byte{version}, []byte(key)...)
	plaintext, err := l.aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to decrypt file %s: %w", key, err)
	}

	return io.NopCloser(bytes.NewReader(plaintext)), nil
}
