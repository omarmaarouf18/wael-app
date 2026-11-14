package iap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestAppleRootCAG3_EmbeddedFingerprint(t *testing.T) {
	v := DefaultVerifier()
	if v == nil {
		t.Fatal("DefaultVerifier returned nil")
	}

	root := v.TrustedRootCertificate()
	if root == nil {
		t.Fatal("TrustedRootCertificate returned nil")
	}

	if root.Subject.CommonName != "Apple Root CA - G3" {
		t.Errorf("subject CN = %q, want 'Apple Root CA - G3'", root.Subject.CommonName)
	}

	fp := sha256.Sum256(root.Raw)
	fpHex := hex.EncodeToString(fp[:])

	if !strings.EqualFold(fpHex, AppleRootCAG3Fingerprint) {
		t.Fatalf("SHA-256 fingerprint = %s, want %s", fpHex, AppleRootCAG3Fingerprint)
	}

	if !strings.EqualFold(v.PinnedFingerprint(), AppleRootCAG3Fingerprint) {
		t.Errorf("PinnedFingerprint = %s, want %s", v.PinnedFingerprint(), AppleRootCAG3Fingerprint)
	}
}

type testCertHierarchy struct {
	rootKey   *ecdsa.PrivateKey
	rootCert  *x509.Certificate
	interKey  *ecdsa.PrivateKey
	interCert *x509.Certificate
	leafKey   *ecdsa.PrivateKey
	leafCert  *x509.Certificate
	verifier  *Verifier
}

func newTestCertHierarchy(t *testing.T, leafOID, interOID string, validFrom, validTo time.Time) *testCertHierarchy {
	t.Helper()

	// 1. Root
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Apple Root CA - G3",
			Organization: []string{"Apple Inc."},
		},
		NotBefore:             validFrom.Add(-24 * time.Hour),
		NotAfter:              validTo.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create root cert: %v", err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatalf("parse root cert: %v", err)
	}

	// 2. Intermediate
	interKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate inter key: %v", err)
	}
	interExts := make([]pkix.Extension, 0)
	if interOID != "" {
		parsedOID, err := parseOID(interOID)
		if err != nil {
			t.Fatalf("parse inter OID: %v", err)
		}
		interExts = append(interExts, pkix.Extension{
			Id:    parsedOID,
			Value: []byte{0x05, 0x00}, // ASN.1 NULL
		})
	}
	interTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName:   "Apple Worldwide Developer Relations Certification Authority",
			Organization: []string{"Apple Inc."},
		},
		NotBefore:             validFrom.Add(-12 * time.Hour),
		NotAfter:              validTo.Add(12 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtraExtensions:       interExts,
	}
	interDER, err := x509.CreateCertificate(rand.Reader, interTemplate, rootCert, &interKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create inter cert: %v", err)
	}
	interCert, err := x509.ParseCertificate(interDER)
	if err != nil {
		t.Fatalf("parse inter cert: %v", err)
	}

	// 3. Leaf
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	leafExts := make([]pkix.Extension, 0)
	if leafOID != "" {
		parsedOID, err := parseOID(leafOID)
		if err != nil {
			t.Fatalf("parse leaf OID: %v", err)
		}
		leafExts = append(leafExts, pkix.Extension{
			Id:    parsedOID,
			Value: []byte{0x05, 0x00},
		})
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			CommonName:   "Mac App Store and iTunes Store Receipt Signing",
			Organization: []string{"Apple Inc."},
		},
		NotBefore:       validFrom,
		NotAfter:        validTo,
		KeyUsage:        x509.KeyUsageDigitalSignature,
		ExtraExtensions: leafExts,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, interCert, &leafKey.PublicKey, interKey)
	if err != nil {
		t.Fatalf("create leaf cert: %v", err)
	}
	leafCert, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}

	verifier, err := NewVerifier(rootDER, "")
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	return &testCertHierarchy{
		rootKey:   rootKey,
		rootCert:  rootCert,
		interKey:  interKey,
		interCert: interCert,
		leafKey:   leafKey,
		leafCert:  leafCert,
		verifier:  verifier,
	}
}

