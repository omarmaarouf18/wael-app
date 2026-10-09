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
	// ErrNotFound indicates that no object is stored under the key.
	ErrNotFound = errors.New("storage: object not found")
)

// Storage defines the interface for secure document and attachment storage.
type Storage interface {
	Upload(ctx context.Context, key string, reader io.Reader, contentType string) error
	OpenFile(key string) (io.ReadCloser, error)
	// Size returns the plaintext size of the stored object in bytes, without
	// decrypting it (used for Content-Length). A missing object is ErrNotFound.
	Size(key string) (int64, error)
	// Delete removes the stored object. A missing object is not an error, so
	// Delete is idempotent.
	Delete(ctx context.Context, key string) error
}

// headerSize is the on-disk prefix before the ciphertext: version byte + nonce.
func (l *LocalStorage) headerSize() int {
	return 1 + l.aead.NonceSize()
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

	// File format: [version=1][nonce 12][ciphertext+tag]. The plaintext is
	// read into one buffer right after room for the header and sealed in
	// place, so an upload holds about one copy of the file in memory.
	hdr := l.headerSize()
	buf := bytes.NewBuffer(make([]byte, hdr, hdr+bytes.MinRead))
	if _, err := buf.ReadFrom(reader); err != nil {
		return fmt.Errorf("storage: failed to read file content: %w", err)
	}
	data := buf.Bytes()

	nonce := data[1:hdr]
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("storage: failed to generate nonce: %w", err)
	}
	data[0] = fileFormatVersion
	aad := append([]byte{fileFormatVersion}, []byte(key)...)

	ciphertext := l.aead.Seal(data[:hdr], nonce, data[hdr:], aad)

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
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
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
	// Decrypt in place (ciphertext[:0]): the format is one AES-GCM seal over
	// the whole file, so the plaintext is authenticated only after all of it
	// is read; holding one buffer instead of two halves the peak memory.
	plaintext, err := l.aead.Open(ciphertext[:0], nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to decrypt file %s: %w", key, err)
	}

	return io.NopCloser(bytes.NewReader(plaintext)), nil
}

// Size returns the plaintext size of the object stored under key, computed
// from the on-disk size minus the fixed header and GCM tag (no decryption).
// Only a regular file inside the root counts; a missing key is ErrNotFound.
func (l *LocalStorage) Size(key string) (int64, error) {
	if err := validateUUIDKey(key); err != nil {
		return 0, err
	}
	info, err := l.root.Stat(key)
	if err != nil {
		if isEscapeError(err) {
			return 0, fmt.Errorf("storage: directory traversal detected: %w", err)
		}
		if errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("%w: %s", ErrNotFound, key)
		}
		return 0, fmt.Errorf("storage: failed to stat file %s: %w", key, err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("storage: %s is not a regular file", key)
	}
	size := info.Size() - int64(l.headerSize()+l.aead.Overhead())
	if size < 0 {
		return 0, fmt.Errorf("storage: file %s is too short to contain valid header", key)
	}
	return size, nil
}

// Delete removes the object stored under key inside the root. It is
// idempotent: a missing key returns nil. The key must be a canonical UUID,
// and only a regular file or a symlink entry (the link itself, never its
// target) is removed; a directory is refused.
func (l *LocalStorage) Delete(ctx context.Context, key string) error {
	if err := validateUUIDKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := l.root.Lstat(key)
	if err != nil {
		if isEscapeError(err) {
			return fmt.Errorf("storage: directory traversal detected: %w", err)
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("storage: failed to stat file %s: %w", key, err)
	}
	if info.IsDir() {
		return fmt.Errorf("storage: %s is a directory, refusing to delete", key)
	}
	if err := l.root.Remove(key); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if isEscapeError(err) {
			return fmt.Errorf("storage: directory traversal detected: %w", err)
		}
		return fmt.Errorf("storage: failed to delete file %s: %w", key, err)
	}
	return nil
}
