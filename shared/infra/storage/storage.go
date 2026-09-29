package storage

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Storage defines the interface for secure document and attachment storage.
type Storage interface {
	Upload(ctx context.Context, key string, reader io.Reader, contentType string) error
	GetSignedURL(ctx context.Context, key string, expires time.Duration) (string, error)
	GetSignedURLWithPath(ctx context.Context, pathPrefix string, key string, expires time.Duration) (string, error)
	GetSignedURLWithClaims(ctx context.Context, pathPrefix string, claims DocClaims, expires time.Duration) (string, error)
	ValidateSignedURLToken(tokenStr string) (string, error)
	ValidateSignedURLTokenWithClaims(tokenStr string) (*DocClaims, error)
	OpenFile(key string) (io.ReadCloser, error)
}

// LocalStorage implements Storage using local disk with AES-256-GCM encryption at rest.
type LocalStorage struct {
	baseDir         string
	baseURL         string
	defaultViewPath string
	jwtSecret       []byte
	aead            cipher.AEAD
}

// DocClaims holds JWT claims for securing document and attachment viewing access.
type DocClaims struct {
	Key      string `json:"key"`
	TicketID string `json:"ticket_id,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	jwt.RegisteredClaims
}

func createDocAEAD(hexKey, appEnv string) (cipher.AEAD, error) {
	var keyBytes []byte
	var err error

	isLocalOrTest := appEnv == "local" || appEnv == "test" || appEnv == ""

	if hexKey == "" {
		if appEnv == "production" {
			return nil, fmt.Errorf("storage: DOCUMENT_ENCRYPTION_KEY is required in environment: %s", appEnv)
		}
		// Generate a random 32-byte key for local/test ephemeral storage if empty
		keyBytes = make([]byte, 32)
		if _, err := rand.Read(keyBytes); err != nil {
			return nil, fmt.Errorf("storage: failed to generate random key: %w", err)
		}
	} else {
		keyBytes, err = hex.DecodeString(hexKey)
		if err != nil {
			if !isLocalOrTest && appEnv == "production" {
				return nil, fmt.Errorf("storage: DOCUMENT_ENCRYPTION_KEY must be a valid hex string: %w", err)
			}
			// If not valid hex in local/test, use raw bytes padded/truncated to 32
			keyBytes = make([]byte, 32)
			copy(keyBytes, []byte(hexKey))
		} else if len(keyBytes) != 32 {
			if !isLocalOrTest && appEnv == "production" {
				return nil, fmt.Errorf("storage: DOCUMENT_ENCRYPTION_KEY must be exactly 32 bytes (64 hex characters), got %d bytes", len(keyBytes))
			}
		}
	}

	if len(keyBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded, keyBytes)
		keyBytes = padded
	} else if len(keyBytes) > 32 {
		keyBytes = keyBytes[:32]
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

// NewLocalStorage initializes a new LocalStorage defaulting view path to /auth/documents/view.
func NewLocalStorage(baseDir, baseURL, secret, encKey, appEnv string) (*LocalStorage, error) {
	return NewLocalStorageWithPath(baseDir, baseURL, "/auth/documents/view", secret, encKey, appEnv)
}

// NewLocalStorageWithPath initializes a new LocalStorage with an explicit default view path.
func NewLocalStorageWithPath(baseDir, baseURL, defaultViewPath, secret, encKey, appEnv string) (*LocalStorage, error) {
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("storage: failed to create base directory: %w", err)
	}

	aead, err := createDocAEAD(encKey, appEnv)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to create encryption cipher: %w", err)
	}

	cleanViewPath := defaultViewPath
	if cleanViewPath == "" {
		cleanViewPath = "/auth/documents/view"
	}
	if !strings.HasPrefix(cleanViewPath, "/") {
		cleanViewPath = "/" + cleanViewPath
	}

	return &LocalStorage{
		baseDir:         baseDir,
		baseURL:         strings.TrimSuffix(baseURL, "/"),
		defaultViewPath: cleanViewPath,
		jwtSecret:       []byte(secret),
		aead:            aead,
	}, nil
}

// Upload writes an encrypted document file to the local disk.
func (l *LocalStorage) Upload(ctx context.Context, key string, reader io.Reader, contentType string) error {
	destPath := filepath.Join(l.baseDir, filepath.Clean(key))
	absBase, err := filepath.Abs(l.baseDir)
	if err != nil {
		return fmt.Errorf("storage: invalid base directory: %w", err)
	}
	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return fmt.Errorf("storage: invalid destination path: %w", err)
	}
	if !strings.HasPrefix(absDest, absBase) {
		return fmt.Errorf("storage: directory traversal detected")
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

	if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
		return fmt.Errorf("storage: failed to create subdirectories: %w", err)
	}

	// #nosec G304 //nolint:gosec -- path prefix validation ensures file is scoped to storage directory, preventing directory traversal
	file, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("storage: failed to open destination file: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(ciphertext); err != nil {
		return fmt.Errorf("storage: failed to write encrypted file: %w", err)
	}

	return nil
}

// GetSignedURL generates a short-lived signed URL to access the document using the default view path.
func (l *LocalStorage) GetSignedURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	return l.GetSignedURLWithPath(ctx, l.defaultViewPath, key, expires)
}

// GetSignedURLWithPath generates a short-lived signed URL using an explicit view path.
func (l *LocalStorage) GetSignedURLWithPath(ctx context.Context, pathPrefix string, key string, expires time.Duration) (string, error) {
	claims := DocClaims{
		Key: key,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expires)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return l.GetSignedURLWithClaims(ctx, pathPrefix, claims, expires)
}

// GetSignedURLWithClaims generates a short-lived signed URL with custom claims (e.g. scoping to TicketID and UserID).
func (l *LocalStorage) GetSignedURLWithClaims(ctx context.Context, pathPrefix string, claims DocClaims, expires time.Duration) (string, error) {
	cleanPath := pathPrefix
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}

	if claims.ExpiresAt == nil {
		claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(expires))
	}
	if claims.IssuedAt == nil {
		claims.IssuedAt = jwt.NewNumericDate(time.Now())
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(l.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("storage: failed to sign token: %w", err)
	}

	return fmt.Sprintf("%s%s?token=%s", l.baseURL, cleanPath, tokenStr), nil
}

// ValidateSignedURLToken parses and validates a signed view URL token, returning the storage key.
func (l *LocalStorage) ValidateSignedURLToken(tokenStr string) (string, error) {
	claims, err := l.ValidateSignedURLTokenWithClaims(tokenStr)
	if err != nil {
		return "", err
	}
	return claims.Key, nil
}

// ValidateSignedURLTokenWithClaims parses and validates a signed view URL token, returning all claims.
func (l *LocalStorage) ValidateSignedURLTokenWithClaims(tokenStr string) (*DocClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &DocClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return l.jwtSecret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("storage: token validation failed: %w", err)
	}

	claims, ok := token.Claims.(*DocClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("storage: invalid token claims")
	}

	return claims, nil
}

// OpenFile opens and decrypts the local file for reading.
func (l *LocalStorage) OpenFile(key string) (io.ReadCloser, error) {
	destPath := filepath.Join(l.baseDir, filepath.Clean(key))
	absBase, err := filepath.Abs(l.baseDir)
	if err != nil {
		return nil, fmt.Errorf("storage: invalid base directory: %w", err)
	}
	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return nil, fmt.Errorf("storage: invalid destination path: %w", err)
	}
	if !strings.HasPrefix(absDest, absBase) {
		return nil, fmt.Errorf("storage: directory traversal detected")
	}

	// #nosec G304 //nolint:gosec -- path prefix validation ensures file is scoped to storage directory, preventing directory traversal
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