func (h *testCertHierarchy) makeJWS(t *testing.T, payload *JWSTransactionPayload, mutateHeader func(*jwsHeader), mutateSig func([]byte) []byte) string {
	t.Helper()

	header := jwsHeader{
		Alg: "ES256",
		X5c: []string{
			base64.StdEncoding.EncodeToString(h.leafCert.Raw),
			base64.StdEncoding.EncodeToString(h.interCert.Raw),
			base64.StdEncoding.EncodeToString(h.rootCert.Raw),
		},
	}
	if mutateHeader != nil {
		mutateHeader(&header)
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signingInput := headerB64 + "." + payloadB64
	digest := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, h.leafKey, digest[:])
	if err != nil {
		t.Fatalf("sign digest: %v", err)
	}

	sigBytes := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)

	if mutateSig != nil {
		sigBytes = mutateSig(sigBytes)
	}

	sigB64 := base64.RawURLEncoding.EncodeToString(sigBytes)
	return signingInput + "." + sigB64
}

func parseOID(oidStr string) (asn1.ObjectIdentifier, error) {
	parts := strings.Split(oidStr, ".")
	oid := make(asn1.ObjectIdentifier, len(parts))
	for i, p := range parts {
		var n int
		for _, ch := range p {
			n = n*10 + int(ch-'0')
		}
		oid[i] = n
	}
	return oid, nil
}

func sampleValidPayload(signedTime time.Time) *JWSTransactionPayload {
	ms := signedTime.UnixMilli()
	return &JWSTransactionPayload{
		TransactionID:         "2000000123456789",
		OriginalTransactionID: "1000000123456789",
		BundleID:              "com.wael.app",
		ProductID:             "com.wael.app.pkg.year1",
		PurchaseDate:          ms,
		OriginalPurchaseDate:  ms,
		SignedDate:            ms,
		Type:                  "Non-Renewing Subscription",
		Environment:           "Production",
		AppAccountToken:       "a1b2c3d4-e5f6-5a7b-8c9d-0e1f2a3b4c5d",
	}
}

func TestVerifier_ValidTransaction(t *testing.T) {
	now := time.Now().UTC()
	th := newTestCertHierarchy(t, AppleMarkerOIDLeaf, AppleMarkerOIDIntermediate, now.Add(-1*time.Hour), now.Add(1*time.Hour))

	payload := sampleValidPayload(now)
	jws := th.makeJWS(t, payload, nil, nil)

	got, err := th.verifier.Verify(jws)
	if err != nil {
		t.Fatalf("expected valid verification, got error: %v", err)
	}

	if got.TransactionID != payload.TransactionID {
		t.Errorf("TransactionID = %s, want %s", got.TransactionID, payload.TransactionID)
	}
	if got.BundleID != payload.BundleID {
		t.Errorf("BundleID = %s, want %s", got.BundleID, payload.BundleID)
	}
	if got.ProductID != payload.ProductID {
		t.Errorf("ProductID = %s, want %s", got.ProductID, payload.ProductID)
	}
}

func TestVerifier_BadSignature(t *testing.T) {
	now := time.Now().UTC()
	th := newTestCertHierarchy(t, AppleMarkerOIDLeaf, AppleMarkerOIDIntermediate, now.Add(-1*time.Hour), now.Add(1*time.Hour))

	payload := sampleValidPayload(now)
	jws := th.makeJWS(t, payload, nil, func(sig []byte) []byte {
		// Flip last byte of signature
		sig[63] ^= 0xff
		return sig
	})

	_, err := th.verifier.Verify(jws)
	if err == nil {
		t.Fatal("expected error on tampered signature, got nil")
	}
}

func TestVerifier_WrongAlgorithm(t *testing.T) {
	now := time.Now().UTC()
	th := newTestCertHierarchy(t, AppleMarkerOIDLeaf, AppleMarkerOIDIntermediate, now.Add(-1*time.Hour), now.Add(1*time.Hour))

	payload := sampleValidPayload(now)
	jws := th.makeJWS(t, payload, func(h *jwsHeader) {
		h.Alg = "RS256"
	}, nil)

	_, err := th.verifier.Verify(jws)
	if err == nil || !strings.Contains(err.Error(), "unsupported JWS algorithm") {
		t.Fatalf("expected ErrInvalidAlg, got: %v", err)
	}
}

func TestVerifier_WrongChainLength(t *testing.T) {
	now := time.Now().UTC()
	th := newTestCertHierarchy(t, AppleMarkerOIDLeaf, AppleMarkerOIDIntermediate, now.Add(-1*time.Hour), now.Add(1*time.Hour))

	payload := sampleValidPayload(now)
	jws := th.makeJWS(t, payload, func(h *jwsHeader) {
		h.X5c = h.X5c[:2] // only 2 certs
	}, nil)

	_, err := th.verifier.Verify(jws)
	if err == nil || !strings.Contains(err.Error(), "exactly 3 certificates") {
		t.Fatalf("expected ErrInvalidChainLength, got: %v", err)
	}
}

