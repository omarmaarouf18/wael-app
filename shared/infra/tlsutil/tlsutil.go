package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
)

// LoadServerTLSConfig loads server-side TLS configuration requiring client auth.
func LoadServerTLSConfig(certPath, keyPath, caPath string) (*tls.Config, error) {
	// #nosec G304 //nolint:gosec -- paths are loaded from bootstrap configuration, not user controlled
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cert file %q: %w", certPath, err)
	}
	// #nosec G304 //nolint:gosec -- paths are loaded from bootstrap configuration, not user controlled
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file %q: %w", keyPath, err)
	}
	// #nosec G304 //nolint:gosec -- paths are loaded from bootstrap configuration, not user controlled
	caBytes, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA file %q: %w", caPath, err)
	}

	cert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load X509 key pair: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caBytes) {
		return nil, fmt.Errorf("failed to append CA certs from PEM")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caCertPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// LoadClientTLSConfig loads client-side TLS configuration presenting client cert and verifying server cert.
func LoadClientTLSConfig(certPath, keyPath, caPath string) (*tls.Config, error) {
	// #nosec G304 //nolint:gosec -- paths are loaded from bootstrap configuration, not user controlled
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cert file %q: %w", certPath, err)
	}
	// #nosec G304 //nolint:gosec -- paths are loaded from bootstrap configuration, not user controlled
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file %q: %w", keyPath, err)
	}
	// #nosec G304 //nolint:gosec -- paths are loaded from bootstrap configuration, not user controlled
	caBytes, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA file %q: %w", caPath, err)
	}

	cert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load X509 key pair: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caBytes) {
		return nil, fmt.Errorf("failed to append CA certs from PEM")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// NewClient returns an http.Client configured with client-side TLS.
func NewClient(certPath, keyPath, caPath string) (*http.Client, error) {
	cfg, err := LoadClientTLSConfig(certPath, keyPath, caPath)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: cfg,
		},
	}, nil
}
