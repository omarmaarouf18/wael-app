package handlers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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

// mTLS verify path (Phase 4.1, owner review item 1c): academy calls
// auth-service POST /internal/admin/verify over mTLS. This test exercises the
// real path with generated certs: the server side uses the REAL
// tlsutil.LoadServerTLSConfig (the same constructor auth-service's admin
// listener uses: RequireAndVerifyClientCert against the local CA, no
// CN/SAN allowlist), and the client side uses the REAL
// tlsutil.LoadClientTLSConfig (the same constructor academy wires in
// buildVerifyClient). If auth ever restricted client certs (e.g. to an
// admin-console CN), the academy-CN handshake below would fail and this
// test would catch it.

// testPKI is a throwaway CA plus file paths in the academy layout.
type testPKI struct {
	dir        string
	caFile     string
	srvCert    string
	srvKey     string
	clientCert string
	clientKey  string
}

func mustPEM(t *testing.T, dir, name string, block *pem.Block) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func newTestPKI(t *testing.T, serverCN string, serverIPs []net.IP, clientCN string) *testPKI {
	t.Helper()
	dir := t.TempDir()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	caTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "wael-app-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("ca cert: %v", err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse ca: %v", err)
	}
	caFile := mustPEM(t, dir, "ca.crt", &pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	issue := func(cn string, dns []string, ips []net.IP, eku []x509.ExtKeyUsage, serial int64, name string) (string, string) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("key %s: %v", name, err)
		}
		tpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: cn},
			DNSNames:     dns,
			IPAddresses:  ips,
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:  eku,
		}
		der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatalf("cert %s: %v", name, err)
		}
		keyDER, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			t.Fatalf("marshal key %s: %v", name, err)
		}
		return mustPEM(t, dir, name+".crt", &pem.Block{Type: "CERTIFICATE", Bytes: der}),
			mustPEM(t, dir, name+".key", &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	}

	srvCert, srvKey := issue(serverCN, []string{serverCN, "localhost"}, serverIPs,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, 2, "server")
	clientCert, clientKey := issue(clientCN, nil, nil,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, 3, "client")
	return &testPKI{dir: dir, caFile: caFile, srvCert: srvCert, srvKey: srvKey, clientCert: clientCert, clientKey: clientKey}
}

// startVerifyListener serves fn over TLS configured exactly like the
// auth-service admin listener.
func startVerifyListener(t *testing.T, pki *testPKI, fn func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	serverTLS, err := tlsutil.LoadServerTLSConfig(pki.srvCert, pki.srvKey, pki.caFile)
	if err != nil {
		t.Fatalf("LoadServerTLSConfig: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/admin/verify" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		fn(w, r)
	}))
	srv.TLS = serverTLS
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// academyClient builds the verify client exactly like buildVerifyClient.
func academyClient(t *testing.T, pki *testPKI, certFile, keyFile string) *http.Client {
	t.Helper()
	clientTLS, err := tlsutil.LoadClientTLSConfig(certFile, keyFile, pki.caFile)
	if err != nil {
		t.Fatalf("LoadClientTLSConfig: %v", err)
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}, Timeout: adminVerifyTimeout}
}

func TestAdminVerify_mTLSRealPath(t *testing.T) {
	pki := newTestPKI(t, "auth-service", []net.IP{net.ParseIP("127.0.0.1")}, "academy-service")
	srv := startVerifyListener(t, pki, okVerify)

	s, _ := newAdminTestServer(t, okVerify)
	s.AuthAdminURL = srv.URL
	s.VerifyClient = academyClient(t, pki, pki.clientCert, pki.clientKey)

	rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "test-internal-token", "operator-token", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("mTLS verify path: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// The server really demanded a client cert: a client without one fails.
	noCertClient := &http.Client{Timeout: adminVerifyTimeout}
	s2, _ := newAdminTestServer(t, okVerify)
	s2.AuthAdminURL = srv.URL
	s2.VerifyClient = noCertClient
	rec2 := doAdmin(t, s2, http.MethodGet, "/internal/admin/audit-log", "test-internal-token", "operator-token", "")
	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("client without cert: expected 503 fail-closed, got %d", rec2.Code)
	}
}

func TestAdminVerify_mTLSForeignClientCertRejected(t *testing.T) {
	pki := newTestPKI(t, "auth-service", []net.IP{net.ParseIP("127.0.0.1")}, "academy-service")
	srv := startVerifyListener(t, pki, okVerify)

	// A client cert signed by a DIFFERENT CA must be rejected by the
	// listener: academy fails closed with 503. (If auth ever allowlisted
	// client identities, this is the shape of failure the academy would see.)
	other := newTestPKI(t, "auth-service", []net.IP{net.ParseIP("127.0.0.1")}, "academy-service")

	s, _ := newAdminTestServer(t, okVerify)
	s.AuthAdminURL = srv.URL
	// Trust the real CA for the server cert but present the foreign client
	// cert: only the listener's CLIENT-certificate check is exercised.
	foreignTLS, err := tlsutil.LoadClientTLSConfig(other.clientCert, other.clientKey, pki.caFile)
	if err != nil {
		t.Fatalf("LoadClientTLSConfig: %v", err)
	}
	s.VerifyClient = &http.Client{Transport: &http.Transport{TLSClientConfig: foreignTLS}, Timeout: adminVerifyTimeout}

	rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "test-internal-token", "operator-token", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("foreign client cert: expected 503 fail-closed, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAdminVerify_mTLSServerNameVerified(t *testing.T) {
	// Server cert without an IP SAN: dialing 127.0.0.1 must fail hostname
	// verification and academy must fail closed.
	pki := newTestPKI(t, "auth-service", nil, "academy-service")
	srv := startVerifyListener(t, pki, okVerify)

	s, _ := newAdminTestServer(t, okVerify)
	s.AuthAdminURL = srv.URL
	s.VerifyClient = academyClient(t, pki, pki.clientCert, pki.clientKey)

	rec := doAdmin(t, s, http.MethodGet, "/internal/admin/audit-log", "test-internal-token", "operator-token", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("bad server name: expected 503 fail-closed, got %d (%s)", rec.Code, rec.Body.String())
	}
}