func TestVerifier_UntrustedRoot(t *testing.T) {
	now := time.Now().UTC()
	th := newTestCertHierarchy(t, AppleMarkerOIDLeaf, AppleMarkerOIDIntermediate, now.Add(-1*time.Hour), now.Add(1*time.Hour))

	// Another hierarchy with different root
	otherTH := newTestCertHierarchy(t, AppleMarkerOIDLeaf, AppleMarkerOIDIntermediate, now.Add(-1*time.Hour), now.Add(1*time.Hour))

	payload := sampleValidPayload(now)
	jws := otherTH.makeJWS(t, payload, nil, nil)

	// Verify otherTH's JWS against th's verifier
	_, err := th.verifier.Verify(jws)
	if err == nil || !strings.Contains(err.Error(), "root certificate is not trusted") {
		t.Fatalf("expected ErrUntrustedRoot, got: %v", err)
	}
}

func TestVerifier_MissingMarkerOIDs(t *testing.T) {
	now := time.Now().UTC()

	t.Run("Missing_Leaf_Marker_OID", func(t *testing.T) {
		th := newTestCertHierarchy(t, "", AppleMarkerOIDIntermediate, now.Add(-1*time.Hour), now.Add(1*time.Hour))
		payload := sampleValidPayload(now)
		jws := th.makeJWS(t, payload, nil, nil)

		_, err := th.verifier.Verify(jws)
		if err == nil || !strings.Contains(err.Error(), "leaf missing") {
			t.Fatalf("expected leaf missing marker OID error, got: %v", err)
		}
	})

	t.Run("Missing_Intermediate_Marker_OID", func(t *testing.T) {
		th := newTestCertHierarchy(t, AppleMarkerOIDLeaf, "", now.Add(-1*time.Hour), now.Add(1*time.Hour))
		payload := sampleValidPayload(now)
		jws := th.makeJWS(t, payload, nil, nil)

		_, err := th.verifier.Verify(jws)
		if err == nil || !strings.Contains(err.Error(), "intermediate missing") {
			t.Fatalf("expected intermediate missing marker OID error, got: %v", err)
		}
	})
}

func TestVerifier_ExpiredCertificateAtSignedDate(t *testing.T) {
	now := time.Now().UTC()
	validFrom := now.Add(-10 * time.Hour)
	validTo := now.Add(-2 * time.Hour)

	th := newTestCertHierarchy(t, AppleMarkerOIDLeaf, AppleMarkerOIDIntermediate, validFrom, validTo)

	// signedDate is NOW (after validTo)
	payload := sampleValidPayload(now)
	jws := th.makeJWS(t, payload, nil, nil)

	_, err := th.verifier.Verify(jws)
	if err == nil || !strings.Contains(err.Error(), "certificate chain verification failed") {
		t.Fatalf("expected certificate chain expired error, got: %v", err)
	}
}

func TestVerifier_MissingOrMalformedPayloadFields(t *testing.T) {
	now := time.Now().UTC()
	th := newTestCertHierarchy(t, AppleMarkerOIDLeaf, AppleMarkerOIDIntermediate, now.Add(-1*time.Hour), now.Add(1*time.Hour))

	t.Run("Missing_TransactionID", func(t *testing.T) {
		payload := sampleValidPayload(now)
		payload.TransactionID = ""
		jws := th.makeJWS(t, payload, nil, nil)
		_, err := th.verifier.Verify(jws)
		if err == nil {
			t.Fatal("expected error for missing TransactionID")
		}
	})

	t.Run("Missing_ProductID", func(t *testing.T) {
		payload := sampleValidPayload(now)
		payload.ProductID = ""
		jws := th.makeJWS(t, payload, nil, nil)
		_, err := th.verifier.Verify(jws)
		if err == nil {
			t.Fatal("expected error for missing ProductID")
		}
	})

	t.Run("Invalid_Environment", func(t *testing.T) {
		payload := sampleValidPayload(now)
		payload.Environment = "Staging"
		jws := th.makeJWS(t, payload, nil, nil)
		_, err := th.verifier.Verify(jws)
		if err == nil || !strings.Contains(err.Error(), "unknown environment") {
			t.Fatalf("expected unknown environment error, got: %v", err)
		}
	})
}
