package iap

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Pinned certificate fingerprint and marker OIDs as required by ADR-0015 and contract Section 4.2.
const (
	// AppleRootCAG3Fingerprint is the SHA-256 fingerprint of the official Apple Root CA - G3 certificate.
	AppleRootCAG3Fingerprint = "63343abfb89a6a03ebb57e9b3f5fa7be7c4f5c756f3017b3a8c488c3653e9179"

	// AppleMarkerOIDLeaf is the Apple App Store receipt signing leaf certificate extension OID.
	AppleMarkerOIDLeaf = "1.2.840.113635.100.6.11.1"

	// AppleMarkerOIDIntermediate is the Apple Worldwide Developer Relations (WWDR) intermediate CA extension OID.
	AppleMarkerOIDIntermediate = "1.2.840.113635.100.6.2.1"
)

// Standard error definitions for JWS transaction verification.
var (
	ErrInvalidJWS         = errors.New("invalid JWS structure")
	ErrInvalidAlg         = errors.New("unsupported JWS algorithm, expected ES256")
	ErrInvalidChainLength = errors.New("x5c header must contain exactly 3 certificates")
	ErrUntrustedRoot      = errors.New("root certificate is not trusted Apple Root CA - G3")
	ErrInvalidChain       = errors.New("certificate chain verification failed")
	ErrMissingMarkerOID   = errors.New("certificate missing required Apple marker OID")
	ErrInvalidKey         = errors.New("leaf certificate public key is not ECDSA P-256")
	ErrInvalidSignature   = errors.New("invalid ES256 signature")
	ErrInvalidPayload     = errors.New("invalid transaction payload")
	ErrInvalidSignedDate  = errors.New("invalid or missing signedDate")
)

//go:embed certs/AppleRootCA-G3.pem
var appleRootCAG3PEM []byte

// JWSTransactionPayload represents the decoded payload of a StoreKit 2 transaction JWS.
// Field names conform to Apple's JWSTransactionDecodedPayload specification.
type JWSTransactionPayload struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	PurchaseDate          int64  `json:"purchaseDate"`
	OriginalPurchaseDate  int64  `json:"originalPurchaseDate,omitempty"`
	SignedDate            int64  `json:"signedDate"`
	Type                  string `json:"type"`
	Environment           string `json:"environment"`
	AppAccountToken       string `json:"appAccountToken,omitempty"`
	RevocationDate        int64  `json:"revocationDate,omitempty"`
	RevocationReason      int    `json:"revocationReason,omitempty"`
	Quantity              int    `json:"quantity,omitempty"`
}

type jwsHeader struct {
	Alg string   `json:"alg"`
	X5c []string `json:"x5c"`
}

// Verifier verifies StoreKit 2 JWS transactions against a trusted root certificate.
type Verifier struct {
	trustedRoot       *x509.Certificate
	trustedRootPool   *x509.CertPool
	pinnedFingerprint string
}

var defaultVerifier *Verifier

func init() {
	v, err := NewVerifier(appleRootCAG3PEM, AppleRootCAG3Fingerprint)
	if err != nil {
		panic(fmt.Sprintf("failed to initialize default Apple Root CA - G3 verifier: %v", err))
	}
	defaultVerifier = v
}

// DefaultVerifier returns the global production verifier initialized with the embedded Apple Root CA - G3.
func DefaultVerifier() *Verifier {
	return defaultVerifier
}

// NewVerifier creates a Verifier with custom root PEM and expected SHA-256 fingerprint.
// If expectedFingerprint is empty, fingerprint check against the root certificate is omitted
// (useful for throwaway test CA certificates).
func NewVerifier(rootPEM []byte, expectedFingerprint string) (*Verifier, error) {
	block, _ := x509.ParseCertificate(rootPEM)
	// If DER parse failed, attempt to parse as PEM
	var rootCert *x509.Certificate
	if block != nil {
		rootCert = block
	} else {
		// Try parsing from PEM
		certs, pErr := parsePEMCertificates(rootPEM)
		if pErr != nil || len(certs) == 0 {
			return nil, fmt.Errorf("failed to parse root certificate: %w", pErr)
		}
		rootCert = certs[0]
	}

	if expectedFingerprint != "" {
		fp := sha256.Sum256(rootCert.Raw)
		fpHex := hex.EncodeToString(fp[:])
		if !strings.EqualFold(fpHex, expectedFingerprint) {
			return nil, fmt.Errorf("root certificate fingerprint %s does not match expected %s", fpHex, expectedFingerprint)
		}
	}

	pool := x509.NewCertPool()
	pool.AddCert(rootCert)

	return &Verifier{
		trustedRoot:       rootCert,
		trustedRootPool:   pool,
		pinnedFingerprint: expectedFingerprint,
	}, nil
}

// PinnedFingerprint returns the SHA-256 fingerprint expected for the root certificate.
func (v *Verifier) PinnedFingerprint() string {
	return v.pinnedFingerprint
}

// TrustedRootCertificate returns the parsed root certificate used by this verifier.
func (v *Verifier) TrustedRootCertificate() *x509.Certificate {
	return v.trustedRoot
}

