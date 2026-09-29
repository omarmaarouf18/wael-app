package tlsutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func generateSelfSignedCertPEM() ([]byte, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Co"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	return certPEM, keyPEM, nil
}

func TestTLSUtil_FailureModes(t *testing.T) {
	// Setup a temporary directory for cert files
	tempDir := t.TempDir()

	validCertPath := filepath.Join(tempDir, "valid_cert.pem")
	validKeyPath := filepath.Join(tempDir, "valid_key.pem")
	validCAPath := filepath.Join(tempDir, "valid_ca.pem")

	// Write dummy data that is not actual PEM certificates
	invalidData := []byte("this is not a valid pem format cert or key")

	if err := os.WriteFile(validCertPath, invalidData, 0644); err != nil {
		t.Fatalf("failed to write mock cert file: %v", err)
	}
	if err := os.WriteFile(validKeyPath, invalidData, 0644); err != nil {
		t.Fatalf("failed to write mock key file: %v", err)
	}
	if err := os.WriteFile(validCAPath, invalidData, 0644); err != nil {
		t.Fatalf("failed to write mock CA file: %v", err)
	}

	nonExistentPath := filepath.Join(tempDir, "does_not_exist.pem")

	t.Run("LoadServerTLSConfig_MissingCert", func(t *testing.T) {
		_, err := LoadServerTLSConfig(nonExistentPath, validKeyPath, validCAPath)
		if err == nil {
			t.Error("expected error for missing cert path, got nil")
		}
	})

	t.Run("LoadServerTLSConfig_MissingKey", func(t *testing.T) {
		_, err := LoadServerTLSConfig(validCertPath, nonExistentPath, validCAPath)
		if err == nil {
			t.Error("expected error for missing key path, got nil")
		}
	})

	t.Run("LoadServerTLSConfig_MissingCA", func(t *testing.T) {
		_, err := LoadServerTLSConfig(validCertPath, validKeyPath, nonExistentPath)
		if err == nil {
			t.Error("expected error for missing CA path, got nil")
		}
	})

	t.Run("LoadServerTLSConfig_InvalidKeyPair", func(t *testing.T) {
		_, err := LoadServerTLSConfig(validCertPath, validKeyPath, validCAPath)
		if err == nil {
			t.Error("expected error for invalid key pair content, got nil")
		}
	})

	t.Run("LoadClientTLSConfig_MissingCert", func(t *testing.T) {
		_, err := LoadClientTLSConfig(nonExistentPath, validKeyPath, validCAPath)
		if err == nil {
			t.Error("expected error for missing cert path, got nil")
		}
	})

	t.Run("LoadClientTLSConfig_MissingKey", func(t *testing.T) {
		_, err := LoadClientTLSConfig(validCertPath, nonExistentPath, validCAPath)
		if err == nil {
			t.Error("expected error for missing key path, got nil")
		}
	})

	t.Run("LoadClientTLSConfig_MissingCA", func(t *testing.T) {
		_, err := LoadClientTLSConfig(validCertPath, validKeyPath, nonExistentPath)
		if err == nil {
			t.Error("expected error for missing CA path, got nil")
		}
	})

	t.Run("LoadClientTLSConfig_InvalidKeyPair", func(t *testing.T) {
		_, err := LoadClientTLSConfig(validCertPath, validKeyPath, validCAPath)
		if err == nil {
			t.Error("expected error for invalid key pair content, got nil")
		}
	})

	t.Run("NewClient_InvalidConfig", func(t *testing.T) {
		_, err := NewClient(validCertPath, validKeyPath, validCAPath)
		if err == nil {
			t.Error("expected error for NewClient with invalid certs, got nil")
		}
	})

	// Test case where key pair is valid but CA is invalid
	t.Run("InvalidCAWithValidKeyPair", func(t *testing.T) {
		certPEM, keyPEM, err := generateSelfSignedCertPEM()
		if err != nil {
			t.Fatalf("failed to generate self-signed cert PEM: %v", err)
		}

		realCertPath := filepath.Join(tempDir, "real_cert.pem")
		realKeyPath := filepath.Join(tempDir, "real_key.pem")

		if err := os.WriteFile(realCertPath, certPEM, 0644); err != nil {
			t.Fatalf("failed to write real cert: %v", err)
		}
		if err := os.WriteFile(realKeyPath, keyPEM, 0644); err != nil {
			t.Fatalf("failed to write real key: %v", err)
		}

		// LoadServerTLSConfig should fail to append certs from the invalid CA path
		_, err = LoadServerTLSConfig(realCertPath, realKeyPath, validCAPath)
		if err == nil {
			t.Error("expected error when CA cert append fails, got nil")
		}

		// LoadClientTLSConfig should also fail
		_, err = LoadClientTLSConfig(realCertPath, realKeyPath, validCAPath)
		if err == nil {
			t.Error("expected error when CA cert append fails for client config, got nil")
		}
	})
}
