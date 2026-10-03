package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/shared/infra/tlsutil"
)

// testPKI is a throwaway CA that can issue leaf certificates.
type testPKI struct {
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
	caPEM  []byte
}

func newPKI(t *testing.T, name string) *testPKI {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return &testPKI{caCert: cert, caKey: key, caPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issue returns PEM-encoded certificate and key for a leaf valid for 127.0.0.1.
func (p *testPKI) issue(t *testing.T, cn string, serial int64) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func writePEMs(t *testing.T, cert, key, ca []byte) (certPath, keyPath, caPath string) {
	t.Helper()
	dir := t.TempDir()
	certPath, keyPath, caPath = filepath.Join(dir, "c.crt"), filepath.Join(dir, "c.key"), filepath.Join(dir, "ca.crt")
	for path, data := range map[string][]byte{certPath: cert, keyPath: key, caPath: ca} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return
}

// mtlsUpstream is an admin listener that requires a client certificate signed by pki.
func mtlsUpstream(t *testing.T, pki *testPKI) string {
	t.Helper()
	certPEM, keyPEM := pki.issue(t, "auth-service", 2)
	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(pki.caCert)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"admin_id":"a","name":"Wael"}`))
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.URL
}

func whoamiStatus(t *testing.T, client *http.Client, url string) int {
	t.Helper()
	p, err := New(Options{InternalToken: testInternal, AuthURL: url, AcademyURL: url, Client: client, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return do(p.Whoami, http.MethodGet, "/api/whoami", "", withToken()).Code
}

// academyUpstream is an academy admin listener that requires a client
// certificate signed by pki and serves the levels list.
func academyUpstream(t *testing.T, pki *testPKI) string {
	t.Helper()
	certPEM, keyPEM := pki.issue(t, "academy-service", 5)
	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(pki.caCert)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/admin/levels" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"levels":[]}`))
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.URL
}

func levelsStatus(t *testing.T, client *http.Client, authURL, academyURL string) int {
	t.Helper()
	p, err := New(Options{InternalToken: testInternal, AuthURL: authURL, AcademyURL: academyURL, Client: client, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return do(p.LevelsList, http.MethodGet, "/api/levels", "", withToken()).Code
}

func TestMTLS_AcademyCallsUseTheSameClientCertificate(t *testing.T) {
	pki := newPKI(t, "wael-test-ca")
	authURL := mtlsUpstream(t, pki)
	academyURL := academyUpstream(t, pki)

	certPEM, keyPEM := pki.issue(t, "admin-console", 6)
	certPath, keyPath, caPath := writePEMs(t, certPEM, keyPEM, pki.caPEM)
	cfg, err := tlsutil.LoadClientTLSConfig(certPath, keyPath, caPath)
	if err != nil {
		t.Fatal(err)
	}
	good := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
	if got := levelsStatus(t, good, authURL, academyURL); got != http.StatusOK {
		t.Fatalf("mTLS levels call = %d, want 200", got)
	}

	// Without a client certificate the academy handshake fails and the
	// browser sees a safe 503; the auth listener is never a fallback.
	noCert := &tls.Config{RootCAs: cfg.RootCAs, MinVersion: tls.VersionTLS12}
	if got := levelsStatus(t, &http.Client{Transport: &http.Transport{TLSClientConfig: noCert}}, authURL, academyURL); got != http.StatusServiceUnavailable {
		t.Fatalf("levels call without client cert = %d, want 503", got)
	}
}

func TestMTLS_ClientCertificateAndLocalCAAreRequired(t *testing.T) {
	pki := newPKI(t, "wael-test-ca")
	url := mtlsUpstream(t, pki)

	// Same loader main uses: presents the client cert, verifies the server against the local CA.
	certPEM, keyPEM := pki.issue(t, "admin-console", 3)
	certPath, keyPath, caPath := writePEMs(t, certPEM, keyPEM, pki.caPEM)
	cfg, err := tlsutil.LoadClientTLSConfig(certPath, keyPath, caPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := whoamiStatus(t, &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}, url); got != http.StatusOK {
		t.Fatalf("mTLS call = %d, want 200", got)
	}

	// No client certificate: the upstream refuses the handshake, the browser sees a safe 503.
	noCert := &tls.Config{RootCAs: cfg.RootCAs, MinVersion: tls.VersionTLS12}
	if got := whoamiStatus(t, &http.Client{Transport: &http.Transport{TLSClientConfig: noCert}}, url); got != http.StatusServiceUnavailable {
		t.Fatalf("call without client cert = %d, want 503", got)
	}

	// Server certificate from an unknown CA is not trusted (no skip-verify anywhere).
	other := newPKI(t, "other-ca")
	otherCert, otherKey := other.issue(t, "admin-console", 4)
	op, ok, oc := writePEMs(t, otherCert, otherKey, other.caPEM)
	wrongCA, err := tlsutil.LoadClientTLSConfig(op, ok, oc)
	if err != nil {
		t.Fatal(err)
	}
	if got := whoamiStatus(t, &http.Client{Transport: &http.Transport{TLSClientConfig: wrongCA}}, url); got != http.StatusServiceUnavailable {
		t.Fatalf("call trusting the wrong CA = %d, want 503", got)
	}

}