// Verify decodes and validates a StoreKit 2 transaction JWS string.
// Returns the decoded transaction payload on success or an error explaining validation failure.
func (v *Verifier) Verify(jwsCompact string) (*JWSTransactionPayload, error) {
	parts := strings.Split(jwsCompact, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidJWS
	}

	// 1. Decode header
	headerBytes, err := decodeBase64URL(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: header base64 decode: %v", ErrInvalidJWS, err)
	}
	var header jwsHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("%w: header json unmarshal: %v", ErrInvalidJWS, err)
	}

	if header.Alg != "ES256" {
		return nil, ErrInvalidAlg
	}
	if len(header.X5c) != 3 {
		return nil, ErrInvalidChainLength
	}

	// 2. Parse certificate chain
	certs := make([]*x509.Certificate, 3)
	for i, cStr := range header.X5c {
		der, err := base64.StdEncoding.DecodeString(cStr)
		if err != nil {
			return nil, fmt.Errorf("%w: x5c[%d] base64: %v", ErrInvalidJWS, i, err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("%w: x5c[%d] parse: %v", ErrInvalidChain, i, err)
		}
		certs[i] = cert
	}

	leafCert := certs[0]
	intermediateCert := certs[1]
	rootCert := certs[2]

	// 3. Verify root certificate matches trusted root
	if !bytes.Equal(rootCert.Raw, v.trustedRoot.Raw) {
		return nil, ErrUntrustedRoot
	}
	if v.pinnedFingerprint != "" {
		fp := sha256.Sum256(rootCert.Raw)
		if !strings.EqualFold(hex.EncodeToString(fp[:]), v.pinnedFingerprint) {
			return nil, ErrUntrustedRoot
		}
	}

	// 4. Decode payload to obtain signedDate for chain validation
	payloadBytes, err := decodeBase64URL(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: payload base64 decode: %v", ErrInvalidPayload, err)
	}
	var payload JWSTransactionPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("%w: payload json unmarshal: %v", ErrInvalidPayload, err)
	}
	if payload.SignedDate <= 0 {
		return nil, ErrInvalidSignedDate
	}
	signedTime := time.UnixMilli(payload.SignedDate)

	// 5. Verify certificate chain at signedTime
	intermediates := x509.NewCertPool()
	intermediates.AddCert(intermediateCert)

	opts := x509.VerifyOptions{
		Roots:         v.trustedRootPool,
		Intermediates: intermediates,
		CurrentTime:   signedTime,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	if _, err := leafCert.Verify(opts); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidChain, err)
	}

	// 6. Verify Apple marker OIDs
	if !hasExtensionOID(leafCert, AppleMarkerOIDLeaf) {
		return nil, fmt.Errorf("%w: leaf missing %s", ErrMissingMarkerOID, AppleMarkerOIDLeaf)
	}
	if !hasExtensionOID(intermediateCert, AppleMarkerOIDIntermediate) {
		return nil, fmt.Errorf("%w: intermediate missing %s", ErrMissingMarkerOID, AppleMarkerOIDIntermediate)
	}

	// 7. Verify ES256 signature
	pubKey, ok := leafCert.PublicKey.(*ecdsa.PublicKey)
	if !ok || pubKey.Curve != elliptic.P256() {
		return nil, ErrInvalidKey
	}

	sigBytes, err := decodeBase64URL(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: signature base64 decode: %v", ErrInvalidSignature, err)
	}
	if len(sigBytes) != 64 {
		return nil, fmt.Errorf("%w: signature must be exactly 64 bytes, got %d", ErrInvalidSignature, len(sigBytes))
	}

	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])

	signingInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signingInput))

	if !ecdsa.Verify(pubKey, digest[:], r, s) {
		return nil, ErrInvalidSignature
	}

	// 8. Validate required payload attributes
	if payload.TransactionID == "" || payload.OriginalTransactionID == "" || payload.BundleID == "" ||
		payload.ProductID == "" || payload.PurchaseDate <= 0 || payload.Type == "" {
		return nil, fmt.Errorf("%w: missing required transaction fields", ErrInvalidPayload)
	}
	if payload.Environment != "Production" && payload.Environment != "Sandbox" {
		return nil, fmt.Errorf("%w: unknown environment %q", ErrInvalidPayload, payload.Environment)
	}

	return &payload, nil
}

// Verify uses the default production Verifier to verify a StoreKit 2 JWS transaction.
func Verify(jwsCompact string) (*JWSTransactionPayload, error) {
	return defaultVerifier.Verify(jwsCompact)
}

func hasExtensionOID(cert *x509.Certificate, oidStr string) bool {
	for _, ext := range cert.Extensions {
		if ext.Id.String() == oidStr {
			return true
		}
	}
	return false
}

func decodeBase64URL(s string) ([]byte, error) {
	// Support unpadded (RawURLEncoding) as well as padded (URLEncoding)
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

func parsePEMCertificates(pemData []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := pemData
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Bytes) == 0 {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate in PEM: %w", err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, errors.New("no certificates found in PEM data")
	}
	return certs, nil
}
